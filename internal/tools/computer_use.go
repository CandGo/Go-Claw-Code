package tools

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/CandGo/Go-Claw-Code/internal/native"
)

// computerTool implements an Anthropic-style computer use tool.
// It handles screenshot capture, mouse, and keyboard operations in a single tool,
// automatically taking screenshots after actions so the model can verify results.
func computerTool() *ToolSpec {
	return &ToolSpec{
		Name:       "computer",
		Permission: PermDangerFullAccess,
		Description: `Control the computer desktop. Performs mouse/keyboard actions and takes screenshots.
Actions: screenshot, mouse_move, left_click, right_click, double_click, type, key, scroll, drag.
After click/scroll/drag actions, a screenshot is automatically taken so you can see the result.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type": "string",
					"enum": []string{"screenshot", "mouse_move", "left_click", "right_click",
						"double_click", "type", "key", "scroll", "drag"},
					"description": "The action to perform",
				},
				"coordinate": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "integer"},
					"description": "[x, y] pixel coordinates for mouse actions (DPI-aware)",
				},
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Text to type (for 'type' action) or key combo (for 'key' action, e.g. 'ctrl+c')",
				},
				"start_coordinate": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "integer"},
					"description": "Start [x, y] for drag action",
				},
				"direction": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"up", "down"},
					"description": "Scroll direction",
				},
				"clicks": map[string]interface{}{
					"type":        "integer",
					"description": "Number of scroll clicks (default 3)",
					"default":     3,
				},
			},
			"required": []string{"action"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			action, _ := input["action"].(string)
			coord := parseCoord(input["coordinate"])
			text, _ := input["text"].(string)

			switch action {
			case "screenshot":
				return takeScreenshot()

			case "mouse_move":
				if coord == nil {
					return "", fmt.Errorf("coordinate [x, y] required for mouse_move")
				}
				if err := native.MouseMove(coord[0], coord[1]); err != nil {
					return "", fmt.Errorf("mouse_move failed: %w", err)
				}
				return fmt.Sprintf("Moved mouse to (%d, %d)", coord[0], coord[1]), nil

			case "left_click":
				return clickAndScreenshot(coord, "left", false)

			case "right_click":
				return clickAndScreenshot(coord, "right", false)

			case "double_click":
				return clickAndScreenshot(coord, "left", true)

			case "type":
				if text == "" {
					return "", fmt.Errorf("text required for 'type' action")
				}
				if err := native.TypeText(text); err != nil {
					return "", fmt.Errorf("type failed: %w", err)
				}
				return fmt.Sprintf("Typed: %s", truncateStr(text, 100)), nil

			case "key":
				if text == "" {
					return "", fmt.Errorf("text (key combo) required for 'key' action")
				}
				if err := native.KeyPress(text); err != nil {
					return "", fmt.Errorf("key press failed: %w", err)
				}
				return fmt.Sprintf("Pressed: %s", text), nil

			case "scroll":
				direction, _ := input["direction"].(string)
				if direction == "" {
					direction = "down"
				}
				clicks := 3
				if c, ok := input["clicks"].(float64); ok && c > 0 {
					clicks = int(c)
				}
				// Move to coordinate if provided
				if coord != nil {
					native.MouseMove(coord[0], coord[1])
				}
				if err := native.Scroll(direction, clicks); err != nil {
					return "", fmt.Errorf("scroll failed: %w", err)
				}
				// Auto-screenshot after scroll
				ss, ssErr := takeScreenshot()
				if ssErr != nil {
					return fmt.Sprintf("Scrolled %s %d clicks", direction, clicks), nil
				}
				return fmt.Sprintf("Scrolled %s %d clicks\n%s", direction, clicks, ss), nil

			case "drag":
				startCoord := parseCoord(input["start_coordinate"])
				if startCoord == nil || coord == nil {
					return "", fmt.Errorf("both start_coordinate and coordinate required for drag")
				}
				// Move to start, click, move to end, release
				native.MouseMove(startCoord[0], startCoord[1])
				native.MouseClick("left", false)
				native.MouseMove(coord[0], coord[1])
				// Release is handled by the click already (mouse_event up)
				ss, ssErr := takeScreenshot()
				if ssErr != nil {
					return fmt.Sprintf("Dragged from (%d,%d) to (%d,%d)", startCoord[0], startCoord[1], coord[0], coord[1]), nil
				}
				return fmt.Sprintf("Dragged from (%d,%d) to (%d,%d)\n%s", startCoord[0], startCoord[1], coord[0], coord[1], ss), nil

			default:
				return "", fmt.Errorf("unknown action: %s", action)
			}
		},
	}
}

// takeScreenshot captures the screen and returns JPEG data with [img:] marker.
func takeScreenshot() (string, error) {
	result, err := native.CaptureScreenshot(0)
	if err != nil {
		return "", fmt.Errorf("screenshot failed: %w", err)
	}

	// Compress to JPEG for vision model
	jpegData, err := native.ResizeForVisionJPEG(result.Data)
	if err != nil {
		jpegData = result.Data
	}

	b64 := base64.StdEncoding.EncodeToString(jpegData)
	return fmt.Sprintf("[Screenshot %dx%d JPEG %dKB]\n[img:image/jpeg:%s]",
		result.Width, result.Height, len(jpegData)/1024, b64), nil
}

// clickAndScreenshot moves to coordinate (if provided), clicks, then takes a screenshot.
func clickAndScreenshot(coord []int, button string, double bool) (string, error) {
	if coord != nil {
		if err := native.MouseMove(coord[0], coord[1]); err != nil {
			return "", fmt.Errorf("mouse_move failed: %w", err)
		}
	}
	if err := native.MouseClick(button, double); err != nil {
		return "", fmt.Errorf("click failed: %w", err)
	}
	clickDesc := fmt.Sprintf("%s click at (%d,%d)", button, coord[0], coord[1])
	if coord == nil {
		clickDesc = fmt.Sprintf("%s click at current position", button)
	}
	if double {
		clickDesc = "double " + clickDesc
	}

	// Auto-screenshot after click
	ss, ssErr := takeScreenshot()
	if ssErr != nil {
		return clickDesc, nil
	}
	return clickDesc + "\n" + ss, nil
}

// parseCoord extracts [x, y] from a coordinate array in tool input.
func parseCoord(v interface{}) []int {
	if v == nil {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok || len(arr) < 2 {
		return nil
	}
	x := intFloat(arr[0])
	y := intFloat(arr[1])
	if x == 0 && y == 0 {
		// Check if they were actually provided
		if _, ok1 := arr[0].(float64); !ok1 {
			return nil
		}
	}
	return []int{x, y}
}

// truncateStr truncates a string to max length.
func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// suppress unused import warning
var _ = strings.TrimSpace
