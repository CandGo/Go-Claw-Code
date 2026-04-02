package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func sleepTool() *ToolSpec {
	return &ToolSpec{
		Name:       "sleep",
		Permission: PermReadOnly,
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
		Name:       "ToolSearch",
		Permission: PermReadOnly,
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
			query, _ := input["query"].(string)
			maxResults := 10
			if m, ok := input["max_results"].(float64); ok && m > 0 {
				maxResults = int(m)
			}

			if strings.HasPrefix(query, "select:") {
				names := strings.Split(strings.TrimPrefix(query, "select:"), ",")
				var results []string
				for _, n := range names {
					n = strings.TrimSpace(n)
					if spec, ok := globalRegistry.specs[n]; ok {
						results = append(results, fmt.Sprintf("- %s: %s [perm=%s]", spec.Name, spec.Description, permName(spec.Permission)))
					}
				}
				if len(results) == 0 {
					return "No tools found.", nil
				}
				return strings.Join(results, "\n"), nil
			}

			lowerQuery := strings.ToLower(query)
			var results []string
			for name, spec := range globalRegistry.specs {
				if strings.Contains(strings.ToLower(name), lowerQuery) ||
					strings.Contains(strings.ToLower(spec.Description), lowerQuery) {
					results = append(results, fmt.Sprintf("- %s: %s [perm=%s]", name, spec.Description, permName(spec.Permission)))
				}
				if len(results) >= maxResults {
					break
				}
			}
			if len(results) == 0 {
				return "No tools found matching query.", nil
			}
			return strings.Join(results, "\n"), nil
		},
	}
}

func notebookEditTool() *ToolSpec {
	return &ToolSpec{
		Name:       "NotebookEdit",
		Permission: PermWorkspaceWrite,
		Description: "Edit a Jupyter notebook cell.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"notebook_path": map[string]interface{}{"type": "string", "description": "Absolute path to the .ipynb file"},
				"cell_id":       map[string]interface{}{"type": "string", "description": "Cell ID to edit"},
				"new_source":    map[string]interface{}{"type": "string", "description": "New cell source"},
				"cell_type":     map[string]interface{}{"type": "string", "description": "code or markdown"},
				"edit_mode":     map[string]interface{}{"type": "string", "description": "replace, insert, or delete"},
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
				return "", fmt.Errorf("file must be .ipynb")
			}

			data, err := os.ReadFile(path)
			if err != nil {
				notebook := map[string]interface{}{
					"cells": []interface{}{
						map[string]interface{}{
							"cell_type": "code", "source": newSource,
							"metadata": map[string]interface{}{}, "execution_count": nil, "outputs": []interface{}{},
						},
					},
					"metadata":    map[string]interface{}{},
					"nbformat":    4, "nbformat_minor": 5,
				}
				os.MkdirAll(filepath.Dir(path), 0755)
				nbData, _ := json.MarshalIndent(notebook, "", "  ")
				os.WriteFile(path, nbData, 0644)
				return "Created notebook: " + path, nil
			}

			var notebook map[string]interface{}
			json.Unmarshal(data, &notebook)
			cells, _ := notebook["cells"].([]interface{})
			cellID, _ := input["cell_id"].(string)
			editMode, _ := input["edit_mode"].(string)
			if editMode == "" {
				editMode = "replace"
			}

			switch editMode {
			case "insert":
				cellType, _ := input["cell_type"].(string)
				if cellType == "" {
					cellType = "code"
				}
				cells = append(cells, map[string]interface{}{
					"cell_type": cellType, "source": newSource,
					"metadata": map[string]interface{}{}, "execution_count": nil, "outputs": []interface{}{},
				})
				notebook["cells"] = cells
			case "delete":
				for i, c := range cells {
					if cell, ok := c.(map[string]interface{}); ok {
						if id, ok := cell["id"].(string); ok && id == cellID {
							cells = append(cells[:i], cells[i+1:]...)
							break
						}
					}
				}
				notebook["cells"] = cells
			default:
				if cellID != "" {
					for i, c := range cells {
						if cell, ok := c.(map[string]interface{}); ok {
							if id, ok := cell["id"].(string); ok && id == cellID {
								cell["source"] = newSource
								cells[i] = cell
								break
							}
						}
					}
				} else if len(cells) > 0 {
					if cell, ok := cells[len(cells)-1].(map[string]interface{}); ok {
						cell["source"] = newSource
					}
				}
				notebook["cells"] = cells
			}

			nbData, _ := json.MarshalIndent(notebook, "", "  ")
			os.WriteFile(path, nbData, 0644)
			return fmt.Sprintf("Notebook edited: %s (%s)", path, editMode), nil
		},
	}
}
