//go:build windows

package native

import (
	"fmt"
	"image"
	"time"

	"github.com/kbinani/screenshot"
)

// CaptureScreenshot captures a screenshot of the specified monitor on Windows.
func CaptureScreenshot(monitor int) (*ScreenshotResult, error) {
	var img *image.RGBA
	var err error

	if monitor < 0 {
		n := screenshot.NumActiveDisplays()
		if n == 0 {
			return nil, fmt.Errorf("no active displays found")
		}
		img, err = screenshot.CaptureDisplay(0)
	} else {
		img, err = screenshot.CaptureDisplay(monitor)
	}

	if err != nil {
		return nil, fmt.Errorf("screenshot capture failed: %w", err)
	}

	pngData, encErr := EncodePNG(img)
	if encErr != nil {
		return nil, fmt.Errorf("PNG encoding failed: %w", encErr)
	}

	return &ScreenshotResult{
		Width:     img.Bounds().Dx(),
		Height:    img.Bounds().Dy(),
		Data:      pngData,
		Monitor:   monitor,
		Timestamp: time.Now(),
	}, nil
}

// CaptureRect captures a rectangular region of the screen on Windows.
func CaptureRect(x, y, width, height int) (*ScreenshotResult, error) {
	img, err := screenshot.Capture(x, y, width, height)
	if err != nil {
		return nil, fmt.Errorf("rect capture failed: %w", err)
	}

	pngData, encErr := EncodePNG(img)
	if encErr != nil {
		return nil, fmt.Errorf("PNG encoding failed: %w", encErr)
	}

	return &ScreenshotResult{
		Width:     width,
		Height:    height,
		Data:      pngData,
		Monitor:   -1,
		Timestamp: time.Now(),
	}, nil
}
