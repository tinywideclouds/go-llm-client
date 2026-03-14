package geminiservice

import (
	"errors"
	"log/slog"
	"net/http"

	"google.golang.org/genai"

	"github.com/tinywideclouds/go-llm-client/geminiservice/config"
	"github.com/tinywideclouds/go-llm-client/internal/api"
	llmservice "github.com/tinywideclouds/go-llm-client/internal/service"
	"github.com/tinywideclouds/go-llm-client/internal/session"
	"github.com/tinywideclouds/go-llm-client/internal/store"
	"github.com/tinywideclouds/go-microservice-base/pkg/microservice"
	"github.com/tinywideclouds/go-microservice-base/pkg/middleware"
)

type Wrapper struct {
	*microservice.BaseServer
	logger *slog.Logger
}

// NewGeminiService creates and wires up the entire LLM service.
func NewGeminiService(
	cfg *config.Config,
	client *genai.Client,
	fetcher store.Fetcher,
	sessionStore store.SessionStore,
	authMiddleware func(http.Handler) http.Handler,
	logger *slog.Logger,
) *Wrapper {
	// 1. Create the standard base server using the config port.
	baseServer := microservice.NewBaseServer(logger, cfg.HTTPListenAddr)

	// 2. Initialize Domain Services.
	sessionService := session.NewService(sessionStore)
	managerLogger := logger.With("component", "LLMManager")
	llmManager := llmservice.NewLLMManager(client, fetcher, managerLogger)

	// 3. Create the service-specific API handlers.
	apiHandler := &api.API{
		SessionService: sessionService,
		LLM:            llmManager,
		Fetcher:        fetcher,
		CachePolicy:    api.NewCachePolicy(),
		Timeouts:       cfg.Timeouts, // FIX: Inject the loaded timeouts here!
		Logger:         logger,
	}

	// 4. Get the mux and create CORS middleware.
	mux := baseServer.Mux()
	corsMiddleware := middleware.NewCorsMiddleware(cfg.CorsConfig, logger)
	optionsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	// 5. Register OPTIONS for CORS pre-flight across the routes
	mux.Handle("OPTIONS /v1/llm/compiled_cache/build", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/llm/generate", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/llm/generate-stream", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/llm/session/{id}/proposals", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/llm/session/{id}/proposals/{propId}", corsMiddleware(optionsHandler))

	// 6. Register API Routes with CORS and Auth
	buildCacheHandler := http.HandlerFunc(apiHandler.BuildCompiledCacheHandler)
	mux.Handle("POST /v1/llm/compiled_cache/build", corsMiddleware(authMiddleware(buildCacheHandler)))

	generateHandler := http.HandlerFunc(apiHandler.GenerateHandler)
	mux.Handle("POST /v1/llm/generate", corsMiddleware(authMiddleware(generateHandler)))

	generateStreamHandler := http.HandlerFunc(apiHandler.GenerateStreamHandler)
	mux.Handle("POST /v1/llm/generate-stream", corsMiddleware(authMiddleware(generateStreamHandler)))

	listProposalsHandler := http.HandlerFunc(apiHandler.ListProposalsHandler)
	mux.Handle("GET /v1/llm/session/{id}/proposals", corsMiddleware(authMiddleware(listProposalsHandler)))

	// Unified DELETE route replaces both Accept and Reject
	removeProposalHandler := http.HandlerFunc(apiHandler.RemoveProposalHandler)
	mux.Handle("DELETE /v1/llm/session/{id}/proposals/{propId}", corsMiddleware(authMiddleware(removeProposalHandler)))

	return &Wrapper{
		BaseServer: baseServer,
		logger:     logger,
	}
}

func (w *Wrapper) Start() error {
	errChan := make(chan error, 1)
	httpReadyChan := make(chan struct{})
	w.BaseServer.SetReadyChannel(httpReadyChan)

	go func() {
		if err := w.BaseServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			w.logger.Error("HTTP server failed", "err", err)
			errChan <- err
		}
		close(errChan)
	}()

	select {
	case <-httpReadyChan:
		w.logger.Info("HTTP listener is active.")
		w.SetReady(true)
		w.logger.Info("Service is marked as ready.")
		return nil
	case err := <-errChan:
		return err
	}
}
