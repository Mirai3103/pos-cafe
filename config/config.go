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
		ReadTimeout:        15 * time.Second,
		WriteTimeout:       15 * time.Second,
		IdleTimeout:        60 * time.Second,
		ReadHeaderTimeout:  5 * time.Second,
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

	return nil
}

func getEnv(key, defaultValue string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultValue
}
