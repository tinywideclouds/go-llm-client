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
	t.Run("Empty Overlays", func(t *testing.T) {
		// FIX: Updated to builder.FileState
		overlays := make(map[string]builder.FileState)
		assert.Equal(t, "", api.BuildOverlayPrompt(overlays))
	})

	t.Run("Modified and Deleted Files", func(t *testing.T) {
		// FIX: Updated to builder.FileState
		overlays := map[string]builder.FileState{
			"modified.go": {Content: "func new() {}", IsDeleted: false},
			"removed.go":  {Content: "", IsDeleted: true},
		}

		result := api.BuildOverlayPrompt(overlays)

		assert.Contains(t, result, "<system_note>")
		assert.Contains(t, result, "unsaved changes")
		assert.Contains(t, result, `<file path="modified.go">`)
		assert.Contains(t, result, `func new() {}`)
		assert.Contains(t, result, `<file path="removed.go" status="deleted" />`)
		assert.Contains(t, result, "</system_note>")
	})
}
