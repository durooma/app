package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Gemini implements Provider using Google's Generative Language API. It mirrors
// the batching/prompt approach from the original Apps Script.
type Gemini struct {
	apiKey string
	model  string
	client *http.Client
}

func NewGemini(apiKey, model string) *Gemini {
	if model == "" {
		model = "gemini-3.1-flash-lite"
	}
	return &Gemini{apiKey: apiKey, model: model, client: &http.Client{Timeout: 60 * time.Second}}
}

func (g *Gemini) Name() string { return "gemini/" + g.model }

func (g *Gemini) Classify(ctx context.Context, items []Item, categories []CategoryDef) ([]string, error) {
	if g.apiKey == "" {
		return nil, fmt.Errorf("gemini: AI_API_KEY not configured")
	}

	var ctxLines []string
	for _, c := range categories {
		ctxLines = append(ctxLines, c.Description)
	}
	var batch strings.Builder
	for i, it := range items {
		fmt.Fprintf(&batch, "[ID: %d] Transaction: %q  Amount: %.2f\n", i, it.Desc, it.Amount)
	}

	prompt := fmt.Sprintf(`You are a financial assistant that classifies bank transactions.

Category Definitions:
%s

Transactions to Categorize:
%s
Task:
Return ONLY a JSON array of category-name strings, one per transaction, in the
same order. Use exactly the category names given above. Example: ["Dining","Transport"]`,
		strings.Join(ctxLines, "\n"), batch.String())

	reqBody := map[string]any{
		"contents": []any{
			map[string]any{"parts": []any{map[string]any{"text": prompt}}},
		},
	}
	buf, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		g.model, g.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini: status %d", resp.StatusCode)
	}

	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini: empty response")
	}

	text := parsed.Candidates[0].Content.Parts[0].Text
	text = strings.ReplaceAll(text, "```json", "")
	text = strings.ReplaceAll(text, "```", "")
	text = strings.TrimSpace(text)

	var names []string
	if err := json.Unmarshal([]byte(text), &names); err != nil {
		return nil, fmt.Errorf("gemini: could not parse JSON array: %w", err)
	}
	return names, nil
}
