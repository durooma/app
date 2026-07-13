package web

import (
	"net/http"
	"time"

	"durooma/internal/ai"
	"durooma/internal/config"
	"durooma/internal/fx"
	"durooma/internal/importer"
	"durooma/internal/store"
)

// Server holds the HTTP handlers and their dependencies.
type Server struct {
	cfg       *config.Config
	store     *store.Store
	importer  *importer.Importer
	ai        *ai.Service
	fx        *fx.Converter
	templates *templates
}

func NewServer(cfg *config.Config, st *store.Store, imp *importer.Importer, aiSvc *ai.Service, conv *fx.Converter) (*Server, error) {
	tmpls, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:       cfg,
		store:     st,
		importer:  imp,
		ai:        aiSvc,
		fx:        conv,
		templates: tmpls,
	}, nil
}

// Handler builds the application router using the standard library's
// method+path patterns (Go 1.22+), avoiding a third-party framework.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", staticHandler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Home
	mux.HandleFunc("GET /{$}", s.handleHome)

	// Unified transaction view
	mux.HandleFunc("GET /transactions", s.handleTransactions)
	mux.HandleFunc("POST /transactions/categorize", s.handleCategorizeAll)
	mux.HandleFunc("POST /transactions/{id}/categorize", s.handleCategorizeOne)
	mux.HandleFunc("POST /transactions/{id}/category", s.handleSetCategory)
	mux.HandleFunc("POST /transactions/{id}/months", s.handleSetMonths)
	mux.HandleFunc("POST /transactions/{id}/note", s.handleSetNote)
	mux.HandleFunc("POST /transactions/{id}/delete", s.handleDeleteTransaction)

	// Categories
	mux.HandleFunc("GET /categories", s.handleCategories)
	mux.HandleFunc("POST /categories", s.handleCreateCategory)
	mux.HandleFunc("POST /categories/{id}", s.handleUpdateCategory)
	mux.HandleFunc("POST /categories/{id}/delete", s.handleDeleteCategory)

	// Rules
	mux.HandleFunc("GET /rules", s.handleRules)
	mux.HandleFunc("POST /rules", s.handleCreateRule)
	mux.HandleFunc("POST /rules/{id}/delete", s.handleDeleteRule)

	// Accounts
	mux.HandleFunc("GET /accounts", s.handleAccounts)
	mux.HandleFunc("POST /accounts", s.handleCreateAccount)

	// Import
	mux.HandleFunc("GET /import", s.handleImportForm)
	mux.HandleFunc("POST /import", s.handleImport)

	// Reports
	mux.HandleFunc("GET /reports/year", s.handleYearDeepDive)
	mux.HandleFunc("GET /reports/month", s.handleMonthDeepDive)
	mux.HandleFunc("GET /reports/year-overview", s.handleYearOverview)
	mux.HandleFunc("GET /reports/years", s.handleMultiYear)

	return logRequests(mux)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		_ = start // request logging kept minimal to save resources
	})
}
