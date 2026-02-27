package service

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"time"

	"github.com/tinywideclouds/go-llm/pkg/builder/v1"

	"google.golang.org/genai"
)

type LLMManager interface {
	CreateRepositoryCache(ctx context.Context, model string, files map[string]string, ttl time.Duration, explicitInstructions map[string]string) (string, error)
	GenerateResponse(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID string) (*genai.GenerateContentResponse, error)
	GenerateStreamResponse(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID string) iter.Seq2[*genai.GenerateContentResponse, error]
}

type geminiManager struct {
	client *genai.Client
	Logger *slog.Logger
}

func NewLLMManager(client *genai.Client, logger *slog.Logger) LLMManager {
	return &geminiManager{
		client: client,
		Logger: logger,
	}
}

// CreateRepositoryCache uploads the entire file map to Google's caching infrastructure.
func (m *geminiManager) CreateRepositoryCache(ctx context.Context, model string, files map[string]string, ttl time.Duration, explicitInstructions map[string]string) (string, error) {
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

	// Use the NEW SDK configuration struct
	config := &genai.CreateCachedContentConfig{
		Contents:          contents,
		SystemInstruction: sysInst,
		TTL:               ttl,
		DisplayName:       "repo-cache-" + time.Now().Format("20060102150405"),
	}

	// The new SDK requires (ctx, model, config)
	createdCache, err := m.client.Caches.Create(ctx, model, config)
	if err != nil {
		return "", fmt.Errorf("failed to create context cache on Google servers: %w", err)
	}

	m.Logger.Info("Gemini Cache created successfully", "cache_name", createdCache.Name)
	return createdCache.Name, nil
}

// GenerateResponse generates a single blocking response utilizing the cache if provided.
func (m *geminiManager) GenerateResponse(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID string) (*genai.GenerateContentResponse, error) {
	contents := BuildHistoryContents(history, overlayPrompt)

	config := &genai.GenerateContentConfig{
		Tools: GetTools(),
	}

	// Inject the Cache ID if we have one!
	if cacheID != "" {
		config.CachedContent = cacheID
		m.Logger.Debug("Using Gemini Context Cache for generation", "cache_name", cacheID)
	}

	return m.client.Models.GenerateContent(ctx, model, contents, config)
}

// GenerateStreamResponse streams the generation back utilizing the cache if provided.
func (m *geminiManager) GenerateStreamResponse(ctx context.Context, model string, overlayPrompt string, history []builder.Message, cacheID string) iter.Seq2[*genai.GenerateContentResponse, error] {
	contents := BuildHistoryContents(history, overlayPrompt)

	config := &genai.GenerateContentConfig{
		Tools: GetTools(),
	}

	// Inject the Cache ID if we have one!
	if cacheID != "" {
		config.CachedContent = cacheID
		m.Logger.Debug("Using Gemini Context Cache for stream generation", "cache_name", cacheID)
	}

	return m.client.Models.GenerateContentStream(ctx, model, contents, config)
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
