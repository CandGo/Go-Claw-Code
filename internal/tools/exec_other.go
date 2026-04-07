//go:build !windows

package tools

import "os/exec"

func setProcessFlags(cmd *exec.Cmd) {
	// No-op on non-Windows
}
