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

// BuildCacheHandler handles POST /v1/cache/build
func (a *API) BuildCacheHandler(w http.ResponseWriter, r *http.Request) {
	var req builder.BuildCacheRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if len(req.Attachments) == 0 {
		response.WriteJSONError(w, http.StatusBadRequest, "At least one attachment is required to build a cache")
		return
	}

	// 1. Fetch all requested files from Firestore
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
	cacheName, err := a.LLM.CreateRepositoryCache(r.Context(), req.Model, allFiles, 1*time.Hour, nil)
	if err != nil {
		a.Logger.Error("Failed to build Gemini cache", "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to build context cache on Google servers")
		return
	}

	// 3. Save the CompiledCache metadata to Firestore
	baseCacheID := req.Attachments[0].CacheID
	compiledCache := &builder.CompiledCache{
		ID:              fmt.Sprintf("cc-%d", time.Now().UnixNano()),
		ExternalID:      cacheName,
		Provider:        "gemini",
		AttachmentsUsed: req.Attachments,
		CreatedAt:       time.Now(),
	}

	// Delegated to domain service
	if err := a.SessionService.SaveCompiledCache(r.Context(), baseCacheID, compiledCache); err != nil {
		a.Logger.Error("Failed to save compiled cache metadata", "error", err)
	}

	cacheResponse := &builder.BuildCacheResponse{
		GeminiCacheId: cacheName,
	}

	response.WriteJSON(w, http.StatusCreated, cacheResponse)
}

// GenerateHandler handles the POST /v1/generate request.
func (a *API) GenerateHandler(w http.ResponseWriter, r *http.Request) {
	var req builder.GenerateStreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if len(req.History) == 0 {
		response.WriteJSONError(w, http.StatusBadRequest, "History cannot be empty")
		return
	}

	model := req.Model
	if model == "" {
		model = "gemini-3-flash-preview"
	}

	session, err := a.SessionService.GetSession(r.Context(), req.SessionID)
	if err != nil {
		a.Logger.Error("Failed to retrieve session", "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve session state")
		return
	}

	overlayPrompt := BuildOverlayPrompt(session.AcceptedOverlays)

	resp, err := a.LLM.GenerateResponse(r.Context(), model, overlayPrompt, req.History, req.GeminiCacheID)
	if err != nil {
		a.Logger.Error("Gemini request failed", "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "AI Provider Error")
		return
	}

	response.WriteJSON(w, http.StatusOK, resp)
}

// GenerateStreamHandler handles POST /v1/generate-stream
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

	inlineContext := BuildInlineContext(r.Context(), a.Fetcher, req.InlineAttachments, a.Logger)
	req.History = InjectInlineContext(req.History, inlineContext)

	sess, err := a.SessionService.GetSession(r.Context(), req.SessionID)
	if err != nil {
		a.Logger.Error("Failed to retrieve session", "error", err)
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", "Failed to retrieve session state")
		flusher.Flush()
		return
	}

	overlayPrompt := BuildOverlayPrompt(sess.AcceptedOverlays)

	stream := a.LLM.GenerateStreamResponse(r.Context(), model, overlayPrompt, req.History, req.GeminiCacheID)

	for chunk, err := range stream {
		if err != nil {
			a.Logger.Error("Stream error", "error", err)
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", "Stream interrupted")
			flusher.Flush()
			return
		}

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

// ListProposalsHandler handles GET /v1/session/{id}/proposals
func (a *API) ListProposalsHandler(w http.ResponseWriter, r *http.Request) {
	sess, err := a.SessionService.GetSession(r.Context(), r.PathValue("id"))
	if err != nil {
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve session state")
		return
	}
	response.WriteJSON(w, http.StatusOK, sess.PendingProposals)
}

// GetDiffsHandler handles GET /v1/session/{id}/diffs
func (a *API) GetDiffsHandler(w http.ResponseWriter, r *http.Request) {
	sess, err := a.SessionService.GetSession(r.Context(), r.PathValue("id"))
	if err != nil {
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve session state")
		return
	}
	response.WriteJSON(w, http.StatusOK, sess.AcceptedOverlays)
}

// AcceptProposalHandler handles POST /v1/session/{id}/proposals/{propId}/accept
func (a *API) AcceptProposalHandler(w http.ResponseWriter, r *http.Request) {
	err := a.SessionService.AcceptProposal(r.Context(), r.PathValue("id"), r.PathValue("propId"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "merged"})
}

// RejectProposalHandler handles POST /v1/session/{id}/proposals/{propId}/reject
func (a *API) RejectProposalHandler(w http.ResponseWriter, r *http.Request) {
	err := a.SessionService.RejectProposal(r.Context(), r.PathValue("id"), r.PathValue("propId"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}
