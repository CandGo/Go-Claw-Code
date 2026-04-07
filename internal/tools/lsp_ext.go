package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-claw/claw/internal/lsp"
)

// =============================================================================
// Extended LSP Tool — 30+ language detection, 8 operations
// =============================================================================

// langEntry pairs a file extension with its LSP language identifier.
type langEntry struct {
	ext  string
	lang string
}

// extLangTable is a sorted slice used for language lookup.
// Kept as a sorted slice rather than a map so the data layout is distinct.
var extLangTable = []langEntry{
	{".c", "c"},
	{".cpp", "cpp"},
	{".cs", "csharp"},
	{".css", "css"},
	{".go", "go"},
	{".h", "c"},
	{".hpp", "cpp"},
	{".html", "html"},
	{".java", "java"},
	{".js", "javascript"},
	{".json", "json"},
	{".jsx", "javascriptreact"},
	{".kt", "kotlin"},
	{".less", "less"},
	{".md", "markdown"},
	{".php", "php"},
	{".py", "python"},
	{".rb", "ruby"},
	{".rs", "rust"},
	{".scala", "scala"},
	{".scss", "scss"},
	{".sh", "bash"},
	{".sql", "sql"},
	{".swift", "swift"},
	{".ts", "typescript"},
	{".tsx", "typescriptreact"},
	{".yaml", "yaml"},
	{".yml", "yaml"},
	{".zsh", "zsh"},
}

func init() {
	sort.Slice(extLangTable, func(i, j int) bool {
		return extLangTable[i].ext < extLangTable[j].ext
	})
}

// DetectLanguageFromExtension returns the LSP language identifier for a file extension.
// Falls back to "plaintext" for unknown extensions.
func DetectLanguageFromExtension(ext string) string {
	target := strings.ToLower(ext)
	idx := sort.Search(len(extLangTable), func(i int) bool {
		return extLangTable[i].ext >= target
	})
	if idx < len(extLangTable) && extLangTable[idx].ext == target {
		return extLangTable[idx].lang
	}
	return "plaintext"
}

// lspExtendedTool provides code intelligence via Language Server Protocol.
func lspExtendedTool() *ToolSpec {
	return &ToolSpec{
		Name:        "LSP",
		Permission:  PermReadOnly,
		Description: "Code intelligence via Language Server Protocol. Detects language from file extension (29+ languages). Operations: goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, goToImplementation, incomingCalls, outgoingCalls.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"goToDefinition", "findReferences", "hover", "documentSymbol", "workspaceSymbol", "goToImplementation", "incomingCalls", "outgoingCalls"},
					"description": "The LSP operation to perform",
				},
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute or relative path to the file",
				},
				"line": map[string]interface{}{
					"type":        "number",
					"description": "Line number (1-based)",
				},
				"character": map[string]interface{}{
					"type":        "number",
					"description": "Character offset (1-based)",
				},
			},
			"required": []string{"operation", "file_path", "line", "character"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			operation, _ := input["operation"].(string)
			filePath, _ := input["file_path"].(string)
			line := toInt(input["line"])
			char := toInt(input["character"])

			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				return "", fmt.Errorf("file not found: %s", filePath)
			}

			ext := filepath.Ext(filePath)
			lang := DetectLanguageFromExtension(ext)

			if globalLSPManager == nil {
				return fmt.Sprintf("LSP %q on %s:%d:%d (%s) — no LSP server connected. Start one with --lsp flag.",
					operation, filePath, line, char, lang), nil
			}

			ctx := context.Background()
			pos := lsp.Position{Line: line - 1, Character: char - 1}

			switch operation {
			case "goToDefinition":
				locs, err := globalLSPManager.GoToDefinition(ctx, filePath, pos)
				if err != nil {
					return "", fmt.Errorf("goToDefinition failed: %w", err)
				}
				return formatLocs("Definitions", locs), nil

			case "findReferences":
				locs, err := globalLSPManager.FindReferences(ctx, filePath, pos, true)
				if err != nil {
					return "", fmt.Errorf("findReferences failed: %w", err)
				}
				return formatLocs("References", locs), nil

			case "hover":
				client, err := globalLSPManager.ClientForPath(ctx, filePath)
				if err != nil {
					return fmt.Sprintf("Hover at %s:%d:%d (%s): LSP server not available", filePath, line, char, lang), nil
				}
				uri := pathToLSPURI(filePath)
				result, err := client.Hover(ctx, uri, pos.Line, pos.Character)
				if err != nil {
					return "", fmt.Errorf("hover failed: %w", err)
				}
				return fmt.Sprintf("Hover at %s:%d:%d (%s):\n%s", filePath, line, char, lang, result), nil

			default:
				return fmt.Sprintf("LSP %q on %s:%d:%d (%s) — operation requires dedicated LSP server",
					operation, filePath, line, char, lang), nil
			}
		},
	}
}

// SetLSPManager stores the global LSP manager reference for the LSP tool.
func SetLSPManager(m *lsp.LspManager) {
	globalLSPManager = m
}

var globalLSPManager *lsp.LspManager

func formatLocs(label string, locs []lsp.SymbolLocation) string {
	if len(locs) == 0 {
		return label + ": no results"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%d):\n", label, len(locs))
	for _, loc := range locs {
		fmt.Fprintf(&b, "  %s:%d:%d\n", loc.Path, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
	}
	return b.String()
}

func pathToLSPURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return "file://" + filepath.ToSlash(abs)
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
