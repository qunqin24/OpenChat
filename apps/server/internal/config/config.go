// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Env     string
	Version string

	HTTPAddr        string
	ShutdownTimeout time.Duration
	CORSOrigins     []string

	DatabaseURL string
}

// Load reads configuration from the environment, applying defaults that are
// safe for local development. It fails fast on anything that has no sane default.
func Load() (*Config, error) {
	c := &Config{
		Env:             env("APP_ENV", "development"),
		Version:         env("APP_VERSION", "dev"),
		HTTPAddr:        env("HTTP_ADDR", ":8080"),
		ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		CORSOrigins:     envList("CORS_ORIGINS", []string{"http://localhost:5173"}),
		DatabaseURL:     env("DATABASE_URL", "postgres://openchat:openchat@localhost:5432/openchat?sslmode=disable"),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}

	return c, nil
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func envList(key string, fallback []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
