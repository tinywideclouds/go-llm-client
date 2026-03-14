package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"

	"github.com/tinywideclouds/go-llm-client/geminiservice/config"
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
	Timeouts       config.TimeoutsConfig
	Logger         *slog.Logger
}

// BuildCompiledCacheHandler handles POST /v1/llm/compiled_cache/build
func (a *API) BuildCompiledCacheHandler(w http.ResponseWriter, r *http.Request) {
	var req builder.BuildCacheRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body or malformed URNs")
		return
	}

	if len(req.Sources) == 0 {
		response.WriteJSONError(w, http.StatusBadRequest, "At least one attachment is required to build a cache")
		return
	}

	allFiles := make(map[string]string)

	// DB Phase - strict timeout for fetching from Firestore
	dbCtx, dbCancel := context.WithTimeout(r.Context(), a.Timeouts.DatabaseIO)
	defer dbCancel()

	for _, att := range req.Sources {
		files, err := a.Fetcher.FetchCacheFiles(dbCtx, att.DataSourceID, att.ProfileID)
		if err != nil {
			a.Logger.Error("Failed to fetch cache files", "DataSourceID", att.DataSourceID.String(), "error", err)

			if errors.Is(err, store.ErrContextTooLarge) {
				response.WriteJSONError(w, http.StatusRequestEntityTooLarge, "Context data exceeds the maximum allowed memory limit. Please apply a filter profile (e.g., exclude vendor/ or node_modules/) to reduce the repository size.")
				return
			}
			if errors.Is(dbCtx.Err(), context.DeadlineExceeded) {
				response.WriteJSONError(w, http.StatusGatewayTimeout, "Database fetch timed out")
				return
			}

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

	// Build Phase - longer timeout for Google API ingestion
	buildCtx, buildCancel := context.WithTimeout(r.Context(), a.Timeouts.CacheBuild)
	defer buildCancel()

	compiledCacheURN, err := a.LLM.CreateRepositoryCache(buildCtx, req.Model, allFiles, ttl, nil)
	if err != nil {
		a.Logger.Error("Failed to build Gemini cache", "error", err)
		if errors.Is(buildCtx.Err(), context.DeadlineExceeded) {
			response.WriteJSONError(w, http.StatusGatewayTimeout, "Gemini cache creation timed out")
			return
		}
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to build context cache on Google servers")
		return
	}

	baseCacheID := req.Sources[0].DataSourceID

	compiledCache := &builder.CompiledCache{
		ID:        compiledCacheURN,
		Provider:  "gemini",
		Sources:   req.Sources,
		CreatedAt: time.Now(),
		ExpiresAt: expires,
	}

	// Re-use DB context for saving metadata
	saveCtx, saveCancel := context.WithTimeout(r.Context(), a.Timeouts.DatabaseIO)
	defer saveCancel()

	if err := a.SessionService.SaveCompiledCache(saveCtx, baseCacheID, compiledCache); err != nil {
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
	var req builder.GenerateRequest // Uses the new, dedicated type
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if req.Prompt == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Prompt cannot be empty")
		return
	}

	model := req.Model
	if model == "" {
		model = "gemini-3.1-flash" // or default
	}

	genCtx, cancel := context.WithTimeout(r.Context(), a.Timeouts.LLMStream)
	defer cancel()

	genResponse, err := a.LLM.GenerateResponse(genCtx, model, req.SystemPrompt, req.Prompt)
	if err != nil {
		a.Logger.Error("Non-streaming generation failed", "error", err)
		if errors.Is(genCtx.Err(), context.DeadlineExceeded) {
			response.WriteJSONError(w, http.StatusGatewayTimeout, "LLM generation timed out")
			return
		}
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to generate response")
		return
	}

	response.WriteJSON(w, http.StatusOK, genResponse)
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

	// DB Phase - fetch context and proposals quickly
	dbCtx, dbCancel := context.WithTimeout(r.Context(), a.Timeouts.DatabaseIO)
	defer dbCancel()

	inlineContext := BuildInlineContext(dbCtx, a.Fetcher, req.InlineAttachments, a.Logger)
	req.History = InjectInlineContext(req.History, inlineContext)

	pendingProposals, err := a.SessionService.ListProposalsBySession(dbCtx, req.SessionID)
	if err != nil {
		a.Logger.Warn("Failed to retrieve pending proposals for context", "error", err)
	}

	var extraHistory []*genai.Content

	a.Logger.Info("Starting to process LLM stream", "session_id", req.SessionID.String())

	// Stream Phase - explicit overarching stream timeout
	streamCtx, streamCancel := context.WithTimeout(r.Context(), a.Timeouts.LLMStream)
	defer streamCancel()

	for turn := 0; turn < maxTurns; turn++ {
		a.Logger.Debug("Starting generation loop turn", "turn", turn)

		stream := a.LLM.GenerateStreamResponse(streamCtx, model, BuildOverlayPrompt(pendingProposals), req.History, req.CompiledCacheID, extraHistory, req.ThinkingLevel)

		// FIX: Use a string builder to consolidate fragmented stream text
		var turnTextBuilder strings.Builder
		var turnFunctionCalls []*genai.Part
		var turnToolResponses []*genai.Part
		madeToolCall := false

		for chunk, err := range stream {
			if err != nil {
				a.Logger.Error("Stream error", "error", err)

				errMsg := "Stream interrupted"
				if errors.Is(streamCtx.Err(), context.DeadlineExceeded) {
					errMsg = "LLM generation timed out"
				}

				fmt.Fprintf(w, "event: error\ndata: %s\n\n", errMsg)
				flusher.Flush()
				return
			}

			// FIX: Only keep explicitly valid data to prevent sending empty parts back to the API
			if len(chunk.Candidates) > 0 && chunk.Candidates[0].Content != nil {
				for _, part := range chunk.Candidates[0].Content.Parts {
					if part.FunctionCall != nil {
						turnFunctionCalls = append(turnFunctionCalls, part)
					} else if part.Text != "" {
						turnTextBuilder.WriteString(part.Text)
					}
				}
			}

			props, toolErr := a.LLM.InterceptToolCalls(streamCtx, chunk, req.SessionID)
			if toolErr != nil {
				a.Logger.Error("Tool processing failed", "error", toolErr)
			}

			if len(props) > 0 {
				madeToolCall = true

				saveCtx, saveCancel := context.WithTimeout(r.Context(), a.Timeouts.DatabaseIO)
				for _, prop := range props {
					if err := a.SessionService.CreateProposal(saveCtx, prop); err != nil {
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
				saveCancel()
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

		// FIX: Reconstruct a clean, valid `Content` payload for the next loop
		var finalTurnParts []*genai.Part
		if turnTextBuilder.Len() > 0 {
			finalTurnParts = append(finalTurnParts, &genai.Part{Text: turnTextBuilder.String()})
		}
		finalTurnParts = append(finalTurnParts, turnFunctionCalls...)

		extraHistory = append(extraHistory, &genai.Content{
			Role:  "model",
			Parts: finalTurnParts,
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

	dbCtx, dbCancel := context.WithTimeout(r.Context(), a.Timeouts.DatabaseIO)
	defer dbCancel()

	proposals, err := a.SessionService.ListProposalsBySession(dbCtx, sessionID)
	if err != nil {
		if errors.Is(dbCtx.Err(), context.DeadlineExceeded) {
			response.WriteJSONError(w, http.StatusGatewayTimeout, "Database fetch timed out")
			return
		}
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

	dbCtx, dbCancel := context.WithTimeout(r.Context(), a.Timeouts.DatabaseIO)
	defer dbCancel()

	err = a.SessionService.RemoveProposal(dbCtx, sessionID, r.PathValue("propId"))
	if err != nil {
		if errors.Is(dbCtx.Err(), context.DeadlineExceeded) {
			response.WriteJSONError(w, http.StatusGatewayTimeout, "Database operation timed out")
			return
		}
		response.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
