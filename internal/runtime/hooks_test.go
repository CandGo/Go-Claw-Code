package runtime

import (
	"testing"
)

func TestMatchToolPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		toolName string
		want     bool
	}{
		{"*", "anything", true},
		{"bash", "bash", true},
		{"bash", "read_file", false},
		{"bash:*", "bash", true},
		{"bash:*", "bash:sub", true},
		{"bash:*", "read_file", false},
		{"mcp__*", "mcp__server_tool", false}, // Only :* suffix is supported, not *
		{"mcp__*", "read_file", false},
	}

	for _, tt := range tests {
		got := matchToolPattern(tt.pattern, tt.toolName)
		if got != tt.want {
			t.Errorf("matchToolPattern(%q, %q) = %v, want %v", tt.pattern, tt.toolName, got, tt.want)
		}
	}
}
