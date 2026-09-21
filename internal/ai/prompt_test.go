package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDefaultPromptPreservesProductionFormat(t *testing.T) {
	got, err := RenderPrompt("", []Item{{Desc: "Coffee", Amount: -4.5}}, []CategoryDef{{Name: "Dining", Description: "Dining: Restaurants"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `You are a financial assistant that classifies bank transactions.

Category Definitions:
Dining: Restaurants

Transactions to Categorize:
[ID: 0] Transaction: "Coffee"  Amount: -4.50

Task:
Return ONLY a JSON array of category-name strings, one per transaction, in the
same order. Use exactly the category names given above. Example: ["Dining","Transport"]`
	if got != want {
		t.Fatalf("production prompt changed:\n%s", got)
	}
}

func TestGeminiSendsCustomPrompt(t *testing.T) {
	g := NewGeminiWithPrompt("test-key", "test-model", "CUSTOM\n{{.Categories}}\n{{.Transactions}}")
	g.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body struct {
			Contents []struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Contents) != 1 || !strings.HasPrefix(body.Contents[0].Parts[0].Text, "CUSTOM\nDining") {
			t.Fatalf("wrong request: %+v", body)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"parts":[{"text":"[\"Dining\"]"}]}}]}`))}, nil
	})
	names, err := g.Classify(context.Background(), []Item{{Desc: "Coffee"}}, []CategoryDef{{Name: "Dining", Description: "Dining"}})
	if err != nil || len(names) != 1 || names[0] != "Dining" {
		t.Fatalf("names=%v err=%v", names, err)
	}
}
