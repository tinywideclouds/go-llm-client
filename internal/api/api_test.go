package api_test

import (
	"context"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tinywideclouds/go-llm-client/geminiservice/config"
	"github.com/tinywideclouds/go-llm-client/internal/api"
	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"

	"google.golang.org/genai"
)

// --- Mocks ---

type mockSessionService struct {
	acceptErr error
	session   *builder.Session
	proposals []builder.ChangeProposal
}

func (m *mockSessionService) GetSession(ctx context.Context, sessionID urn.URN) (*builder.Session, error) {
	if m.session != nil {
		return m.session, nil
	}
	return &builder.Session{}, nil
}

func (m *mockSessionService) RemoveProposal(ctx context.Context, sessionID urn.URN, proposalID string) error {
	return m.acceptErr
}

func (m *mockSessionService) ListProposalsBySession(ctx context.Context, sessionID urn.URN) ([]builder.ChangeProposal, error) {
	return m.proposals, nil
}

func (m *mockSessionService) CreateProposal(ctx context.Context, proposal *builder.ChangeProposal) error {
	return nil
}

func (m *mockSessionService) SaveCompiledCache(ctx context.Context, firestoreCacheID urn.URN, cache *builder.CompiledCache) error {
	return nil
}

type mockLLMManager struct {
	CreateRepositoryCacheFunc  func(ctx context.Context, model string, files map[string]string, ttl time.Duration, explicitInstructions map[string]string) (urn.URN, error)
	GenerateStreamResponseFunc func(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID *urn.URN, extraHistory []*genai.Content, thinkingLevel *builder.ThinkingLevel) iter.Seq2[*genai.GenerateContentResponse, error]
	InterceptToolCallsFunc     func(ctx context.Context, chunk *genai.GenerateContentResponse, sessionID urn.URN) ([]*builder.ChangeProposal, error)
	// NEW
	GenerateResponseFunc func(ctx context.Context, model string, systemPrompt string, prompt string) (*builder.GenerateResponse, error)
}

func (m *mockLLMManager) CreateRepositoryCache(ctx context.Context, model string, files map[string]string, ttl time.Duration, explicitInstructions map[string]string) (urn.URN, error) {
	if m.CreateRepositoryCacheFunc != nil {
		return m.CreateRepositoryCacheFunc(ctx, model, files, ttl, explicitInstructions)
	}
	return mustURN("urn:llm:compiled-cache:gemini-123"), nil
}

func (m *mockLLMManager) GenerateStreamResponse(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID *urn.URN, extraHistory []*genai.Content, thinkingLevel *builder.ThinkingLevel) iter.Seq2[*genai.GenerateContentResponse, error] {
	if m.GenerateStreamResponseFunc != nil {
		return m.GenerateStreamResponseFunc(ctx, model, overlayPrompt, history, cacheID, extraHistory, thinkingLevel)
	}
	return func(yield func(*genai.GenerateContentResponse, error) bool) {}
}

func (m *mockLLMManager) InterceptToolCalls(ctx context.Context, chunk *genai.GenerateContentResponse, sessionID urn.URN) ([]*builder.ChangeProposal, error) {
	if m.InterceptToolCallsFunc != nil {
		return m.InterceptToolCallsFunc(ctx, chunk, sessionID)
	}
	return nil, nil
}

// NEW
func (m *mockLLMManager) GenerateResponse(ctx context.Context, model string, systemPrompt string, prompt string) (*builder.GenerateResponse, error) {
	if m.GenerateResponseFunc != nil {
		return m.GenerateResponseFunc(ctx, model, systemPrompt, prompt)
	}
	return &builder.GenerateResponse{Content: "default response"}, nil
}

func setupAPI() (*api.API, *mockSessionService, *mockFetcher, *mockLLMManager) {
	sessionSvc := &mockSessionService{}
	fetcher := &mockFetcher{}
	llmMgr := &mockLLMManager{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	apiHandler := &api.API{
		SessionService: sessionSvc,
		LLM:            llmMgr,
		Fetcher:        fetcher,
		CachePolicy:    api.NewCachePolicy(),
		Timeouts: config.TimeoutsConfig{
			DatabaseIO:  10 * time.Second,
			CacheBuild:  120 * time.Second,
			LLMGenerate: 60 * time.Second,
			LLMStream:   5 * time.Minute,
		},
		Logger: logger,
	}

	return apiHandler, sessionSvc, fetcher, llmMgr
}

// --- Tests ---

func TestGenerateHandler_Success(t *testing.T) {
	apiHandler, _, _, llmMgr := setupAPI()

	llmMgr.GenerateResponseFunc = func(ctx context.Context, model string, systemPrompt string, prompt string) (*builder.GenerateResponse, error) {
		return &builder.GenerateResponse{
			Content:             "Here is the digest.",
			FinishReason:        "STOP",
			PromptTokenCount:    100,
			CandidateTokenCount: 20,
		}, nil
	}

	reqBody := `{"model": "gemini-3.1-pro", "systemPrompt": "Summarize.", "prompt": "text to summarize"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/llm/generate", strings.NewReader(reqBody))

	w := httptest.NewRecorder()
	apiHandler.GenerateHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", res.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(res.Body)
	bodyStr := string(bodyBytes)

	// Validate protojson camelCase serialization works correctly
	if !strings.Contains(bodyStr, `"content":"Here is the digest."`) {
		t.Errorf("Missing content in response: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"promptTokenCount":100`) {
		t.Errorf("Missing token metric in response: %s", bodyStr)
	}
}

func TestGenerateStreamHandler_ToolInterception(t *testing.T) {
	apiHandler, _, _, llmMgr := setupAPI()

	llmMgr.InterceptToolCallsFunc = func(ctx context.Context, chunk *genai.GenerateContentResponse, sessionID urn.URN) ([]*builder.ChangeProposal, error) {
		prop := &builder.ChangeProposal{
			ID:        "prop-123",
			SessionID: sessionID,
			FilePath:  "test.go",
			Patch:     "@@ diff @@",
		}
		return []*builder.ChangeProposal{prop}, nil
	}

	llmMgr.GenerateStreamResponseFunc = func(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID *urn.URN, extraHistory []*genai.Content, thinkingLevel *builder.ThinkingLevel) iter.Seq2[*genai.GenerateContentResponse, error] {
		return func(yield func(*genai.GenerateContentResponse, error) bool) {
			dummyChunk := &genai.GenerateContentResponse{}
			yield(dummyChunk, nil)
		}
	}

	// Use valid strict URN formatting for the request body
	reqBody := `{"sessionId": "urn:llm:session:sess-1", "history": [{"role": "user", "content": "Update the file"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/llm/generate-stream", strings.NewReader(reqBody))

	w := httptest.NewRecorder()
	apiHandler.GenerateStreamHandler(w, req)

	res := w.Result()
	bodyBytes, _ := io.ReadAll(res.Body)
	bodyStr := string(bodyBytes)

	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected Content-Type text/event-stream, got %s", res.Header.Get("Content-Type"))
	}

	if !strings.Contains(bodyStr, "event: proposal_created") {
		t.Error("Expected 'proposal_created' event in stream output")
	}

	if !strings.Contains(bodyStr, `"patch":"@@ diff @@"`) {
		t.Error("Expected patch in SSE payload")
	}
}
