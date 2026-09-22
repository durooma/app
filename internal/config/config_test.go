package config

import (
	"reflect"
	"testing"
	"time"
)

func TestLegacyAIEnvironmentIsIgnored(t *testing.T) {
	for _, key := range []string{"AI_PROVIDER", "AI_API_KEY", "AI_MODEL", "AI_BATCH_SIZE", "AI_REQUESTS_PER_DAY", "AI_REQUEST_INTERVAL", "AI_POLL_INTERVAL", "CREDENTIALS_KEY_FILE"} {
		t.Setenv(key, "")
	}
	baseline, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"AI_PROVIDER", "AI_API_KEY", "AI_MODEL", "AI_BATCH_SIZE", "AI_REQUESTS_PER_DAY", "AI_REQUEST_INTERVAL"} {
		t.Setenv(key, "legacy-value-must-not-be-used")
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, baseline) || got.AIPollInterval != 30*time.Second || got.CredentialsKeyFile != ".durooma/credentials.key" {
		t.Fatal("legacy AI config changed runtime defaults")
	}
}
func TestInvalidPollInterval(t *testing.T) {
	for _, v := range []string{"0s", "-1m", "bad", "25h"} {
		t.Setenv("AI_POLL_INTERVAL", v)
		if _, err := Load(); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
}
