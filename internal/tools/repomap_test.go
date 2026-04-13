package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoMap(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "pkg"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(
		"package main\n\nfunc main() {}\nfunc helper(x int) string { return \"\" }\n"), 0644)
	os.WriteFile(filepath.Join(dir, "pkg", "types.go"), []byte(
		"package pkg\n\ntype Config struct {\n  Name string\n}\nfunc NewConfig() *Config { return nil }\n"), 0644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hello"), 0644)

	mapStr, shown, total, _, trunc, _, err := RepoMap(dir, nil, nil)
	if err != nil {
		t.Fatalf("RepoMap failed: %v", err)
	}
	if shown != 3 {
		t.Errorf("files_shown = %d, want 3", shown)
	}
	if total != 3 {
		t.Errorf("files_total = %d, want 3", total)
	}
	if trunc {
		t.Error("should not be truncated")
	}
	if !strings.Contains(mapStr, "[pkg: main]") {
		t.Error("map should contain [pkg: main]")
	}
	if !strings.Contains(mapStr, "func main()") {
		t.Error("map should contain func main()")
	}
	if !strings.Contains(mapStr, "struct Config") {
		t.Error("map should contain struct Config")
	}
	if !strings.Contains(mapStr, "readme.md") {
		t.Error("map should contain readme.md")
	}
}

func TestRepoMapTruncation(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%d.go", i)),
			[]byte("package main\n"), 0644)
	}
	limit := 3
	_, shown, total, _, trunc, _, err := RepoMap(dir, nil, &limit)
	if err != nil {
		t.Fatalf("RepoMap failed: %v", err)
	}
	if !trunc {
		t.Error("should be truncated")
	}
	if shown != 3 {
		t.Errorf("files_shown = %d, want 3", shown)
	}
	if total != 10 {
		t.Errorf("files_total = %d, want 10", total)
	}
}

func TestExtractGoSymbols(t *testing.T) {
	code := `package mypkg

type Server struct {
    Addr string
}

type Handler interface {
    Serve() error
}

func NewServer(addr string) *Server { return nil }
func (s *Server) Start() error { return nil }
var Version = "1.0"
const MaxRetries = 3
`
	pkg, syms := extractGoSymbols(code)
	if pkg != "mypkg" {
		t.Errorf("pkg = %q, want mypkg", pkg)
	}
	if len(syms) < 5 {
		t.Errorf("got %d symbols, want at least 5: %v", len(syms), syms)
	}

	var hasStruct, hasInterface, hasNewServer, hasStart bool
	for _, s := range syms {
		if strings.Contains(s, "struct Server") {
			hasStruct = true
		}
		if strings.Contains(s, "interface Handler") {
			hasInterface = true
		}
		if strings.Contains(s, "NewServer") {
			hasNewServer = true
		}
		if strings.Contains(s, "Start") {
			hasStart = true
		}
	}
	if !hasStruct {
		t.Error("missing struct Server")
	}
	if !hasInterface {
		t.Error("missing interface Handler")
	}
	if !hasNewServer {
		t.Error("missing func NewServer")
	}
	if !hasStart {
		t.Error("missing func Start (method)")
	}
}

func TestExtractGenericSymbolsPython(t *testing.T) {
	code := `class MyClass:
    def __init__(self):
        pass
    def process(self, data):
        pass

async def fetch(url):
    pass
`
	syms := extractGenericSymbols(code, ".py")
	// Only top-level definitions are extracted (class MyClass, async func fetch)
	if len(syms) < 2 {
		t.Errorf("got %d symbols, want at least 2: %v", len(syms), syms)
	}
	hasClass := false
	hasFetch := false
	for _, s := range syms {
		if strings.Contains(s, "class MyClass") {
			hasClass = true
		}
		if strings.Contains(s, "async func fetch") {
			hasFetch = true
		}
	}
	if !hasClass {
		t.Error("missing class MyClass")
	}
	if !hasFetch {
		t.Error("missing async func fetch")
	}
}

func TestRepoMapIgnoreDirs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0755)
	os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "index.js"),
		[]byte("export function hello() {}"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\nfunc main() {}\n"), 0644)

	_, shown, total, _, _, _, err := RepoMap(dir, nil, nil)
	if err != nil {
		t.Fatalf("RepoMap failed: %v", err)
	}
	if total != 1 {
		t.Errorf("files_total = %d, want 1 (node_modules should be skipped)", total)
	}
	if shown != 1 {
		t.Errorf("files_shown = %d, want 1", shown)
	}
}
