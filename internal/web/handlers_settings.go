package web

import (
	"net/http"

	"durooma/internal/store"
)

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	enabled, err := s.store.BackgroundCategorizationEnabled(r.Context(), store.LocalUserID)
	if err != nil {
		s.fail(w, err)
		return
	}
	data := s.base(r.Context(), "Settings", "settings")
	data["BackgroundEnabled"] = enabled
	data["AIReady"] = s.cfg.AIReady()
	if enabled {
		coverage, err := s.store.CategorizationCoverage(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		data["Coverage"] = coverage
	}
	data["Saved"] = r.URL.Query().Get("saved") == "1"
	s.templates.render(w, "settings", data)
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	value := r.PostForm.Get("background_categorization")
	if value != "" && value != "on" {
		http.Error(w, "invalid background categorization preference", http.StatusBadRequest)
		return
	}
	if err := s.store.SetBackgroundCategorization(r.Context(), store.LocalUserID, value == "on"); err != nil {
		s.fail(w, err)
		return
	}
	// Wake the controller immediately; no server restart is needed.
	s.background.Notify()
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

// Only the coverage fragment refreshes, leaving unsaved preference edits alone.
func (s *Server) handleCategorizationCoverage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	enabled, err := s.store.BackgroundCategorizationEnabled(r.Context(), store.LocalUserID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !enabled {
		s.templates.renderPartial(w, "settings", "categorization-coverage", nil)
		return
	}
	coverage, err := s.store.CategorizationCoverage(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.templates.renderPartial(w, "settings", "categorization-coverage", coverage)
}
