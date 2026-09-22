package web

import (
	"net/http"
	"strconv"
	"strings"

	"durooma/internal/models"
	"durooma/internal/store"
)

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, status int, settings models.AISettings, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := s.base(r.Context(), "Settings", "settings")
	data["Settings"] = settings
	data["ModelChoices"] = categorizationModelChoices
	data["CustomModel"] = true
	for _, choice := range categorizationModelChoices {
		if choice.ID == settings.Model {
			data["CustomModel"] = false
			break
		}
	}
	data["BackgroundEnabled"] = settings.Background && settings.HasKey
	data["AIReady"] = settings.HasKey
	data["SettingsError"] = message
	data["Saved"] = r.URL.Query().Get("saved") == "1"
	data["KeyRemoved"] = r.URL.Query().Get("key_removed") == "1" && !settings.HasKey
	if settings.Background && settings.HasKey {
		coverage, err := s.store.CategorizationCoverage(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		data["Coverage"] = coverage
	}
	w.WriteHeader(status)
	s.templates.render(w, "settings", data)
}
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.AISettings(r.Context(), store.LocalUserID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.renderSettings(w, r, http.StatusOK, settings, "")
}
func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid settings form", http.StatusBadRequest)
		return
	}
	settings, err := s.store.AISettings(r.Context(), store.LocalUserID)
	if err != nil {
		http.Error(w, "could not load settings", http.StatusInternalServerError)
		return
	}
	fail := func(message string) { s.renderSettings(w, r, http.StatusBadRequest, settings, message) }
	background := r.PostForm.Get("background_categorization")
	remove := r.PostForm.Get("remove_api_key")
	if background != "" && background != "on" || remove != "" && remove != "on" {
		fail("Invalid categorization preference.")
		return
	}
	settings.Background = background == "on"
	settings.Model = strings.TrimSpace(r.PostForm.Get("ai_model"))
	if settings.Model == "custom" {
		settings.Model = strings.TrimSpace(r.PostForm.Get("ai_custom_model"))
	}
	for _, field := range []struct {
		name  string
		value *int
	}{{"ai_interval_ms", &settings.IntervalMS}, {"ai_concurrency", &settings.Concurrency}, {"ai_batch_size", &settings.BatchSize}} {
		n, err := strconv.Atoi(r.PostForm.Get(field.name))
		if err != nil {
			fail("Request limits must be whole numbers.")
			return
		}
		*field.value = n
	}
	if err := settings.Validate(); err != nil {
		fail(err.Error())
		return
	}
	key := strings.TrimSpace(r.PostForm.Get("api_key"))
	// Never put the submitted credential in template data, error text or a URL.
	if remove == "on" && key != "" {
		fail("Choose either replace or remove API key.")
		return
	}
	if key != "" && (len(key) < 16 || len(key) > 512 || strings.IndexFunc(key, func(r rune) bool { return r < 33 || r > 126 }) >= 0) {
		fail("Enter a valid OpenRouter API key.")
		return
	}
	if settings.Background && !settings.HasKey && key == "" && remove != "on" {
		fail("Save your own OpenRouter API key before enabling background categorization.")
		return
	}
	if err := s.store.SaveAISettings(r.Context(), store.LocalUserID, settings, key, remove == "on"); err != nil {
		http.Error(w, "Could not save settings. Check server credential storage and try again.", http.StatusInternalServerError)
		return
	}
	s.cancelCategorization()
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

func (s *Server) handleRemoveAPIKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	settings, err := s.store.AISettings(r.Context(), store.LocalUserID)
	if err != nil {
		http.Error(w, "Could not load settings.", http.StatusInternalServerError)
		return
	}
	// Use persisted preferences: deleting a key must not submit or validate
	// unrelated, possibly unfinished edits in the settings form.
	if err := s.store.SaveAISettings(r.Context(), store.LocalUserID, settings, "", true); err != nil {
		http.Error(w, "Could not remove API key. Try again.", http.StatusInternalServerError)
		return
	}
	s.cancelCategorization()
	http.Redirect(w, r, "/settings?key_removed=1", http.StatusSeeOther)
}

func (s *Server) cancelCategorization() {
	// Cancel work using the previous settings/key, including manual jobs.
	s.catJob.abort()
	if s.ai != nil {
		s.ai.CancelActive()
	}
	if s.background != nil {
		s.background.Notify()
	}
}

func (s *Server) handleCategorizationCoverage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	settings, err := s.store.AISettings(r.Context(), store.LocalUserID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !settings.Background || !settings.HasKey {
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
