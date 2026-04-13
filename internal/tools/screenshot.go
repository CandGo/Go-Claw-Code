package tools

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CandGo/Go-Claw-Code/internal/native"
)

func screenshotTool() *ToolSpec {
	return &ToolSpec{
		Name:        "screenshot",
		Permission:  PermReadOnly,
		Description: "Capture a screenshot of the screen or a specific region. Returns a base64-encoded image for the vision model.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"monitor": map[string]interface{}{
					"type":        "integer",
					"description": "Monitor index (-1 for all, 0 for primary)",
					"default":     0,
				},
				"region": map[string]interface{}{
					"type":        "object",
					"description": "Optional rectangular region to capture",
					"properties": map[string]interface{}{
						"x":      map[string]interface{}{"type": "integer"},
						"y":      map[string]interface{}{"type": "integer"},
						"width":  map[string]interface{}{"type": "integer"},
						"height": map[string]interface{}{"type": "integer"},
					},
				},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			monitor := 0
			if m, ok := input["monitor"].(float64); ok {
				monitor = int(m)
			}

			var result *native.ScreenshotResult
			var err error

			if region, ok := input["region"].(map[string]interface{}); ok {
				x := intFloat(region["x"])
				y := intFloat(region["y"])
				w := intFloat(region["width"])
				h := intFloat(region["height"])
				if w == 0 || h == 0 {
					return "", fmt.Errorf("region width and height must be > 0")
				}
				result, err = native.CaptureRect(x, y, w, h)
			} else {
				result, err = native.CaptureScreenshot(monitor)
			}

			if err != nil {
				return "", err
			}

			// Compress to JPEG for vision models (~100-200KB instead of ~2MB PNG)
			jpegData, err := native.ResizeForVisionJPEG(result.Data)
			if err != nil {
				// Fallback: use original PNG data
				jpegData = result.Data
			}

			// Save to temp file for CLI/web users
			tmpDir := os.TempDir()
			tmpFile := filepath.Join(tmpDir, fmt.Sprintf("screenshot_%d.jpg", result.Timestamp.Unix()))
			os.WriteFile(tmpFile, jpegData, 0644)

			// Build result with [img:mediaType:base64data] marker for runtime parsing
			b64 := base64.StdEncoding.EncodeToString(jpegData)
			return fmt.Sprintf("[Screenshot %dx%d JPEG %dKB saved to %s]\n[img:image/jpeg:%s]",
				result.Width, result.Height, len(jpegData)/1024, tmpFile, b64), nil
		},
	}
}

// intFloat converts float64 or int from JSON to int.
func intFloat(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}
