package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// HookEventType represents the type of hook event.
type HookEventType int

const (
	HookPreToolUse    HookEventType = iota
	HookPostToolUse
	HookNotification
	HookStop
	HookSubagentBefore
	HookSubagentAfter
)

func (e HookEventType) String() string {
	switch e {
	case HookPreToolUse:
		return "PreToolUse"
	case HookPostToolUse:
		return "PostToolUse"
	case HookNotification:
		return "Notification"
	case HookStop:
		return "Stop"
	case HookSubagentBefore:
		return "SubagentBefore"
	case HookSubagentAfter:
		return "SubagentAfter"
	default:
		return "Unknown"
	}
}

// HookRunResult is the outcome of running a set of hook commands.
// Mirrors Rust HookRunResult.
type HookRunResult struct {
	denied   bool
	messages []string
}

// AllowHookResult creates an allowing result with optional messages.
func AllowHookResult(messages []string) HookRunResult {
	return HookRunResult{denied: false, messages: messages}
}

// IsDenied returns whether the hook result denies the tool invocation.
func (r HookRunResult) IsDenied() bool {
	return r.denied
}

// Messages returns the accumulated messages from hook execution.
func (r HookRunResult) Messages() []string {
	return r.messages
}

// HookConfig defines a single hook configuration.
type HookConfig struct {
	Type      string   `json:"type"`
	ToolMatch string   `json:"tool_match,omitempty"`
	Command   string   `json:"command"`
	Timeout   int      `json:"timeout,omitempty"`
}

// HookRunner executes configured hooks.
// Mirrors Rust HookRunner.
type HookRunner struct {
	preToolUse  []string
	postToolUse []string
	hooks       []HookConfig
	timeout     time.Duration
}

// NewHookRunner creates a hook runner from hook configurations.
func NewHookRunner(hooks []HookConfig) *HookRunner {
	var pre, post []string
	for _, h := range hooks {
		switch h.Type {
		case "PreToolUse":
			pre = append(pre, h.Command)
		case "PostToolUse":
			post = append(post, h.Command)
		}
	}
	return &HookRunner{
		preToolUse:  pre,
		postToolUse: post,
		hooks:       hooks,
		timeout:     30 * time.Second,
	}
}

// NewHookRunnerFromCommands creates a hook runner with explicit pre/post command lists.
func NewHookRunnerFromCommands(preToolUse, postToolUse []string) *HookRunner {
	return &HookRunner{
		preToolUse:  preToolUse,
		postToolUse: postToolUse,
		timeout:     30 * time.Second,
	}
}

// SetTimeout sets the default timeout for hook execution.
func (h *HookRunner) SetTimeout(d time.Duration) {
	h.timeout = d
}

// RunPreToolUse runs all pre-tool hooks matching the given tool name.
func (h *HookRunner) RunPreToolUse(toolName string, toolInput string) HookRunResult {
	return h.runCommands(HookPreToolUse, h.preToolUse, toolName, toolInput, "", false)
}

// RunPostToolUse runs all post-tool hooks matching the given tool name.
func (h *HookRunner) RunPostToolUse(toolName, toolInput, toolOutput string, isError bool) HookRunResult {
	return h.runCommands(HookPostToolUse, h.postToolUse, toolName, toolInput, toolOutput, isError)
}

// RunNotification runs notification hooks.
func (h *HookRunner) RunNotification(message, source string) HookRunResult {
	return h.runHooksOfType("Notification", "", "", message, false)
}

// RunStop runs stop hooks when the agent stops.
func (h *HookRunner) RunStop(reason string) HookRunResult {
	return h.runHooksOfType("Stop", "", reason, "", false)
}

// RunSubagentBefore runs hooks before a subagent is launched.
func (h *HookRunner) RunSubagentBefore(agentType, prompt string) HookRunResult {
	return h.runHooksOfType("SubagentBefore", agentType, prompt, "", false)
}

// RunSubagentAfter runs hooks after a subagent completes.
func (h *HookRunner) RunSubagentAfter(agentType, result string, isError bool) HookRunResult {
	return h.runHooksOfType("SubagentAfter", agentType, "", result, isError)
}

func (h *HookRunner) runHooksOfType(hookType, toolName, toolInput, toolOutput string, isError bool) HookRunResult {
	var commands []string
	for _, hook := range h.hooks {
		if hook.Type != hookType {
			continue
		}
		if hook.ToolMatch != "" && toolName != "" && !matchToolPattern(hook.ToolMatch, toolName) {
			continue
		}
		commands = append(commands, hook.Command)
	}
	return h.runCommands(HookEventType(hookTypeFromString(hookType)), commands, toolName, toolInput, toolOutput, isError)
}

func hookTypeFromString(s string) HookEventType {
	switch s {
	case "PreToolUse":
		return HookPreToolUse
	case "PostToolUse":
		return HookPostToolUse
	case "Notification":
		return HookNotification
	case "Stop":
		return HookStop
	case "SubagentBefore":
		return HookSubagentBefore
	case "SubagentAfter":
		return HookSubagentAfter
	default:
		return HookPreToolUse
	}
}

func (h *HookRunner) runCommands(event HookEventType, commands []string, toolName, toolInput string, toolOutput string, isError bool) HookRunResult {
	if len(commands) == 0 {
		return AllowHookResult(nil)
	}

	payload := buildHookPayload(event, toolName, toolInput, toolOutput, isError)

	var messages []string
	for _, command := range commands {
		outcome := runSingleCommand(command, event, toolName, toolInput, toolOutput, isError, payload, h.timeout)
		switch outcome.kind {
		case outcomeAllow:
			if outcome.message != "" {
				messages = append(messages, outcome.message)
			}
		case outcomeDeny:
			msg := outcome.message
			if msg == "" {
				msg = fmt.Sprintf("%s hook denied tool `%s`", event.String(), toolName)
			}
			messages = append(messages, msg)
			return HookRunResult{denied: true, messages: messages}
		case outcomeWarn:
			messages = append(messages, outcome.message)
		}
	}

	return AllowHookResult(messages)
}

type hookOutcomeKind int

const (
	outcomeAllow hookOutcomeKind = iota
	outcomeDeny
	outcomeWarn
)

type hookOutcome struct {
	kind    hookOutcomeKind
	message string
}

func buildHookPayload(event HookEventType, toolName, toolInput, toolOutput string, isError bool) string {
	var parsedInput interface{}
	if err := json.Unmarshal([]byte(toolInput), &parsedInput); err != nil {
		parsedInput = map[string]interface{}{"raw": toolInput}
	}

	payload := map[string]interface{}{
		"hook_event_name":     event.String(),
		"tool_name":           toolName,
		"tool_input":          parsedInput,
		"tool_input_json":     toolInput,
		"tool_output":         toolOutput,
		"tool_result_is_error": isError,
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func runSingleCommand(command string, event HookEventType, toolName, toolInput, toolOutput string, isError bool, payload string, defaultTimeout time.Duration) hookOutcome {
	timeout := defaultTimeout

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		if isExistingFilePath(command) {
			cmd = exec.Command("sh", command)
		} else {
			cmd = exec.Command("sh", "-lc", command)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd.Stdin = bytes.NewReader([]byte(payload))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(),
		"HOOK_EVENT="+event.String(),
		"HOOK_TOOL_NAME="+toolName,
		"HOOK_TOOL_INPUT="+toolInput,
		fmt.Sprintf("HOOK_TOOL_IS_ERROR=%d", boolToInt(isError)),
	)
	if toolOutput != "" {
		cmd.Env = append(cmd.Env, "HOOK_TOOL_OUTPUT="+toolOutput)
	}

	err := cmd.Run()
	stdoutStr := strings.TrimSpace(stdout.String())
	stderrStr := strings.TrimSpace(stderr.String())

	if ctx.Err() == context.DeadlineExceeded {
		return hookOutcome{
			kind:    outcomeWarn,
			message: fmt.Sprintf("Hook `%s` timed out after %s", command, timeout),
		}
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			switch {
			case code == 0:
				return hookOutcome{kind: outcomeAllow, message: stdoutStr}
			case code == 2:
				return hookOutcome{kind: outcomeDeny, message: stdoutStr}
			default:
				return hookOutcome{
					kind:    outcomeWarn,
					message: formatHookWarning(command, code, stdoutStr, stderrStr),
				}
			}
		}
		return hookOutcome{
			kind:    outcomeWarn,
			message: fmt.Sprintf("%s hook `%s` failed to start for `%s`: %v", event.String(), command, toolName, err),
		}
	}

	return hookOutcome{kind: outcomeAllow, message: stdoutStr}
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

// isExistingFilePath checks whether the command looks like a file path
// (starts with "./" or "../" or is an absolute path starting with "/") and
// whether that file actually exists. When true, the hook runner will execute
// the file directly via "sh <path>" instead of "sh -lc <command>".
func isExistingFilePath(command string) bool {
	if !strings.HasPrefix(command, "./") && !strings.HasPrefix(command, "../") && !strings.HasPrefix(command, "/") {
		return false
	}
	_, err := os.Stat(command)
	return err == nil
}

// matchToolPattern matches a tool name against a hook pattern.
// Supports:
//   - "bash" — exact match
//   - "bash:*" — matches bash with any subtool
//   - "*" — matches all tools
func matchToolPattern(pattern, toolName string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == toolName {
		return true
	}
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, ":*")
		return strings.HasPrefix(toolName, prefix)
	}
	return false
}

// Backwards-compatible type aliases

// HookType is the string-based hook type (deprecated, use HookEventType).
type HookType = string

const (
	HookPreToolUseS     HookType = "PreToolUse"
	HookPostToolUseS    HookType = "PostToolUse"
	HookNotificationS   HookType = "Notification"
	HookStopS           HookType = "Stop"
	HookSubagentBeforeS HookType = "SubagentBefore"
	HookSubagentAfterS  HookType = "SubagentAfter"
)

// HookResult is the legacy hook result type.
type HookResult = HookRunResult

// RunPreToolUseMap runs pre-tool hooks with map input (backwards-compatible).
func (h *HookRunner) RunPreToolUseMap(toolName string, input map[string]interface{}) HookRunResult {
	inputJSON := marshalHookJSON(input)
	return h.RunPreToolUse(toolName, inputJSON)
}

// RunPostToolUseMap runs post-tool hooks with map input (backwards-compatible).
func (h *HookRunner) RunPostToolUseMap(toolName string, input map[string]interface{}, output string, isError bool) HookRunResult {
	inputJSON := marshalHookJSON(input)
	return h.RunPostToolUse(toolName, inputJSON, output, isError)
}

func marshalHookJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
