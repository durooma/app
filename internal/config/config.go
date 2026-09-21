package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration, sourced from environment variables so
// the same binary runs identically on a DigitalOcean droplet or a home server.
type Config struct {
	DatabaseURL  string
	HTTPAddr     string
	BaseCurrency string // e.g. "CHF" — the currency all reports are normalised to

	// AI categorization
	AIProvider        string // "gemini" or "none"
	AIModel           string
	AIAPIKey          string
	AIBatchSize       int
	AIRequestsPerDay  int
	AIRequestInterval time.Duration
	AIPollInterval    time.Duration

	// FX rate source (frankfurter.app compatible endpoint)
	FXBaseURL string
}

// Load reads configuration from the environment, applying sensible defaults that
// work out of the box with the bundled docker-compose setup.
func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:  env("DATABASE_URL", "postgres://durooma:durooma@localhost:5432/durooma?sslmode=disable"),
		HTTPAddr:     env("HTTP_ADDR", ":8080"),
		BaseCurrency: strings.ToUpper(env("BASE_CURRENCY", "CHF")),
		AIProvider:   strings.ToLower(env("AI_PROVIDER", "gemini")),
		AIModel:      env("AI_MODEL", "gemini-3.1-flash-lite"),
		AIAPIKey:     env("AI_API_KEY", ""),
		FXBaseURL:    env("FX_BASE_URL", "https://api.frankfurter.app"),
	}
	for _, setting := range []struct {
		key, def string
		dest     *int
		max      int
	}{
		{"AI_BATCH_SIZE", "30", &c.AIBatchSize, 100},
		{"AI_REQUESTS_PER_DAY", "1500", &c.AIRequestsPerDay, 86400},
	} {
		n, err := strconv.Atoi(env(setting.key, setting.def))
		if err != nil || n < 1 || n > setting.max {
			return nil, fmt.Errorf("%s must be between 1 and %d", setting.key, setting.max)
		}
		*setting.dest = n
	}
	for _, setting := range []struct {
		key, def string
		dest     *time.Duration
	}{
		{"AI_REQUEST_INTERVAL", "57.6s", &c.AIRequestInterval},
		{"AI_POLL_INTERVAL", "30s", &c.AIPollInterval},
	} {
		d, err := time.ParseDuration(env(setting.key, setting.def))
		if err != nil || d < time.Second || d > 24*time.Hour {
			return nil, fmt.Errorf("%s must be a duration between 1s and 24h", setting.key)
		}
		*setting.dest = d
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

// AIEnabled reports whether an AI categorization provider is configured.
func (c *Config) AIEnabled() bool {
	return c.AIProvider != "" && c.AIProvider != "none" && c.AIProvider != "disabled"
}

// AIReady reports provider availability, independent of user preferences.
func (c *Config) AIReady() bool {
	return c.AIEnabled() && c.AIAPIKey != ""
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
