package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DBPath      string
	Environment string
}

func Load() *Config {
	// Attempt to load .env file if present, ignore if not found
	_ = godotenv.Load()

	return &Config{
		Port:        getEnv("PORT", "8080"),
		DBPath:      getEnv("DB_PATH", "pos_cafe.db"),
		Environment: getEnv("APP_ENV", "development"),
	}
}

func getEnv(key, defaultValue string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultValue
}
