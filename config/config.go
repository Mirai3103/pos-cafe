package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port               string
	DatabaseURL        string
	Environment        string
	CORSAllowedOrigins []string
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	ReadHeaderTimeout  time.Duration
}

func Load() (*Config, error) {
	// Attempt to load .env file if present, ignore if not found
	_ = godotenv.Load()

	corsOriginsStr := getEnv("CORS_ALLOWED_ORIGINS", "*")
	corsOrigins := strings.Split(corsOriginsStr, ",")
	for i := range corsOrigins {
		corsOrigins[i] = strings.TrimSpace(corsOrigins[i])
	}

	cfg := &Config{
		Port:               getEnv("PORT", "8080"),
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos?sslmode=disable"),
		Environment:        getEnv("APP_ENV", "development"),
		CORSAllowedOrigins: corsOrigins,
		ReadTimeout:        getEnvDuration("HTTP_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:       getEnvDuration("HTTP_WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:        getEnvDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
		ReadHeaderTimeout:  getEnvDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL must not be empty")
	}

	portNum, err := strconv.Atoi(c.Port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return fmt.Errorf("PORT must be a valid port number (1-65535), got: %s", c.Port)
	}

	// A non-positive timeout means "no timeout" in net/http, which silently
	// removes the protection these settings exist to provide.
	for _, t := range []struct {
		name  string
		value time.Duration
	}{
		{"HTTP_READ_TIMEOUT", c.ReadTimeout},
		{"HTTP_WRITE_TIMEOUT", c.WriteTimeout},
		{"HTTP_IDLE_TIMEOUT", c.IdleTimeout},
		{"HTTP_READ_HEADER_TIMEOUT", c.ReadHeaderTimeout},
	} {
		if t.value <= 0 {
			return fmt.Errorf("%s must be a positive duration, got: %s", t.name, t.value)
		}
	}

	return nil
}

// getEnvDuration reads a Go duration string such as "15s" or "2m". An
// unparseable value falls back to the default rather than failing startup: the
// Validate pass below is what rejects genuinely unusable values.
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return defaultValue
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func getEnv(key, defaultValue string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultValue
}
