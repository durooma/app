package ai

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// DefaultPromptTemplate is shared by production and the evaluation baseline.
const DefaultPromptTemplate = `You are a financial assistant that classifies bank transactions.

Category Definitions:
{{.Categories}}

Transactions to Categorize:
{{.Transactions}}
Task:
Return ONLY a JSON array of category-name strings, one per transaction, in the
same order. Use exactly the category names given above. Example: ["Dining","Transport"]`

// RenderPrompt exposes only model inputs, never evaluation labels. Category
// descriptions follow the service convention: "Name: description".
func RenderPrompt(source string, items []Item, categories []CategoryDef) (string, error) {
	if source == "" {
		source = DefaultPromptTemplate
	}
	t, err := template.New("categorization").Option("missingkey=error").Parse(source)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, c := range categories {
		lines = append(lines, c.Description)
	}
	var batch strings.Builder
	for i, it := range items {
		fmt.Fprintf(&batch, "[ID: %d] Transaction: %q  Amount: %.2f\n", i, it.Desc, it.Amount)
	}
	var out bytes.Buffer
	err = t.Execute(&out, map[string]string{"Categories": strings.Join(lines, "\n"), "Transactions": batch.String()})
	return out.String(), err
}
