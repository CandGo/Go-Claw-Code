package runtime

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultSystemPromptContains(t *testing.T) {
	prompt := DefaultSystemPrompt("glm-5.1")

	// Should contain key sections
	sections := []string{
		"Platform",
		"Working directory",
	}
	for _, s := range sections {
		if !strings.Contains(prompt, s) {
			t.Errorf("system prompt missing section %q", s)
		}
	}
}

func TestDefaultSystemPromptListsTools(t *testing.T) {
	prompt := DefaultSystemPrompt("glm-5.1")

	tools := []string{
		"Bash", "Read", "Write", "Edit",
		"Glob", "Grep", "WebFetch", "WebSearch",
		"TodoWrite", "Agent",
	}
	for _, tool := range tools {
		if !strings.Contains(prompt, tool) {
			t.Errorf("system prompt missing tool %q", tool)
		}
	}
}

func TestBuildSystemReminder(t *testing.T) {
	reminder := BuildSystemReminder("test message", "test_source")
	if reminder == nil {
		t.Fatal("BuildSystemReminder returned nil")
	}
	if reminder.Content != "test message" {
		t.Errorf("Content = %q", reminder.Content)
	}
	if reminder.Source != "test_source" {
		t.Errorf("Source = %q", reminder.Source)
	}
	if reminder.ContentType() != "system_reminder" {
		t.Errorf("ContentType = %q", reminder.ContentType())
	}
}

func TestBuildCachedSystemBlocks(t *testing.T) {
	builder := NewSystemPromptBuilder().
		WithOS("windows", "10.0").
		WithModelName("claude-sonnet-4-6")

	blocks := builder.BuildCachedSystem()

	if len(blocks) < 2 {
		t.Fatalf("expected at least 2 cached blocks, got %d", len(blocks))
	}

	// First block should be static content with cache_control
	if blocks[0].Type != "text" {
		t.Errorf("block[0].Type = %q, want 'text'", blocks[0].Type)
	}
	if blocks[0].CacheControl == nil {
		t.Error("block[0] should have cache_control (static section)")
	}
	if blocks[0].CacheControl.Type != "ephemeral" {
		t.Errorf("block[0] cache_control type = %q, want 'ephemeral'", blocks[0].CacheControl.Type)
	}

	// Static block should contain key system prompt sections
	if !strings.Contains(blocks[0].Text, "Doing tasks") {
		t.Error("static block should contain 'Doing tasks' section")
	}
	if strings.Contains(blocks[0].Text, "Platform") {
		t.Error("static block should NOT contain dynamic content like 'Platform'")
	}

	// Last block should have cache_control
	last := blocks[len(blocks)-1]
	if last.CacheControl == nil {
		t.Error("last block should have cache_control")
	}
}

func TestBuildCachedSystemJSON(t *testing.T) {
	builder := NewSystemPromptBuilder().
		WithOS("darwin", "").
		WithModelName("glm-5.1")

	blocks := builder.BuildCachedSystem()

	// Verify it serializes correctly to JSON (as used in buildRequest)
	data, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	s := string(data)
	if !strings.Contains(s, `"cache_control":{"type":"ephemeral"}`) {
		t.Error("JSON should contain cache_control markers")
	}
	if !strings.Contains(s, `"type":"text"`) {
		t.Error("JSON should contain text blocks")
	}
}
