//go:build !windows && !linux

package native

import "fmt"

// MouseMove returns an error on unsupported platforms.
func MouseMove(x, y int) error { return fmt.Errorf("mouse_move not supported on this platform") }

// MouseClick returns an error on unsupported platforms.
func MouseClick(button string, doubleClick bool) error { return fmt.Errorf("mouse_click not supported on this platform") }

// TypeText returns an error on unsupported platforms.
func TypeText(text string) error { return fmt.Errorf("type_text not supported on this platform") }

// KeyPress returns an error on unsupported platforms.
func KeyPress(keys string) error { return fmt.Errorf("key_press not supported on this platform") }

// Scroll returns an error on unsupported platforms.
func Scroll(direction string, clicks int) error { return fmt.Errorf("scroll not supported on this platform") }

// GetScreenSize returns an error on unsupported platforms.
func GetScreenSize() (int, int, error) { return 0, 0, fmt.Errorf("get_screen_size not supported on this platform") }
