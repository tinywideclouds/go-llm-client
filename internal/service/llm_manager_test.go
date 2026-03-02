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

	if len(contents[0].Parts) == 0 {
		t.Fatalf("Expected parts in the first content block")
	}
	if !strings.Contains(fmt.Sprint(contents[0].Parts[0]), "Hello") {
		t.Errorf("Expected first message to contain 'Hello', got %v", contents[0].Parts[0])
	}

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

func TestInterceptToolCalls(t *testing.T) {
	fetcher := &mockFetcher{}
	manager := service.NewLLMManager(nil, fetcher, slog.Default())

	t.Run("Successfully intercepts propose_change", func(t *testing.T) {
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

		props, err := manager.InterceptToolCalls(context.Background(), chunk, "sess-1")

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(props) == 0 {
			t.Fatal("Expected proposals to be extracted")
		}
		if props[0].FilePath != "main.go" {
			t.Errorf("Expected FilePath 'main.go', got '%s'", props[0].FilePath)
		}
	})
}
