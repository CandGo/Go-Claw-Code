package tools

import (
	"testing"
)

func TestToolSearchSelect(t *testing.T) {
	NewToolRegistry() // Initialize global registry
	result, err := globalRegistry.Execute("ToolSearch", map[string]interface{}{
		"query": "select:Bash,Read",
	})
	if err != nil {
		t.Fatalf("ToolSearch select failed: %v", err)
	}
	if !contains(result, "Bash") {
		t.Error("ToolSearch select should contain 'Bash'")
	}
	if !contains(result, "Read") {
		t.Error("ToolSearch select should contain 'Read'")
	}
}

func TestToolSearchByKeyword(t *testing.T) {
	NewToolRegistry()
	result, err := globalRegistry.Execute("ToolSearch", map[string]interface{}{
		"query": "file read",
	})
	if err != nil {
		t.Fatalf("ToolSearch failed: %v", err)
	}
	if !contains(result, "Read") {
		t.Errorf("ToolSearch 'file read' should find Read: %s", result)
	}
}

func TestToolSearchRequiredTerms(t *testing.T) {
	NewToolRegistry()
	result, err := globalRegistry.Execute("ToolSearch", map[string]interface{}{
		"query": "+Bash execute",
	})
	if err != nil {
		t.Fatalf("ToolSearch failed: %v", err)
	}
	// Should find Bash tool
	if !contains(result, "Bash") {
		t.Errorf("ToolSearch '+Bash execute' should find Bash: %s", result)
	}
}

func TestToolSearchMaxResults(t *testing.T) {
	NewToolRegistry()
	result, err := globalRegistry.Execute("ToolSearch", map[string]interface{}{
		"query":       "tool",
		"max_results": float64(2),
	})
	if err != nil {
		t.Fatalf("ToolSearch failed: %v", err)
	}
	if !contains(result, "results") {
		t.Errorf("ToolSearch result should contain results: %s", result)
	}
}

func TestCanonicalToolToken(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Read", "read"},
		{"WebFetch", "webfetch"},
		{"Bash", "bash"},
		{"ToolSearch", "toolsearch"},
		{"NotebookEdit", "notebookedit"},
	}
	for _, tt := range tests {
		result := canonicalToolToken(tt.input)
		if result != tt.expected {
			t.Errorf("canonicalToolToken(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
