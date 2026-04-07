package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "session.json")

	s := NewSession()
	s.Info.Model = "claude-sonnet-4-6"

	s.AddUserMessage("Hello")
	s.AddAssistantMessage([]ContentBlock{
		&TextBlock{Text: "Hi there!"},
		&ToolUseBlock{ID: "t1", Name: "bash", Input: map[string]interface{}{"command": "ls"}},
	}, &TokenUsage{InputTokens: 10, OutputTokens: 20})

	// Test ToolResult with tool_name
	s.AddUserMessageBlocks(&ToolResultBlock{
		ToolUseID: "t1",
		ToolName:  "bash",
		Content:   "file1.go\nfile2.go",
	})

	s.AddSystemMessage(&SystemReminderBlock{
		Content: "compacted summary",
		Source:  "compaction",
	})

	if err := s.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession failed: %v", err)
	}

	if len(loaded.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(loaded.Messages))
	}

	// Check message roles
	if loaded.Messages[0].Role != MsgRoleUser {
		t.Errorf("msg 0 role = %s, want user", loaded.Messages[0].Role)
	}
	if loaded.Messages[1].Role != MsgRoleAssistant {
		t.Errorf("msg 1 role = %s, want assistant", loaded.Messages[1].Role)
	}
	if loaded.Messages[3].Role != MsgRoleSystem {
		t.Errorf("msg 3 role = %s, want system", loaded.Messages[3].Role)
	}

	// Check usage persisted
	if loaded.Messages[1].Usage == nil {
		t.Fatal("assistant message usage should be persisted")
	}
	if loaded.Messages[1].Usage.InputTokens != 10 {
		t.Errorf("usage.InputTokens = %d, want 10", loaded.Messages[1].Usage.InputTokens)
	}

	// Check tool_name on tool result
	trb, ok := loaded.Messages[2].Content[0].(*ToolResultBlock)
	if !ok {
		t.Fatal("msg 2 content should be ToolResultBlock")
	}
	if trb.ToolName != "bash" {
		t.Errorf("tool_name = %s, want bash", trb.ToolName)
	}

	// Check system reminder
	srb, ok := loaded.Messages[3].Content[0].(*SystemReminderBlock)
	if !ok {
		t.Fatal("msg 3 content should be SystemReminderBlock")
	}
	if srb.Source != "compaction" {
		t.Errorf("source = %s, want compaction", srb.Source)
	}
}

func TestSessionVersionMigration(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "old_session.json")

	// Write a v1 session (old format)
	oldFormat := `{"version": 1, "messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}]}`
	os.WriteFile(path, []byte(oldFormat), 0644)

	s, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession v1 failed: %v", err)
	}
	if s.Version != currentSessionVersion {
		t.Errorf("version should be migrated to %d, got %d", currentSessionVersion, s.Version)
	}
}

func TestLoadSessionInfo(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "session.json")

	s := NewSession()
	s.Info.Model = "test-model"
	s.AddUserMessage("hello")
	s.Save(path)

	info, err := LoadSessionInfo(path)
	if err != nil {
		t.Fatalf("LoadSessionInfo failed: %v", err)
	}
	if info.Model != "test-model" {
		t.Errorf("model = %s, want test-model", info.Model)
	}
	if info.Version != currentSessionVersion {
		t.Errorf("version = %d, want %d", info.Version, currentSessionVersion)
	}
}
