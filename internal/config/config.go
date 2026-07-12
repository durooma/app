package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds all runtime configuration, sourced from environment variables so
// the same binary runs identically on a DigitalOcean droplet or a home server.
type Config struct {
	DatabaseURL  string
	HTTPAddr     string
	BaseCurrency string // e.g. "CHF" — the currency all reports are normalised to

	// AI categorization
	AIProvider string // "gemini", "openai", or "none"
	AIModel    string
	AIAPIKey   string

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
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return c, nil
}

// AIEnabled reports whether an AI categorization provider is configured.
func (c *Config) AIEnabled() bool {
	return c.AIProvider != "" && c.AIProvider != "none"
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
