package runtime

import (
	"testing"
)

func TestTokenUsageAdd(t *testing.T) {
	a := &TokenUsage{InputTokens: 100, OutputTokens: 50}
	b := &TokenUsage{InputTokens: 200, OutputTokens: 100, CacheReadInputTokens: 10}

	a.Add(b)

	if a.InputTokens != 300 {
		t.Errorf("expected InputTokens=300, got %d", a.InputTokens)
	}
	if a.OutputTokens != 150 {
		t.Errorf("expected OutputTokens=150, got %d", a.OutputTokens)
	}
	if a.CacheReadInputTokens != 10 {
		t.Errorf("expected CacheReadInputTokens=10, got %d", a.CacheReadInputTokens)
	}
}

func TestTokenUsageTotal(t *testing.T) {
	u := &TokenUsage{InputTokens: 100, OutputTokens: 50}
	if u.Total() != 150 {
		t.Errorf("expected Total=150, got %d", u.Total())
	}
}

func TestTokenUsageAddNil(t *testing.T) {
	a := &TokenUsage{InputTokens: 100}
	a.Add(nil)
	if a.InputTokens != 100 {
		t.Errorf("Add(nil) should not change values, got %d", a.InputTokens)
	}
}

func TestMessageRoles(t *testing.T) {
	if MsgRoleUser != "user" {
		t.Errorf("MsgRoleUser should be 'user'")
	}
	if MsgRoleAssistant != "assistant" {
		t.Errorf("MsgRoleAssistant should be 'assistant'")
	}
	if MsgRoleSystem != "system" {
		t.Errorf("MsgRoleSystem should be 'system'")
	}
}

func TestContentBlockTypes(t *testing.T) {
	tb := &TextBlock{Text: "hello"}
	if tb.ContentType() != "text" {
		t.Errorf("TextBlock.ContentType() = %s, want text", tb.ContentType())
	}

	tub := &ToolUseBlock{ID: "1", Name: "bash", Input: map[string]interface{}{}}
	if tub.ContentType() != "tool_use" {
		t.Errorf("ToolUseBlock.ContentType() = %s, want tool_use", tub.ContentType())
	}

	trb := &ToolResultBlock{ToolUseID: "1", ToolName: "bash", Content: "ok"}
	if trb.ContentType() != "tool_result" {
		t.Errorf("ToolResultBlock.ContentType() = %s, want tool_result", trb.ContentType())
	}
	if trb.ToolName != "bash" {
		t.Errorf("ToolResultBlock.ToolName = %s, want bash", trb.ToolName)
	}

	srb := &SystemReminderBlock{Content: "test", Source: "compaction"}
	if srb.ContentType() != "system_reminder" {
		t.Errorf("SystemReminderBlock.ContentType() = %s, want system_reminder", srb.ContentType())
	}
}
