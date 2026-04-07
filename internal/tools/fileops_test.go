package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAndWriteFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "read-write.txt")

	writeOut, err := WriteFile(path, "one\ntwo\nthree")
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if writeOut.Kind != "create" {
		t.Errorf("kind = %q, want create", writeOut.Kind)
	}

	offset := 1
	limit := 1
	readOut, err := ReadFile(path, &offset, &limit)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if readOut.File.Content != "two" {
		t.Errorf("content = %q, want %q", readOut.File.Content, "two")
	}
	if readOut.File.StartLine != 2 {
		t.Errorf("start_line = %d, want 2", readOut.File.StartLine)
	}
}

func TestWriteFileUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update.txt")

	_, err := WriteFile(path, "original")
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	writeOut, err := WriteFile(path, "updated")
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if writeOut.Kind != "update" {
		t.Errorf("kind = %q, want update", writeOut.Kind)
	}
	if writeOut.OriginalFile == nil || *writeOut.OriginalFile != "original" {
		t.Error("original_file should be 'original'")
	}
}

func TestEditFileContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edit.txt")
	_, _ = WriteFile(path, "alpha beta alpha")

	output, err := EditFile(path, "alpha", "omega", true)
	if err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}
	if !output.ReplaceAll {
		t.Error("replace_all should be true")
	}
	if output.OldString != "alpha" {
		t.Errorf("old_string = %q, want alpha", output.OldString)
	}
	if output.NewString != "omega" {
		t.Errorf("new_string = %q, want omega", output.NewString)
	}

	// Verify file was actually changed
	data, _ := os.ReadFile(path)
	if string(data) != "omega beta omega" {
		t.Errorf("file content = %q, want %q", string(data), "omega beta omega")
	}
}

func TestEditFileOldStringNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.txt")
	_, _ = WriteFile(path, "hello")

	_, err := EditFile(path, "missing", "replacement", false)
	if err == nil {
		t.Error("expected error when old_string not found")
	}
}

func TestEditFileSameStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "same.txt")
	_, _ = WriteFile(path, "hello")

	_, err := EditFile(path, "hello", "hello", false)
	if err == nil {
		t.Error("expected error when old_string == new_string")
	}
}

func TestGlobSearch(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("text"), 0644)
	os.WriteFile(filepath.Join(dir, "sub", "d.go"), []byte("package main"), 0644)

	out, err := GlobSearch("*.go", dir)
	if err != nil {
		t.Fatalf("GlobSearch failed: %v", err)
	}
	if out.NumFiles != 2 {
		t.Errorf("num_files = %d, want 2", out.NumFiles)
	}
}

func TestGrepSearch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "demo.go"), []byte("func main() {\n\tprintln(\"hello\")\n}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "other.go"), []byte("func other() {}\n"), 0644)

	out, err := GrepSearch(&GrepSearchInput{
		Pattern:    "hello",
		Path:       dir,
		OutputMode: "content",
		HeadLimit:  intPtr(10),
	})
	if err != nil {
		t.Fatalf("GrepSearch failed: %v", err)
	}
	if out.NumFiles != 1 {
		t.Errorf("num_files = %d, want 1", out.NumFiles)
	}
	if out.Content == nil || !containsStr(*out.Content, "hello") {
		t.Errorf("content should contain 'hello', got %v", out.Content)
	}
}

func TestGrepSearchCount(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("foo bar foo\nbaz foo\n"), 0644)

	out, err := GrepSearch(&GrepSearchInput{
		Pattern:    "foo",
		Path:       dir,
		OutputMode: "count",
	})
	if err != nil {
		t.Fatalf("GrepSearch failed: %v", err)
	}
	if out.NumMatches == nil || *out.NumMatches != 3 {
		t.Errorf("num_matches = %v, want 3", out.NumMatches)
	}
}

func TestMakePatch(t *testing.T) {
	patch := makePatch("line1\nline2", "line1\nline3")
	if len(patch) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(patch))
	}
	hunk := patch[0]
	if hunk.OldLines != 2 {
		t.Errorf("old_lines = %d, want 2", hunk.OldLines)
	}
	if hunk.NewLines != 2 {
		t.Errorf("new_lines = %d, want 2", hunk.NewLines)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
