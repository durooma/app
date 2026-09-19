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
	SetUncategorizedCategory(ctx context.Context, id int64, categoryID *int64) error
	PendingTransactions(ctx context.Context, ids []int64) ([]models.Transaction, error)
}

type Service struct {
	store     storeIface
	provider  Provider
	batchSize int
	gate      chan struct{}
}

func NewService(store storeIface, provider Provider) *Service {
	return NewServiceWithBatchSize(store, provider, itemsPerPrompt)
}

// NewServiceWithBatchSize configures the number of transactions per model request.
func NewServiceWithBatchSize(store storeIface, provider Provider, batchSize int) *Service {
	return &Service{store: store, provider: provider, batchSize: max(1, batchSize), gate: make(chan struct{}, 1)}
}

// Report summarises a categorization run.
type Report struct {
	Total      int
	ByRules    int
	ByAI       int
	Unresolved int
	Provider   string
}

// Progress is emitted as a run advances so callers can show a progress bar.
// Done counts transactions already resolved one way or another (categorized,
// or given up on); it reaches Total on a run that is allowed to finish.
type Progress struct {
	Done  int
	Total int
}

// ProviderName reports the configured backend, for display before a run has
// produced a Report.
func (s *Service) ProviderName() string { return s.provider.Name() }

const itemsPerPrompt = 15

// Categorize applies rules first, then the AI provider, to the supplied
// transactions, writing results back. Transactions that already have a category
// are skipped so existing (possibly hand-picked) categorizations are never
// overwritten. Every transaction handed in is processed: bounding the size of a
// run is the caller's job, since only the caller knows how it selected them.
func (s *Service) Categorize(ctx context.Context, txns []models.Transaction) (Report, error) {
	return s.CategorizeWithProgress(ctx, txns, nil)
}

// CategorizeWithProgress is Categorize with a callback invoked as the run
// advances, and honouring cancellation of ctx between units of work. A
// cancelled run returns what it managed to finish alongside ctx.Err(); writes
// already committed are kept, so aborting is safe rather than destructive.
// onProgress may be nil, and is called from the calling goroutine.
func (s *Service) CategorizeWithProgress(ctx context.Context, txns []models.Transaction, onProgress func(Progress)) (Report, error) {
	rep := Report{Provider: s.provider.Name()}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return rep, ctx.Err()
	}
	ids := make([]int64, 0, len(txns))
	for _, t := range txns {
		if t.CategoryID == nil {
			ids = append(ids, t.ID)
		}
	}
	var err error
	txns, err = s.store.PendingTransactions(ctx, ids)
	if err != nil {
		return rep, err
	}
	done := 0
	progress := func() {
		if onProgress != nil {
			onProgress(Progress{Done: done, Total: rep.Total})
		}
	}

	// Never touch transactions that already have a category.
	var pending []models.Transaction
	for _, t := range txns {
		if t.CategoryID == nil {
			pending = append(pending, t)
		}
	}
	txns = pending
	rep.Total = len(txns)
	progress() // publish the denominator before the first slow call

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

	// Pass 1: substring rules.
	var remaining []models.Transaction
	for _, t := range txns {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		matched := false
		lower := strings.ToLower(t.Description)
		for _, r := range rules {
			if r.Pattern != "" && strings.Contains(lower, strings.ToLower(r.Pattern)) {
				cid := r.CategoryID
				if err := s.store.SetUncategorizedCategory(ctx, t.ID, &cid); err != nil {
					return rep, err
				}
				rep.ByRules++
				done++
				matched = true
				break
			}
		}
		if !matched {
			remaining = append(remaining, t)
		}
	}
	progress()

	// Pass 2: AI provider, batched. Cancellation is checked per batch: a batch
	// is the smallest unit worth interrupting, since it maps to one provider
	// round trip.
	for i := 0; i < len(remaining); i += s.batchSize {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		end := min(i+s.batchSize, len(remaining))
		batch := remaining[i:end]

		items := make([]Item, len(batch))
		for j, t := range batch {
			items[j] = Item{Desc: t.Description, Amount: t.BaseAmount}
		}

		results, err := s.provider.Classify(ctx, items, defs)
		if err != nil {
			// An aborted run must stop here rather than racing through the
			// remaining batches marking them all unresolved.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return rep, ctxErr
			}
			// Leave this batch uncategorized rather than failing the whole run.
			rep.Unresolved += len(batch)
			done += len(batch)
			progress()
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
			if err := s.store.SetUncategorizedCategory(ctx, t.ID, &c); err != nil {
				return rep, err
			}
			rep.ByAI++
		}
		done += len(batch)
		progress()
	}
	return rep, nil
}
