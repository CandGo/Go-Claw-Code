package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// TodoItem represents a single todo item.
type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"` // pending, in_progress, completed
	ActiveForm string `json:"activeForm,omitempty"`
}

var (
	todoItems []TodoItem
	todoMu    sync.Mutex
)

func todoWriteTool() *ToolSpec {
	return &ToolSpec{
		Name:        "TodoWrite",
		Description: "Create and manage a structured task list for your current coding session. Helps track progress and organize complex tasks. Use proactively when: tasks require 3+ distinct steps, user provides a list of items, or you need to plan multi-step work. Mark tasks as in_progress BEFORE starting work and completed IMMEDIATELY when done. Only ONE task should be in_progress at any time. Each task needs content (imperative) and activeForm (present continuous) fields.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"todos": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"content":    map[string]interface{}{"type": "string", "minLength": 1},
							"status":     map[string]interface{}{"type": "string", "enum": []string{"pending", "in_progress", "completed"}},
							"activeForm": map[string]interface{}{"type": "string", "minLength": 1},
						},
						"required": []string{"content", "status", "activeForm"},
						"additionalProperties": false,
					},
				},
			},
			"required": []string{"todos"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			todoMu.Lock()
			defer todoMu.Unlock()

			raw, ok := input["todos"]
			if !ok {
				return "", fmt.Errorf("todos is required")
			}

			data, err := json.Marshal(raw)
			if err != nil {
				return "", fmt.Errorf("invalid todos: %w", err)
			}

			var items []TodoItem
			if err := json.Unmarshal(data, &items); err != nil {
				return "", fmt.Errorf("invalid todos: %w", err)
			}

			todoItems = items
			var lines []string
			for i, item := range items {
				icon := " "
				switch item.Status {
				case "completed":
					icon = "x"
				case "in_progress":
					icon = ">"
				default:
					icon = "o"
				}
				lines = append(lines, fmt.Sprintf("  %s %d. %s", icon, i+1, item.Content))
			}
			return fmt.Sprintf("Updated %d todo items:\n%s", len(items), strings.Join(lines, "\n")), nil
		},
	}
}

// GetTodos returns the current todo list.
func GetTodos() []TodoItem {
	todoMu.Lock()
	defer todoMu.Unlock()
	return append([]TodoItem{}, todoItems...)
}
