package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DatabaseURL string
	Environment string
}

func Load() *Config {
	// Attempt to load .env file if present, ignore if not found
	_ = godotenv.Load()

	return &Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://cafe_pos:cafe_pos_dev@localhost:5432/cafe_pos?sslmode=disable"),
		Environment: getEnv("APP_ENV", "development"),
	}
}

func getEnv(key, defaultValue string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultValue
}
