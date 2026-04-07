package runtime

import (
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
		"bash", "read_file", "write_file", "edit_file",
		"glob", "grep", "WebFetch", "WebSearch",
		"TodoWrite", "Agent", "Skill", "NotebookEdit",
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
