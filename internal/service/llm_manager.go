// internal/service/llm_manager.go
package service

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"time"

	"google.golang.org/genai"
)

// Message represents a single turn in the conversation.
type Message struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"` // Typically "user" or "model"
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

type LLMManager interface {
	CreateRepositoryCache(ctx context.Context, model string, files map[string]string, ttl time.Duration, explicitInstructions map[string]string) (string, error)
	GenerateResponse(ctx context.Context, model string, overlayPrompt string, history []Message, cacheID string) (*genai.GenerateContentResponse, error)
	GenerateStreamResponse(ctx context.Context, model string, overlayPrompt string, history []Message, cacheID string) iter.Seq2[*genai.GenerateContentResponse, error]
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

func (m *geminiManager) CreateRepositoryCache(
	ctx context.Context,
	model string,
	files map[string]string,
	ttl time.Duration,
	explicitInstructions map[string]string,
) (string, error) {
	config := BuildCacheConfig(files, ttl, explicitInstructions)

	// Execute actual genai SDK call here using m.client and the assembled config
	_ = config
	_ = model

	return "", fmt.Errorf("network call implementation pending original SDK logic")
}

func (m *geminiManager) GenerateResponse(ctx context.Context, model string, overlayPrompt string, history []Message, cacheID string) (*genai.GenerateContentResponse, error) {
	var contents = BuildHistoryContents(history, overlayPrompt)

	genConfig := &genai.GenerateContentConfig{
		CachedContent:  cacheID,
		Tools:          GetTools(),
		ThinkingConfig: &genai.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: genai.ThinkingLevelHigh},
	}

	// Unpack the slice of contents into the variadic GenerateContent method
	return m.client.Models.GenerateContent(ctx, model, contents, genConfig)
}

func (m *geminiManager) GenerateStreamResponse(ctx context.Context, model string, overlayPrompt string, history []Message, cacheID string) iter.Seq2[*genai.GenerateContentResponse, error] {
	contents := BuildHistoryContents(history, overlayPrompt)

	genConfig := &genai.GenerateContentConfig{
		CachedContent:  cacheID,
		Tools:          GetTools(),
		ThinkingConfig: &genai.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: genai.ThinkingLevelHigh},
	}

	m.Logger.Debug("Starting GenerateStreamResponse", "model", model, "cacheID", cacheID, "historyLength", len(history), "overlayPromptLength", len(overlayPrompt))

	return m.client.Models.GenerateContentStream(ctx, model, contents, genConfig)
}

// BuildHistoryContents converts domain messages to genai.Content and injects the overlay
func BuildHistoryContents(history []Message, overlayPrompt string) []*genai.Content {
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

func BuildCacheConfig(
	files map[string]string,
	ttl time.Duration,
	explicitInstructions map[string]string,
) *genai.CreateCachedContentConfig {

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

	createConfig := &genai.CreateCachedContentConfig{
		DisplayName: "codebase-cache",
		Contents: []*genai.Content{
			{
				Role:  "user",
				Parts: codebaseParts,
			},
		},
		TTL: ttl,
	}

	if len(instructionParts) > 0 {
		createConfig.SystemInstruction = &genai.Content{
			Role:  "system",
			Parts: instructionParts,
		}
	}

	return createConfig
}
