// internal/config/yaml_config_test.go
package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-llm-client/geminiservice/config"
	"github.com/tinywideclouds/go-microservice-base/pkg/middleware"
)

func TestNewConfigFromYaml(t *testing.T) {
	logger := newTestLogger()

	t.Run("Success - maps all fields correctly from YAML struct including custom timeouts and memory limit", func(t *testing.T) {
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

		yamlCfg.Timeouts.DatabaseIO = 5 * time.Second
		yamlCfg.Firestore.MaxFetchMB = 200

		cfg, err := config.NewConfigFromYaml(yamlCfg, logger)

		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "test-mode", cfg.RunMode)
		assert.Equal(t, ":9090", cfg.HTTPListenAddr)
		assert.Equal(t, "http://yaml-identity.com", cfg.IdentityServiceURL)

		assert.NotNil(t, cfg.CorsConfig)
		assert.Equal(t, []string{"http://origin1.com", "http://origin2.com"}, cfg.CorsConfig.AllowedOrigins)
		assert.Equal(t, middleware.CorsRole("my-custom-role"), cfg.CorsConfig.Role)

		// Check the custom memory limit
		assert.Equal(t, int64(200*1024*1024), cfg.Store.MaxFetchBytes)

		// Check the custom timeout
		assert.Equal(t, 5*time.Second, cfg.Timeouts.DatabaseIO)
		// Check that other timeouts defaulted correctly
		assert.Equal(t, 5*time.Minute, cfg.Timeouts.LLMStream)

		// GeminiAPIKey should be empty as it is environment-only
		assert.Empty(t, cfg.GeminiAPIKey)
	})

	t.Run("Success - applies defaults when none specified", func(t *testing.T) {
		yamlCfg := &config.YamlConfig{} // Empty struct

		cfg, err := config.NewConfigFromYaml(yamlCfg, logger)

		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, int64(100*1024*1024), cfg.Store.MaxFetchBytes)
		assert.Equal(t, 10*time.Second, cfg.Timeouts.DatabaseIO)
		assert.Equal(t, 120*time.Second, cfg.Timeouts.CacheBuild)
		assert.Equal(t, 60*time.Second, cfg.Timeouts.LLMGenerate)
		assert.Equal(t, 5*time.Minute, cfg.Timeouts.LLMStream)
	})
}
