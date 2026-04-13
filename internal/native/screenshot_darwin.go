//go:build darwin

package native

import (
	"fmt"
	"time"
)

// CaptureScreenshot captures a screenshot on macOS.
func CaptureScreenshot(monitor int) (*ScreenshotResult, error) {
	return nil, fmt.Errorf("macOS screenshot not yet implemented")
}

// CaptureRect captures a rectangular region of the screen on macOS.
func CaptureRect(x, y, width, height int) (*ScreenshotResult, error) {
	return nil, fmt.Errorf("macOS screenshot not yet implemented")
}
