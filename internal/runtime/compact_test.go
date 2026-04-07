package runtime

import (
	"strings"
	"testing"
)

func TestCompactXMLSummary(t *testing.T) {
	s := NewSession()
	// Add many messages to trigger compaction
	for i := 0; i < 10; i++ {
		s.AddUserMessage("Do something with file.go")
		s.AddAssistantMessage([]ContentBlock{
			&TextBlock{Text: "I'll edit the file for you."},
			&ToolUseBlock{ID: "t1", Name: "Edit", Input: map[string]interface{}{"path": "file.go"}},
		}, nil)
		s.AddUserMessageBlocks(&ToolResultBlock{ToolUseID: "t1", ToolName: "Edit", Content: "replaced 1 occurrence"})
	}

	cfg := DefaultCompactionConfig()
	cfg.PreserveRecentMessages = 2
	cfg.MaxEstimatedTokens = 100

	result := s.Compact(cfg)
	if result.MessagesBefore <= result.MessagesAfter {
		t.Errorf("compaction should reduce messages: before=%d after=%d", result.MessagesBefore, result.MessagesAfter)
	}

	// First message should be a system summary
	if len(s.Messages) == 0 {
		t.Fatal("session should have messages after compaction")
	}
	if s.Messages[0].Role != MsgRoleSystem {
		t.Errorf("first message should be system, got %s", s.Messages[0].Role)
	}

	tb, ok := s.Messages[0].Content[0].(*TextBlock)
	if !ok {
		t.Fatal("first block should be TextBlock")
	}

	// Should contain continuation preamble and formatted summary
	if !strings.Contains(tb.Text, "continued from a previous conversation") {
		t.Errorf("summary should contain continuation preamble, got: %s", tb.Text[:min(200, len(tb.Text))])
	}
	if !strings.Contains(tb.Text, "compacted") {
		t.Error("summary should mention compaction")
	}
	if !strings.Contains(tb.Text, "Summary:") {
		t.Error("summary should contain 'Summary:'")
	}
}

func TestUnicodeTruncation(t *testing.T) {
	s := "你好世界"
	result := truncateUnicode(s, 3)
	if result != "你好世..." {
		t.Errorf("truncateUnicode(%q, 3) = %q", s, result)
	}

	// Exact fit
	result = truncateUnicode("hello", 5)
	if result != "hello" {
		t.Errorf("truncateUnicode(exact) = %q", result)
	}
}

func TestCollapseBlankLines(t *testing.T) {
	input := "line1\n\n\n\nline2"
	result := collapseBlankLines(input)
	if strings.Contains(result, "\n\n\n") {
		t.Errorf("should collapse 3+ blank lines: %q", result)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
