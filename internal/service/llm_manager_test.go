package service_test

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/tinywideclouds/go-llm-client/internal/service"
	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
)

// Add a mock fetcher at the top of the file
type mockFetcher struct {
	files map[string]string
}

func (m *mockFetcher) FetchCacheFiles(ctx context.Context, cacheID, profileID string) (map[string]string, error) {
	return m.files, nil
}
func (m *mockFetcher) Close() error { return nil }

func TestBuildHistoryContents(t *testing.T) {
	history := []builder.Message{
		{Role: "user", Content: "Hello"},
		{Role: "model", Content: "Hi there"},
		{Role: "user", Content: "Fix this code"},
	}

	overlay := "<system_note>unsaved changes</system_note>"

	contents := service.BuildHistoryContents(history, overlay)

	if len(contents) != 3 {
		t.Fatalf("Expected 3 content blocks, got %d", len(contents))
	}

	// Check that previous messages are untouched
	if len(contents[0].Parts) == 0 {
		t.Fatalf("Expected parts in the first content block")
	}
	// The genai SDK uses pointers, so we need to safely extract the string
	if !strings.Contains(fmt.Sprint(contents[0].Parts[0]), "Hello") {
		t.Errorf("Expected first message to contain 'Hello', got %v", contents[0].Parts[0])
	}

	// Check that the overlay was appended to the last message
	if len(contents[2].Parts) == 0 {
		t.Fatalf("Expected parts in the last content block")
	}
	lastText := fmt.Sprint(contents[2].Parts[0])

	if !strings.Contains(lastText, overlay) {
		t.Error("Expected overlay to be injected into the final user message")
	}
	if !strings.Contains(lastText, "User Query: Fix this code") {
		t.Error("Expected final message content to be preserved after overlay")
	}
}

func TestInterceptToolCall(t *testing.T) {
	fetcher := &mockFetcher{}
	// We can pass nil for the client since InterceptToolCall doesn't use it directly
	manager := service.NewLLMManager(nil, fetcher, slog.Default())

	t.Run("Successfully intercepts propose_change", func(t *testing.T) {
		// Simulate a Gemini FunctionCall response
		chunk := &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Parts: []*genai.Part{
							{
								FunctionCall: &genai.FunctionCall{
									Name: "propose_change",
									Args: map[string]any{
										"file_path": "main.go",
										"patch":     "@@ -1,3 +1,4 @@\n package main\n+\n+import \"fmt\"\n \n func main() {}",
										"reasoning": "Added fmt import",
									},
								},
							},
						},
					},
				},
			},
		}

		prop, isTool, err := manager.InterceptToolCall(context.Background(), chunk, "sess-1")

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !isTool {
			t.Fatal("Expected isTool to be true")
		}
		if prop.FilePath != "main.go" {
			t.Errorf("Expected FilePath 'main.go', got '%s'", prop.FilePath)
		}
		if prop.Patch == "" {
			t.Error("Expected patch to be populated")
		}
		if prop.NewContent != "" {
			t.Error("Expected newContent to be empty")
		}
	})
}
