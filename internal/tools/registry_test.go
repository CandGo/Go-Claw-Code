package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewToolRegistryHasAllTools(t *testing.T) {
	r := NewToolRegistry()

	expectedTools := []string{
		"bash", "read_file", "write_file", "edit_file",
		"glob", "grep",
		"WebFetch", "WebSearch",
		"TodoWrite", "Agent", "Skill",
		"NotebookEdit", "sleep", "ToolSearch",
		"SendUserMessage",
		"CronCreate", "CronDelete",
		"EnterWorktree", "ExitWorktree",
		"Config", "StructuredOutput", "REPL", "PowerShell",
	}

	for _, name := range expectedTools {
		if _, ok := r.specs[name]; !ok {
			t.Errorf("tool %q not found in registry", name)
		}
	}
}

func TestRegistryExecuteUnknown(t *testing.T) {
	r := NewToolRegistry()
	_, err := r.Execute("nonexistent_tool", nil)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestRegistryAvailableTools(t *testing.T) {
	r := NewToolRegistry()
	defs := r.AvailableTools()
	if len(defs) < 20 {
		t.Errorf("AvailableTools() returned %d tools, want >= 20", len(defs))
	}
	for _, d := range defs {
		if d.Name == "" {
			t.Error("tool definition has empty name")
		}
		if d.InputSchema == nil {
			t.Errorf("tool %q has nil InputSchema", d.Name)
		}
	}
}

func TestRegisterDynamic(t *testing.T) {
	r := NewToolRegistry()
	called := false
	r.RegisterDynamic("custom_tool", "A custom tool", map[string]interface{}{
		"type": "object",
	}, func(input map[string]interface{}) (string, error) {
		called = true
		return "custom result", nil
	})

	result, err := r.Execute("custom_tool", map[string]interface{}{})
	if err != nil {
		t.Fatalf("Execute custom_tool failed: %v", err)
	}
	if !called {
		t.Error("custom tool handler not called")
	}
	if result != "custom result" {
		t.Errorf("custom tool result = %q, want 'custom result'", result)
	}
}

func TestBashTool(t *testing.T) {
	r := NewToolRegistry()
	result, err := r.Execute("Bash", map[string]interface{}{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("bash tool failed: %v", err)
	}
	// Windows cmd.exe outputs \r\n, Unix bash outputs \n
	result = strings.TrimRight(result, "\r\n")
	if result != "hello" {
		t.Errorf("bash echo result = %q, want 'hello'", result)
	}
}

func TestReadFileTool(t *testing.T) {
	// Create a temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := "line1\nline2\nline3\n"
	os.WriteFile(tmpFile, []byte(content), 0644)

	r := NewToolRegistry()
	result, err := r.Execute("Read", map[string]interface{}{
		"path": tmpFile,
	})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	if result == "" {
		t.Error("read_file returned empty string")
	}
	if !contains(result, "line1") {
		t.Error("read_file output missing file content")
	}
}

func TestReadFileWithOffsetLimit(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(tmpFile, []byte("a\nb\nc\nd\ne\n"), 0644)

	r := NewToolRegistry()
	result, err := r.Execute("Read", map[string]interface{}{
		"path":   tmpFile,
		"offset": float64(1),
		"limit":  float64(2),
	})
	if err != nil {
		t.Fatalf("read_file with offset/limit failed: %v", err)
	}
	if !contains(result, "b") || !contains(result, "c") {
		t.Errorf("read_file offset/limit result = %q", result)
	}
}

func TestWriteFileTool(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "newfile.txt")

	r := NewToolRegistry()
	result, err := r.Execute("Write", map[string]interface{}{
		"path": tmpFile,
			"content": "hello world",
	})
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}
	if !contains(result, "wrote") {
		t.Errorf("write_file result should mention wrote: %q", result)
	}

	// Verify file was written
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("file content = %q, want 'hello world'", string(data))
	}
}

func TestEditFileTool(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "edit.txt")
	os.WriteFile(tmpFile, []byte("foo bar baz"), 0644)

	r := NewToolRegistry()
	result, err := r.Execute("Edit", map[string]interface{}{
		"path": tmpFile,
			"old_string": "bar",
		"new_string": "qux",
	})
	if err != nil {
		t.Fatalf("edit_file failed: %v", err)
	}
	if !contains(result, "replaced") {
		t.Errorf("edit_file result should mention replaced: %q", result)
	}

	data, _ := os.ReadFile(tmpFile)
	if string(data) != "foo qux baz" {
		t.Errorf("after edit: %q, want 'foo qux baz'", string(data))
	}
}

func TestEditFileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "edit.txt")
	os.WriteFile(tmpFile, []byte("foo bar baz"), 0644)

	r := NewToolRegistry()
	_, err := r.Execute("Edit", map[string]interface{}{
		"path": tmpFile,
			"old_string": "nonexistent",
		"new_string": "x",
	})
	if err == nil {
		t.Error("expected error when old_string not found")
	}
}

func TestGlobTool(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(tmpDir, "c.txt"), []byte(""), 0644)

	r := NewToolRegistry()
	result, err := r.Execute("Glob", map[string]interface{}{
		"pattern": "*.go",
		"path":    tmpDir,
	})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if !contains(result, "a.go") || !contains(result, "b.go") {
		t.Errorf("glob result = %q", result)
	}
	if contains(result, "c.txt") {
		t.Error("glob should not match c.txt")
	}
}

func TestGrepToolFilesWithMatches(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello world\nfoo bar"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("no match here"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "c.txt"), []byte("hello again"), 0644)

	r := NewToolRegistry()
	result, err := r.Execute("Grep", map[string]interface{}{
		"pattern":     "hello",
		"path":        tmpDir,
		"output_mode": "files_with_matches",
	})
	if err != nil {
		t.Fatalf("grep failed: %v", err)
	}
	if !contains(result, "a.txt") {
		t.Error("grep should find a.txt")
	}
	if !contains(result, "c.txt") {
		t.Error("grep should find c.txt")
	}
	if contains(result, "b.txt") {
		t.Error("grep should not find b.txt")
	}
}

func TestGrepToolCountMode(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello\nhello world\n"), 0644)

	r := NewToolRegistry()
	result, err := r.Execute("Grep", map[string]interface{}{
		"pattern":     "hello",
		"path":        tmpDir,
		"output_mode": "count",
	})
	if err != nil {
		t.Fatalf("grep count failed: %v", err)
	}
	if !contains(result, "a.txt") {
		t.Errorf("grep count result missing a.txt: %q", result)
	}
}

func TestGrepToolCaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("HELLO World"), 0644)

	r := NewToolRegistry()
	result, err := r.Execute("Grep", map[string]interface{}{
		"pattern": "hello",
		"path":    tmpDir,
		"-i":      true,
	})
	if err != nil {
		t.Fatalf("grep -i failed: %v", err)
	}
	if !contains(result, "a.txt") {
		t.Error("grep -i should find match in a.txt")
	}
}

func TestSleepTool(t *testing.T) {
	r := NewToolRegistry()
	result, err := r.Execute("sleep", map[string]interface{}{
		"duration_ms": float64(10),
	})
	if err != nil {
		t.Fatalf("sleep failed: %v", err)
	}
	if !contains(result, "Slept") {
		t.Errorf("sleep result = %q", result)
	}
}

func TestSleepToolTooLong(t *testing.T) {
	r := NewToolRegistry()
	_, err := r.Execute("sleep", map[string]interface{}{
		"duration_ms": float64(60000),
	})
	if err == nil {
		t.Error("expected error for sleep > 30s")
	}
}

func TestToolFilters(t *testing.T) {
	r := NewToolRegistry()

	// ReadOnly filter
	readOnly := ReadOnlyFilter()
	readOnlyDefs := r.FilterTools(readOnly)
	for _, d := range readOnlyDefs {
		if d.Name == "bash" || d.Name == "write_file" || d.Name == "edit_file" {
			t.Errorf("ReadOnlyFilter should exclude %q", d.Name)
		}
	}

	// Verification filter should include bash but not write_file
	verification := VerificationFilter()
	verificationDefs := r.FilterTools(verification)
	hasBash := false
	for _, d := range verificationDefs {
		if d.Name == "bash" {
			hasBash = true
		}
		if d.Name == "write_file" {
			t.Error("VerificationFilter should exclude write_file")
		}
	}
	if !hasBash {
		t.Error("VerificationFilter should include bash")
	}

	// All tools filter returns nil (no filtering)
	allDefs := r.FilterTools(AllToolsFilter())
	if len(allDefs) != len(r.AvailableTools()) {
		t.Error("AllToolsFilter should not filter")
	}
}

func TestFilterForAgentType(t *testing.T) {
	tests := []struct {
		agentType   string
		shouldHave  []string
		shouldntHave []string
	}{
		{"Explore", []string{"read_file", "glob", "grep"}, []string{"bash", "write_file", "edit_file"}},
		{"Plan", []string{"read_file", "Agent", "TodoWrite"}, []string{"bash", "write_file"}},
		{"Verification", []string{"bash", "read_file", "grep"}, []string{"write_file", "edit_file"}},
		{"general-purpose", []string{"bash", "write_file", "edit_file"}, nil},
	}
	for _, tt := range tests {
		filter := FilterForAgentType(tt.agentType)
		r := NewToolRegistry()
		filtered := r.FilteredRegistry(filter)
		for _, name := range tt.shouldHave {
			if _, ok := filtered.specs[name]; !ok {
				t.Errorf("FilterForAgentType(%q): should have %q", tt.agentType, name)
			}
		}
		for _, name := range tt.shouldntHave {
			if _, ok := filtered.specs[name]; ok {
				t.Errorf("FilterForAgentType(%q): should NOT have %q", tt.agentType, name)
			}
		}
	}
}

func TestMaxIterationsForAgent(t *testing.T) {
	tests := []struct {
		agentType string
		want      int
	}{
		{"Explore", 5},
		{"Plan", 3},
		{"Verification", 10},
		{"claw-guide", 8},
		{"statusline-setup", 10},
		{"general-purpose", 32},
		{"unknown", 32},
	}
	for _, tt := range tests {
		got := MaxIterationsForAgent(tt.agentType)
		if got != tt.want {
			t.Errorf("MaxIterationsForAgent(%q) = %d, want %d", tt.agentType, got, tt.want)
		}
	}
}

func TestTodoWriteTool(t *testing.T) {
	r := NewToolRegistry()
	result, err := r.Execute("TodoWrite", map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"content": "Task 1",
				"status":  "pending",
			},
			map[string]interface{}{
				"content": "Task 2",
				"status":  "in_progress",
				"activeForm": "Working on Task 2",
			},
		},
	})
	if err != nil {
		t.Fatalf("TodoWrite failed: %v", err)
	}
	if !contains(result, "2") {
		t.Errorf("TodoWrite result = %q", result)
	}

	todos := GetTodos()
	if len(todos) != 2 {
		t.Fatalf("GetTodos() returned %d items, want 2", len(todos))
	}
	if todos[0].Content != "Task 1" {
		t.Errorf("todos[0].Content = %q", todos[0].Content)
	}
	if todos[1].Status != "in_progress" {
		t.Errorf("todos[1].Status = %q", todos[1].Status)
	}
	if todos[1].ActiveForm != "Working on Task 2" {
		t.Errorf("todos[1].ActiveForm = %q", todos[1].ActiveForm)
	}
}

func TestGetSpec(t *testing.T) {
	r := NewToolRegistry()
	spec, ok := r.GetSpec("bash")
	if !ok {
		t.Fatal("GetSpec(bash) returned false")
	}
	if spec.Name != "bash" {
		t.Errorf("spec.Name = %q", spec.Name)
	}
	if spec.Permission != PermDangerFullAccess {
		t.Errorf("bash permission = %d, want PermDangerFullAccess", spec.Permission)
	}

	_, ok = r.GetSpec("nonexistent")
	if ok {
		t.Error("GetSpec(nonexistent) should return false")
	}
}

func TestToolAliases(t *testing.T) {
	r := NewToolRegistry()

	// Test that alias names resolve to the correct registered tool
	aliasTests := []struct {
		alias      string
		registered string
	}{
		{"Read", "read_file"},
		{"Write", "write_file"},
		{"Edit", "edit_file"},
		{"Bash", "bash"},
		{"Glob", "glob"},
		{"Grep", "grep"},
		{"run_command", "bash"},
		{"shell", "bash"},
		{"create_file", "write_file"},
		{"search_files", "grep"},
		{"find_files", "glob"},
		{"web_fetch", "WebFetch"},
		{"web_search", "WebSearch"},
		{"read_file", "read_file"},    // exact name still works
		{"bash", "bash"},              // exact name still works
	}

	for _, tc := range aliasTests {
		if tc.registered == "read_file" {
			tmpFile := filepath.Join(t.TempDir(), "alias_test.txt")
			os.WriteFile(tmpFile, []byte("hello"), 0644)
			defer os.Remove(tmpFile)
			result, err := r.Execute(tc.alias, map[string]interface{}{
				"path": tmpFile,
			})
			if err != nil {
				t.Errorf("Execute(%q → %q) failed: %v", tc.alias, tc.registered, err)
			}
			if result != "" && !strings.Contains(result, "hello") {
				t.Errorf("Execute(%q) result should contain 'hello', got: %q", tc.alias, result)
			}
		} else if tc.registered == "bash" {
			_, err := r.Execute(tc.alias, map[string]interface{}{
				"command": "echo hello",
			})
			if err != nil {
				t.Errorf("Execute(%q → %q) failed: %v", tc.alias, tc.registered, err)
			}
		} else {
			_, err := r.Execute(tc.alias, map[string]interface{}{})
			if err != nil && filepath.Base(err.Error()) == "unknown tool: "+tc.alias {
				t.Errorf("Execute(%q) returned unknown tool error: %v", tc.alias, err)
			}
		}
	}

	// Verify unknown aliases still fail
	_, err := r.Execute("totally_fake_tool", map[string]interface{}{})
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestCaseInsensitiveMatch(t *testing.T) {
	r := NewToolRegistry()

	// Test case-insensitive matching (e.g. "BASH" should match "bash")
	_, err := r.Execute("BASH", map[string]interface{}{
		"command": "echo hello",
	})
	if err != nil {
		t.Errorf("Execute('BASH') should match 'bash' case-insensitively: %v", err)
	}

	// Create the temp file first
	tmpFile := filepath.Join(t.TempDir(), "case_test.txt")
	os.WriteFile(tmpFile, []byte("case test"), 0644)
	defer os.Remove(tmpFile)

	_, err = r.Execute("READ_FILE", map[string]interface{}{
		"path": tmpFile,
	})
	if err != nil {
		t.Errorf("Execute('READ_FILE') should match 'read_file' case-insensitively: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
