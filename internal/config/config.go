package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// User AI credentials, models and limits live in encrypted user settings.
// Legacy AI_API_KEY/AI_PROVIDER/AI_MODEL and pacing variables are ignored.
type Config struct {
	DatabaseURL        string
	HTTPAddr           string
	BaseCurrency       string
	CredentialsKeyFile string
	AIPollInterval     time.Duration
	FXBaseURL          string
}

func Load() (*Config, error) {
	c := &Config{
		DatabaseURL: env("DATABASE_URL", "postgres://durooma:durooma@localhost:5432/durooma?sslmode=disable"),
		HTTPAddr:    env("HTTP_ADDR", ":8080"), BaseCurrency: strings.ToUpper(env("BASE_CURRENCY", "CHF")),
		CredentialsKeyFile: env("CREDENTIALS_KEY_FILE", ".durooma/credentials.key"),
		FXBaseURL:          env("FX_BASE_URL", "https://api.frankfurter.app"),
	}
	d, err := time.ParseDuration(env("AI_POLL_INTERVAL", "30s"))
	if err != nil || d < time.Second || d > 24*time.Hour {
		return nil, fmt.Errorf("AI_POLL_INTERVAL must be a duration between 1s and 24h")
	}
	c.AIPollInterval = d
	return c, nil
}
func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
