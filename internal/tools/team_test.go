package tools

import (
	"strings"
	"testing"
)

// =============================================================================
// Team tools tests
// =============================================================================

func TestTeamCreate(t *testing.T) {
	r := NewToolRegistry()
	// Clear any pre-existing workspaces
	wsMu.Lock()
	for k := range workspaces {
		delete(workspaces, k)
	}
	wsMu.Unlock()

	result, err := r.Execute("TeamCreate", map[string]interface{}{
		"team_name":   "test-team",
		"description": "a test workspace",
	})
	if err != nil {
		t.Fatalf("TeamCreate: %v", err)
	}
	if !strings.Contains(result, "test-team") {
		t.Errorf("result should mention team name, got %q", result)
	}
}

func TestTeamCreateDuplicate(t *testing.T) {
	r := NewToolRegistry()
	wsMu.Lock()
	for k := range workspaces {
		delete(workspaces, k)
	}
	wsMu.Unlock()

	r.Execute("TeamCreate", map[string]interface{}{"team_name": "dup"})
	_, err := r.Execute("TeamCreate", map[string]interface{}{"team_name": "dup"})
	if err == nil {
		t.Error("expected error creating duplicate team")
	}
}

func TestTeamDeleteNonexistent(t *testing.T) {
	r := NewToolRegistry()
	_, err := r.Execute("TeamDelete", map[string]interface{}{"team_name": "ghost"})
	if err == nil {
		t.Error("expected error deleting nonexistent team")
	}
}

func TestTeamCreateAndDelete(t *testing.T) {
	r := NewToolRegistry()
	wsMu.Lock()
	for k := range workspaces {
		delete(workspaces, k)
	}
	wsMu.Unlock()

	_, err := r.Execute("TeamCreate", map[string]interface{}{"team_name": "temp"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	result, err := r.Execute("TeamDelete", map[string]interface{}{"team_name": "temp"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(result, "temp") {
		t.Errorf("delete result should mention name, got %q", result)
	}
}

func TestSendMessage_Direct(t *testing.T) {
	r := NewToolRegistry()
	result, err := r.Execute("SendMessage", map[string]interface{}{
		"to":      "agent-1",
		"message": "check the auth module",
		"summary": "auth check",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if !strings.Contains(result, "agent-1") {
		t.Errorf("result should mention recipient, got %q", result)
	}
	if !strings.Contains(result, "auth check") {
		t.Errorf("result should include summary, got %q", result)
	}
}

func TestSendMessage_Broadcast(t *testing.T) {
	r := NewToolRegistry()
	result, err := r.Execute("SendMessage", map[string]interface{}{
		"to":      "*",
		"message": "sync up everyone",
	})
	if err != nil {
		t.Fatalf("SendMessage broadcast: %v", err)
	}
	if !strings.Contains(result, "Broadcast") {
		t.Errorf("broadcast should mention 'Broadcast', got %q", result)
	}
}

func TestSendMessage_EmptyRecipient(t *testing.T) {
	r := NewToolRegistry()
	_, err := r.Execute("SendMessage", map[string]interface{}{
		"to":      "",
		"message": "hello",
	})
	if err == nil {
		t.Error("expected error with empty recipient")
	}
}

// =============================================================================
// LSP extension tests
// =============================================================================

func TestDetectLanguageFromExtension(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".go", "go"},
		{".py", "python"},
		{".ts", "typescript"},
		{".tsx", "typescriptreact"},
		{".js", "javascript"},
		{".jsx", "javascriptreact"},
		{".rs", "rust"},
		{".java", "java"},
		{".c", "c"},
		{".cpp", "cpp"},
		{".cs", "csharp"},
		{".rb", "ruby"},
		{".swift", "swift"},
		{".kt", "kotlin"},
		{".scala", "scala"},
		{".html", "html"},
		{".css", "css"},
		{".sql", "sql"},
		{".sh", "bash"},
		{".yaml", "yaml"},
		{".yml", "yaml"},
		{".json", "json"},
		{".md", "markdown"},
		{".zzz", "plaintext"},
		{"", "plaintext"},
	}
	for _, tt := range tests {
		got := DetectLanguageFromExtension(tt.ext)
		if got != tt.want {
			t.Errorf("DetectLanguageFromExtension(%q) = %q, want %q", tt.ext, got, tt.want)
		}
	}
}

func TestDetectLanguageFromExtension_CaseInsensitive(t *testing.T) {
	if got := DetectLanguageFromExtension(".GO"); got != "go" {
		t.Errorf("should be case-insensitive, got %q", got)
	}
	if got := DetectLanguageFromExtension(".Py"); got != "python" {
		t.Errorf("should be case-insensitive, got %q", got)
	}
}

func TestLSPTool_FileNotExist(t *testing.T) {
	r := NewToolRegistry()
	_, err := r.Execute("LSP", map[string]interface{}{
		"operation": "goToDefinition",
		"file_path": "/nonexistent/path/file.go",
		"line":      float64(1),
		"character": float64(1),
	})
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
