package tools

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// multiEditTool applies multiple edits to a single file in one invocation.
func multiEditTool() *ToolSpec {
	return &ToolSpec{
		Name:       "MultiEdit",
		Permission: PermWorkspaceWrite,
		Description: "Apply multiple string replacements to a single file in one invocation. Each edit replaces old_string with new_string. Edits are applied sequentially. Use this when you need to make several changes to the same file - it is more efficient than calling Edit multiple times. Always read the file first to verify the exact strings to replace.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{"type": "string", "description": "Absolute path to the file to edit"},
				"edits": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]interface{}{
							"old_string":  map[string]interface{}{"type": "string", "description": "Text to replace"},
							"new_string":  map[string]interface{}{"type": "string", "description": "Replacement text"},
							"replace_all": map[string]interface{}{"type": "boolean", "default": false, "description": "Replace all occurrences"},
						},
						"required": []string{"old_string", "new_string"},
					},
					"description": "Array of edits to apply sequentially",
					"minItems":    1,
				},
			},
			"required": []string{"file_path", "edits"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			path, _ := input["file_path"].(string)
			editsRaw, ok := input["edits"].([]interface{})
			if !ok || len(editsRaw) == 0 {
				return "", fmt.Errorf("edits array is required and must not be empty")
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("failed to read %s: %w", path, err)
			}
			content := string(data)
			totalReplacements := 0

			for i, editRaw := range editsRaw {
				edit, ok := editRaw.(map[string]interface{})
				if !ok {
					return "", fmt.Errorf("edit[%d] is not an object", i)
				}
				oldStr, _ := edit["old_string"].(string)
				newStr, _ := edit["new_string"].(string)
				replaceAll := false
				if r, ok := edit["replace_all"].(bool); ok {
					replaceAll = r
				}

				count := strings.Count(content, oldStr)
				if count == 0 {
					return "", fmt.Errorf("edit[%d]: old_string not found in %s", i, path)
				}
				if count > 1 && !replaceAll {
					return "", fmt.Errorf("edit[%d]: old_string found %d times; use replace_all", i, count)
				}

				if replaceAll {
					content = strings.ReplaceAll(content, oldStr, newStr)
				} else {
					content = strings.Replace(content, oldStr, newStr, 1)
				}
				totalReplacements += count
			}

			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return "", err
			}

			return fmt.Sprintf("<file_result>\n  <path>%s</path>\n  <edits>%d</edits>\n  <total_replacements>%d</total_replacements>\n</file_result>",
				path, len(editsRaw), totalReplacements), nil
		},
	}
}

// todoReadTool returns the current todo list as a tool the LLM can call.
func todoReadTool() *ToolSpec {
	return &ToolSpec{
		Name:        "TodoRead",
		Permission:  PermReadOnly,
		Description: "Read the current todo list. Returns all todo items with their content, status, and active form.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties":          map[string]interface{}{},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			items := GetTodos()
			if len(items) == 0 {
				return "No todo items.", nil
			}
			var buf strings.Builder
			for i, item := range items {
				status := item.Status
				if status == "" {
					status = "pending"
				}
				active := item.ActiveForm
				if active == "" {
					active = item.Content
				}
				fmt.Fprintf(&buf, "%d. [%s] %s (%s)\n", i+1, status, item.Content, active)
			}
			return buf.String(), nil
		},
	}
}

// clearScreenTool clears the terminal screen.
func clearScreenTool() *ToolSpec {
	return &ToolSpec{
		Name:        "ClearScreen",
		Permission:  PermReadOnly,
		Description: "Clear the terminal screen.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties":          map[string]interface{}{},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			if globalClearScreenFunc != nil {
				globalClearScreenFunc()
			}
			// ANSI escape sequence to clear screen and move cursor to home
			return "\033[2J\033[H", nil
		},
	}
}

// statusLineTool updates the status line display.
func statusLineTool() *ToolSpec {
	return &ToolSpec{
		Name:        "StatusLine",
		Permission:  PermReadOnly,
		Description: "Update the status line in the terminal UI to show persistent status information to the user. Use to display current operation, progress, or context. The status line persists between interactions.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"message": map[string]interface{}{"type": "string", "description": "Status message to display"},
				"type":    map[string]interface{}{"type": "string", "enum": []string{"info", "warning", "error", "success"}, "description": "Status type (default: info)"},
			},
			"required": []string{"message"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			message, _ := input["message"].(string)
			statusType, _ := input["type"].(string)
			if statusType == "" {
				statusType = "info"
			}
			if globalStatusLineFunc != nil {
				globalStatusLineFunc(message, statusType)
			}
			return fmt.Sprintf("Status line updated [%s]: %s", statusType, message), nil
		},
	}
}

// Global callbacks for clear screen and status line
var (
	globalClearScreenFunc func()
	globalStatusLineFunc  func(message, statusType string)
)

// SetClearScreenFunc sets the callback for clearing the screen.
func SetClearScreenFunc(fn func()) {
	globalClearScreenFunc = fn
}

// SetStatusLineFunc sets the callback for updating the status line.
func SetStatusLineFunc(fn func(string, string)) {
	globalStatusLineFunc = fn
}

// isImageFile checks if a file path has an image extension.
func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return true
	}
	return false
}

// isPDFFile checks if a file path has a PDF extension.
func isPDFFile(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".pdf"
}

// encodeImageAsBase64 reads an image file and returns its base64-encoded content with media type.
func encodeImageAsBase64(path string) (mediaType string, encoded string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("failed to read image %s: %w", path, err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		mediaType = "image/png"
	case ".jpg", ".jpeg":
		mediaType = "image/jpeg"
	case ".gif":
		mediaType = "image/gif"
	case ".webp":
		mediaType = "image/webp"
	case ".bmp":
		mediaType = "image/bmp"
	case ".svg":
		mediaType = "image/svg+xml"
	default:
		mediaType = "application/octet-stream"
	}

	encoded = base64.StdEncoding.EncodeToString(data)
	return mediaType, encoded, nil
}
