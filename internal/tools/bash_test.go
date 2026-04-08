package tools

import (
	"testing"

	"github.com/CandGo/Go-Claw-Code/internal/sandbox"
)

func TestExecuteBashSimpleCommand(t *testing.T) {
	output, err := ExecuteBash(BashCommandInput{
		Command:       "printf 'hello'",
		Timeout:       uintPtr(5000),
		Description:   "",
		RunInBackground: boolPtr(false),
		DangerouslyDisableSandbox: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("ExecuteBash failed: %v", err)
	}
	if output.Stdout != "hello" {
		t.Errorf("stdout = %q, want %q", output.Stdout, "hello")
	}
	if output.Interrupted {
		t.Error("should not be interrupted")
	}
	if output.SandboxStatus == nil {
		t.Error("sandbox status should be set")
	}
}

func TestExecuteBashDisableSandbox(t *testing.T) {
	output, err := ExecuteBash(BashCommandInput{
		Command:       "printf 'hello'",
		Timeout:       uintPtr(5000),
		DangerouslyDisableSandbox: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("ExecuteBash failed: %v", err)
	}
	if output.Stdout != "hello" {
		t.Errorf("stdout = %q, want %q", output.Stdout, "hello")
	}
	if output.SandboxStatus != nil && output.SandboxStatus.Enabled {
		t.Error("sandbox should be disabled")
	}
}

func TestExecuteBashTimeout(t *testing.T) {
	output, err := ExecuteBash(BashCommandInput{
		Command: "sleep 10",
		Timeout: uintPtr(100),
	})
	if err != nil {
		t.Fatalf("ExecuteBash failed: %v", err)
	}
	if !output.Interrupted {
		t.Error("should be interrupted due to timeout")
	}
	if output.ReturnCodeInterpretation == nil || *output.ReturnCodeInterpretation != "timeout" {
		t.Error("return code interpretation should be 'timeout'")
	}
}

func TestExecuteBashExitCode(t *testing.T) {
	output, err := ExecuteBash(BashCommandInput{
		Command: "exit 42",
		Timeout: uintPtr(5000),
	})
	if err != nil {
		t.Fatalf("ExecuteBash failed: %v", err)
	}
	if output.ReturnCodeInterpretation == nil {
		t.Fatal("return code interpretation should be set")
	}
	if *output.ReturnCodeInterpretation != "exit_code:42" {
		t.Errorf("return code = %q, want %q", *output.ReturnCodeInterpretation, "exit_code:42")
	}
}

func TestPrepareCommandCreatesSandboxDirs(t *testing.T) {
	tmpDir := t.TempDir()
	sbStatus := sandbox.SandboxStatus{
		Enabled:          false,
		FilesystemActive: true,
	}
	_ = prepareCommand("echo hi", tmpDir, &sbStatus, true)
}
