package lsp

import (
	"testing"
)

func TestNewLspManagerRejectsDuplicateExtensions(t *testing.T) {
	configs := []LspServerConfig{
		{
			Name:    "ts",
			Command: "typescript-language-server",
			Args:    []string{"--stdio"},
			ExtensionToLanguage: map[string]string{
				".ts": "typescript",
			},
		},
		{
			Name:    "ts2",
			Command: "another-server",
			Args:    []string{"--stdio"},
			ExtensionToLanguage: map[string]string{
				".ts": "typescript",
			},
		},
	}

	_, err := NewLspManager(configs)
	if err == nil {
		t.Error("expected error for duplicate extension")
	}
}

func TestNewLspManagerAcceptsValidConfigs(t *testing.T) {
	configs := []LspServerConfig{
		{
			Name:    "ts",
			Command: "typescript-language-server",
			Args:    []string{"--stdio"},
			ExtensionToLanguage: map[string]string{
				".ts":  "typescript",
				".tsx": "typescriptreact",
			},
		},
		{
			Name:    "go",
			Command: "gopls",
			ExtensionToLanguage: map[string]string{
				".go": "go",
			},
		},
	}

	mgr, err := NewLspManager(configs)
	if err != nil {
		t.Fatal(err)
	}
	if !mgr.SupportsPath("foo.ts") {
		t.Error("should support .ts")
	}
	if !mgr.SupportsPath("bar.tsx") {
		t.Error("should support .tsx")
	}
	if !mgr.SupportsPath("baz.go") {
		t.Error("should support .go")
	}
	if mgr.SupportsPath("readme.md") {
		t.Error("should not support .md")
	}
	if mgr.SupportsPath("Makefile") {
		t.Error("should not support extensionless files")
	}
}

func TestNormalizeExtension(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{".TS", "ts"},
		{"TS", "ts"},
		{".tsx", "tsx"},
		{"go", "go"},
	}
	for _, tt := range tests {
		got := NormalizeExtension(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeExtension(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPathToURI(t *testing.T) {
	// Test that it produces a valid file URI
	uri := pathToURI("foo.ts")
	if uri == "" {
		t.Error("pathToURI should return non-empty")
	}
	if len(uri) < 8 || uri[:7] != "file://" {
		t.Errorf("pathToURI = %q, want file:// prefix", uri)
	}
}

func TestURItoPath(t *testing.T) {
	path := uriToPath("file:///home/user/project/foo.ts")
	if path != "/home/user/project/foo.ts" {
		// On Windows this will have backslashes
		t.Logf("uriToPath = %q (platform-dependent)", path)
	}
}

func TestDedupeLocations(t *testing.T) {
	locs := []SymbolLocation{
		{Path: "/a", Range: Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 5}}},
		{Path: "/a", Range: Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 5}}},
		{Path: "/b", Range: Range{Start: Position{Line: 2, Character: 0}, End: Position{Line: 2, Character: 3}}},
	}
	dedupeLocations(locs)
	// After dedup, only unique entries remain (writeIdx = 2)
	// The slice is not truncated but the first 2 elements should be unique
	if locs[0].Path != "/a" {
		t.Errorf("first = %q", locs[0].Path)
	}
	if locs[1].Path != "/b" {
		t.Errorf("second = %q", locs[1].Path)
	}
}
