package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// HookRunResult mirrors Rust HookRunResult.
type HookRunResult struct {
	denied   bool
	messages []string
}

// HookAllow returns a result that allows the tool use.
func HookAllow(messages []string) HookRunResult {
	return HookRunResult{denied: false, messages: messages}
}

// IsDenied returns true if the hook denied the tool use.
func (r HookRunResult) IsDenied() bool { return r.denied }

// Messages returns any messages from the hook.
func (r HookRunResult) Messages() []string { return r.messages }

// PluginHookRunner runs hooks from the plugin system.
// Mirrors Rust plugins/hooks.rs HookRunner.
type PluginHookRunner struct {
	hooks PluginHooks
}

// NewPluginHookRunner creates a hook runner from aggregated plugin hooks.
func NewPluginHookRunner(hooks PluginHooks) *PluginHookRunner {
	return &PluginHookRunner{hooks: hooks}
}

// NewPluginHookRunnerFromManager creates a hook runner from a plugin manager.
// Mirrors Rust HookRunner::from_registry.
func NewPluginHookRunnerFromManager(m *PluginManager) *PluginHookRunner {
	hooks, _ := m.AggregatedHooks()
	return &PluginHookRunner{hooks: hooks}
}

// RunPreToolUse runs pre-tool-use hooks.
// Mirrors Rust run_pre_tool_use.
func (r *PluginHookRunner) RunPreToolUse(toolName, toolInput string) HookRunResult {
	return r.runCommands("PreToolUse", r.hooks.PreToolUse, toolName, toolInput, "", false)
}

// RunPostToolUse runs post-tool-use hooks.
// Mirrors Rust run_post_tool_use.
func (r *PluginHookRunner) RunPostToolUse(toolName, toolInput, toolOutput string, isError bool) HookRunResult {
	return r.runCommands("PostToolUse", r.hooks.PostToolUse, toolName, toolInput, toolOutput, isError)
}

func (r *PluginHookRunner) runCommands(event string, commands []string, toolName, toolInput, toolOutput string, isError bool) HookRunResult {
	if len(commands) == 0 {
		return HookAllow(nil)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"hook_event_name":   event,
		"tool_name":         toolName,
		"tool_input":        parseToolInput(toolInput),
		"tool_input_json":   toolInput,
		"tool_output":       toolOutput,
		"tool_result_is_error": isError,
	})

	var messages []string
	for _, command := range commands {
		outcome := r.runCommand(command, event, toolName, toolInput, toolOutput, isError, string(payload))
		switch outcome.Kind {
		case hookAllow:
			if outcome.Message != "" {
				messages = append(messages, outcome.Message)
			}
		case hookDeny:
			msg := outcome.Message
			if msg == "" {
				msg = fmt.Sprintf("%s hook denied tool `%s`", event, toolName)
			}
			messages = append(messages, msg)
			return HookRunResult{denied: true, messages: messages}
		case hookWarn:
			messages = append(messages, outcome.Message)
		}
	}

	return HookAllow(messages)
}

type hookOutcomeKind int

const (
	hookAllow hookOutcomeKind = iota
	hookDeny
	hookWarn
)

type hookOutcome struct {
	Kind    hookOutcomeKind
	Message string
}

func (r *PluginHookRunner) runCommand(command, event, toolName, toolInput, toolOutput string, isError bool, payload string) hookOutcome {
	// Enforce a 30-second timeout so hooks cannot block indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-lc", command)
	}

	cmd.Env = append(os.Environ(),
		"HOOK_EVENT="+event,
		"HOOK_TOOL_NAME="+toolName,
		"HOOK_TOOL_INPUT="+toolInput,
		fmt.Sprintf("HOOK_TOOL_IS_ERROR=%d", boolToInt(isError)),
	)
	if toolOutput != "" {
		cmd.Env = append(cmd.Env, "HOOK_TOOL_OUTPUT="+toolOutput)
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return hookOutcome{Kind: hookWarn, Message: fmt.Sprintf("%s hook `%s` failed to create stdin for `%s`: %v", event, command, toolName, err)}
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return hookOutcome{Kind: hookWarn, Message: fmt.Sprintf("%s hook `%s` failed to start for `%s`: %v", event, command, toolName, err)}
	}
	stdinPipe.Write([]byte(payload))
	stdinPipe.Close()

	err = cmd.Wait()
	stdoutStr := strings.TrimSpace(stdout.String())
	stderrStr := strings.TrimSpace(stderr.String())

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			switch {
			case code == 0:
				return hookOutcome{Kind: hookAllow, Message: stdoutStr}
			case code == 2:
				return hookOutcome{Kind: hookDeny, Message: stdoutStr}
			default:
				return hookOutcome{Kind: hookWarn, Message: formatHookWarning(command, code, stdoutStr, stderrStr)}
			}
		}
		return hookOutcome{Kind: hookWarn, Message: fmt.Sprintf("%s hook `%s` terminated by signal while handling `%s`", event, command, toolName)}
	}

	return hookOutcome{Kind: hookAllow, Message: stdoutStr}
}

func parseToolInput(input string) interface{} {
	var parsed interface{}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return map[string]interface{}{"raw": input}
	}
	return parsed
}

func formatHookWarning(command string, code int, stdout, stderr string) string {
	msg := fmt.Sprintf("Hook `%s` exited with status %d; allowing tool execution to continue", command, code)
	if stdout != "" {
		msg += ": " + stdout
	} else if stderr != "" {
		msg += ": " + stderr
	}
	return msg
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
