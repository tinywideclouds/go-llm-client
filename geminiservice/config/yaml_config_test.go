// internal/config/yaml_config_test.go
package config_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-llm-client/geminiservice/config"
	"github.com/tinywideclouds/go-microservice-base/pkg/middleware"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewConfigFromYaml(t *testing.T) {
	logger := newTestLogger()

	t.Run("Success - maps all fields correctly from YAML struct", func(t *testing.T) {
		yamlCfg := &config.YamlConfig{
			RunMode:            "test-mode",
			HTTPListenAddr:     ":9090",
			IdentityServiceURL: "http://yaml-identity.com",
			Cors: struct {
				AllowedOrigins []string `yaml:"allowed_origins"`
				Role           string   `yaml:"cors_role"`
			}{
				AllowedOrigins: []string{"http://origin1.com", "http://origin2.com"},
				Role:           "my-custom-role",
			},
		}

		cfg, err := config.NewConfigFromYaml(yamlCfg, logger)

		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "test-mode", cfg.RunMode)
		assert.Equal(t, ":9090", cfg.HTTPListenAddr)
		assert.Equal(t, "http://yaml-identity.com", cfg.IdentityServiceURL)

		assert.NotNil(t, cfg.CorsConfig)
		assert.Equal(t, []string{"http://origin1.com", "http://origin2.com"}, cfg.CorsConfig.AllowedOrigins)
		assert.Equal(t, middleware.CorsRole("my-custom-role"), cfg.CorsConfig.Role)

		// GeminiAPIKey should be empty as it is environment-only
		assert.Empty(t, cfg.GeminiAPIKey)
	})
}
