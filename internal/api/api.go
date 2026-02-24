package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/tinywideclouds/go-llm-client/internal/service"
	"github.com/tinywideclouds/go-llm-client/internal/session"
	"github.com/tinywideclouds/go-microservice-base/pkg/response"
)

// API holds the injected domain managers and handles HTTP request parsing.
type API struct {
	Sessions *session.Manager
	LLM      service.LLMManager
	Logger   *slog.Logger
}

// GenerateHandler handles the POST /v1/generate request.
func (a *API) GenerateHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string            `json:"session_id"`
		History   []service.Message `json:"history"`
		CacheID   string            `json:"cache_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	// Basic validation to ensure we don't index out of bounds in the service layer
	if len(req.History) == 0 {
		response.WriteJSONError(w, http.StatusBadRequest, "History cannot be empty")
		return
	}

	sess := a.Sessions.GetOrCreateSession(req.SessionID, req.CacheID)

	overlayPrompt := ""
	sess.Mutex.RLock()
	if len(sess.AcceptedOverlays) > 0 {
		overlayPrompt += "\n<system_note>\nThe following files have unsaved changes:\n"
		for path, state := range sess.AcceptedOverlays {
			if !state.IsDeleted {
				overlayPrompt += fmt.Sprintf("<file path=%q>\n%s\n</file>\n", path, state.Content)
			}
		}
		overlayPrompt += "</system_note>\n"
	}
	sess.Mutex.RUnlock()

	resp, err := a.LLM.GenerateResponse(r.Context(), "gemini-3-flash-preview", overlayPrompt, req.History, req.CacheID)
	if err != nil {
		a.Logger.Error("Gemini request failed", "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "AI Provider Error")
		return
	}

	response.WriteJSON(w, http.StatusOK, resp)
}

func (a *API) GenerateStreamHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Establish SSE Headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported by server", http.StatusInternalServerError)
		return
	}

	var req struct {
		SessionID string            `json:"session_id"`
		History   []service.Message `json:"history"`
		CacheID   string            `json:"cache_id"`
	}

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

	llmSession := a.Sessions.GetOrCreateSession(req.SessionID, req.CacheID)

	overlayPrompt := ""
	llmSession.Mutex.RLock()
	if len(llmSession.AcceptedOverlays) > 0 {
		overlayPrompt += "\n<system_note>\nThe following files have unsaved changes:\n"
		for path, state := range llmSession.AcceptedOverlays {
			if !state.IsDeleted {
				overlayPrompt += fmt.Sprintf("<file path=%q>\n%s\n</file>\n", path, state.Content)
			}
		}
		overlayPrompt += "</system_note>\n"
	}
	llmSession.Mutex.RUnlock()

	a.Logger.Info("asking llm", "history_length", len(req.History))

	// 2. Start the stream
	stream := a.LLM.GenerateStreamResponse(r.Context(), "gemini-3-flash-preview", overlayPrompt, req.History, req.CacheID)

	// 3. Loop over the Go iterator
	for chunk, err := range stream {
		if err != nil {
			a.Logger.Error("Stream error", "error", err)
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", "Stream interrupted")
			flusher.Flush()
			return
		}

		// Marshal the full chunk so the client has access to Thoughts, Text, and Tool calls
		chunkBytes, err := json.Marshal(chunk)
		if err != nil {
			a.Logger.Error("Failed to marshal chunk", "error", err)
			continue
		}

		a.Logger.Debug("processed chunk", "bytes_length", len(chunkBytes))

		// Safely write the SSE payload to the HTTP ResponseWriter
		fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
		flusher.Flush()
	}

	// 4. Terminate the stream cleanly
	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
	a.Logger.Debug("processed all message chunks, sent done event")
}

// ListProposalsHandler handles GET /v1/session/{id}/proposals
func (a *API) ListProposalsHandler(w http.ResponseWriter, r *http.Request) {
	sess := a.Sessions.GetOrCreateSession(r.PathValue("id"), "")
	response.WriteJSON(w, http.StatusOK, sess.ListPendingProposals())
}

// GetDiffsHandler handles GET /v1/session/{id}/diffs
func (a *API) GetDiffsHandler(w http.ResponseWriter, r *http.Request) {
	sess := a.Sessions.GetOrCreateSession(r.PathValue("id"), "")

	sess.Mutex.RLock()
	defer sess.Mutex.RUnlock()
	response.WriteJSON(w, http.StatusOK, sess.AcceptedOverlays)
}

// AcceptProposalHandler handles POST /v1/session/{id}/proposals/{propId}/accept
func (a *API) AcceptProposalHandler(w http.ResponseWriter, r *http.Request) {
	sess := a.Sessions.GetOrCreateSession(r.PathValue("id"), "")
	if err := sess.AcceptChange(r.PathValue("propId")); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "merged"})
}

// RejectProposalHandler handles POST /v1/session/{id}/proposals/{propId}/reject
func (a *API) RejectProposalHandler(w http.ResponseWriter, r *http.Request) {
	sess := a.Sessions.GetOrCreateSession(r.PathValue("id"), "")
	if err := sess.RejectChange(r.PathValue("propId")); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}
