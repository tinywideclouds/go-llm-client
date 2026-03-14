package config

import (
	"log/slog"
	"time"

	"github.com/tinywideclouds/go-llm-client/internal/store"
	"github.com/tinywideclouds/go-microservice-base/pkg/middleware"
)

// YamlConfig is the structure that mirrors the raw local.yaml file.
type YamlConfig struct {
	RunMode            string `yaml:"run_mode"`
	HTTPListenAddr     string `yaml:"http_listen_addr"`
	IdentityServiceURL string `yaml:"identity_service_url"`

	GoogleProjectID string `yaml:"google_project_id"`
	Firestore       struct {
		BundleCollection   string `yaml:"bundle_collection"`
		FilesCollection    string `yaml:"files_collection"`
		ProfilesCollection string `yaml:"profiles_collection"`
		MaxFetchMB         int64  `yaml:"max_fetch_mb"`
	} `yaml:"firestore"`

	Timeouts struct {
		DatabaseIO  time.Duration `yaml:"database_io"`
		CacheBuild  time.Duration `yaml:"cache_build"`
		LLMGenerate time.Duration `yaml:"llm_generate"`
		LLMStream   time.Duration `yaml:"llm_stream"`
	} `yaml:"timeouts"`

	Cors struct {
		AllowedOrigins []string `yaml:"allowed_origins"`
		Role           string   `yaml:"cors_role"`
	} `yaml:"cors"`
}

// NewConfigFromYaml converts the YamlConfig into a clean, base Config struct.
// This struct is the "Stage 1" configuration, ready to be augmented by environment overrides.
func NewConfigFromYaml(baseCfg *YamlConfig, logger *slog.Logger) (*Config, error) {
	logger.Debug("Mapping YAML config to base config struct")

	bc := baseCfg.Firestore.BundleCollection
	if bc == "" {
		bc = "CacheBundles"
	}

	fc := baseCfg.Firestore.FilesCollection
	if fc == "" {
		fc = "Files"
	}

	pc := baseCfg.Firestore.ProfilesCollection
	if pc == "" {
		pc = "FilterProfiles"
	}

	maxFetchBytes := baseCfg.Firestore.MaxFetchMB * 1024 * 1024
	if maxFetchBytes <= 0 {
		maxFetchBytes = 100 * 1024 * 1024 // Default to 100MB
	}

	// Apply sensible defaults if not specified in YAML
	dbIO := baseCfg.Timeouts.DatabaseIO
	if dbIO == 0 {
		dbIO = 10 * time.Second
	}

	cacheBuild := baseCfg.Timeouts.CacheBuild
	if cacheBuild == 0 {
		cacheBuild = 120 * time.Second
	}

	llmGen := baseCfg.Timeouts.LLMGenerate
	if llmGen == 0 {
		llmGen = 60 * time.Second
	}

	llmStream := baseCfg.Timeouts.LLMStream
	if llmStream == 0 {
		llmStream = 5 * time.Minute
	}

	cfg := &Config{
		RunMode:            baseCfg.RunMode,
		HTTPListenAddr:     baseCfg.HTTPListenAddr,
		IdentityServiceURL: baseCfg.IdentityServiceURL,
		GoogleProjectID:    baseCfg.GoogleProjectID,
		Store: store.StoreCollections{
			BundleCollection:   bc,
			FilesCollection:    fc,
			ProfilesCollection: pc,
			MaxFetchBytes:      maxFetchBytes,
		},
		Timeouts: TimeoutsConfig{
			DatabaseIO:  dbIO,
			CacheBuild:  cacheBuild,
			LLMGenerate: llmGen,
			LLMStream:   llmStream,
		},
		CorsConfig: middleware.CorsConfig{
			AllowedOrigins: baseCfg.Cors.AllowedOrigins,
			Role:           middleware.CorsRole(baseCfg.Cors.Role),
		},
	}
	// Note: GeminiAPIKey is intentionally left blank here, as it's an environment override.

	logger.Debug("YAML config mapping complete",
		"run_mode", cfg.RunMode,
		"http_listen_addr", cfg.HTTPListenAddr,
		"identity_service_url", cfg.IdentityServiceURL,
	)

	return cfg, nil
}
