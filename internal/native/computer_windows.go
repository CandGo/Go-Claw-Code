//go:build windows

package native

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// dpiPreamble enables DPI-aware mode so coordinates match physical pixels.
const dpiPreamble = `
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class DPI {
    [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr value);
    [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();
    [DllImport("user32.dll")] public static extern int GetSystemMetrics(int nIndex);
}
"@
try { [DPI]::SetProcessDpiAwarenessContext([IntPtr]::new(-4)) } catch {}
[DPI]::SetProcessDPIAware() | Out-Null
`

// MouseMove moves the mouse cursor to absolute (x, y) coordinates.
func MouseMove(x, y int) error {
	script := dpiPreamble + fmt.Sprintf(
		`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.Cursor]::Position = New-Object System.Drawing.Point(%d,%d)`,
		x, y,
	)
	return runPowerShell(script)
}

// MouseClick performs a mouse click.
func MouseClick(button string, doubleClick bool) error {
	clickScript := dpiPreamble + `
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class Mouse {
    [DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, IntPtr dwExtraInfo);
}
"@
`
	switch strings.ToLower(button) {
	case "right":
		clickScript += "[Mouse]::mouse_event(8, 0, 0, 0, [IntPtr]::Zero)\nStart-Sleep -Milliseconds 50\n[Mouse]::mouse_event(16, 0, 0, 0, [IntPtr]::Zero)\n"
	case "middle":
		clickScript += "[Mouse]::mouse_event(32, 0, 0, 0, [IntPtr]::Zero)\nStart-Sleep -Milliseconds 50\n[Mouse]::mouse_event(64, 0, 0, 0, [IntPtr]::Zero)\n"
	default:
		clickScript += "[Mouse]::mouse_event(2, 0, 0, 0, [IntPtr]::Zero)\nStart-Sleep -Milliseconds 50\n[Mouse]::mouse_event(4, 0, 0, 0, [IntPtr]::Zero)\n"
	}

	if doubleClick {
		clickScript += "Start-Sleep -Milliseconds 50\n"
		switch strings.ToLower(button) {
		case "right":
			clickScript += "[Mouse]::mouse_event(8, 0, 0, 0, [IntPtr]::Zero)\n[Mouse]::mouse_event(16, 0, 0, 0, [IntPtr]::Zero)\n"
		case "middle":
			clickScript += "[Mouse]::mouse_event(32, 0, 0, 0, [IntPtr]::Zero)\n[Mouse]::mouse_event(64, 0, 0, 0, [IntPtr]::Zero)\n"
		default:
			clickScript += "[Mouse]::mouse_event(2, 0, 0, 0, [IntPtr]::Zero)\n[Mouse]::mouse_event(4, 0, 0, 0, [IntPtr]::Zero)\n"
		}
	}

	return runPowerShell(clickScript)
}

// TypeText types a string using the keyboard via clipboard (more reliable than SendKeys for special chars).
func TypeText(text string) error {
	// Use clipboard + Ctrl+V for reliable text input with special characters
	escaped := strings.ReplaceAll(text, "'", "''")
	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.Clipboard]::SetText('%s')
[System.Windows.Forms.SendKeys]::SendWait('^v')
`, escaped)
	return runPowerShell(script)
}

// KeyPress presses and releases a key combination (e.g. "ctrl+c", "win").
// Uses keybd_event for Win key and SendKeys for other combos.
func KeyPress(keys string) error {
	lower := strings.ToLower(strings.TrimSpace(keys))

	// Special case: Win key (SendKeys doesn't support it)
	if lower == "win" || lower == "windows" {
		return keybdEvent(0x5B, 0x5B) // VK_LWIN down + up
	}

	// Special case: Win+something combos
	if strings.HasPrefix(lower, "win+") {
		rest := lower[4:]
		vk, ok := keyToVK(rest)
		if !ok {
			return fmt.Errorf("unknown key in combo: %s", keys)
		}
		return keybdEventCombo(0x5B, vk) // Win down, key down, key up, Win up
	}

	// For other keys, use SendKeys
	mapped := mapSendKeys(keys)
	script := fmt.Sprintf(
		`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait('%s')`,
		mapped,
	)
	return runPowerShell(script)
}

// keyToVK maps key names to Windows virtual key codes.
func keyToVK(key string) (uint16, bool) {
	k := strings.ToLower(key)
	vkMap := map[string]uint16{
		"enter": 0x0D, "return": 0x0D,
		"tab": 0x09, "escape": 0x1B, "esc": 0x1B,
		"backspace": 0x08, "delete": 0x2E, "del": 0x2E,
		"up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
		"home": 0x24, "end": 0x23,
		"pageup": 0x21, "pagedown": 0x22,
		"space": 0x20,
		"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73,
		"f5": 0x74, "f6": 0x75, "f7": 0x76, "f8": 0x77,
		"f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
		"a": 0x41, "b": 0x42, "c": 0x43, "d": 0x44,
		"e": 0x45, "f": 0x46, "g": 0x47, "h": 0x48,
		"i": 0x49, "j": 0x4A, "k": 0x4B, "l": 0x4C,
		"m": 0x4D, "n": 0x4E, "o": 0x4F, "p": 0x50,
		"q": 0x51, "r": 0x52, "s": 0x53, "t": 0x54,
		"u": 0x55, "v": 0x56, "w": 0x57, "x": 0x58,
		"y": 0x59, "z": 0x5A,
		"0": 0x30, "1": 0x31, "2": 0x32, "3": 0x33, "4": 0x34,
		"5": 0x35, "6": 0x36, "7": 0x37, "8": 0x38, "9": 0x39,
	}
	vk, ok := vkMap[k]
	return vk, ok
}

// keybdEvent sends a key down + up using Win32 keybd_event.
func keybdEvent(vkDown, vkUp uint16) error {
	script := fmt.Sprintf(`
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class KB {
    [DllImport("user32.dll")] public static extern void keybd_event(byte bVk, byte bScan, uint dwFlags, IntPtr dwExtraInfo);
}
"@
[KB]::keybd_event(%d, 0, 0, [IntPtr]::Zero)
Start-Sleep -Milliseconds 50
[KB]::keybd_event(%d, 0, 2, [IntPtr]::Zero)
`, vkDown, vkUp)
	return runPowerShell(script)
}

// keybdEventCombo sends modifier down, key down, key up, modifier up.
func keybdEventCombo(modVK, keyVK uint16) error {
	script := fmt.Sprintf(`
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class KB {
    [DllImport("user32.dll")] public static extern void keybd_event(byte bVk, byte bScan, uint dwFlags, IntPtr dwExtraInfo);
}
"@
[KB]::keybd_event(%d, 0, 0, [IntPtr]::Zero)
Start-Sleep -Milliseconds 50
[KB]::keybd_event(%d, 0, 0, [IntPtr]::Zero)
Start-Sleep -Milliseconds 50
[KB]::keybd_event(%d, 0, 2, [IntPtr]::Zero)
Start-Sleep -Milliseconds 50
[KB]::keybd_event(%d, 0, 2, [IntPtr]::Zero)
`, modVK, keyVK, keyVK, modVK)
	return runPowerShell(script)
}

// Scroll performs a mouse scroll.
func Scroll(direction string, clicks int) error {
	delta := clicks * 120
	script := dpiPreamble + `
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class Scroll {
    [DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, IntPtr dwExtraInfo);
}
"@
`

	if strings.ToLower(direction) == "down" {
		script += fmt.Sprintf("[Scroll]::mouse_event(0x0800, 0, 0, [uint32](-%d), [IntPtr]::Zero)\n", delta)
	} else {
		script += fmt.Sprintf("[Scroll]::mouse_event(0x0800, 0, 0, [uint32]%d, [IntPtr]::Zero)\n", delta)
	}

	return runPowerShell(script)
}

// GetScreenSize returns the physical screen dimensions in pixels.
func GetScreenSize() (int, int, error) {
	// Use GetSystemMetrics with DPI-aware context for real pixel dimensions
	script := dpiPreamble + `
$w = [DPI]::GetSystemMetrics(0)
$h = [DPI]::GetSystemMetrics(1)
Write-Output "$w x $h"
`
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get screen size: %w", err)
	}

	// Parse "W x H" format
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) < 3 {
		return 0, 0, fmt.Errorf("unexpected screen size format: %s", string(out))
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[2])
	if w == 0 || h == 0 {
		return 0, 0, fmt.Errorf("got zero screen size: %s", string(out))
	}
	return w, h, nil
}

func runPowerShell(script string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell error: %w (%s)", err, string(out))
	}
	return nil
}

func mapSendKeys(keys string) string {
	parts := strings.Split(keys, "+")
	if len(parts) == 1 {
		return "{" + strings.ToUpper(parts[0]) + "}"
	}
	result := ""
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "ctrl", "control":
			result += "^"
		case "alt":
			result += "%"
		case "shift":
			result += "+"
		}
	}
	result += "{" + strings.ToUpper(parts[len(parts)-1]) + "}"
	return result
}
