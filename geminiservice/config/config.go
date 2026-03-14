package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tinywideclouds/go-llm-client/internal/store"
	"github.com/tinywideclouds/go-microservice-base/pkg/middleware"
)

// TimeoutsConfig groups the various context deadlines for the service.
type TimeoutsConfig struct {
	DatabaseIO  time.Duration
	CacheBuild  time.Duration
	LLMGenerate time.Duration
	LLMStream   time.Duration
}

// Config defines the *single*, authoritative configuration for the LLM Service.
type Config struct {
	// Fields loaded from YAML
	RunMode            string
	HTTPListenAddr     string
	IdentityServiceURL string

	GoogleProjectID string
	Store           store.StoreCollections

	Timeouts TimeoutsConfig

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

	// Memory Limit Env Override
	if maxMB := os.Getenv("MAX_FETCH_MB"); maxMB != "" {
		if mb, err := strconv.ParseInt(maxMB, 10, 64); err == nil && mb > 0 {
			logger.Debug("Overriding config value", "key", "MAX_FETCH_MB", "source", "env")
			cfg.Store.MaxFetchBytes = mb * 1024 * 1024
		} else {
			logger.Warn("Failed to parse MAX_FETCH_MB from env or value is invalid", "error", err)
		}
	}

	// Timeout Env Overrides
	if dbIO := os.Getenv("TIMEOUT_DB_IO"); dbIO != "" {
		if d, err := time.ParseDuration(dbIO); err == nil {
			logger.Debug("Overriding config value", "key", "TIMEOUT_DB_IO", "source", "env")
			cfg.Timeouts.DatabaseIO = d
		} else {
			logger.Warn("Failed to parse TIMEOUT_DB_IO from env", "error", err)
		}
	}

	if cacheBuild := os.Getenv("TIMEOUT_CACHE_BUILD"); cacheBuild != "" {
		if d, err := time.ParseDuration(cacheBuild); err == nil {
			logger.Debug("Overriding config value", "key", "TIMEOUT_CACHE_BUILD", "source", "env")
			cfg.Timeouts.CacheBuild = d
		} else {
			logger.Warn("Failed to parse TIMEOUT_CACHE_BUILD from env", "error", err)
		}
	}

	if llmGen := os.Getenv("TIMEOUT_LLM_GENERATE"); llmGen != "" {
		if d, err := time.ParseDuration(llmGen); err == nil {
			logger.Debug("Overriding config value", "key", "TIMEOUT_LLM_GENERATE", "source", "env")
			cfg.Timeouts.LLMGenerate = d
		} else {
			logger.Warn("Failed to parse TIMEOUT_LLM_GENERATE from env", "error", err)
		}
	}

	if llmStream := os.Getenv("TIMEOUT_LLM_STREAM"); llmStream != "" {
		if d, err := time.ParseDuration(llmStream); err == nil {
			logger.Debug("Overriding config value", "key", "TIMEOUT_LLM_STREAM", "source", "env")
			cfg.Timeouts.LLMStream = d
		} else {
			logger.Warn("Failed to parse TIMEOUT_LLM_STREAM from env", "error", err)
		}
	}

	return cfg, nil
}
