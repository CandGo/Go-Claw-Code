package tools

import (
	"fmt"
	"os"
	"time"

	"github.com/CandGo/Go-Claw-Code/internal/native"
)

func mouseMoveTool() *ToolSpec {
	return &ToolSpec{
		Name:        "mouse_move",
		Permission:  PermDangerFullAccess,
		Description: "Move the mouse cursor to absolute (x, y) screen coordinates.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"x": map[string]interface{}{"type": "integer", "description": "X coordinate in pixels"},
				"y": map[string]interface{}{"type": "integer", "description": "Y coordinate in pixels"},
			},
			"required": []string{"x", "y"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			x := intFloat(input["x"])
			y := intFloat(input["y"])
			fmt.Fprintf(os.Stderr, "  [computer_use] mouse_move: (%d, %d)\n", x, y)
			time.Sleep(100 * time.Millisecond)
			if err := native.MouseMove(x, y); err != nil {
				return "", err
			}
			return fmt.Sprintf("Mouse moved to (%d, %d)", x, y), nil
		},
	}
}

func mouseClickTool() *ToolSpec {
	return &ToolSpec{
		Name:        "mouse_click",
		Permission:  PermDangerFullAccess,
		Description: "Click the mouse at the current position.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"button": map[string]interface{}{
					"type":        "string",
					"description": "Mouse button: left, right, middle",
					"default":     "left",
				},
				"double": map[string]interface{}{
					"type":        "boolean",
					"description": "Double click",
					"default":     false,
				},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			button := "left"
			if b, ok := input["button"].(string); ok {
				button = b
			}
			doubleClick := false
			if d, ok := input["double"].(bool); ok {
				doubleClick = d
			}
			fmt.Fprintf(os.Stderr, "  [computer_use] mouse_click: %s double=%v\n", button, doubleClick)
			time.Sleep(100 * time.Millisecond)
			if err := native.MouseClick(button, doubleClick); err != nil {
				return "", err
			}
			return fmt.Sprintf("Clicked %s (double=%v)", button, doubleClick), nil
		},
	}
}

func typeTextTool() *ToolSpec {
	return &ToolSpec{
		Name:        "type_text",
		Permission:  PermDangerFullAccess,
		Description: "Type a string of text using the keyboard.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]interface{}{"type": "string", "description": "Text to type"},
			},
			"required": []string{"text"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			text, _ := input["text"].(string)
			if text == "" {
				return "", fmt.Errorf("text is required")
			}
			preview := text
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			fmt.Fprintf(os.Stderr, "  [computer_use] type_text: %s\n", preview)
			time.Sleep(100 * time.Millisecond)
			if err := native.TypeText(text); err != nil {
				return "", err
			}
			return fmt.Sprintf("Typed %d characters", len(text)), nil
		},
	}
}

func keyPressTool() *ToolSpec {
	return &ToolSpec{
		Name:        "key_press",
		Permission:  PermDangerFullAccess,
		Description: "Press a key or key combination (e.g. 'ctrl+c', 'alt+tab', 'enter').",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"keys": map[string]interface{}{"type": "string", "description": "Key combination (e.g. 'ctrl+c', 'enter', 'escape')"},
			},
			"required": []string{"keys"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			keys, _ := input["keys"].(string)
			if keys == "" {
				return "", fmt.Errorf("keys is required")
			}
			fmt.Fprintf(os.Stderr, "  [computer_use] key_press: %s\n", keys)
			time.Sleep(100 * time.Millisecond)
			if err := native.KeyPress(keys); err != nil {
				return "", err
			}
			return fmt.Sprintf("Pressed: %s", keys), nil
		},
	}
}

func scrollTool() *ToolSpec {
	return &ToolSpec{
		Name:        "scroll",
		Permission:  PermDangerFullAccess,
		Description: "Scroll the mouse wheel up or down.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"direction": map[string]interface{}{
					"type":        "string",
					"description": "Scroll direction: up or down",
					"default":     "down",
				},
				"clicks": map[string]interface{}{
					"type":        "integer",
					"description": "Number of scroll clicks",
					"default":     3,
				},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			direction := "down"
			if d, ok := input["direction"].(string); ok {
				direction = d
			}
			clicks := 3
			if c, ok := input["clicks"].(float64); ok {
				clicks = int(c)
			}
			fmt.Fprintf(os.Stderr, "  [computer_use] scroll: %s x%d\n", direction, clicks)
			time.Sleep(100 * time.Millisecond)
			if err := native.Scroll(direction, clicks); err != nil {
				return "", err
			}
			return fmt.Sprintf("Scrolled %s %d clicks", direction, clicks), nil
		},
	}
}
