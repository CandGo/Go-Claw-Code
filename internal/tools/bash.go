package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-claw/claw/internal/sandbox"
)

// BashCommandInput mirrors Rust BashCommandInput.
type BashCommandInput struct {
	Command                 string   `json:"command"`
	Timeout                 *uint64  `json:"timeout,omitempty"`
	Description             string   `json:"description,omitempty"`
	RunInBackground         *bool    `json:"run_in_background,omitempty"`
	DangerouslyDisableSandbox *bool  `json:"dangerouslyDisableSandbox,omitempty"`
	NamespaceRestrictions   *bool    `json:"namespaceRestrictions,omitempty"`
	IsolateNetwork          *bool    `json:"isolateNetwork,omitempty"`
	FilesystemMode          *sandbox.FilesystemIsolationMode `json:"filesystemMode,omitempty"`
	AllowedMounts           []string `json:"allowedMounts,omitempty"`
	Stdin                   string   `json:"stdin,omitempty"`
	Env                     map[string]string `json:"env,omitempty"`
	Cwd                     string   `json:"cwd,omitempty"`
}

// BashCommandOutput mirrors Rust BashCommandOutput.
type BashCommandOutput struct {
	Stdout                  string                   `json:"stdout"`
	Stderr                  string                   `json:"stderr"`
	RawOutputPath           *string                  `json:"rawOutputPath,omitempty"`
	Interrupted             bool                     `json:"interrupted"`
	IsImage                 *bool                    `json:"isImage,omitempty"`
	BackgroundTaskID        *string                  `json:"backgroundTaskId,omitempty"`
	BackgroundedByUser      *bool                    `json:"backgroundedByUser,omitempty"`
	AssistantAutoBackgrounded *bool                  `json:"assistantAutoBackgrounded,omitempty"`
	DangerouslyDisableSandbox *bool                  `json:"dangerouslyDisableSandbox,omitempty"`
	ReturnCodeInterpretation *string                 `json:"returnCodeInterpretation,omitempty"`
	NoOutputExpected        *bool                    `json:"noOutputExpected,omitempty"`
	StructuredContent       []map[string]interface{} `json:"structuredContent,omitempty"`
	PersistedOutputPath     *string                  `json:"persistedOutputPath,omitempty"`
	PersistedOutputSize     *uint64                  `json:"persistedOutputSize,omitempty"`
	SandboxStatus           *sandbox.SandboxStatus   `json:"sandboxStatus,omitempty"`
}

// ExecuteBash mirrors Rust execute_bash. It runs a bash command with sandbox support.
func ExecuteBash(input BashCommandInput) (BashCommandOutput, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return BashCommandOutput{}, fmt.Errorf("get cwd: %w", err)
	}
	// Override cwd if provided
	if input.Cwd != "" {
		cwd = input.Cwd
	}
	sbStatus := sandboxStatusForInput(&input, cwd)

	if input.RunInBackground != nil && *input.RunInBackground {
		cmd := prepareCommand(input.Command, cwd, &sbStatus, false)
		applyExtraEnv(cmd, input.Env)
		if input.Stdin != "" {
			cmd.Stdin = strings.NewReader(input.Stdin)
		} else {
			cmd.Stdin = nil
		}
		// Capture output for TaskOutput
		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		if err := cmd.Start(); err != nil {
			return BashCommandOutput{}, fmt.Errorf("start background command: %w", err)
		}
		pidStr := fmt.Sprintf("%d", cmd.Process.Pid)

		// Register with task manager for TaskOutput/TaskStop
		if globalTaskManager != nil {
			task := &BackgroundTask{
				ID:        pidStr,
				Command:   input.Command,
				Process:   cmd.Process,
				Status:    "running",
				StartedAt: time.Now(),
				done:     make(chan struct{}),
			}
			globalTaskManager.mu.Lock()
			globalTaskManager.tasks[pidStr] = task
			globalTaskManager.mu.Unlock()

			// Monitor completion in background goroutine
			go func() {
				cmd.Wait()
				output := outBuf.String() + errBuf.String()
				globalTaskManager.mu.Lock()
				task.Output = output
				if cmd.ProcessState.Success() {
					task.Status = "completed"
				} else {
					task.Status = "failed"
				}
				close(task.done)
				globalTaskManager.mu.Unlock()
			}()
		}

		noOut := true
		return BashCommandOutput{
			Stdout:                  "",
			Stderr:                  "",
			Interrupted:             false,
			BackgroundTaskID:        &pidStr,
			BackgroundedByUser:      boolPtr(false),
			AssistantAutoBackgrounded: boolPtr(false),
			DangerouslyDisableSandbox: input.DangerouslyDisableSandbox,
			NoOutputExpected:        &noOut,
			SandboxStatus:           &sbStatus,
		}, nil
	}

	return executeBashSync(input, sbStatus, cwd)
}

func executeBashSync(input BashCommandInput, sbStatus sandbox.SandboxStatus, cwd string) (BashCommandOutput, error) {
	var timeoutMs uint64 = 120_000
	if input.Timeout != nil && *input.Timeout > 0 {
		timeoutMs = *input.Timeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	cmd := prepareCommandContext(ctx, input.Command, cwd, &sbStatus, true)
	applyExtraEnv(cmd, input.Env)

	// Pipe stdin if provided, otherwise close stdin to prevent console handle inheritance
	if input.Stdin != "" {
		cmd.Stdin = strings.NewReader(input.Stdin)
	} else {
		cmd.Stdin = strings.NewReader("")
	}

	out, err := cmd.CombinedOutput()
	interrupted := false
	noOut := len(out) == 0

	if ctx.Err() == context.DeadlineExceeded {
		return BashCommandOutput{
			Stdout:                  "",
			Stderr:                  fmt.Sprintf("Command exceeded timeout of %d ms", timeoutMs),
			Interrupted:             true,
			DangerouslyDisableSandbox: input.DangerouslyDisableSandbox,
			ReturnCodeInterpretation: strPtr("timeout"),
			NoOutputExpected:        boolPtr(true),
			SandboxStatus:           &sbStatus,
		}, nil
	}

	stdout := string(out)
	stderr := ""
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
			code := exitErr.ExitCode()
			if code != 0 {
				return BashCommandOutput{
					Stdout:                  stdout,
					Stderr:                  stderr,
					Interrupted:             interrupted,
					DangerouslyDisableSandbox: input.DangerouslyDisableSandbox,
					ReturnCodeInterpretation: strPtr(fmt.Sprintf("exit_code:%d", code)),
					NoOutputExpected:        &noOut,
					SandboxStatus:           &sbStatus,
				}, nil
			}
		}
	}

	return BashCommandOutput{
		Stdout:                  stdout,
		Stderr:                  stderr,
		Interrupted:             interrupted,
		DangerouslyDisableSandbox: input.DangerouslyDisableSandbox,
		NoOutputExpected:        &noOut,
		SandboxStatus:           &sbStatus,
	}, nil
}

// sandboxStatusForInput mirrors Rust sandbox_status_for_input.
func sandboxStatusForInput(input *BashCommandInput, cwd string) sandbox.SandboxStatus {
	cfg := sandbox.SandboxConfig{}
	if globalSandbox != nil {
		cfg = globalSandbox.Config()
	}

	var enabledOverride *bool
	if input.DangerouslyDisableSandbox != nil {
		v := !*input.DangerouslyDisableSandbox
		enabledOverride = &v
	}

	req := cfg.ResolveRequest(
		enabledOverride,
		input.NamespaceRestrictions,
		input.IsolateNetwork,
		input.FilesystemMode,
		&input.AllowedMounts,
	)
	return sandbox.ResolveSandboxStatusForRequest(&req, cwd)
}

// prepareCommand mirrors Rust prepare_command / prepare_tokio_command.
func prepareCommand(command string, cwd string, sbStatus *sandbox.SandboxStatus, createDirs bool) *exec.Cmd {
	if createDirs {
		prepareSandboxDirs(cwd)
	}

	// Try Linux sandbox launcher first
	launcher := sandbox.BuildLinuxSandboxCommand(command, cwd, sbStatus)
	if launcher != nil {
		cmd := exec.Command(launcher.Program, launcher.Args...)
		cmd.Dir = cwd
		env := make([]string, len(launcher.Env))
		for i, e := range launcher.Env {
			env[i] = e[0] + "=" + e[1]
		}
		cmd.Env = env
		return cmd
	}

	// Fallback: regular shell execution
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd.exe", "/c", command)
		// Warn if sandbox was requested — no OS-level isolation on Windows
		if sbStatus != nil && sbStatus.FilesystemActive {
			fmt.Fprintf(os.Stderr, "  \033[33mWarning: sandbox isolation not supported on Windows — only command validation active\033[0m\n")
		}
	} else {
		cmd = exec.Command("sh", "-lc", command)
	}
	cmd.Dir = cwd

	if sbStatus.FilesystemActive {
		cmd.Env = append(os.Environ(),
			"HOME="+filepath.Join(cwd, ".sandbox-home"),
			"TMPDIR="+filepath.Join(cwd, ".sandbox-tmp"),
		)
	}

	return cmd
}

// prepareCommandContext creates a command with context-aware timeout.
func prepareCommandContext(ctx context.Context, command, cwd string, sbStatus *sandbox.SandboxStatus, createDirs bool) *exec.Cmd {
	cmd := prepareCommand(command, cwd, sbStatus, createDirs)
	setProcessFlags(cmd)
	// Kill the process when context is cancelled (timeout/interrupt)
	go func() {
		<-ctx.Done()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()
	return cmd
}

// prepareSandboxDirs mirrors Rust prepare_sandbox_dirs.
func prepareSandboxDirs(cwd string) {
	os.MkdirAll(filepath.Join(cwd, ".sandbox-home"), 0755)
	os.MkdirAll(filepath.Join(cwd, ".sandbox-tmp"), 0755)
}

// Helper constructors
func boolPtr(v bool) *bool       { return &v }
func strPtr(v string) *string    { return &v }
func uintPtr(v uint64) *uint64   { return &v }

// applyExtraEnv appends key=VALUE pairs from the env map to the command's environment.
// If the command already has an explicitly set Env (e.g., from sandbox), the extra
// variables are appended to that slice. Otherwise they are appended to os.Environ().
func applyExtraEnv(cmd *exec.Cmd, env map[string]string) {
	if len(env) == 0 {
		return
	}
	base := cmd.Env
	if base == nil {
		base = os.Environ()
	}
	for k, v := range env {
		base = append(base, k+"="+v)
	}
	cmd.Env = base
}
