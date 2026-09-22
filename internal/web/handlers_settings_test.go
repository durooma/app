package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"durooma/internal/config"
	"durooma/internal/db"
	"durooma/internal/models"
	"durooma/internal/secrets"
	"durooma/internal/store"
)

func TestSettingsCredentialsAndCategorizationGates(t *testing.T) {
	databaseURL := os.Getenv("DUROOMA_TEST_DB")
	if databaseURL == "" {
		t.Skip("set DUROOMA_TEST_DB for integration tests")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	// Integration databases are disposable; never run this against production.
	if _, err = pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id=$1`, store.LocalUserID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id=$1`, store.LocalUserID)
	vault, _ := secrets.New(bytes.Repeat([]byte{8}, 32))
	st := store.NewWithVault(pool, vault)
	srv, err := NewServer(&config.Config{}, st, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	form := func() url.Values {
		return url.Values{"ai_model": {models.DefaultAIModel}, "ai_interval_ms": {"250"}, "ai_concurrency": {"8"}, "ai_batch_size": {"15"}}
	}
	post := func(values url.Values, origin string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "http://example.com/settings", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		handler.ServeHTTP(w, r)
		return w
	}
	page := get("/settings")
	if page.Code != 200 || !strings.Contains(page.Body.String(), "No key saved") || page.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("initial settings: %d", page.Code)
	}
	for _, path := range []string{"/transactions/categorize", "/transactions/1/categorize"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != 403 {
			t.Fatalf("no-key route %s status %d", path, w.Code)
		}
	}
	f := form()
	f.Set("background_categorization", "on")
	if w := post(f, ""); w.Code != 400 {
		t.Fatal("background enabled without own key")
	}
	f.Set("api_key", "sk-or-test-private-key-0001")
	if w := post(f, "https://attacker.example"); w.Code != 403 {
		t.Fatal("cross-origin credential change accepted")
	}
	if w := post(f, "http://example.com"); w.Code != 303 {
		t.Fatalf("save failed: %d %s", w.Code, w.Body.String())
	}
	settings, err := st.AISettings(ctx, store.LocalUserID)
	if err != nil || !settings.HasKey || !settings.Background {
		t.Fatal("key/background not saved")
	}
	page = get("/settings")
	if strings.Contains(page.Body.String(), `id="api-key"`) || !strings.Contains(page.Body.String(), `form="remove-api-key-form"`) {
		t.Fatal("saved key must show a removable row instead of a password input")
	}
	if strings.Contains(page.Body.String(), "sk-or-test-private") || !strings.Contains(page.Body.String(), "A key is saved") || !strings.Contains(page.Body.String(), `id="categorization-coverage"`) {
		t.Fatal("unsafe/incomplete saved-key page")
	}
	f = form()
	f.Set("ai_model", "z-ai/glm-5.3-flash")
	f.Set("ai_interval_ms", "500")
	f.Set("ai_concurrency", "2")
	if w := post(f, ""); w.Code != 303 {
		t.Fatal("settings update failed")
	}
	key, err := st.OpenRouterKey(ctx, store.LocalUserID)
	if err != nil || key != "sk-or-test-private-key-0001" {
		t.Fatal("blank key failed to preserve credential")
	}
	f.Set("api_key", "sk-or-test-replacement-0002")
	if w := post(f, ""); w.Code != 303 {
		t.Fatal("replace failed")
	}
	key, _ = st.OpenRouterKey(ctx, store.LocalUserID)
	if key != "sk-or-test-replacement-0002" {
		t.Fatal("replacement not used")
	}
	f.Set("ai_concurrency", "999")
	if w := post(f, ""); w.Code != 400 || strings.Contains(w.Body.String(), key) {
		t.Fatal("invalid settings accepted or key reflected")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/settings/api-key/remove", nil))
	if w.Code != 303 || w.Header().Get("Location") != "/settings?key_removed=1" {
		t.Fatal("one-click removal failed")
	}
	settings, _ = st.AISettings(ctx, store.LocalUserID)
	key, _ = st.OpenRouterKey(ctx, store.LocalUserID)
	if settings.HasKey || settings.Background || key != "" {
		t.Fatal("removal failed to disable categorization")
	}
	if settings.Model != "z-ai/glm-5.3-flash" || settings.IntervalMS != 500 || settings.Concurrency != 2 {
		t.Fatal("key removal changed unrelated saved preferences")
	}
	page = get("/settings?key_removed=1")
	if !strings.Contains(page.Body.String(), `id="api-key"`) || strings.Contains(page.Body.String(), `form="remove-api-key-form"`) || !strings.Contains(page.Body.String(), "API key removed") {
		t.Fatal("key removal did not restore the edit box")
	}
	if w := get("/settings/categorization-coverage"); strings.Contains(w.Body.String(), `hx-trigger="every 5s"`) {
		t.Fatal("disabled coverage still polling")
	}
}

func TestCrossOriginProtectionRunsBeforeHandlers(t *testing.T) {
	s := &Server{}
	for _, path := range []string{"/settings", "/settings/api-key/remove", "/transactions/categorize", "/transactions/1/categorize"} {
		r := httptest.NewRequest(http.MethodPost, "http://example.com"+path, nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("cross-site mutation reached handler")
		}
	}
}
