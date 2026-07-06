// Package ai provides provider-agnostic transaction categorization. A rules
// pass runs first (cheap, deterministic); anything left is delegated to a
// pluggable LLM Provider (Gemini by default).
package ai

import (
	"context"
	"strings"

	"durooma/internal/models"
)

// Item is a single transaction to classify.
type Item struct {
	Desc   string
	Amount float64
}

// CategoryDef is a category name plus its description, used as LLM context.
type CategoryDef struct {
	Name        string
	Description string
}

// Provider is the swappable LLM backend. Implementations return one category
// name per item, in order. Swap Gemini for OpenAI/Claude by adding a Provider.
type Provider interface {
	Name() string
	// Classify returns len(items) category names, aligned to items.
	Classify(ctx context.Context, items []Item, categories []CategoryDef) ([]string, error)
}

// storeIface is the subset of *store.Store the categorization service needs.
type storeIface interface {
	ListCategories(ctx context.Context) ([]models.Category, error)
	ListRules(ctx context.Context) ([]models.Rule, error)
	UncategorizedForAI(ctx context.Context, limit int) ([]models.Transaction, error)
	UpdateTransactionCategory(ctx context.Context, id int64, categoryID *int64) error
}

type Service struct {
	store    storeIface
	provider Provider
}

func NewService(store storeIface, provider Provider) *Service {
	return &Service{store: store, provider: provider}
}

// Report summarises a categorization run.
type Report struct {
	Total      int
	ByRules    int
	ByAI       int
	Unresolved int
	Provider   string
}

const (
	itemsPerPrompt = 15
	maxPerRun      = 500
)

// Categorize applies rules first, then the AI provider, writing results back.
func (s *Service) Categorize(ctx context.Context) (Report, error) {
	rep := Report{Provider: s.provider.Name()}

	cats, err := s.store.ListCategories(ctx)
	if err != nil {
		return rep, err
	}
	validName := map[string]int64{}
	var defs []CategoryDef
	fallback := int64(0)
	for _, c := range cats {
		validName[strings.ToLower(c.Name)] = c.ID
		if strings.EqualFold(c.Name, "General") {
			fallback = c.ID
		}
		desc := c.Name
		if c.Description != "" {
			desc = c.Name + ": " + c.Description
		}
		defs = append(defs, CategoryDef{Name: c.Name, Description: desc})
	}

	rules, err := s.store.ListRules(ctx)
	if err != nil {
		return rep, err
	}

	txns, err := s.store.UncategorizedForAI(ctx, maxPerRun)
	if err != nil {
		return rep, err
	}
	rep.Total = len(txns)

	// Pass 1: substring rules.
	var remaining []models.Transaction
	for _, t := range txns {
		matched := false
		lower := strings.ToLower(t.Description)
		for _, r := range rules {
			if r.Pattern != "" && strings.Contains(lower, strings.ToLower(r.Pattern)) {
				cid := r.CategoryID
				if err := s.store.UpdateTransactionCategory(ctx, t.ID, &cid); err != nil {
					return rep, err
				}
				rep.ByRules++
				matched = true
				break
			}
		}
		if !matched {
			remaining = append(remaining, t)
		}
	}

	// Pass 2: AI provider, batched.
	for i := 0; i < len(remaining); i += itemsPerPrompt {
		end := min(i+itemsPerPrompt, len(remaining))
		batch := remaining[i:end]

		items := make([]Item, len(batch))
		for j, t := range batch {
			items[j] = Item{Desc: t.Description, Amount: t.BaseAmount}
		}

		results, err := s.provider.Classify(ctx, items, defs)
		if err != nil {
			// Leave this batch uncategorized rather than failing the whole run.
			rep.Unresolved += len(batch)
			continue
		}
		for j, t := range batch {
			name := ""
			if j < len(results) {
				name = strings.TrimSpace(results[j])
			}
			cid, ok := validName[strings.ToLower(name)]
			if !ok {
				if fallback == 0 {
					rep.Unresolved++
					continue
				}
				cid = fallback
			}
			c := cid
			if err := s.store.UpdateTransactionCategory(ctx, t.ID, &c); err != nil {
				return rep, err
			}
			rep.ByAI++
		}
	}
	return rep, nil
}
