package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"durooma/internal/ai"
	"durooma/internal/config"
	"durooma/internal/db"
	"durooma/internal/store"
)

func TestSettingsSaveAndReload(t *testing.T) {
	url := os.Getenv("DUROOMA_TEST_DB")
	if url == "" {
		t.Skip("set DUROOMA_TEST_DB to run settings integration test")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := store.New(pool)
	original, err := st.BackgroundCategorizationEnabled(ctx, store.LocalUserID)
	if err != nil {
		t.Fatal(err)
	}
	defer st.SetBackgroundCategorization(ctx, store.LocalUserID, original)
	if err := st.SetBackgroundCategorization(ctx, store.LocalUserID, false); err != nil {
		t.Fatal(err)
	}
	control := ai.NewBackgroundControl(func(context.Context) (bool, error) { return false, nil }, func(context.Context) {}, time.Hour)
	srv, err := NewServer(&config.Config{AIProvider: "none"}, st, nil, nil, nil, control)
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()
	get := func(want string) string {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings", nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("settings page: status %d body %s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	get(`<option value="" selected>Off</option>`)
	for _, tc := range []struct {
		value   string
		enabled bool
	}{{"on", true}, {"", false}} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader("background_categorization="+tc.value))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/settings?saved=1" {
			t.Fatalf("save: %d %s", w.Code, w.Body.String())
		}
		enabled, err := st.BackgroundCategorizationEnabled(ctx, store.LocalUserID)
		if err != nil || enabled != tc.enabled {
			t.Fatalf("saved = %v err = %v", enabled, err)
		}
		if enabled {
			page := get(`<option value="on" selected>On</option>`)
			if !strings.Contains(page, `id="categorization-coverage"`) {
				t.Fatal("enabled settings omit coverage")
			}
		} else {
			page := get(`<option value="" selected>Off</option>`)
			if strings.Contains(page, `id="categorization-coverage"`) {
				t.Fatal("disabled settings show coverage")
			}
		}
		fragment := httptest.NewRecorder()
		handler.ServeHTTP(fragment, httptest.NewRequest(http.MethodGet, "/settings/categorization-coverage", nil))
		if fragment.Code != http.StatusOK || fragment.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("coverage response: %d %s", fragment.Code, fragment.Body.String())
		}
		if strings.Contains(fragment.Body.String(), `hx-trigger="every 5s"`) != enabled {
			t.Fatalf("coverage polling disagrees with saved preference: %s", fragment.Body.String())
		}
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader("background_categorization=invalid"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid setting accepted: %d", w.Code)
	}
}
