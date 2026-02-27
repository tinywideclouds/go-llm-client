package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/genai"
	"gopkg.in/yaml.v3"

	"github.com/tinywideclouds/go-llm-client/geminiservice"
	"github.com/tinywideclouds/go-llm-client/geminiservice/config"
	"github.com/tinywideclouds/go-llm-client/internal/store"
	"github.com/tinywideclouds/go-microservice-base/pkg/middleware"
)

//go:embed local.yaml
var configFile []byte

func main() {
	// 1. Setup structured logging
	var logLevel slog.Level
	switch os.Getenv("LOG_LEVEL") {
	case "debug", "DEBUG":
		logLevel = slog.LevelDebug
	case "warn", "WARN":
		logLevel = slog.LevelWarn
	case "error", "ERROR":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	logger.Info("Starting LLM Service", "logLevel", logLevel)
	ctx := context.Background()

	// --- 1. Load Configuration (Stage 1: From YAML) ---
	var yamlCfg config.YamlConfig
	if err := yaml.Unmarshal(configFile, &yamlCfg); err != nil {
		logger.Error("Failed to unmarshal embedded yaml config", "err", err)
		os.Exit(1)
	}

	baseCfg, err := config.NewConfigFromYaml(&yamlCfg, logger)
	if err != nil {
		logger.Error("Failed to build base configuration from YAML", "err", err)
		os.Exit(1)
	}

	// --- 2. Apply Overrides (Stage 2: From Env) ---
	cfg, err := config.UpdateConfigWithEnvOverrides(baseCfg, logger)
	if err != nil {
		logger.Error("Failed to finalize configuration with environment overrides", "err", err)
		os.Exit(1)
	}
	logger.Info("Configuration loaded", "run_mode", cfg.RunMode)

	// --- 3. Dependency Injection ---

	// 3a. LLM Client & Datastores
	genaiClient, firestoreFetcher, sessionStore, err := newDependencies(ctx, cfg, logger)
	if err != nil {
		logger.Error("Failed to initialize core dependencies", "err", err)
		os.Exit(1)
	}
	defer firestoreFetcher.Close()

	// 3b. Authentication Middleware
	authMiddleware, err := newAuthMiddleware(cfg, logger)
	if err != nil {
		logger.Error("Failed to initialize authentication middleware", "err", err)
		os.Exit(1)
	}

	// --- 4. Create Service Instance ---
	// Pass the new sessionStore into the wrapper constructor
	svc := geminiservice.NewGeminiService(cfg, genaiClient, firestoreFetcher, sessionStore, authMiddleware, logger)

	// --- 5. Start Service and Handle Shutdown ---
	errChan := make(chan error, 1)
	go func() {
		logger.Info("Starting service...", "address", cfg.HTTPListenAddr)
		if startErr := svc.Start(); startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
			errChan <- startErr
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		logger.Error("Service failed", "err", err)
		os.Exit(1)
	case sig := <-quit:
		logger.Info("OS signal received, initiating shutdown.", "signal", sig.String())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if shutdownErr := svc.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error("Service shutdown failed", "err", shutdownErr)
		} else {
			logger.Info("Service shutdown complete")
		}
	}
}

// newDependencies builds the GenAI and Firestore clients.
func newDependencies(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*genai.Client, store.Fetcher, store.SessionStore, error) {
	if cfg.GeminiAPIKey == "" {
		logger.Error("GEMINI_API_KEY config is missing")
		return nil, nil, nil, fmt.Errorf("GEMINI_API_KEY is required")
	}

	clientConfig := &genai.ClientConfig{
		APIKey: cfg.GeminiAPIKey,
	}

	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	fsClient, err := firestore.NewClient(ctx, cfg.GoogleProjectID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create Firestore client: %w", err)
	}

	// FIX: Use the shared cache.StoreCollections type correctly
	fetcher := store.NewFirestoreFetcher(
		fsClient,
		cfg.Store,
		logger.With("component", "FirestoreFetcher"),
	)

	// Initialize the new Session Store using the BundleCollection string
	sessionStore := store.NewFirestoreSessionStore(
		fsClient,
		cfg.Store.BundleCollection,
		logger.With("component", "FirestoreSession"),
	)

	logger.Info("Core dependencies initialized successfully")

	return client, fetcher, sessionStore, nil
}

// newAuthMiddleware creates the JWT-validating middleware, or a bypass if in DEV mode.
func newAuthMiddleware(cfg *config.Config, logger *slog.Logger) (func(http.Handler) http.Handler, error) {
	if os.Getenv("DISABLE_AUTH") == "true" {
		logger.Warn("⚠️  AUTH DISABLED: Running in development mode with NoopAuth")
		return middleware.NoopAuth(true, "dev-user-123"), nil
	}

	sanitizedIdentityURL := strings.Trim(cfg.IdentityServiceURL, "\"")
	logger.Debug("Discovering JWT config", "identity_url", sanitizedIdentityURL)

	jwksURL, err := middleware.DiscoverAndValidateJWTConfig(sanitizedIdentityURL, middleware.RSA256, logger)
	if err != nil {
		logger.Warn("JWT configuration validation failed. This may be fatal if auth is required.", "err", err)
	} else {
		logger.Info("VERIFIED JWKS CONFIG")
	}

	authMiddleware, err := middleware.NewJWKSAuthMiddleware(jwksURL, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth middleware: %w", err)
	}

	logger.Debug("JWKS auth middleware created successfully")
	return authMiddleware, nil
}
