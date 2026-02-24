package service

import (
	"strings"
	"testing"
	"time"
)

func TestBuildCacheConfig(t *testing.T) {
	files := map[string]string{
		"main.go": "package main\nfunc main() {}",
	}
	instructions := map[string]string{
		"rules.md": "Use standard formatting",
	}
	ttl := 1 * time.Hour

	config := BuildCacheConfig(files, ttl, instructions)

	if config.TTL != ttl {
		t.Errorf("Expected TTL %v, got %v", ttl, config.TTL)
	}
	if config.DisplayName != "codebase-cache" {
		t.Errorf("Expected DisplayName 'codebase-cache', got '%s'", config.DisplayName)
	}

	if len(config.Contents) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(config.Contents))
	}
	userContent := config.Contents[0]
	if userContent.Role != "user" {
		t.Errorf("Expected role 'user', got '%s'", userContent.Role)
	}
	if len(userContent.Parts) != 1 {
		t.Fatalf("Expected 1 user part, got %d", len(userContent.Parts))
	}

	partText := userContent.Parts[0].Text
	if !strings.Contains(partText, "<file path=\"main.go\">") {
		t.Error("Missing XML tag for main.go")
	}

	if config.SystemInstruction == nil {
		t.Fatal("Expected SystemInstruction to be populated")
	}
	if config.SystemInstruction.Role != "system" {
		t.Errorf("Expected system role, got '%s'", config.SystemInstruction.Role)
	}
	if len(config.SystemInstruction.Parts) != 1 {
		t.Fatalf("Expected 1 instruction part, got %d", len(config.SystemInstruction.Parts))
	}

	instrText := config.SystemInstruction.Parts[0].Text
	if !strings.Contains(instrText, "<instruction name=\"rules.md\">") {
		t.Error("Missing XML tag for rules.md instruction")
	}
}

func TestBuildCacheConfig_NoInstructions(t *testing.T) {
	config := BuildCacheConfig(map[string]string{"file.txt": "data"}, time.Hour, nil)

	if config.SystemInstruction != nil {
		t.Error("Expected SystemInstruction to be nil when no instructions are provided")
	}
}

func TestBuildHistoryContents(t *testing.T) {
	history := []Message{
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
	if contents[0].Parts[0].Text != "Hello" {
		t.Errorf("Expected first message to be 'Hello', got %q", contents[0].Parts[0].Text)
	}

	// Check that the overlay was appended to the last message
	lastText := contents[2].Parts[0].Text
	if !strings.Contains(lastText, overlay) {
		t.Error("Expected overlay to be injected into the final user message")
	}
	if !strings.Contains(lastText, "User Query: Fix this code") {
		t.Error("Expected final message content to be preserved after overlay")
	}
}
