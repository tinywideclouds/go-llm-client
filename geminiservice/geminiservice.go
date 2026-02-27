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
	sessionStore store.SessionStore, // <-- Injected Session Store
	authMiddleware func(http.Handler) http.Handler,
	logger *slog.Logger,
) *Wrapper {
	// 1. Create the standard base server using the config port.
	baseServer := microservice.NewBaseServer(logger, cfg.HTTPListenAddr)

	// 2. Initialize Domain Services.
	sessionService := session.NewService(sessionStore) // <-- Wire the store to the stateless service
	managerLogger := logger.With("component", "LLMManager")
	llmManager := llmservice.NewLLMManager(client, managerLogger)

	// 3. Create the service-specific API handlers.
	apiHandler := &api.API{
		SessionService: sessionService, // <-- Inject domain service into the API
		LLM:            llmManager,
		Fetcher:        fetcher,
		Logger:         logger,
	}

	// 4. Get the mux and create CORS middleware.
	mux := baseServer.Mux()
	corsMiddleware := middleware.NewCorsMiddleware(cfg.CorsConfig, logger)
	optionsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	// 5. Register OPTIONS for CORS pre-flight across the routes
	mux.Handle("OPTIONS /v1/cache/build", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/generate", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/generate-stream", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/session/{id}/proposals", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/session/{id}/diffs", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/session/{id}/proposals/{propId}/accept", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/session/{id}/proposals/{propId}/reject", corsMiddleware(optionsHandler))

	// 6. Register API Routes with CORS and Auth
	buildCacheHandler := http.HandlerFunc(apiHandler.BuildCacheHandler)
	mux.Handle("POST /v1/cache/build", corsMiddleware(authMiddleware(buildCacheHandler)))

	generateHandler := http.HandlerFunc(apiHandler.GenerateHandler)
	mux.Handle("POST /v1/generate", corsMiddleware(authMiddleware(generateHandler)))

	generateStreamHandler := http.HandlerFunc(apiHandler.GenerateStreamHandler)
	mux.Handle("POST /v1/generate-stream", corsMiddleware(authMiddleware(generateStreamHandler)))

	listProposalsHandler := http.HandlerFunc(apiHandler.ListProposalsHandler)
	mux.Handle("GET /v1/session/{id}/proposals", corsMiddleware(authMiddleware(listProposalsHandler)))

	getDiffsHandler := http.HandlerFunc(apiHandler.GetDiffsHandler)
	mux.Handle("GET /v1/session/{id}/diffs", corsMiddleware(authMiddleware(getDiffsHandler)))

	acceptProposalHandler := http.HandlerFunc(apiHandler.AcceptProposalHandler)
	mux.Handle("POST /v1/session/{id}/proposals/{propId}/accept", corsMiddleware(authMiddleware(acceptProposalHandler)))

	rejectProposalHandler := http.HandlerFunc(apiHandler.RejectProposalHandler)
	mux.Handle("POST /v1/session/{id}/proposals/{propId}/reject", corsMiddleware(authMiddleware(rejectProposalHandler)))

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
