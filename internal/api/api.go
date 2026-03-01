package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/tinywideclouds/go-llm/pkg/builder/v1"

	"github.com/tinywideclouds/go-llm-client/internal/service"
	"github.com/tinywideclouds/go-llm-client/internal/session"
	"github.com/tinywideclouds/go-llm-client/internal/store"
	"github.com/tinywideclouds/go-microservice-base/pkg/response"
)

// API holds the injected domain managers and handles HTTP request parsing.
type API struct {
	SessionService session.Service
	LLM            service.LLMManager
	Fetcher        store.Fetcher
	Logger         *slog.Logger
}

// BuildCompiledCacheHandler handles POST /v1/llm/compiled_cache/build
func (a *API) BuildCompiledCacheHandler(w http.ResponseWriter, r *http.Request) {
	var req builder.BuildCacheRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if len(req.Attachments) == 0 {
		response.WriteJSONError(w, http.StatusBadRequest, "At least one attachment is required to build a cache")
		return
	}

	// 1. Fetch all requested files from Firestore (Original correct implementation)
	allFiles := make(map[string]string)
	for _, att := range req.Attachments {
		if att.CacheID == "" {
			a.Logger.Warn("Received attachment with missing CacheID")
			response.WriteJSONError(w, http.StatusBadRequest, "Invalid attachment: missing cacheId")
			return
		}

		files, err := a.Fetcher.FetchCacheFiles(r.Context(), att.CacheID, att.ProfileID)
		if err != nil {
			a.Logger.Error("Failed to fetch cache files", "cacheId", att.CacheID, "error", err)
			response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve context data from database")
			return
		}
		// Merge maps
		for k, v := range files {
			allFiles[k] = v
		}
	}

	// 2. Upload to Google Gemini to build the cached context
	compiledCacheName, err := a.LLM.CreateRepositoryCache(r.Context(), req.Model, allFiles, 1*time.Hour, nil)
	if err != nil {
		a.Logger.Error("Failed to build Gemini cache", "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to build context cache on Google servers")
		return
	}

	// 3. Save the CompiledCache metadata to Firestore
	baseCacheID := req.Attachments[0].CacheID
	compiledCache := &builder.CompiledCache{
		ID:              fmt.Sprintf("cc-%d", time.Now().UnixNano()),
		ExternalID:      compiledCacheName,
		Provider:        "gemini",
		AttachmentsUsed: req.Attachments,
		CreatedAt:       time.Now(),
	}

	if err := a.SessionService.SaveCompiledCache(r.Context(), baseCacheID, compiledCache); err != nil {
		a.Logger.Error("Failed to save compiled cache metadata", "error", err)
	}

	cacheResponse := &builder.BuildCacheResponse{
		GeminiCacheId: compiledCacheName,
	}

	response.WriteJSON(w, http.StatusCreated, cacheResponse)
}

// GenerateStreamHandler handles POST /v1/llm/generate-stream
func (a *API) GenerateStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported by server", http.StatusInternalServerError)
		return
	}

	var req builder.GenerateStreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", "Invalid JSON body")
		flusher.Flush()
		return
	}

	if len(req.History) == 0 {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", "History cannot be empty")
		flusher.Flush()
		return
	}

	model := req.Model
	if model == "" {
		model = "gemini-3-flash-preview"
	}

	// 1. Build Inline Context (Read-Only Fetcher usage)
	inlineContext := BuildInlineContext(r.Context(), a.Fetcher, req.InlineAttachments, a.Logger)
	req.History = InjectInlineContext(req.History, inlineContext)

	// 2. Fetch the Pending Ephemeral Queue to remind the LLM what it already proposed
	pendingProposals, err := a.SessionService.ListProposalsBySession(r.Context(), req.SessionID)
	if err != nil {
		a.Logger.Warn("Failed to retrieve pending proposals for context", "error", err)
	}

	// 3. Stream Response
	stream := a.LLM.GenerateStreamResponse(r.Context(), model, BuildOverlayPrompt(pendingProposals), req.History, req.GeminiCacheID)

	for chunk, err := range stream {
		if err != nil {
			a.Logger.Error("Stream error", "error", err)
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", "Stream interrupted")
			flusher.Flush()
			return
		}

		// 4. Pure Stateless Tool Interception
		prop, isTool, toolErr := a.LLM.InterceptToolCall(r.Context(), chunk, req.SessionID)
		if toolErr != nil {
			a.Logger.Error("Tool processing failed", "error", toolErr)
		}

		if isTool {
			// 5. Add to the ephemeral queue
			if err := a.SessionService.CreateProposal(r.Context(), prop); err != nil {
				a.Logger.Error("Failed to save pending proposal", "error", err)
			}

			// 6. Emit the lightweight SSE
			eventData := map[string]interface{}{
				"proposal": prop,
			}
			eventBytes, _ := json.Marshal(eventData)
			fmt.Fprintf(w, "event: proposal_created\ndata: %s\n\n", string(eventBytes))
			flusher.Flush()
			continue
		}

		// 7. Standard Text Streaming
		chunkBytes, err := json.Marshal(chunk)
		if err != nil {
			a.Logger.Error("Failed to marshal chunk", "error", err)
			continue
		}

		fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
		flusher.Flush()
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

// ListProposalsHandler handles GET /v1/llm/session/{id}/proposals
func (a *API) ListProposalsHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	proposals, err := a.SessionService.ListProposalsBySession(r.Context(), sessionID)
	if err != nil {
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve proposals")
		return
	}

	response.WriteJSON(w, http.StatusOK, proposals)
}

// RemoveProposalHandler handles DELETE /v1/llm/session/{id}/proposals/{propId}
func (a *API) RemoveProposalHandler(w http.ResponseWriter, r *http.Request) {
	err := a.SessionService.RemoveProposal(r.Context(), r.PathValue("id"), r.PathValue("propId"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
