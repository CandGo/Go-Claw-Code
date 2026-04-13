//go:build linux

package native

import (
	"bytes"
	"fmt"
	"os/exec"
	"time"
)

// CaptureScreenshot captures a screenshot on Linux.
func CaptureScreenshot(monitor int) (*ScreenshotResult, error) {
	var data []byte
	var err error

	if _, pathErr := exec.LookPath("scrot"); pathErr == nil {
		data, err = captureWithScrot()
	} else if _, pathErr := exec.LookPath("import"); pathErr == nil {
		data, err = captureWithImport()
	} else {
		return nil, fmt.Errorf("no screenshot tool found (install scrot or imagemagick)")
	}

	if err != nil {
		return nil, err
	}

	w, h, dimErr := ImageDimensions(data)
	if dimErr != nil {
		w, h = 0, 0
	}

	return &ScreenshotResult{
		Width:     w,
		Height:    h,
		Data:      data,
		Monitor:   monitor,
		Timestamp: time.Now(),
	}, nil
}

// CaptureRect captures a rectangular region of the screen on Linux.
func CaptureRect(x, y, w, h int) (*ScreenshotResult, error) {
	cmd := exec.Command("import", "-window", "root", "-crop", fmt.Sprintf("%dx%d+%d+%d", w, h, x, y), "-")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("import rect failed: %w (%s)", err, stderr.String())
	}

	return &ScreenshotResult{
		Width:     w,
		Height:    h,
		Data:      stdout.Bytes(),
		Monitor:   -1,
		Timestamp: time.Now(),
	}, nil
}

func captureWithScrot() ([]byte, error) {
	cmd := exec.Command("scrot", "-o", "-")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("scrot failed: %w (%s)", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func captureWithImport() ([]byte, error) {
	cmd := exec.Command("import", "-window", "root", "-")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("import failed: %w (%s)", err, stderr.String())
	}
	return stdout.Bytes(), nil
}
