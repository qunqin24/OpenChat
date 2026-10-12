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

	// LogFile, when set, gets a copy of the log as JSON lines, started over
	// at LogFileMB megabytes with LogFileKeep earlier files kept. The Docker
	// image sets it to a file on the data volume, which outlives the container.
	LogFile     string
	LogFileMB   int
	LogFileKeep int
}

// buildVersion is the version this binary was built as, written in at build
// time (the image: -ldflags "-X .../config.buildVersion=1.2.3", from the tag);
// "dev" otherwise. Deliberately not an environment variable: it once was
// (APP_VERSION, set in the image), and panels that upgrade a container by
// recreating it - 1Panel, Portainer - carry the old container's environment
// over, the old image's APP_VERSION with it, so an upgraded server went on
// reporting the version it had replaced.
var buildVersion = "dev"

// Load reads configuration from the environment, applying defaults that are
// safe for local development. It fails fast on anything that has no sane default.
func Load() (*Config, error) {
	c := &Config{
		Env:             env("APP_ENV", "development"),
		Version:         buildVersion,
		HTTPAddr:        env("HTTP_ADDR", ":8080"),
		ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		CORSOrigins:     envList("CORS_ORIGINS", []string{"http://localhost:5173"}),
		DatabaseURL:     env("DATABASE_URL", "postgres://openchat:openchat@localhost:5432/openchat?sslmode=disable"),
		LogFile:         env("LOG_FILE", ""),
		LogFileMB:       envInt("LOG_FILE_MAX_MB", 20),
		LogFileKeep:     envInt("LOG_FILE_KEEP", 5),
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

func envInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscan(v, &n); err != nil || n < 1 {
		return fallback
	}
	return n
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
