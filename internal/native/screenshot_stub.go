//go:build !windows && !linux && !darwin

package native

import (
	"fmt"
)

// CaptureScreenshot returns an error on unsupported platforms.
func CaptureScreenshot(monitor int) (*ScreenshotResult, error) {
	return nil, fmt.Errorf("screenshot not supported on this platform")
}

// CaptureRect returns an error on unsupported platforms.
func CaptureRect(x, y, width, height int) (*ScreenshotResult, error) {
	return nil, fmt.Errorf("screenshot not supported on this platform")
}
