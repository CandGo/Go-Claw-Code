package runtime

import (
	"fmt"
	"strings"
)

// PermissionMode represents the permission level required or active.
// Mirrors Rust PermissionMode exactly.
type PermissionMode int

const (
	PermReadOnly        PermissionMode = iota
	PermWorkspaceWrite
	PermDangerFullAccess
	PermPrompt
	PermAllow
	PermPlan
	PermAcceptEdits
	PermDontAsk
)

func (m PermissionMode) String() string {
	switch m {
	case PermReadOnly:
		return "read-only"
	case PermWorkspaceWrite:
		return "workspace-write"
	case PermDangerFullAccess:
		return "danger-full-access"
	case PermPrompt:
		return "prompt"
	case PermAllow:
		return "allow"
	case PermPlan:
		return "plan"
	case PermAcceptEdits:
		return "acceptEdits"
	case PermDontAsk:
		return "dontAsk"
	default:
		return "unknown"
	}
}

// PermissionRequest describes a pending permission decision.
type PermissionRequest struct {
	ToolName      string
	Input         string
	CurrentMode   PermissionMode
	RequiredMode  PermissionMode
}

// PermissionPromptDecision is the result of prompting the user.
type PermissionPromptDecision int

const (
	DecisionAllow PermissionPromptDecision = iota
	DecisionDeny
	DecisionAllowAlways
)

// PermissionPrompter is the interface for user-facing permission prompts.
type PermissionPrompter interface {
	Decide(request *PermissionRequest) PermissionPromptDecision
}

// PermissionOutcome is the result of an authorization check.
type PermissionOutcome int

const (
	OutcomeAllow PermissionOutcome = iota
	OutcomeDeny
)

// PermissionPolicy enforces per-tool permission requirements.
// Mirrors Rust PermissionPolicy exactly.
type PermissionPolicy struct {
	activeMode       PermissionMode
	toolRequirements map[string]PermissionMode
	allowedAlways    map[string]bool // tools allowed for the rest of this session
	prompter         PermissionPrompter
}

// NewPermissionPolicy creates a policy with the given active mode.
func NewPermissionPolicy(activeMode PermissionMode) PermissionPolicy {
	return PermissionPolicy{
		activeMode:       activeMode,
		toolRequirements: make(map[string]PermissionMode),
		allowedAlways:    make(map[string]bool),
	}
}

// WithToolRequirement adds a tool requirement to the policy (builder pattern).
func (p PermissionPolicy) WithToolRequirement(toolName string, required PermissionMode) PermissionPolicy {
	p.toolRequirements[toolName] = required
	return p
}

// WithPrompter sets the interactive permission prompter (builder pattern).
func (p PermissionPolicy) WithPrompter(prompter PermissionPrompter) PermissionPolicy {
	p.prompter = prompter
	return p
}

// ActiveMode returns the current active permission mode.
func (p PermissionPolicy) ActiveMode() PermissionMode {
	return p.activeMode
}

// RequiredModeFor returns the required permission mode for a tool.
func (p PermissionPolicy) RequiredModeFor(toolName string) PermissionMode {
	if req, ok := p.toolRequirements[toolName]; ok {
		return req
	}
	return PermDangerFullAccess
}

// Authorize checks whether a tool invocation is permitted.
// If the active mode is insufficient, it may prompt the user via the optional prompter.
func (p *PermissionPolicy) Authorize(toolName string, input string, prompter PermissionPrompter) PermissionOutcome {
	// Check allow-always cache first (session-persistent)
	if p.allowedAlways[toolName] {
		return OutcomeAllow
	}

	currentMode := p.ActiveMode()
	requiredMode := p.RequiredModeFor(toolName)

	// Allow mode or sufficient mode passes immediately
	if currentMode == PermAllow || currentMode >= requiredMode {
		return OutcomeAllow
	}

	// Plan mode: read-only + plan tools (plan mode filter handled in registry)
	if currentMode == PermPlan {
		if requiredMode <= PermWorkspaceWrite {
			return OutcomeAllow
		}
		return OutcomeDeny
	}

	// acceptEdits: auto-accept edits, prompt for danger
	if currentMode == PermAcceptEdits {
		if requiredMode <= PermWorkspaceWrite {
			return OutcomeAllow
		}
		if prompter != nil {
			decision := prompter.Decide(&PermissionRequest{ToolName: toolName, Input: input, CurrentMode: currentMode, RequiredMode: requiredMode})
			if decision == DecisionAllow { return OutcomeAllow }
		}
		return OutcomeDeny
	}

	// dontAsk: allow everything
	if currentMode == PermDontAsk {
		return OutcomeAllow
	}

		// Prompt mode or workspace-write→danger escalation triggers prompting
	if currentMode == PermPrompt ||
		(currentMode == PermWorkspaceWrite && requiredMode == PermDangerFullAccess) {
		if prompter != nil {
			request := &PermissionRequest{
				ToolName:     toolName,
				Input:        input,
				CurrentMode:  currentMode,
				RequiredMode: requiredMode,
			}
			decision := prompter.Decide(request)
			if decision == DecisionAllow || decision == DecisionAllowAlways {
				if decision == DecisionAllowAlways {
					p.allowedAlways[toolName] = true
				}
				return OutcomeAllow
			}
			return OutcomeDeny
		}
		return OutcomeDeny
	}

	return OutcomeDeny
}

// DenyReason returns a human-readable reason for a denial.
func (p PermissionPolicy) DenyReason(toolName string) string {
	currentMode := p.ActiveMode()
	requiredMode := p.RequiredModeFor(toolName)
	if currentMode == PermWorkspaceWrite && requiredMode == PermDangerFullAccess {
		return fmt.Sprintf("tool '%s' requires approval to escalate from %s to %s",
			toolName, currentMode.String(), requiredMode.String())
	}
	return fmt.Sprintf("tool '%s' requires %s permission; current mode is %s",
		toolName, requiredMode.String(), currentMode.String())
}

// PermissionModeFromString parses a permission mode string.
func PermissionModeFromString(s string) PermissionMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "read-only", "readonly", "read":
		return PermReadOnly
	case "workspace-write", "write", "default", "":
		return PermWorkspaceWrite
	case "danger-full-access", "danger", "full-access", "full":
		return PermDangerFullAccess
	case "prompt", "ask":
		return PermPrompt
	case "allow", "auto":
		return PermAllow
	case "plan":
		return PermPlan
	case "acceptedits", "accept-edits", "accept_edits":
		return PermAcceptEdits
	case "dontask", "dont-ask", "dont_ask":
		return PermDontAsk
	default:
		return PermWorkspaceWrite
	}
}

// DefaultPermissionPolicy returns the standard policy with workspace-write mode.
func DefaultPermissionPolicy() PermissionPolicy {
	return NewPermissionPolicy(PermWorkspaceWrite).
		WithToolRequirement("Read", PermReadOnly).
		WithToolRequirement("Glob", PermReadOnly).
		WithToolRequirement("Grep", PermReadOnly).
		WithToolRequirement("WebFetch", PermReadOnly).
		WithToolRequirement("WebSearch", PermReadOnly).
		WithToolRequirement("ToolSearch", PermReadOnly).
		WithToolRequirement("Skill", PermReadOnly).
		WithToolRequirement("NotebookEdit", PermWorkspaceWrite).
		WithToolRequirement("Write", PermWorkspaceWrite).
		WithToolRequirement("Edit", PermWorkspaceWrite).
		WithToolRequirement("TodoWrite", PermWorkspaceWrite).
		WithToolRequirement("CronCreate", PermWorkspaceWrite).
		WithToolRequirement("CronDelete", PermWorkspaceWrite).
		WithToolRequirement("WriteMemory", PermWorkspaceWrite).
		WithToolRequirement("Agent", PermDangerFullAccess).
		WithToolRequirement("Bash", PermDangerFullAccess).
		WithToolRequirement("PowerShell", PermDangerFullAccess)
}

// Check returns the required permission mode for a tool (backwards-compatible).
func (p PermissionPolicy) Check(toolName string) PermissionMode {
	return p.RequiredModeFor(toolName)
}
