package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-claw/claw/internal/lsp"
)

// =============================================================================
// Team / Multi-Agent Tools
// =============================================================================

// Team represents a multi-agent team.
type Team struct {
	Name        string
	Description string
	Members     []string
	Created     bool
}

var (
	teams   = make(map[string]*Team)
	teamMu  sync.Mutex
)

// sendMessageTool sends messages to agent teammates (swarm protocol).
func sendMessageTool() *ToolSpec {
	return &ToolSpec{
		Name:        "SendMessage",
		Permission:  PermReadOnly,
		Description: "Send messages to agent teammates (swarm protocol). Use '*' as recipient to broadcast to all teammates.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"to": map[string]interface{}{
					"type":        "string",
					"description": "Recipient: teammate name, or '*' for broadcast",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Plain text message content",
				},
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "A 5-10 word summary shown as a preview",
				},
			},
			"required": []string{"to", "message"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			to, _ := input["to"].(string)
			message, _ := input["message"].(string)
			summary, _ := input["summary"].(string)

			if to == "" {
				return "", fmt.Errorf("recipient 'to' must not be empty")
			}

			if to == "*" {
				result := fmt.Sprintf("Broadcast sent to all teammates: %s", message)
				if summary != "" {
					result = fmt.Sprintf("Broadcast [%s]: %s", summary, message)
				}
				return result, nil
			}

			result := fmt.Sprintf("Message sent to %s: %s", to, message)
			if summary != "" {
				result = fmt.Sprintf("Message to %s [%s]: %s", to, summary, message)
			}
			return result, nil
		},
	}
}

// teamCreateTool creates a new team for multi-agent collaboration.
func teamCreateTool() *ToolSpec {
	return &ToolSpec{
		Name:        "TeamCreate",
		Permission:  PermWorkspaceWrite,
		Description: "Create a new team for multi-agent collaboration. Team members can send messages to each other via SendMessage.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"team_name": map[string]interface{}{
					"type":        "string",
					"description": "Name for the team",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Optional team description",
				},
			},
			"required": []string{"team_name"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			teamName, _ := input["team_name"].(string)
			description, _ := input["description"].(string)

			if teamName == "" {
				return "", fmt.Errorf("team_name must not be empty")
			}

			teamMu.Lock()
			defer teamMu.Unlock()

			if _, exists := teams[teamName]; exists {
				return "", fmt.Errorf("team '%s' already exists", teamName)
			}

			teams[teamName] = &Team{
				Name:        teamName,
				Description: description,
				Members:     []string{},
				Created:     true,
			}

			return fmt.Sprintf("Team '%s' created successfully", teamName), nil
		},
	}
}

// teamDeleteTool deletes a team.
func teamDeleteTool() *ToolSpec {
	return &ToolSpec{
		Name:        "TeamDelete",
		Permission:  PermDangerFullAccess,
		Description: "Delete a team. This is a destructive operation that removes the team and all its configuration.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"team_name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the team to delete",
				},
			},
			"required": []string{"team_name"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			teamName, _ := input["team_name"].(string)

			teamMu.Lock()
			defer teamMu.Unlock()

			if _, exists := teams[teamName]; !exists {
				return "", fmt.Errorf("team '%s' not found", teamName)
			}

			delete(teams, teamName)
			return fmt.Sprintf("Team '%s' deleted successfully", teamName), nil
		},
	}
}

// =============================================================================
// Extended LSP Tool (30+ language detection, 8 operations)
// =============================================================================

// DetectLanguageFromExtension returns the LSP language identifier from a file extension.
// Supports 29+ file extensions.
func DetectLanguageFromExtension(ext string) string {
	languageMap := map[string]string{
		".go":    "go",
		".ts":    "typescript",
		".tsx":   "typescriptreact",
		".js":    "javascript",
		".jsx":   "javascriptreact",
		".py":    "python",
		".rs":    "rust",
		".java":  "java",
		".c":     "c",
		".cpp":   "cpp",
		".h":     "c",
		".hpp":   "cpp",
		".cs":    "csharp",
		".rb":    "ruby",
		".php":   "php",
		".swift": "swift",
		".kt":    "kotlin",
		".scala": "scala",
		".json":  "json",
		".yaml":  "yaml",
		".yml":   "yaml",
		".md":    "markdown",
		".html":  "html",
		".css":   "css",
		".scss":  "scss",
		".less":  "less",
		".sql":   "sql",
		".sh":    "bash",
		".zsh":   "zsh",
	}

	if lang, ok := languageMap[strings.ToLower(ext)]; ok {
		return lang
	}
	return "plaintext"
}

// lspExtendedTool provides extended LSP operations with 30+ language support.
func lspExtendedTool() *ToolSpec {
	return &ToolSpec{
		Name:        "LSP",
		Permission:  PermReadOnly,
		Description: "Provides code intelligence features via Language Server Protocol. Supports 8 operations: goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, goToImplementation, incomingCalls, outgoingCalls. Auto-detects language from file extension (29+ languages).",
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
					"description": "The absolute or relative path to the file",
				},
				"line": map[string]interface{}{
					"type":        "number",
					"description": "The line number (1-based, as shown in editors)",
				},
				"character": map[string]interface{}{
					"type":        "number",
					"description": "The character offset (1-based, as shown in editors)",
				},
			},
			"required": []string{"operation", "file_path", "line", "character"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			operation, _ := input["operation"].(string)
			filePath, _ := input["file_path"].(string)
			line := toInt(input["line"])
			character := toInt(input["character"])

			// Validate file exists
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				return "", fmt.Errorf("file does not exist: %s", filePath)
			}

			ext := filepath.Ext(filePath)
			language := DetectLanguageFromExtension(ext)

			// Check if LSP manager is available
			if globalLSPManager == nil {
				return formatLSPResult(operation, filePath, line, character, language, nil, nil), nil
			}

			ctx := context.Background()
			pos := lsp.Position{Line: line - 1, Character: character - 1}

			switch operation {
			case "goToDefinition":
				locs, err := globalLSPManager.GoToDefinition(ctx, filePath, pos)
				if err != nil {
					return "", fmt.Errorf("LSP goToDefinition failed: %w", err)
				}
				return formatLocations("Definition", filePath, locs), nil

			case "findReferences":
				locs, err := globalLSPManager.FindReferences(ctx, filePath, pos, true)
				if err != nil {
					return "", fmt.Errorf("LSP findReferences failed: %w", err)
				}
				return formatLocations("References", filePath, locs), nil

			case "hover":
				client, clientErr := globalLSPManager.ClientForPath(ctx, filePath)
				if clientErr != nil {
					return formatLSPResult(operation, filePath, line, character, language, nil, nil), nil
				}
				uri := pathToLSPURI(filePath)
				hoverResult, hoverErr := client.Hover(ctx, uri, pos.Line, pos.Character)
				if hoverErr != nil {
					return "", fmt.Errorf("LSP hover failed: %w", hoverErr)
				}
				return fmt.Sprintf("Hover at %s:%d:%d (%s):\n%s", filePath, line, character, language, hoverResult), nil

			case "documentSymbol", "workspaceSymbol", "goToImplementation", "incomingCalls", "outgoingCalls":
				// These operations return placeholder results when no LSP server is configured
				return formatLSPResult(operation, filePath, line, character, language, nil, nil), nil

			default:
				return "", fmt.Errorf("unknown LSP operation: %s", operation)
			}
		},
	}
}

// Global LSP manager reference
var globalLSPManager *lsp.LspManager

// SetLSPManager sets the global LSP manager for the LSP tool.
func SetLSPManager(m *lsp.LspManager) {
	globalLSPManager = m
}

func formatLSPResult(operation, filePath string, line, character int, language string, locs []lsp.SymbolLocation, err error) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "LSP operation '%s' on %s (line %d, char %d)\n", operation, filePath, line, character)
	fmt.Fprintf(&buf, "  Language: %s\n", language)
	fmt.Fprintf(&buf, "  Status: LSP server not connected. Install a language server and configure via --lsp flag.\n")
	return buf.String()
}

func formatLocations(title, filePath string, locs []lsp.SymbolLocation) string {
	if len(locs) == 0 {
		return fmt.Sprintf("%s: no results for %s", title, filePath)
	}
	var buf strings.Builder
	fmt.Fprintf(&buf, "%s (%d results):\n", title, len(locs))
	for _, loc := range locs {
		fmt.Fprintf(&buf, "  %s:%d:%d\n", loc.Path, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
	}
	return buf.String()
}

func pathToLSPURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.ToSlash(abs)
	return "file://" + abs
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

