package tools

import (
	"encoding/json"
	"fmt"
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
		Description: "Update the todo list to track progress on tasks.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"todos": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"content":    map[string]interface{}{"type": "string"},
							"status":     map[string]interface{}{"type": "string", "enum": []string{"pending", "in_progress", "completed"}},
							"activeForm": map[string]interface{}{"type": "string"},
						},
						"required": []string{"content", "status"},
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
			return fmt.Sprintf("Updated %d todo items", len(items)), nil
		},
	}
}

// GetTodos returns the current todo list.
func GetTodos() []TodoItem {
	todoMu.Lock()
	defer todoMu.Unlock()
	return append([]TodoItem{}, todoItems...)
}
