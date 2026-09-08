package config_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigLoadAndValidate(t *testing.T) {
	t.Run("default configuration is valid", func(t *testing.T) {
		cfg, err := config.Load()
		require.NoError(t, err)
		assert.NotEmpty(t, cfg.Port)
		assert.NotEmpty(t, cfg.DatabaseURL)
		assert.NotEmpty(t, cfg.Environment)
		assert.NotEmpty(t, cfg.CORSAllowedOrigins)
	})

	t.Run("invalid database URL", func(t *testing.T) {
		cfg := &config.Config{
			Port:        "8080",
			DatabaseURL: "",
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "DATABASE_URL must not be empty")
	})

	t.Run("invalid port string", func(t *testing.T) {
		cfg := &config.Config{
			Port:        "invalid-port",
			DatabaseURL: "postgres://localhost/test",
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "PORT must be a valid port number")
	})

	t.Run("out of range port", func(t *testing.T) {
		cfg := &config.Config{
			Port:        "70000",
			DatabaseURL: "postgres://localhost/test",
		}
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "PORT must be a valid port number")
	})
}
