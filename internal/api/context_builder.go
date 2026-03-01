package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/tinywideclouds/go-llm-client/internal/store"
	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
)

// BuildInlineContext fetches the files for the given attachments and formats them into an XML string.
func BuildInlineContext(ctx context.Context, fetcher store.Fetcher, attachments []builder.Attachment, logger *slog.Logger) string {
	if len(attachments) == 0 {
		return ""
	}

	inlineContext := ""
	for _, att := range attachments {
		files, err := fetcher.FetchCacheFiles(ctx, att.CacheID, att.ProfileID)
		if err != nil {
			logger.Warn("Failed to fetch inline attachment, skipping", "cacheId", att.CacheID, "error", err)
			continue
		}
		for path, content := range files {
			inlineContext += fmt.Sprintf("<file path=%q>\n%s\n</file>\n", path, content)
		}
	}
	return inlineContext
}

// InjectInlineContext safely appends the inline context to the final user message in the history.
func InjectInlineContext(history []builder.Message, inlineContext string) []builder.Message {
	if inlineContext == "" || len(history) == 0 {
		return history
	}

	lastIdx := len(history) - 1
	history[lastIdx].Content = fmt.Sprintf("Inline Context Provided by User:\n%s\n\n%s", inlineContext, history[lastIdx].Content)
	return history
}

// BuildOverlayPrompt generates the <system_note> XML block for PENDING proposals in the queue.
func BuildOverlayPrompt(pending []builder.ChangeProposal) string {
	if len(pending) == 0 {
		return ""
	}

	prompt := "<system_note>\nThe following files have PENDING changes awaiting user approval. Take these proposed changes into account to avoid contradicting them:\n"
	for _, p := range pending {
		prompt += fmt.Sprintf("<pending_proposal file=%q>\n", p.FilePath)
		if p.Patch != "" {
			prompt += fmt.Sprintf("Patch:\n%s\n", p.Patch)
		} else if p.NewContent != "" {
			prompt += fmt.Sprintf("New Content:\n%s\n", p.NewContent)
		}
		prompt += "</pending_proposal>\n"
	}
	prompt += "</system_note>\n"
	return prompt
}
