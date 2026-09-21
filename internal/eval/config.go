// Package eval compares categorization providers on a fixed, labeled dataset.
// It has no database dependency and never writes transaction categories.
package eval

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"durooma/internal/ai"
)

type Category struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Example struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	Expected    string  `json:"expected"`
	Source      string  `json:"source,omitempty"`
	Confidence  string  `json:"confidence,omitempty"`
}

type Dataset struct {
	Name       string     `json:"name"`
	Categories []Category `json:"categories"`
	Examples   []Example  `json:"examples"`
}

type Model struct {
	Routing   *ai.OpenRouterRouting `json:"routing,omitempty"`
	Name      string                `json:"name"`
	Provider  string                `json:"provider"`
	Model     string                `json:"model"`
	APIKeyEnv string                `json:"api_key_env"`
}

type Prompt struct {
	Name         string `json:"name"`
	TemplateFile string `json:"template_file,omitempty"`
	Template     string `json:"template,omitempty"`
}

type Config struct {
	Retry           RetryPolicy `json:"retry"`
	Concurrency     int         `json:"concurrency"`
	Models          []Model     `json:"models"`
	Prompts         []Prompt    `json:"prompts"`
	BatchSizes      []int       `json:"batch_sizes"`
	Repeats         int         `json:"repeats"`
	Seed            int64       `json:"seed"`
	Timeout         string      `json:"timeout"`
	RequestInterval string      `json:"request_interval"`
}

func decodeFile(path string, dst any) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("%s: expected a single JSON value", path)
	}
	return b, nil
}

func LoadDataset(path string) (Dataset, string, error) {
	var d Dataset
	b, err := decodeFile(path, &d)
	if err != nil {
		return d, "", err
	}
	if err := d.Validate(); err != nil {
		return d, "", err
	}
	return d, fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

func normalize(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func (d Dataset) Validate() error {
	if strings.TrimSpace(d.Name) == "" || len(d.Categories) == 0 || len(d.Examples) == 0 {
		return fmt.Errorf("dataset needs a name, categories, and examples")
	}
	cats := map[string]bool{}
	for _, c := range d.Categories {
		key := normalize(c.Name)
		if key == "" || c.Name != strings.TrimSpace(c.Name) || cats[key] {
			return fmt.Errorf("empty, padded, or duplicate category %q", c.Name)
		}
		cats[key] = true
	}
	ids := map[string]bool{}
	for _, ex := range d.Examples {
		if strings.TrimSpace(ex.ID) == "" || ids[ex.ID] || strings.TrimSpace(ex.Description) == "" {
			return fmt.Errorf("example %q needs a unique ID and description", ex.ID)
		}
		if !cats[normalize(ex.Expected)] {
			return fmt.Errorf("example %q has unknown expected category %q", ex.ID, ex.Expected)
		}
		if ex.Confidence != "" && ex.Confidence != "high" && ex.Confidence != "medium" && ex.Confidence != "low" {
			return fmt.Errorf("example %q: invalid confidence", ex.ID)
		}
		ids[ex.ID] = true
	}
	return nil
}

// Definitions mirrors the formatting in ai.Service.
func (d Dataset) Definitions() []ai.CategoryDef {
	defs := make([]ai.CategoryDef, len(d.Categories))
	for i, c := range d.Categories {
		desc := c.Name
		if c.Description != "" {
			desc += ": " + c.Description
		}
		defs[i] = ai.CategoryDef{Name: c.Name, Description: desc}
	}
	return defs
}

func LoadConfig(path string) (Config, error) {
	var c Config
	if _, err := decodeFile(path, &c); err != nil {
		return c, err
	}
	for i := range c.Prompts {
		p := &c.Prompts[i]
		if p.TemplateFile != "" {
			if p.Template != "" {
				return c, fmt.Errorf("prompt %q: use template or template_file, not both", p.Name)
			}
			templatePath := p.TemplateFile
			if !filepath.IsAbs(templatePath) {
				templatePath = filepath.Join(filepath.Dir(path), templatePath)
			}
			b, err := os.ReadFile(templatePath)
			if err != nil {
				return c, err
			}
			if strings.TrimSpace(string(b)) == "" {
				return c, fmt.Errorf("prompt %q: empty template file", p.Name)
			}
			p.Template = string(b)
		}
		if p.Template == "" {
			p.Template = ai.DefaultPromptTemplate
		}
	}
	c.Retry = c.Retry.defaults()
	return c, c.Validate()
}

func (c Config) Validate() error {
	if err := c.Retry.validate(); err != nil {
		return err
	}
	if c.Concurrency < 0 || c.Concurrency > 1024 {
		return fmt.Errorf("concurrency must be between 1 and 1024 (0 uses 8)")
	}
	if len(c.Models) == 0 || len(c.Prompts) == 0 || len(c.BatchSizes) == 0 || c.Repeats < 1 {
		return fmt.Errorf("config needs models, prompts, batch_sizes, and repeats >= 1")
	}
	timeout, err := time.ParseDuration(c.Timeout)
	if err != nil || timeout <= 0 {
		return fmt.Errorf("timeout must be a positive duration, e.g. 60s")
	}
	interval, err := time.ParseDuration(c.RequestInterval)
	if err != nil || interval < 0 {
		return fmt.Errorf("request_interval must be a nonnegative duration, e.g. 60s")
	}
	names := map[string]bool{}
	for _, m := range c.Models {
		if strings.TrimSpace(m.Name) == "" || names[m.Name] || strings.TrimSpace(m.Model) == "" || strings.TrimSpace(m.APIKeyEnv) == "" {
			return fmt.Errorf("each model needs a unique name, model ID, and api_key_env")
		}
		if m.Provider != "gemini" && m.Provider != "openrouter" {
			return fmt.Errorf("model %q: unsupported provider %q (supported: gemini, openrouter)", m.Name, m.Provider)
		}
		if m.Routing != nil {
			if m.Provider != "openrouter" {
				return fmt.Errorf("model %q: routing is only supported for openrouter", m.Name)
			}
			if m.Routing.DataCollection != "" && m.Routing.DataCollection != "deny" && m.Routing.DataCollection != "allow" {
				return fmt.Errorf("model %q: data_collection must be deny or allow", m.Name)
			}
			if m.Routing.ZDR && m.Routing.DataCollection == "allow" {
				return fmt.Errorf("model %q: ZDR conflicts with data_collection allow", m.Name)
			}
			for _, provider := range m.Routing.Only {
				if strings.TrimSpace(provider) == "" {
					return fmt.Errorf("model %q: routing.only contains an empty provider", m.Name)
				}
			}
		}
		names[m.Name] = true
	}
	names = map[string]bool{}
	for _, p := range c.Prompts {
		if strings.TrimSpace(p.Name) == "" || names[p.Name] {
			return fmt.Errorf("each prompt needs a unique name")
		}
		names[p.Name] = true
		// Verify both inputs actually survive rendering, catching misspelled
		// placeholders or templates that silently omit the transactions.
		text, err := ai.RenderPrompt(p.Template, []ai.Item{{Desc: "__eval_transaction__"}}, []ai.CategoryDef{{Description: "__eval_categories__"}})
		if err != nil || !strings.Contains(text, "__eval_transaction__") || !strings.Contains(text, "__eval_categories__") {
			return fmt.Errorf("prompt %q must render .Categories and .Transactions successfully", p.Name)
		}
	}
	sizes := map[int]bool{}
	for _, size := range c.BatchSizes {
		if size < 1 || size > 100 || sizes[size] {
			return fmt.Errorf("batch_sizes must be unique integers from 1 to 100")
		}
		sizes[size] = true
	}
	return nil
}

// Workers is a global ceiling across the whole model/prompt/batch matrix.
func (c Config) Workers() int {
	if c.Concurrency == 0 {
		return 8
	}
	return c.Concurrency
}
