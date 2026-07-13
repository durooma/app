package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"strings"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

var monthNames = []string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun",
	"Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"money": func(v float64) string {
			return formatMoney(v)
		},
		"signclass": func(v float64) string {
			if v > 0.005 {
				return "pos"
			}
			if v < -0.005 {
				return "neg"
			}
			return "zero"
		},
		"abs": func(v float64) float64 { return math.Abs(v) },
		"neg": func(v float64) float64 { return -v },
		"month": func(m int) string {
			if m >= 1 && m <= 12 {
				return monthNames[m]
			}
			return ""
		},
		"date":     func(t time.Time) string { return t.Format("2006-01-02") },
		"monthval": func(t time.Time) string { return t.Format("2006-01") },
		"seq": func(a, b int) []int {
			var out []int
			for i := a; i <= b; i++ {
				out = append(out, i)
			}
			return out
		},
		"add": func(a, b int) int { return a + b },
		// dict builds a map from alternating key/value pairs, letting a template
		// pass multiple values into an included block.
		"dict": func(pairs ...any) (map[string]any, error) {
			if len(pairs)%2 != 0 {
				return nil, fmt.Errorf("dict: odd number of arguments")
			}
			m := make(map[string]any, len(pairs)/2)
			for i := 0; i < len(pairs); i += 2 {
				key, ok := pairs[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: key %d is not a string", i)
				}
				m[key] = pairs[i+1]
			}
			return m, nil
		},
	}
}

// formatMoney renders a number with thousands separators and 2 decimals.
func formatMoney(v float64) string {
	neg := v < 0
	v = math.Abs(v)
	s := fmt.Sprintf("%.2f", v)
	intPart, frac, _ := strings.Cut(s, ".")
	var grouped strings.Builder
	n := len(intPart)
	for i, ch := range intPart {
		if i > 0 && (n-i)%3 == 0 {
			grouped.WriteByte('\'')
		}
		grouped.WriteRune(ch)
	}
	out := grouped.String() + "." + frac
	if neg {
		return "-" + out
	}
	return out
}

// templates holds one compiled template per page, each combining the shared
// layout with the page's content block.
type templates struct {
	pages map[string]*template.Template
}

func loadTemplates() (*templates, error) {
	pageFiles, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	t := &templates{pages: map[string]*template.Template{}}
	for _, pf := range pageFiles {
		name := strings.TrimSuffix(strings.TrimPrefix(pf, "templates/"), ".html")
		if name == "layout" {
			continue
		}
		tmpl := template.New("layout.html").Funcs(templateFuncs())
		tmpl, err = tmpl.ParseFS(templateFS, "templates/layout.html", pf)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", pf, err)
		}
		t.pages[name] = tmpl
	}
	return t, nil
}

func (t *templates) render(w http.ResponseWriter, page string, data any) {
	tmpl, ok := t.pages[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderPartial executes a single named block (e.g. an HTMX row fragment)
// defined within the given page's template, without the surrounding layout.
func (t *templates) renderPartial(w http.ResponseWriter, page, block string, data any) {
	tmpl, ok := t.pages[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, block, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func staticHandler() http.Handler {
	sub, _ := fs.Sub(staticFS, "static")
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}
