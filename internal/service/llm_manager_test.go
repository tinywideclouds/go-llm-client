package service_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-llm-client/internal/service"
	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"

	"google.golang.org/genai"
)

func mustURN(s string) urn.URN {
	u, err := urn.Parse(s)
	if err != nil {
		panic("invalid test URN: " + s)
	}
	return u
}

func TestLLMManager_InterceptToolCalls(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Pass nil for genai.Client and store.Fetcher since InterceptToolCalls is a pure function
	manager := service.NewLLMManager(nil, nil, logger)

	sessionID := mustURN("urn:llm:session:123")

	t.Run("Extracts propose_change properly mapping URNs", func(t *testing.T) {
		chunk := &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Parts: []*genai.Part{
							{Text: "I will make the change now."},
							{
								FunctionCall: &genai.FunctionCall{
									Name: "propose_change",
									Args: map[string]any{
										"file_path": "main.go",
										"patch":     "@@ -1,3 +1,4 @@",
										"reasoning": "Adding a print statement",
									},
								},
							},
						},
					},
				},
			},
		}

		proposals, err := manager.InterceptToolCalls(context.Background(), chunk, sessionID)
		require.NoError(t, err)
		require.Len(t, proposals, 1)

		prop := proposals[0]
		assert.Equal(t, sessionID, prop.SessionID) // Verify strict URN mapping
		assert.Equal(t, "main.go", prop.FilePath)
		assert.Equal(t, "@@ -1,3 +1,4 @@", prop.Patch)
		assert.Equal(t, "Adding a print statement", prop.Reasoning)
		assert.NotEmpty(t, prop.ID) // Ephemeral string ID should be generated
	})

	t.Run("Ignores chunks without function calls", func(t *testing.T) {
		chunk := &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Parts: []*genai.Part{
							{Text: "Just a standard text response"},
						},
					},
				},
			},
		}

		proposals, err := manager.InterceptToolCalls(context.Background(), chunk, sessionID)
		require.NoError(t, err)
		assert.Empty(t, proposals)
	})
}

func TestLLMManager_BuildHistoryContents(t *testing.T) {
	history := []builder.Message{
		{Role: "user", Content: "Hello"},
		{Role: "model", Content: "Hi there"},
		{Role: "user", Content: "Make a change"},
	}

	overlay := "<system_note>Pending files...</system_note>"

	contents := service.BuildHistoryContents(history, overlay)
	require.Len(t, contents, 3)

	assert.Equal(t, "user", contents[0].Role)
	assert.Equal(t, "Hello", contents[0].Parts[0].Text)

	// Verify overlay is injected ONLY into the final user message
	assert.Equal(t, "user", contents[2].Role)
	assert.Contains(t, contents[2].Parts[0].Text, "<system_note>Pending files...</system_note>")
	assert.Contains(t, contents[2].Parts[0].Text, "Make a change")
}
