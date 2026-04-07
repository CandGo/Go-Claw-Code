package tools

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestBashTool_BasicCommand verifies executing a simple echo command.
func TestBashTool_BasicCommand(t *testing.T) {
	r := NewToolRegistry()
	result, err := r.Execute("bash", map[string]interface{}{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("bash tool failed: %v", err)
	}
	// The bash handler trims output; expect "hello\n"
	if !strings.Contains(result, "hello") {
		t.Errorf("bash echo result = %q, want to contain 'hello'", result)
	}
}

// TestReadTool_ReadFile creates a temp file, reads it, and verifies content.
func TestReadTool_ReadFile(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "readme.txt")
	content := "line one\nline two\nline three\n"
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	r := NewToolRegistry()
	result, err := r.Execute("read_file", map[string]interface{}{
		"path": tmpFile,
	})
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	for _, want := range []string{"line one", "line two", "line three"} {
		if !strings.Contains(result, want) {
			t.Errorf("read_file result missing %q; got: %s", want, result)
		}
	}
}

// TestWriteTool_WriteFile writes to a temp path and verifies the file content.
func TestWriteTool_WriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "output.txt")

	r := NewToolRegistry()
	result, err := r.Execute("write_file", map[string]interface{}{
		"path": tmpFile,
			"content": "hello world",
	})
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}
		if !strings.Contains(result, "wrote") {
		t.Errorf("write_file result should mention wrote: %q", result)
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("file content = %q, want 'hello world'", string(data))
	}
}

// TestEditTool_Replace creates a file, edits a string, and verifies replacement.
func TestEditTool_Replace(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "edit.txt")
	os.WriteFile(tmpFile, []byte("foo bar baz qux"), 0644)

	r := NewToolRegistry()
	result, err := r.Execute("edit_file", map[string]interface{}{
		"path": tmpFile,
			"old_string": "bar baz",
		"new_string": "REPLACED",
	})
	if err != nil {
		t.Fatalf("edit_file failed: %v", err)
	}
		if !strings.Contains(result, "replaced") {
		t.Errorf("edit_file result should mention replaced: %q", result)
	}

	data, _ := os.ReadFile(tmpFile)
	if string(data) != "foo REPLACED qux" {
		t.Errorf("file content after edit = %q, want 'foo REPLACED qux'", string(data))
	}
}

// TestEditTool_ReplaceAll creates content with a repeated string, uses
// replace_all, and verifies all occurrences are replaced.
func TestEditTool_ReplaceAll(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		old      string
		new_     string
		expected string
	}{
		{
			name:     "multiple occurrences",
			content:  "alpha beta alpha gamma alpha",
			old:      "alpha",
			new_:     "Z",
			expected: "Z beta Z gamma Z",
		},
		{
			name:     "single occurrence with replace_all",
			content:  "only one match here",
			old:      "one",
			new_:     "TWO",
			expected: "only TWO match here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "replace_all.txt")
			os.WriteFile(tmpFile, []byte(tt.content), 0644)

			r := NewToolRegistry()
			_, err := r.Execute("edit_file", map[string]interface{}{
				"path": tmpFile,
			"old_string":  tt.old,
				"new_string":  tt.new_,
				"replace_all": true,
			})
			if err != nil {
				t.Fatalf("edit_file replace_all failed: %v", err)
			}

			data, _ := os.ReadFile(tmpFile)
			if string(data) != tt.expected {
				t.Errorf("content = %q, want %q", string(data), tt.expected)
			}
		})
	}
}

// TestGrepTool_Search creates temp files, greps for a pattern, and verifies matches.
func TestGrepTool_Search(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("hello world\nsecond line\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("no match here\n"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "c.txt"), []byte("hello again\nmore text\n"), 0644)

	tests := []struct {
		name        string
		outputMode  string
		pattern     string
		wantFiles   []string
		dontWant    []string
	}{
		{
			name:       "files_with_matches",
			outputMode: "files_with_matches",
			pattern:    "hello",
			wantFiles:  []string{"a.txt", "c.txt"},
			dontWant:   []string{"b.txt"},
		},
		{
			name:       "content mode",
			outputMode: "content",
			pattern:    "second",
			wantFiles:  []string{"a.txt"},
			dontWant:   []string{"b.txt", "c.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewToolRegistry()
			result, err := r.Execute("grep", map[string]interface{}{
				"pattern":     tt.pattern,
				"path":        tmpDir,
				"output_mode": tt.outputMode,
			})
			if err != nil {
				t.Fatalf("grep failed: %v", err)
			}
			for _, want := range tt.wantFiles {
				if !strings.Contains(result, want) {
					t.Errorf("grep result missing %q: %s", want, result)
				}
			}
			for _, dont := range tt.dontWant {
				if strings.Contains(result, dont) {
					t.Errorf("grep result should not contain %q: %s", dont, result)
				}
			}
		})
	}
}

// TestGlobTool_Pattern creates temp files, globs for a pattern, and verifies matches.
func TestGlobTool_Pattern(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "util.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "readme.md"), []byte("# readme"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "sub"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "sub", "deep.go"), []byte("package sub"), 0644)

	r := NewToolRegistry()
	result, err := r.Execute("glob", map[string]interface{}{
		"pattern": "*.go",
		"path":    tmpDir,
	})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	for _, want := range []string{"main.go", "util.go"} {
		if !strings.Contains(result, want) {
			t.Errorf("glob result missing %q: %s", want, result)
		}
	}
	if strings.Contains(result, "readme.md") {
		t.Error("glob should not match readme.md with *.go pattern")
	}
}

// TestToolFilter_ReadOnly verifies that the read-only filter includes read_file
// but excludes write_file.
func TestToolFilter_ReadOnly(t *testing.T) {
	r := NewToolRegistry()
	filter := ReadOnlyFilter()
	filtered := r.FilteredRegistry(filter)

	readOnlyExpected := []string{"read_file", "glob", "grep", "WebFetch", "WebSearch"}
	for _, name := range readOnlyExpected {
		if _, ok := filtered.specs[name]; !ok {
			t.Errorf("ReadOnlyFilter should include %q", name)
		}
	}

	writeTools := []string{"write_file", "edit_file", "bash"}
	for _, name := range writeTools {
		if _, ok := filtered.specs[name]; ok {
			t.Errorf("ReadOnlyFilter should exclude %q", name)
		}
	}
}

// TestToolFilter_AllTools verifies that nil filter includes everything.
func TestToolFilter_AllTools(t *testing.T) {
	r := NewToolRegistry()
	allDefs := r.FilterTools(AllToolsFilter())
	totalDefs := r.AvailableTools()

	if len(allDefs) != len(totalDefs) {
		t.Errorf("AllToolsFilter returned %d tools, want %d", len(allDefs), len(totalDefs))
	}
}

// resetCronJobs clears the global cron job store for isolated testing.
func resetCronJobs() {
	cronMu.Lock()
	defer cronMu.Unlock()
	cronJobs = make([]*CronJob, 0)
	cronNextID = 1
}

// TestCronCreateDelete creates a cron job, verifies it exists, deletes it, and verifies gone.
func TestCronCreateDelete(t *testing.T) {
	// Serialize cron tests to avoid races on the global store.
	var cronMu2 sync.Mutex
	cronMu2.Lock()
	defer cronMu2.Unlock()

	resetCronJobs()

	r := NewToolRegistry()

	// Create
	createResult, err := r.Execute("CronCreate", map[string]interface{}{
		"cron":   "*/5 * * * *",
		"prompt": "check status",
	})
	if err != nil {
		t.Fatalf("CronCreate failed: %v", err)
	}
	if !strings.Contains(createResult, "cron_1") {
		t.Errorf("CronCreate result should contain job id: %q", createResult)
	}

	// Verify it exists via list
	listResult, err := r.Execute("CronList", map[string]interface{}{})
	if err != nil {
		t.Fatalf("CronList failed: %v", err)
	}
	if !strings.Contains(listResult, "cron_1") {
		t.Errorf("CronList should show cron_1: %q", listResult)
	}

	// Delete
	deleteResult, err := r.Execute("CronDelete", map[string]interface{}{
		"id": "cron_1",
	})
	if err != nil {
		t.Fatalf("CronDelete failed: %v", err)
	}
	if !strings.Contains(deleteResult, "deleted") {
		t.Errorf("CronDelete result should mention deleted: %q", deleteResult)
	}

	// Verify it's gone
	listResult2, err := r.Execute("CronList", map[string]interface{}{})
	if err != nil {
		t.Fatalf("CronList after delete failed: %v", err)
	}
	if strings.Contains(listResult2, "cron_1") {
		t.Errorf("CronList should not show cron_1 after delete: %q", listResult2)
	}
}

// TestCronList creates multiple jobs, lists them, and verifies count.
func TestCronList(t *testing.T) {
	var cronMu2 sync.Mutex
	cronMu2.Lock()
	defer cronMu2.Unlock()

	resetCronJobs()

	r := NewToolRegistry()

	prompts := []struct {
		cron   string
		prompt string
	}{
		{"0 * * * *", "hourly check"},
		{"0 9 * * 1", "weekly Monday report"},
		{"*/10 * * * *", "every 10 min"},
	}
	for _, p := range prompts {
		_, err := r.Execute("CronCreate", map[string]interface{}{
			"cron":   p.cron,
			"prompt": p.prompt,
		})
		if err != nil {
			t.Fatalf("CronCreate failed for %q: %v", p.prompt, err)
		}
	}

	listResult, err := r.Execute("CronList", map[string]interface{}{})
	if err != nil {
		t.Fatalf("CronList failed: %v", err)
	}

	jobs := GetCronJobs()
	if len(jobs) != 3 {
		t.Errorf("expected 3 cron jobs, got %d", len(jobs))
	}
	for _, prompt := range []string{"hourly check", "weekly Monday report", "every 10 min"} {
		if !strings.Contains(listResult, prompt) {
			t.Errorf("CronList should contain %q: %s", prompt, listResult)
		}
	}
}
