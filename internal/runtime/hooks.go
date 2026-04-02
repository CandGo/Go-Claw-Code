package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// HookEvent represents a tool hook event.
type HookEvent struct {
	EventName    string                 `json:"hook_event_name"`
	ToolName     string                 `json:"tool_name"`
	ToolInput    map[string]interface{} `json:"tool_input"`
	ToolInputJSON string               `json:"tool_input_json"`
	ToolOutput   string                 `json:"tool_output,omitempty"`
	IsError      bool                   `json:"tool_result_is_error,omitempty"`
}

// HookResult is the outcome of running a hook command.
type HookResult struct {
	Allowed bool
	Message string
}

// HookRunner executes pre/post tool hooks.
type HookRunner struct {
	preCommands  []string
	postCommands []string
}

// NewHookRunner creates a hook runner from the given hook commands.
func NewHookRunner(pre, post []string) *HookRunner {
	return &HookRunner{
		preCommands:  pre,
		postCommands: post,
	}
}

// RunPreToolUse runs all pre-tool hooks. Returns whether the tool use is allowed.
func (h *HookRunner) RunPreToolUse(toolName string, input map[string]interface{}) HookResult {
	if len(h.preCommands) == 0 {
		return HookResult{Allowed: true}
	}

	inputJSON, _ := json.Marshal(input)
	event := HookEvent{
		EventName:     "PreToolUse",
		ToolName:      toolName,
		ToolInput:     input,
		ToolInputJSON: string(inputJSON),
	}

	return h.runCommands(h.preCommands, event)
}

// RunPostToolUse runs all post-tool hooks.
func (h *HookRunner) RunPostToolUse(toolName string, input map[string]interface{}, output string, isError bool) HookResult {
	if len(h.postCommands) == 0 {
		return HookResult{Allowed: true}
	}

	inputJSON, _ := json.Marshal(input)
	event := HookEvent{
		EventName:     "PostToolUse",
		ToolName:      toolName,
		ToolInput:     input,
		ToolInputJSON: string(inputJSON),
		ToolOutput:    output,
		IsError:       isError,
	}

	return h.runCommands(h.postCommands, event)
}

func (h *HookRunner) runCommands(commands []string, event HookEvent) HookResult {
	payload, _ := json.Marshal(event)

	for _, cmd := range commands {
		parts := strings.Fields(cmd)
		if len(parts) == 0 {
			continue
		}

		execCmd := exec.Command(parts[0], parts[1:]...)
		execCmd.Stdin = strings.NewReader(string(payload))
		execCmd.Env = append(os.Environ(),
			"HOOK_EVENT="+event.EventName,
			"HOOK_TOOL_NAME="+event.ToolName,
		)

		out, err := execCmd.CombinedOutput()
		output := strings.TrimSpace(string(out))

		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				switch exitErr.ExitCode() {
				case 0:
					// Allowed
				case 2:
					// Denied
					reason := output
					if reason == "" {
						reason = "blocked by hook"
					}
					return HookResult{Allowed: false, Message: reason}
				default:
					// Warning — allow but note it
					return HookResult{Allowed: true, Message: fmt.Sprintf("hook warning: %s", output)}
				}
			}
			// Process didn't run — allow but warn
			return HookResult{Allowed: true, Message: fmt.Sprintf("hook error: %v", err)}
		}
	}

	return HookResult{Allowed: true, Message: ""}
}
