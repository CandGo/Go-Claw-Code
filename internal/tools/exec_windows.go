//go:build windows

package tools

import (
	"os/exec"
	"syscall"
)

func setProcessFlags(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000200, // CREATE_NEW_PROCESS_GROUP — isolate from parent Ctrl+C
	}
}
