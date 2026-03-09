package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"

	"github.com/tinywideclouds/go-llm-client/internal/service"
	"github.com/tinywideclouds/go-llm-client/internal/session"
	"github.com/tinywideclouds/go-llm-client/internal/store"
	"github.com/tinywideclouds/go-microservice-base/pkg/response"
	"google.golang.org/genai"
)

const maxTurns = 4

// API holds the injected domain managers and handles HTTP request parsing.
type API struct {
	SessionService session.Service
	LLM            service.LLMManager
	Fetcher        store.Fetcher
	CachePolicy    *CachePolicy
	Logger         *slog.Logger
}

// BuildCompiledCacheHandler handles POST /v1/llm/compiled_cache/build
func (a *API) BuildCompiledCacheHandler(w http.ResponseWriter, r *http.Request) {
	var req builder.BuildCacheRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body or malformed URNs")
		return
	}

	if len(req.Attachments) == 0 {
		response.WriteJSONError(w, http.StatusBadRequest, "At least one attachment is required to build a cache")
		return
	}

	allFiles := make(map[string]string)
	for _, att := range req.Attachments {
		// No need to check for empty CacheID anymore, JSON unmarshaling guarantees valid URNs

		files, err := a.Fetcher.FetchCacheFiles(r.Context(), att.CacheID, att.ProfileID)
		if err != nil {
			a.Logger.Error("Failed to fetch cache files", "cacheId", att.CacheID.String(), "error", err)
			response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve context data from database")
			return
		}
		for k, v := range files {
			allFiles[k] = v
		}
	}

	now := time.Now()
	expires := now.Add(time.Hour)
	reason := "default"

	if a.CachePolicy != nil {
		hint := ""
		if req.ExpiresAtHint != nil {
			hint = req.ExpiresAtHint.Format(time.RFC3339)
		}
		expires, reason = a.CachePolicy.CalculatePolicyTTL(hint)
	}

	a.Logger.Debug("cache", "expires", expires, "reason", reason)

	ttl := expires.Sub(now)

	// Pass the dynamic TTL to your manager, returns strict URN
	compiledCacheURN, err := a.LLM.CreateRepositoryCache(r.Context(), req.Model, allFiles, ttl, nil)

	if err != nil {
		a.Logger.Error("Failed to build Gemini cache", "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to build context cache on Google servers")
		return
	}

	baseCacheID := req.Attachments[0].CacheID

	// ExternalID is gone. We assign the returned cache URN directly to ID.
	compiledCache := &builder.CompiledCache{
		ID:              compiledCacheURN,
		Provider:        "gemini",
		AttachmentsUsed: req.Attachments,
		CreatedAt:       time.Now(),
		ExpiresAt:       expires,
	}

	if err := a.SessionService.SaveCompiledCache(r.Context(), baseCacheID, compiledCache); err != nil {
		a.Logger.Error("Failed to save compiled cache metadata", "error", err)
	}

	cacheResponse := &builder.BuildCacheResponse{
		CompiledCacheId: compiledCacheURN,
		ExpiresAt:       expires,
	}

	response.WriteJSON(w, http.StatusCreated, cacheResponse)
}

// GenerateHandler handles POST /v1/llm/generate
func (a *API) GenerateHandler(w http.ResponseWriter, r *http.Request) {
	response.WriteJSONError(w, http.StatusNotImplemented, "Generate not implemented, please use GenerateStream")
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
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", "Invalid JSON body or malformed URNs")
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

	pendingProposals, err := a.SessionService.ListProposalsBySession(r.Context(), req.SessionID)
	if err != nil {
		a.Logger.Warn("Failed to retrieve pending proposals for context", "error", err)
	}

	var extraHistory []*genai.Content

	a.Logger.Info("Starting to process LLM stream", "session_id", req.SessionID.String())

	for turn := 0; turn < maxTurns; turn++ {
		a.Logger.Debug("Starting generation loop turn", "turn", turn)

		// Use CompiledCacheID from the request struct
		stream := a.LLM.GenerateStreamResponse(r.Context(), model, BuildOverlayPrompt(pendingProposals), req.History, req.CompiledCacheID, extraHistory)

		var turnModelParts []*genai.Part
		var turnToolResponses []*genai.Part
		madeToolCall := false

		for chunk, err := range stream {
			if err != nil {
				a.Logger.Error("Stream error", "error", err)
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", "Stream interrupted")
				flusher.Flush()
				return
			}

			if len(chunk.Candidates) > 0 && chunk.Candidates[0].Content != nil {
				turnModelParts = append(turnModelParts, chunk.Candidates[0].Content.Parts...)
			}

			props, toolErr := a.LLM.InterceptToolCalls(r.Context(), chunk, req.SessionID)
			if toolErr != nil {
				a.Logger.Error("Tool processing failed", "error", toolErr)
			}

			if len(props) > 0 {
				madeToolCall = true
				for _, prop := range props {
					if err := a.SessionService.CreateProposal(r.Context(), prop); err != nil {
						a.Logger.Error("Failed to save pending proposal", "error", err)
					}

					eventData := map[string]interface{}{"proposal": prop}
					eventBytes, _ := json.Marshal(eventData)
					a.Logger.Info("Emitting proposal_created SSE event", "file_path", prop.FilePath)
					fmt.Fprintf(w, "event: proposal_created\ndata: %s\n\n", string(eventBytes))
					flusher.Flush()

					turnToolResponses = append(turnToolResponses, &genai.Part{
						FunctionResponse: &genai.FunctionResponse{
							Name:     "propose_change",
							Response: map[string]any{"status": "added_to_queue_awaiting_user_review"},
						},
					})
				}
			}

			hasText := false
			if len(chunk.Candidates) > 0 && chunk.Candidates[0].Content != nil {
				for _, part := range chunk.Candidates[0].Content.Parts {
					if part.Text != "" {
						hasText = true
						break
					}
				}
			}

			if !hasText {
				continue
			}

			chunkBytes, err := json.Marshal(chunk)
			if err != nil {
				a.Logger.Error("Failed to marshal chunk", "error", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
			flusher.Flush()
		}

		if !madeToolCall {
			break
		}

		a.Logger.Info("Tool calls detected, looping to satisfy sequential generation dependencies")
		extraHistory = append(extraHistory, &genai.Content{
			Role:  "model",
			Parts: turnModelParts,
		})
		extraHistory = append(extraHistory, &genai.Content{
			Role:  "user",
			Parts: turnToolResponses,
		})
	}

	a.Logger.Info("LLM stream sequence completed successfully", "session_id", req.SessionID.String())
	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

// ListProposalsHandler handles GET /v1/llm/session/{id}/proposals
func (a *API) ListProposalsHandler(w http.ResponseWriter, r *http.Request) {
	sessionID, err := urn.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid session ID format")
		return
	}

	proposals, err := a.SessionService.ListProposalsBySession(r.Context(), sessionID)
	if err != nil {
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve proposals")
		return
	}

	response.WriteJSON(w, http.StatusOK, proposals)
}

// RemoveProposalHandler handles DELETE /v1/llm/session/{id}/proposals/{propId}
func (a *API) RemoveProposalHandler(w http.ResponseWriter, r *http.Request) {
	sessionID, err := urn.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid session ID format")
		return
	}

	err = a.SessionService.RemoveProposal(r.Context(), sessionID, r.PathValue("propId"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
