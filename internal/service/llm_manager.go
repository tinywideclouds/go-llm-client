package service

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"time"

	"github.com/tinywideclouds/go-llm-client/internal/store"
	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"

	"google.golang.org/genai"
)

// extractGeminiCacheName strips the URN formatting to yield the raw GenAI cache name
// e.g., "urn:llm:compiled-cache:cachedContents/123" -> "cachedContents/123"
func extractGeminiCacheName(u *urn.URN) string {
	if u == nil {
		return ""
	}
	str := u.String()
	parts := strings.SplitN(str, ":", 4)
	if len(parts) == 4 {
		return parts[3]
	}
	return str
}

type LLMManager interface {
	CreateRepositoryCache(ctx context.Context, model string, files map[string]string, ttl time.Duration, explicitInstructions map[string]string) (urn.URN, error)
	GenerateResponse(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID *urn.URN, extraHistory []*genai.Content) (*genai.GenerateContentResponse, error)
	GenerateStreamResponse(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID *urn.URN, extraHistory []*genai.Content) iter.Seq2[*genai.GenerateContentResponse, error]

	// Tool Interception - Pure stateless parser
	InterceptToolCalls(ctx context.Context, chunk *genai.GenerateContentResponse, sessionID urn.URN) ([]*builder.ChangeProposal, error)
}

type geminiManager struct {
	client  *genai.Client
	fetcher store.Fetcher
	Logger  *slog.Logger
}

func NewLLMManager(client *genai.Client, fetcher store.Fetcher, logger *slog.Logger) LLMManager {
	return &geminiManager{
		client:  client,
		fetcher: fetcher,
		Logger:  logger,
	}
}

// CreateRepositoryCache uploads the entire file map to Google's caching infrastructure.
func (m *geminiManager) CreateRepositoryCache(ctx context.Context, model string, files map[string]string, ttl time.Duration, explicitInstructions map[string]string) (urn.URN, error) {
	m.Logger.Info("Building Gemini Cache", "file_count", len(files), "ttl", ttl)

	var codebaseParts []*genai.Part
	for fname, content := range files {
		fileText := fmt.Sprintf("<file path=%q>\n%s\n</file>", fname, content)
		codebaseParts = append(codebaseParts, &genai.Part{Text: fileText})
	}

	var instructionParts []*genai.Part
	if len(explicitInstructions) > 0 {
		for name, content := range explicitInstructions {
			instrText := fmt.Sprintf("<instruction name=%q>\n%s\n</instruction>", name, content)
			instructionParts = append(instructionParts, &genai.Part{Text: instrText})
		}
	}

	contents := []*genai.Content{{
		Role:  "user",
		Parts: codebaseParts,
	}}

	var sysInst *genai.Content
	if len(instructionParts) > 0 {
		sysInst = &genai.Content{Parts: instructionParts}
	}

	config := &genai.CreateCachedContentConfig{
		Contents:          contents,
		SystemInstruction: sysInst,
		Tools:             GetWorkspaceTools(),
		TTL:               ttl,
		DisplayName:       "repo-cache-" + time.Now().Format("20060102150405"),
	}

	createdCache, err := m.client.Caches.Create(ctx, model, config)
	if err != nil {
		return urn.URN{}, fmt.Errorf("failed to create context cache on Google servers: %w", err)
	}

	m.Logger.Info("Gemini Cache created successfully", "cache_name", createdCache.Name)

	cacheURNStr := fmt.Sprintf("urn:llm:compiled-cache:%s", createdCache.Name)
	cacheURN, err := urn.Parse(cacheURNStr)
	if err != nil {
		return urn.URN{}, fmt.Errorf("failed to parse generated cache name into URN: %w", err)
	}

	return cacheURN, nil
}

// GenerateResponse generates a single blocking response utilizing the cache if provided.
func (m *geminiManager) GenerateResponse(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID *urn.URN, extraHistory []*genai.Content) (*genai.GenerateContentResponse, error) {
	contents := BuildHistoryContents(history, overlayPrompt)
	if len(extraHistory) > 0 {
		contents = append(contents, extraHistory...)
	}

	config := &genai.GenerateContentConfig{
		Tools: GetWorkspaceTools(),
	}

	if cacheID != nil {
		geminiName := extractGeminiCacheName(cacheID)
		config.CachedContent = geminiName
		m.Logger.Debug("Using Gemini Context Cache for generation", "cache_name", geminiName)
	}

	return m.client.Models.GenerateContent(ctx, model, contents, config)
}

// GenerateStreamResponse streams the generation back utilizing the cache if provided.
func (m *geminiManager) GenerateStreamResponse(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID *urn.URN, extraHistory []*genai.Content) iter.Seq2[*genai.GenerateContentResponse, error] {
	contents := BuildHistoryContents(history, overlayPrompt)
	if len(extraHistory) > 0 {
		contents = append(contents, extraHistory...)
	}

	config := &genai.GenerateContentConfig{}

	if cacheID != nil {
		geminiName := extractGeminiCacheName(cacheID)
		config.CachedContent = geminiName
		// DO NOT set config.Tools here if cacheID is present to avoid GenAI HTTP 400 errors
		m.Logger.Debug("Using Gemini Context Cache for stream generation", "cache_name", geminiName)
	} else {
		// Only set tools for non-cached "fallback" calls
		config.Tools = GetWorkspaceTools()
	}

	return m.client.Models.GenerateContentStream(ctx, model, contents, config)
}

func (m *geminiManager) InterceptToolCalls(ctx context.Context, chunk *genai.GenerateContentResponse, sessionID urn.URN) ([]*builder.ChangeProposal, error) {
	var proposals []*builder.ChangeProposal

	if len(chunk.Candidates) == 0 || chunk.Candidates[0].Content == nil {
		return proposals, nil
	}

	parts := chunk.Candidates[0].Content.Parts
	m.Logger.Debug("InterceptToolCalls inspecting candidate parts", "parts_count", len(parts))

	for i, part := range parts {
		if part.FunctionCall == nil || part.FunctionCall.Name != "propose_change" {
			continue
		}

		m.Logger.Info("Found propose_change tool call in chunk part", "part_index", i)

		args := part.FunctionCall.Args
		filePath, _ := args["file_path"].(string)
		patch, _ := args["patch"].(string)
		newContent, _ := args["new_content"].(string)
		reasoning, _ := args["reasoning"].(string)

		prop := &builder.ChangeProposal{
			ID:         fmt.Sprintf("prop-%d", time.Now().UnixNano()),
			SessionID:  sessionID, // Strictly assigned as urn.URN
			FilePath:   filePath,
			Patch:      patch,
			NewContent: newContent,
			Reasoning:  reasoning,
			CreatedAt:  time.Now(),
		}

		proposals = append(proposals, prop)
	}

	if len(proposals) > 0 {
		m.Logger.Info("Successfully extracted proposals from chunk", "proposal_count", len(proposals))
	}

	return proposals, nil
}

// BuildHistoryContents maps our generic Messages to the genai SDK types and injects the overlay
func BuildHistoryContents(history []builder.Message, overlayPrompt string) []*genai.Content {
	var contents []*genai.Content
	for i, msg := range history {
		text := msg.Content
		if i == len(history)-1 && overlayPrompt != "" {
			text = overlayPrompt + "\nUser Query: " + text
		}
		contents = append(contents, &genai.Content{
			Role:  msg.Role,
			Parts: []*genai.Part{{Text: text}},
		})
	}
	return contents
}
