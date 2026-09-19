package config

import (
	"testing"
	"time"
)

func TestBackgroundConfiguration(t *testing.T) {
	for _, key := range []string{"AI_BATCH_SIZE", "AI_REQUESTS_PER_DAY", "AI_REQUEST_INTERVAL", "AI_POLL_INTERVAL"} {
		t.Setenv(key, "")
	}
	t.Setenv("AI_API_KEY", "test-key")
	t.Setenv("AI_PROVIDER", "gemini")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AIReady() || cfg.AIBatchSize != 30 || cfg.AIRequestsPerDay != 200 || cfg.AIPollInterval != 30*time.Second {
		t.Fatal("incorrect background defaults")
	}
	for _, provider := range []string{"none", "disabled"} {
		cfg.AIProvider = provider
		if cfg.AIReady() {
			t.Fatalf("enabled for %s", provider)
		}
	}
	cfg.AIProvider, cfg.AIAPIKey = "gemini", ""
	if cfg.AIReady() {
		t.Fatal("enabled without credentials")
	}

}

func TestInvalidBackgroundConfiguration(t *testing.T) {
	for key, value := range map[string]string{
		"AI_BATCH_SIZE": "101", "AI_REQUESTS_PER_DAY": "0",
		"AI_REQUEST_INTERVAL": "0s", "AI_POLL_INTERVAL": "-1m",
	} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if _, err := Load(); err == nil {
				t.Fatalf("accepted %s=%s", key, value)
			}
		})
	}
}
