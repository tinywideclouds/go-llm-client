// internal/config/config_test.go
package config_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-llm-client/geminiservice/config"
)

func newBaseConfig() *config.Config {
	return &config.Config{
		RunMode:            "base-mode",
		HTTPListenAddr:     ":8080",
		IdentityServiceURL: "http://base-id-service.com",
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

		cfg, err := config.UpdateConfigWithEnvOverrides(baseCfg, logger)

		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "http://env-id-service.com", cfg.IdentityServiceURL)
		assert.Equal(t, "env-api-key", cfg.GeminiAPIKey)
		assert.Equal(t, ":9090", cfg.HTTPListenAddr)
		assert.Equal(t, []string{"http://env1.com", "http://env2.com"}, cfg.CorsConfig.AllowedOrigins)

		// Non-overridden fields should remain unchanged
		assert.Equal(t, "base-mode", cfg.RunMode)
	})

	t.Run("Success - Only required GEMINI_API_KEY applied", func(t *testing.T) {
		baseCfg := newBaseConfig()

		os.Unsetenv("IDENTITY_SERVICE_URL")
		os.Unsetenv("PORT")
		os.Unsetenv("CORS_ALLOWED_ORIGINS")

		t.Setenv("GEMINI_API_KEY", "my-secret-api-key")

		cfg, err := config.UpdateConfigWithEnvOverrides(baseCfg, logger)

		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "http://base-id-service.com", cfg.IdentityServiceURL)
		assert.Equal(t, ":8080", cfg.HTTPListenAddr)

		assert.Equal(t, "my-secret-api-key", cfg.GeminiAPIKey)
	})
}
