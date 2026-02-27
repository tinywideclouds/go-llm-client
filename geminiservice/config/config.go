package config

import (
	"log/slog"
	"os"
	"strings"

	"github.com/tinywideclouds/go-llm/pkg/cache/v1"
	"github.com/tinywideclouds/go-microservice-base/pkg/middleware"
)

// Config defines the *single*, authoritative configuration for the LLM Service.
type Config struct {
	// Fields loaded from YAML
	RunMode            string
	HTTPListenAddr     string
	IdentityServiceURL string

	GoogleProjectID string
	Store           cache.StoreCollections

	// CorsConfig is the processed, ready-to-use middleware config.
	CorsConfig middleware.CorsConfig

	// GeminiAPIKey is populated exclusively from the "GEMINI_API_KEY" env var.
	GeminiAPIKey string
}

// UpdateConfigWithEnvOverrides takes the base configuration (created from YAML)
// and completes it by applying environment variables.
func UpdateConfigWithEnvOverrides(cfg *Config, logger *slog.Logger) (*Config, error) {
	logger.Debug("Applying environment variable overrides...")

	if idURL := os.Getenv("IDENTITY_SERVICE_URL"); idURL != "" {
		logger.Debug("Overriding config value", "key", "IDENTITY_SERVICE_URL", "source", "env")
		cfg.IdentityServiceURL = idURL
	}

	if projectID := os.Getenv("GOOGLE_PROJECT_ID"); projectID != "" {
		logger.Debug("Loaded config value", "key", "GOOGLE_PROJECT_ID", "source", "env")
		cfg.GoogleProjectID = projectID
	}

	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		logger.Debug("Loaded config value", "key", "GEMINI_API_KEY", "source", "env")
		cfg.GeminiAPIKey = apiKey
	}

	if port := os.Getenv("PORT"); port != "" {
		logger.Debug("Overriding config value", "key", "PORT", "source", "env")
		cfg.HTTPListenAddr = ":" + port
	}

	if corsOrigins := os.Getenv("CORS_ALLOWED_ORIGINS"); corsOrigins != "" {
		logger.Debug("Overriding config value", "key", "CORS_ALLOWED_ORIGINS", "source", "env")
		rawOrigins := strings.Split(corsOrigins, ",")
		var cleanOrigins []string
		for _, o := range rawOrigins {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				cleanOrigins = append(cleanOrigins, trimmed)
			}
		}
		cfg.CorsConfig.AllowedOrigins = cleanOrigins
	}

	return cfg, nil
}
