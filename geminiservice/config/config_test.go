// internal/config/config_test.go
package config_test

import (
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-llm-client/geminiservice/config"
	"github.com/tinywideclouds/go-llm-client/internal/store"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newBaseConfig() *config.Config {
	return &config.Config{
		RunMode:            "base-mode",
		HTTPListenAddr:     ":8080",
		IdentityServiceURL: "http://base-id-service.com",
		Store: store.StoreCollections{
			MaxFetchBytes: 100 * 1024 * 1024, // 100MB base
		},
		Timeouts: config.TimeoutsConfig{
			DatabaseIO:  10 * time.Second,
			CacheBuild:  120 * time.Second,
			LLMGenerate: 60 * time.Second,
			LLMStream:   5 * time.Minute,
		},
	}
}

func TestUpdateConfigWithEnvOverrides(t *testing.T) {
	logger := newTestLogger()

	t.Run("Success - All overrides applied", func(t *testing.T) {
		baseCfg := newBaseConfig()

		t.Setenv("IDENTITY_SERVICE_URL", "http://env-id-service.com")
		t.Setenv("GEMINI_API_KEY", "env-api-key")
		t.Setenv("PORT", "9090")
		t.Setenv("CORS_ALLOWED_ORIGINS", "http://env1.com, http://env2.com ")
		t.Setenv("MAX_FETCH_MB", "250")
		t.Setenv("TIMEOUT_DB_IO", "15s")
		t.Setenv("TIMEOUT_LLM_STREAM", "10m")

		cfg, err := config.UpdateConfigWithEnvOverrides(baseCfg, logger)

		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "http://env-id-service.com", cfg.IdentityServiceURL)
		assert.Equal(t, "env-api-key", cfg.GeminiAPIKey)
		assert.Equal(t, ":9090", cfg.HTTPListenAddr)
		assert.Equal(t, []string{"http://env1.com", "http://env2.com"}, cfg.CorsConfig.AllowedOrigins)

		assert.Equal(t, int64(250*1024*1024), cfg.Store.MaxFetchBytes)
		assert.Equal(t, 15*time.Second, cfg.Timeouts.DatabaseIO)
		assert.Equal(t, 10*time.Minute, cfg.Timeouts.LLMStream)
		// Non-overridden timeouts should remain unchanged
		assert.Equal(t, 120*time.Second, cfg.Timeouts.CacheBuild)

		// Non-overridden fields should remain unchanged
		assert.Equal(t, "base-mode", cfg.RunMode)
	})

	t.Run("Success - Only required GEMINI_API_KEY applied", func(t *testing.T) {
		baseCfg := newBaseConfig()

		os.Unsetenv("IDENTITY_SERVICE_URL")
		os.Unsetenv("PORT")
		os.Unsetenv("CORS_ALLOWED_ORIGINS")
		os.Unsetenv("MAX_FETCH_MB")
		os.Unsetenv("TIMEOUT_DB_IO")

		t.Setenv("GEMINI_API_KEY", "my-secret-api-key")

		cfg, err := config.UpdateConfigWithEnvOverrides(baseCfg, logger)

		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "http://base-id-service.com", cfg.IdentityServiceURL)
		assert.Equal(t, ":8080", cfg.HTTPListenAddr)

		assert.Equal(t, "my-secret-api-key", cfg.GeminiAPIKey)
		assert.Equal(t, int64(100*1024*1024), cfg.Store.MaxFetchBytes) // unchanged base
		assert.Equal(t, 10*time.Second, cfg.Timeouts.DatabaseIO)       // unchanged base
	})

	t.Run("Invalid overrides fall back to base config without error", func(t *testing.T) {
		baseCfg := newBaseConfig()
		t.Setenv("TIMEOUT_DB_IO", "not-a-duration")
		t.Setenv("MAX_FETCH_MB", "not-a-number")

		cfg, err := config.UpdateConfigWithEnvOverrides(baseCfg, logger)
		require.NoError(t, err)
		assert.Equal(t, 10*time.Second, cfg.Timeouts.DatabaseIO)
		assert.Equal(t, int64(100*1024*1024), cfg.Store.MaxFetchBytes)
	})
}
