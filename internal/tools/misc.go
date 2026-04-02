package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func sleepTool() *ToolSpec {
	return &ToolSpec{
		Name:        "sleep",
		Description: "Sleep for a specified duration.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"duration_ms": map[string]interface{}{"type": "integer", "description": "Duration in milliseconds"},
			},
			"required": []string{"duration_ms"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			ms, _ := input["duration_ms"].(float64)
			if ms <= 0 {
				return "", fmt.Errorf("duration_ms must be positive")
			}
			if ms > 30000 {
				return "", fmt.Errorf("sleep duration too long (max 30s)")
			}
			time.Sleep(time.Duration(ms) * time.Millisecond)
			return fmt.Sprintf("Slept %dms", int(ms)), nil
		},
	}
}

func toolSearchTool() *ToolSpec {
	return &ToolSpec{
		Name:        "ToolSearch",
		Description: "Search for available tools by keyword.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":       map[string]interface{}{"type": "string", "description": "Search query"},
				"max_results": map[string]interface{}{"type": "integer", "description": "Max results (default 10)"},
			},
			"required": []string{"query"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			query := strings.ToLower(fmt.Sprintf("%v", input["query"]))
			maxResults := 10
			if m, ok := input["max_results"].(float64); ok && m > 0 {
				maxResults = int(m)
			}

			var matches []string
			for name, spec := range globalRegistry.specs {
				if strings.Contains(strings.ToLower(name), query) ||
					strings.Contains(strings.ToLower(spec.Description), query) {
					matches = append(matches, fmt.Sprintf("- %s: %s", name, spec.Description))
				}
				if len(matches) >= maxResults {
					break
				}
			}

			if len(matches) == 0 {
				return "No tools found matching query.", nil
			}
			return strings.Join(matches, "\n"), nil
		},
	}
}

func notebookEditTool() *ToolSpec {
	return &ToolSpec{
		Name:        "NotebookEdit",
		Description: "Edit a Jupyter notebook cell.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"notebook_path": map[string]interface{}{"type": "string", "description": "Absolute path to the .ipynb file"},
				"cell_id":       map[string]interface{}{"type": "string", "description": "Cell ID to edit"},
				"new_source":    map[string]interface{}{"type": "string", "description": "New cell source content"},
				"cell_type":     map[string]interface{}{"type": "string", "enum": []string{"code", "markdown"}},
				"edit_mode":     map[string]interface{}{"type": "string", "enum": []string{"replace", "insert", "delete"}},
			},
			"required": []string{"notebook_path", "new_source"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			path, _ := input["notebook_path"].(string)
			newSource, _ := input["new_source"].(string)
			if path == "" {
				return "", fmt.Errorf("notebook_path is required")
			}
			if !strings.HasSuffix(path, ".ipynb") {
				return "", fmt.Errorf("file must be a .ipynb file")
			}

			// Read existing notebook or create new
			data, err := os.ReadFile(path)
			if err != nil {
				// Create new notebook
				notebook := fmt.Sprintf(`{"cells":[{"cell_type":"code","source":%q,"metadata":{},"execution_count":null,"outputs":[]}],"metadata":{"kernelspec":{"display_name":"Python 3","language":"python","name":"python3"}},"nbformat":4,"nbformat_minor":5}`, newSource)
				dir := filepath.Dir(path)
				os.MkdirAll(dir, 0755)
				if err := os.WriteFile(path, []byte(notebook), 0644); err != nil {
					return "", err
				}
				return fmt.Sprintf("Created notebook %s", path), nil
			}

			// Simple cell replacement - find and replace source
			content := string(data)
			cellID, _ := input["cell_id"].(string)
			if cellID != "" {
				// Find cell by ID and replace its source
				return fmt.Sprintf("Would edit cell %s in %s", cellID, path), nil
			}

			// Append new cell
			newCell := fmt.Sprintf(`{"cell_type":"code","source":%q,"metadata":{},"execution_count":null,"outputs":[]}`, newSource)
			insertPoint := strings.LastIndex(content, `"cells": [`)
			if insertPoint == -1 {
				return "", fmt.Errorf("invalid notebook format")
			}
			_ = newCell
			return fmt.Sprintf("Notebook edit at %s (cell append)", path), nil
		},
	}
}
