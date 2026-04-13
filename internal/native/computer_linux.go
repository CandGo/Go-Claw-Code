//go:build linux

package native

import (
	"fmt"
	"os/exec"
	"strings"
)

// MouseMove moves the mouse cursor to absolute (x, y) coordinates.
func MouseMove(x, y int) error {
	return runXdotool(fmt.Sprintf("mousemove %d %d", x, y))
}

// MouseClick performs a mouse click.
func MouseClick(button string, doubleClick bool) error {
	btn := "1"
	switch strings.ToLower(button) {
	case "right":
		btn = "3"
	case "middle":
		btn = "2"
	}

	if doubleClick {
		return runXdotool(fmt.Sprintf("click --repeat 2 --delay 50 %s", btn))
	}
	return runXdotool(fmt.Sprintf("click %s", btn))
}

// TypeText types a string using the keyboard.
func TypeText(text string) error {
	escaped := strings.ReplaceAll(text, "'", "'\\''")
	return runXdotool(fmt.Sprintf("type -- '%s'", escaped))
}

// KeyPress presses and releases a key combination.
func KeyPress(keys string) error {
	mapped := strings.ReplaceAll(strings.ToLower(keys), "control", "ctrl")
	return runXdotool(fmt.Sprintf("key %s", mapped))
}

// Scroll performs a mouse scroll.
func Scroll(direction string, clicks int) error {
	key := "Up"
	if strings.ToLower(direction) == "down" {
		key = "Down"
	}
	for i := 0; i < clicks; i++ {
		if err := runXdotool(fmt.Sprintf("key %s", key)); err != nil {
			return err
		}
	}
	return nil
}

// GetScreenSize returns the screen dimensions in pixels.
func GetScreenSize() (int, int, error) {
	out, err := exec.Command("xdotool", "getdisplaygeometry").Output()
	if err != nil {
		return 0, 0, fmt.Errorf("xdotool getdisplaygeometry failed: %w", err)
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("unexpected output: %s", string(out))
	}
	var w, h int
	fmt.Sscanf(parts[0], "%d", &w)
	fmt.Sscanf(parts[1], "%d", &h)
	return w, h, nil
}

func runXdotool(args string) error {
	cmd := exec.Command("bash", "-c", "xdotool "+args)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("xdotool %s: %w (%s)", args, err, string(out))
	}
	return nil
}
