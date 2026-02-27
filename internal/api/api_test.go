package api_test

import (
	"context"
	"errors"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tinywideclouds/go-llm-client/internal/api"
	"github.com/tinywideclouds/go-llm/pkg/builder/v1"

	"google.golang.org/genai"
)

// --- Mocks ---

type mockSessionService struct {
	acceptErr error
	session   *builder.Session
}

func (m *mockSessionService) GetSession(ctx context.Context, sessionID string) (*builder.Session, error) {
	if m.session != nil {
		return m.session, nil
	}
	return &builder.Session{}, nil
}

func (m *mockSessionService) AcceptProposal(ctx context.Context, sessionID, proposalID string) error {
	return m.acceptErr
}

func (m *mockSessionService) RejectProposal(ctx context.Context, sessionID, proposalID string) error {
	return nil
}

func (m *mockSessionService) SaveCompiledCache(ctx context.Context, firestoreCacheID string, cache *builder.CompiledCache) error {
	return nil
}

type mockLLMManager struct {
	GenerateStreamResponseFunc func(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID string) iter.Seq2[*genai.GenerateContentResponse, error]
	CreateRepositoryCacheFunc  func(ctx context.Context, model string, files map[string]string, ttl time.Duration, explicitInstructions map[string]string) (string, error)
}

func (m *mockLLMManager) CreateRepositoryCache(ctx context.Context, model string, files map[string]string, ttl time.Duration, explicitInstructions map[string]string) (string, error) {
	if m.CreateRepositoryCacheFunc != nil {
		return m.CreateRepositoryCacheFunc(ctx, model, files, ttl, explicitInstructions)
	}
	return "mock-cache-id", nil
}

func (m *mockLLMManager) GenerateResponse(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID string) (*genai.GenerateContentResponse, error) {
	return &genai.GenerateContentResponse{}, nil
}

func (m *mockLLMManager) GenerateStreamResponse(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID string) iter.Seq2[*genai.GenerateContentResponse, error] {
	if m.GenerateStreamResponseFunc != nil {
		return m.GenerateStreamResponseFunc(ctx, model, overlayPrompt, history, cacheID)
	}
	return func(yield func(*genai.GenerateContentResponse, error) bool) {}
}

type mockFetcher struct {
	files map[string]string
}

func (m *mockFetcher) FetchCacheFiles(ctx context.Context, cacheID, profileID string) (map[string]string, error) {
	return m.files, nil
}
func (m *mockFetcher) Close() error { return nil }

// --- Setup ---

func setupAPI() (*api.API, *mockSessionService, *mockFetcher, *mockLLMManager) {
	sessionSvc := &mockSessionService{}
	llmMgr := &mockLLMManager{}
	fetcher := &mockFetcher{files: make(map[string]string)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	apiHandler := &api.API{
		SessionService: sessionSvc, // Injected Domain Service
		LLM:            llmMgr,
		Fetcher:        fetcher,
		Logger:         logger,
	}

	return apiHandler, sessionSvc, fetcher, llmMgr
}

// --- Tests ---

func TestAcceptProposalHandler(t *testing.T) {
	apiHandler, sessionSvc, _, _ := setupAPI()

	// Ensure the domain service simulates a successful accept
	sessionSvc.acceptErr = nil

	req := httptest.NewRequest(http.MethodPost, "/v1/session/sess-1/proposals/prop-1/accept", nil)
	req.SetPathValue("id", "sess-1")
	req.SetPathValue("propId", "prop-1")
	w := httptest.NewRecorder()

	apiHandler.AcceptProposalHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}

func TestAcceptProposalHandler_Error(t *testing.T) {
	apiHandler, sessionSvc, _, _ := setupAPI()

	// Simulate business logic rejecting the request (e.g. proposal already accepted)
	sessionSvc.acceptErr = errors.New("proposal is no longer pending")

	req := httptest.NewRequest(http.MethodPost, "/v1/session/sess-1/proposals/prop-1/accept", nil)
	req.SetPathValue("id", "sess-1")
	req.SetPathValue("propId", "prop-1")
	w := httptest.NewRecorder()

	apiHandler.AcceptProposalHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, res.StatusCode)
	}
}
