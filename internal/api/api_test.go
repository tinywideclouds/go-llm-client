package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tinywideclouds/go-llm-client/internal/service"
	"github.com/tinywideclouds/go-llm-client/internal/session"
	"google.golang.org/genai"
)

type mockLLMManager struct {
	GenerateResponseFunc       func(ctx context.Context, model string, overlayPrompt string, history []service.Message, cacheID string) (*genai.GenerateContentResponse, error)
	GenerateStreamResponseFunc func(ctx context.Context, model string, overlayPrompt string, history []service.Message, cacheID string) iter.Seq2[*genai.GenerateContentResponse, error]
}

func (m *mockLLMManager) CreateRepositoryCache(ctx context.Context, model string, files map[string]string, ttl time.Duration, explicitInstructions map[string]string) (string, error) {
	return "mock-cache", nil
}

func (m *mockLLMManager) GenerateResponse(ctx context.Context, model string, overlayPrompt string, history []service.Message, cacheID string) (*genai.GenerateContentResponse, error) {
	if m.GenerateResponseFunc != nil {
		return m.GenerateResponseFunc(ctx, model, overlayPrompt, history, cacheID)
	}
	return &genai.GenerateContentResponse{}, nil
}

func (m *mockLLMManager) GenerateStreamResponse(ctx context.Context, model string, overlayPrompt string, history []service.Message, cacheID string) iter.Seq2[*genai.GenerateContentResponse, error] {
	if m.GenerateStreamResponseFunc != nil {
		return m.GenerateStreamResponseFunc(ctx, model, overlayPrompt, history, cacheID)
	}
	return func(yield func(*genai.GenerateContentResponse, error) bool) {}
}

func setupAPI() (*API, *session.Manager) {
	sessMgr := session.NewManager()
	llmMgr := &mockLLMManager{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &API{
		Sessions: sessMgr,
		LLM:      llmMgr,
		Logger:   logger,
	}, sessMgr
}

func TestGenerateHandler_ValidRequest(t *testing.T) {
	api, _ := setupAPI()

	reqBody := `{"session_id": "sess-1", "history": [{"id": "msg-1", "role": "user", "content": "test prompt", "timestamp": "2023-10-10T10:10:10Z"}], "cache_id": "cache-1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/generate", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	api.GenerateHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}

func TestGenerateHandler_EmptyHistory(t *testing.T) {
	api, _ := setupAPI()

	reqBody := `{"session_id": "sess-1", "history": [], "cache_id": "cache-1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/generate", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	api.GenerateHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, res.StatusCode)
	}
}

func TestGenerateHandler_InvalidJSON(t *testing.T) {
	api, _ := setupAPI()

	reqBody := `{bad_json`
	req := httptest.NewRequest(http.MethodPost, "/v1/generate", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	api.GenerateHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, res.StatusCode)
	}
}

// --- STREAMING TESTS ---

func TestGenerateStreamHandler_ValidRequest(t *testing.T) {
	api, _ := setupAPI()

	mockLLM := api.LLM.(*mockLLMManager)
	mockLLM.GenerateStreamResponseFunc = func(ctx context.Context, model string, overlayPrompt string, history []service.Message, cacheID string) iter.Seq2[*genai.GenerateContentResponse, error] {
		return func(yield func(*genai.GenerateContentResponse, error) bool) {
			if !yield(&genai.GenerateContentResponse{}, nil) {
				return
			}
			yield(&genai.GenerateContentResponse{}, nil)
		}
	}

	reqBody := `{"session_id": "sess-1", "history": [{"id": "msg-1", "role": "user", "content": "stream test", "timestamp": "2023-10-10T10:10:10Z"}], "cache_id": "cache-1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/generate-stream", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	api.GenerateStreamHandler(w, req)
	res := w.Result()

	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", res.Header.Get("Content-Type"))
	}
	if res.Header.Get("Connection") != "keep-alive" {
		t.Errorf("Expected Connection 'keep-alive', got '%s'", res.Header.Get("Connection"))
	}

	body, _ := io.ReadAll(res.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "data: {") {
		t.Errorf("Expected stream to output 'data: {...}' payload, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "event: done\ndata: {}\n\n") {
		t.Errorf("Expected stream to terminate cleanly with the 'done' event, got: %s", bodyStr)
	}
}

func TestGenerateStreamHandler_InvalidJSON(t *testing.T) {
	api, _ := setupAPI()

	reqBody := `{bad_json`
	req := httptest.NewRequest(http.MethodPost, "/v1/generate-stream", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	api.GenerateStreamHandler(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "event: error\ndata: Invalid JSON body\n\n") {
		t.Errorf("Expected an SSE error event for invalid JSON, got: %s", bodyStr)
	}
}

func TestGenerateStreamHandler_EmptyHistory(t *testing.T) {
	api, _ := setupAPI()

	reqBody := `{"session_id": "sess-1", "history": [], "cache_id": "cache-1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/generate-stream", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	api.GenerateStreamHandler(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "event: error\ndata: History cannot be empty\n\n") {
		t.Errorf("Expected an SSE error event for empty history, got: %s", bodyStr)
	}
}

func TestGenerateStreamHandler_StreamError(t *testing.T) {
	api, _ := setupAPI()

	mockLLM := api.LLM.(*mockLLMManager)
	mockLLM.GenerateStreamResponseFunc = func(ctx context.Context, model string, overlayPrompt string, history []service.Message, cacheID string) iter.Seq2[*genai.GenerateContentResponse, error] {
		return func(yield func(*genai.GenerateContentResponse, error) bool) {
			yield(nil, errors.New("fake stream error"))
		}
	}

	reqBody := `{"session_id": "sess-1", "history": [{"id": "msg-1", "role": "user", "content": "stream test", "timestamp": "2023-10-10T10:10:10Z"}], "cache_id": "cache-1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/generate-stream", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	api.GenerateStreamHandler(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "event: error\ndata: Stream interrupted\n\n") {
		t.Errorf("Expected an SSE error event for stream interruption, got: %s", bodyStr)
	}
}

// --- STANDARD TESTS ---

func TestListProposalsHandler(t *testing.T) {
	api, sessMgr := setupAPI()
	sess := sessMgr.GetOrCreateSession("sess-1", "cache-1")
	sess.ProposeChange("file.go", "new content", "reason")

	req := httptest.NewRequest(http.MethodGet, "/v1/session/sess-1/proposals", nil)
	req.SetPathValue("id", "sess-1")
	w := httptest.NewRecorder()

	api.ListProposalsHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, res.StatusCode)
	}

	var proposals []session.ChangeProposal
	if err := json.NewDecoder(res.Body).Decode(&proposals); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if len(proposals) != 1 {
		t.Errorf("Expected 1 proposal, got %d", len(proposals))
	}
}

func TestGetDiffsHandler(t *testing.T) {
	api, sessMgr := setupAPI()
	sess := sessMgr.GetOrCreateSession("sess-1", "cache-1")
	propID := sess.ProposeChange("file.go", "new content", "reason")
	sess.AcceptChange(propID)

	req := httptest.NewRequest(http.MethodGet, "/v1/session/sess-1/diffs", nil)
	req.SetPathValue("id", "sess-1")
	w := httptest.NewRecorder()

	api.GetDiffsHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, res.StatusCode)
	}

	var diffs map[string]session.FileState
	if err := json.NewDecoder(res.Body).Decode(&diffs); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if _, ok := diffs["file.go"]; !ok {
		t.Error("Expected file.go in diffs")
	}
}

func TestAcceptProposalHandler(t *testing.T) {
	api, sessMgr := setupAPI()
	sess := sessMgr.GetOrCreateSession("sess-1", "cache-1")
	propID := sess.ProposeChange("file.go", "new content", "reason")

	req := httptest.NewRequest(http.MethodPost, "/v1/session/sess-1/proposals/"+propID+"/accept", nil)
	req.SetPathValue("id", "sess-1")
	req.SetPathValue("propId", propID)
	w := httptest.NewRecorder()

	api.AcceptProposalHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}

func TestAcceptProposalHandler_NotFound(t *testing.T) {
	api, _ := setupAPI()

	req := httptest.NewRequest(http.MethodPost, "/v1/session/sess-1/proposals/invalid-id/accept", nil)
	req.SetPathValue("id", "sess-1")
	req.SetPathValue("propId", "invalid-id")
	w := httptest.NewRecorder()

	api.AcceptProposalHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, res.StatusCode)
	}
}

func TestRejectProposalHandler(t *testing.T) {
	api, sessMgr := setupAPI()
	sess := sessMgr.GetOrCreateSession("sess-1", "cache-1")
	propID := sess.ProposeChange("file.go", "new content", "reason")

	req := httptest.NewRequest(http.MethodPost, "/v1/session/sess-1/proposals/"+propID+"/reject", nil)
	req.SetPathValue("id", "sess-1")
	req.SetPathValue("propId", propID)
	w := httptest.NewRecorder()

	api.RejectProposalHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, res.StatusCode)
	}
}
