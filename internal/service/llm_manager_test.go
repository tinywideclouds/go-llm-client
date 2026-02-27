package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tinywideclouds/go-llm/pkg/builder/v1"
)

func TestBuildHistoryContents(t *testing.T) {
	history := []builder.Message{
		{Role: "user", Content: "Hello"},
		{Role: "model", Content: "Hi there"},
		{Role: "user", Content: "Fix this code"},
	}

	overlay := "<system_note>unsaved changes</system_note>"

	contents := BuildHistoryContents(history, overlay)

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
