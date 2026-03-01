package api_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tinywideclouds/go-llm-client/internal/api"

	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
)

type mockFetcher struct {
	files map[string]string
}

func (m *mockFetcher) FetchCacheFiles(ctx context.Context, cacheID, profileID string) (map[string]string, error) {
	return m.files, nil
}
func (m *mockFetcher) Close() error { return nil }

func TestBuildInlineContext(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fetcher := &mockFetcher{
		files: map[string]string{
			"main.go": "package main",
			"test.go": "package test",
		},
	}

	attachments := []builder.Attachment{{CacheID: "cache-1"}}
	result := api.BuildInlineContext(context.Background(), fetcher, attachments, logger)

	assert.Contains(t, result, `<file path="main.go">`)
	assert.Contains(t, result, `package main`)
	assert.Contains(t, result, `</file>`)
	assert.Contains(t, result, `<file path="test.go">`)
}

func TestInjectInlineContext(t *testing.T) {
	history := []builder.Message{
		{Role: "user", Content: "Previous message"},
		{Role: "model", Content: "Response"},
		{Role: "user", Content: "What does this file do?"},
	}

	inlineContext := "<file path=\"main.go\">\npackage main\n</file>\n"

	result := api.InjectInlineContext(history, inlineContext)

	assert.Len(t, result, 3)
	// The inline context should be injected right before the final user query
	assert.Contains(t, result[2].Content, "Inline Context Provided by User:")
	assert.Contains(t, result[2].Content, "package main")
	assert.Contains(t, result[2].Content, "What does this file do?")
}

func TestBuildOverlayPrompt(t *testing.T) {
	t.Run("Empty Pending Queue", func(t *testing.T) {
		pending := []builder.ChangeProposal{}
		assert.Equal(t, "", api.BuildOverlayPrompt(pending))
	})

	t.Run("Populated Pending Queue", func(t *testing.T) {
		pending := []builder.ChangeProposal{
			{FilePath: "main.go", Patch: "@@ diff @@"},
			{FilePath: "utils.go", NewContent: "package utils"},
		}

		result := api.BuildOverlayPrompt(pending)
		assert.Contains(t, result, "<system_note>")
		assert.Contains(t, result, "PENDING changes awaiting user approval")
		assert.Contains(t, result, `<pending_proposal file="main.go">`)
		assert.Contains(t, result, "@@ diff @@")
		assert.Contains(t, result, `<pending_proposal file="utils.go">`)
		assert.Contains(t, result, "package utils")
	})
}
