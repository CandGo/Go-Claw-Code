package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-claw/claw/internal/api"
)

// ToolExecutor is the interface for executing tools.
type ToolExecutor interface {
	Execute(toolName string, input map[string]interface{}) (string, error)
	AvailableTools() []api.ToolDefinition
}

// ConversationRuntime manages the conversation loop.
type ConversationRuntime struct {
	provider     api.Provider
	tools        ToolExecutor
	session      *Session
	policy       PermissionPolicy
	hooks        *HookRunner
	usage        *UsageTracker
	model        string
	maxIter      int
	systemPrompt string
}

// NewConversationRuntime creates a new conversation runtime.
func NewConversationRuntime(provider api.Provider, tools ToolExecutor, model string) *ConversationRuntime {
	return &ConversationRuntime{
		provider:     provider,
		tools:        tools,
		session:      NewSession(),
		policy:       DefaultPermissionPolicy(),
		hooks:        NewHookRunner(nil, nil),
		usage:        NewUsageTracker(model),
		model:        model,
		maxIter:      50,
		systemPrompt: DefaultSystemPrompt(),
	}
}

// SetHooks sets the hook runner for the conversation runtime.
func (rt *ConversationRuntime) SetHooks(hooks *HookRunner) {
	rt.hooks = hooks
}

// SetSession replaces the current session (used for resume).
func (rt *ConversationRuntime) SetSession(s *Session) {
	rt.session = s
}

// Model returns the current model name.
func (rt *ConversationRuntime) Model() string {
	return rt.model
}

// MessageCount returns the number of messages in the session.
func (rt *ConversationRuntime) MessageCount() int {
	return len(rt.session.Messages)
}

// Clear resets the conversation session.
func (rt *ConversationRuntime) Clear() {
	rt.session = NewSession()
}

// Usage returns the usage tracker.
func (rt *ConversationRuntime) Usage() *UsageTracker {
	return rt.usage
}

// SaveSession persists the session to a file.
func (rt *ConversationRuntime) SaveSession(path string) error {
	return rt.session.Save(path)
}

// ShouldCompact checks if the session needs compaction.
func (rt *ConversationRuntime) ShouldCompact(cfg CompactionConfig) bool {
	return rt.session.ShouldCompact(cfg)
}

// Compact compacts the session history.
func (rt *ConversationRuntime) Compact(cfg CompactionConfig) {
	rt.session.Compact(cfg)
}

// DefaultCompactionConfig returns default compaction settings.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		PreserveRecent: compactionPreserveRecent,
		MaxTokens:      compactionMaxTokens,
	}
}

// RunTurn executes one user turn: send prompt, get response, execute tools, loop.
func (rt *ConversationRuntime) RunTurn(ctx context.Context, prompt string) ([]TurnOutput, *TokenUsage, error) {
	// Add user message
	rt.session.Messages = append(rt.session.Messages, ConversationMessage{
		Role:    MsgRoleUser,
		Content: []ContentBlock{&TextBlock{Text: prompt}},
	})

	var allOutputs []TurnOutput
	var totalUsage TokenUsage

	for i := 0; i < rt.maxIter; i++ {
		// Build API request
		req := rt.buildRequest()

		// Stream from provider
		eventsCh, err := rt.provider.StreamMessage(ctx, req)
		if err != nil {
			return allOutputs, &totalUsage, fmt.Errorf("stream error: %w", err)
		}

		// Collect streaming events
		events, usage, err := api.CollectStreamEvents(eventsCh)
		if err != nil {
			return allOutputs, &totalUsage, fmt.Errorf("stream error: %w", err)
		}

		totalUsage.InputTokens += usage.InputTokens
		totalUsage.OutputTokens += usage.OutputTokens
		totalUsage.CacheCreationInputTokens += usage.CacheCreationInputTokens
		totalUsage.CacheReadInputTokens += usage.CacheReadInputTokens

		// Record in usage tracker
		rt.usage.Record(TokenUsage{
			InputTokens:              usage.InputTokens,
			OutputTokens:             usage.OutputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
		})

		// Convert events to output
		var textParts []string
		var toolCalls []ToolUseBlock

		for _, evt := range events {
			switch evt.Type {
			case "text":
				textParts = append(textParts, evt.Text)
			case "tool_use":
				toolCalls = append(toolCalls, ToolUseBlock{
					ID:    evt.ToolID,
					Name:  evt.ToolName,
					Input: evt.ToolInput,
				})
			}
		}

		// Build assistant message
		var assistantBlocks []ContentBlock
		if len(textParts) > 0 {
			assistantBlocks = append(assistantBlocks, &TextBlock{Text: joinStrings(textParts)})
		}
		for _, tc := range toolCalls {
			assistantBlocks = append(assistantBlocks, &tc)
		}

		rt.session.Messages = append(rt.session.Messages, ConversationMessage{
			Role:    MsgRoleAssistant,
			Content: assistantBlocks,
			Usage:   &TokenUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens},
		})

		// If no tool calls, we're done
		if len(toolCalls) == 0 {
			allOutputs = append(allOutputs, TurnOutput{Type: "text", Text: joinStrings(textParts)})
			break
		}

		// Emit text output
		if len(textParts) > 0 {
			allOutputs = append(allOutputs, TurnOutput{Type: "text", Text: joinStrings(textParts)})
		}

		// Execute tools (with pre/post hooks)
		var toolResultBlocks []ContentBlock
		for _, tc := range toolCalls {
			allOutputs = append(allOutputs, TurnOutput{
				Type:     "tool_use",
				ToolName: tc.Name,
				ToolID:   tc.ID,
			})

			// Pre-tool hook: check if tool use is allowed
			preResult := rt.hooks.RunPreToolUse(tc.Name, tc.Input)
			if !preResult.Allowed {
				blockedMsg := fmt.Sprintf("blocked by hook: %s", preResult.Message)
				toolResultBlocks = append(toolResultBlocks, &ToolResultBlock{
					ToolUseID: tc.ID,
					Content:   blockedMsg,
					IsError:   true,
				})
				allOutputs = append(allOutputs, TurnOutput{
					Type:     "tool_result",
					ToolName: tc.Name,
					ToolID:   tc.ID,
					Text:     blockedMsg,
					IsError:  true,
				})
				continue
			}

			// Execute the tool
			result, err := rt.tools.Execute(tc.Name, tc.Input)
			isErr := err != nil
			if isErr {
				result = fmt.Sprintf("error: %v", err)
			}

			// Post-tool hook
			postResult := rt.hooks.RunPostToolUse(tc.Name, tc.Input, result, isErr)
			if postResult.Message != "" {
				result += "\n[hook: " + postResult.Message + "]"
			}

			toolResultBlocks = append(toolResultBlocks, &ToolResultBlock{
				ToolUseID: tc.ID,
				Content:   result,
				IsError:   isErr,
			})

			allOutputs = append(allOutputs, TurnOutput{
				Type:     "tool_result",
				ToolName: tc.Name,
				ToolID:   tc.ID,
				Text:     result,
				IsError:  isErr,
			})
		}

		// Add tool results as next user message
		rt.session.Messages = append(rt.session.Messages, ConversationMessage{
			Role:    MsgRoleUser,
			Content: toolResultBlocks,
		})
	}

	return allOutputs, &totalUsage, nil
}

func (rt *ConversationRuntime) buildRequest() *api.MessageRequest {
	msgs := make([]api.InputMessage, 0, len(rt.session.Messages))
	for _, m := range rt.session.Messages {
		im := api.InputMessage{Role: api.MessageRole(m.Role)}
		for _, b := range m.Content {
			switch v := b.(type) {
			case *TextBlock:
				im.Content = append(im.Content, api.InputContentBlock{
					Type: "text",
					Text: v.Text,
				})
			case *ToolUseBlock:
				im.Content = append(im.Content, api.InputContentBlock{
					Type:  "tool_use",
					ID:    v.ID,
					Name:  v.Name,
					Input: v.Input,
				})
			case *ToolResultBlock:
				im.Content = append(im.Content, api.InputContentBlock{
					Type:      "tool_result",
					ToolUseID: v.ToolUseID,
					IsError:   v.IsError,
					Content:   []api.InputContentBlock{api.TextBlock(v.Content)},
				})
			}
		}
		msgs = append(msgs, im)
	}

	sysPrompt, _ := json.Marshal(rt.systemPrompt)
	toolDefs := rt.tools.AvailableTools()

	return &api.MessageRequest{
		Model:      rt.model,
		MaxTokens:  api.MaxTokensForModel(rt.model),
		Messages:   msgs,
		System:     sysPrompt,
		Tools:      toolDefs,
		ToolChoice: api.AutoToolChoice(),
	}
}

// filterToolsForAgent returns a filtered ToolExecutor based on agent type.
func filterToolsForAgent(te ToolExecutor, agentType string) ToolExecutor {
	readOnlyTools := map[string]bool{
		"read_file": true, "glob": true, "grep": true,
		"WebFetch": true, "WebSearch": true,
		"ToolSearch": true, "Skill": true,
		"NotebookEdit": true, "SendUserMessage": true,
		"sleep": true, "TodoWrite": true, "StructuredOutput": true,
	}

	switch agentType {
	case "Explore":
		// readOnlyTools only
	case "Plan":
		readOnlyTools["Agent"] = true
		readOnlyTools["TodoWrite"] = true
	case "Verification":
		readOnlyTools["bash"] = true
		readOnlyTools["PowerShell"] = true
	case "claw-guide":
		// read-only + SendUserMessage (already included)
	case "statusline-setup":
		readOnlyTools = map[string]bool{
			"bash": true, "read_file": true, "write_file": true,
			"edit_file": true, "glob": true, "grep": true, "ToolSearch": true,
		}
	default:
		return nil // no filtering for general-purpose
	}

	return &filteredToolExecutor{inner: te, allowed: readOnlyTools}
}

// filteredToolExecutor wraps a ToolExecutor and filters available tools.
type filteredToolExecutor struct {
	inner   ToolExecutor
	allowed map[string]bool
}

func (f *filteredToolExecutor) Execute(toolName string, input map[string]interface{}) (string, error) {
	return f.inner.Execute(toolName, input)
}

func (f *filteredToolExecutor) AvailableTools() []api.ToolDefinition {
	all := f.inner.AvailableTools()
	var filtered []api.ToolDefinition
	for _, t := range all {
		if f.allowed[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func joinStrings(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n"
		}
		result += p
	}
	return result
}

// ExecuteSubAgent runs a sub-agent with its own isolated session.
// agentType controls tool filtering using FilterForAgentType.
func (rt *ConversationRuntime) ExecuteSubAgent(ctx context.Context, prompt string, maxIterations int, agentType string) (string, error) {
	// Apply tool filtering based on agent type
	toolExec := rt.tools
	if filtered := filterToolsForAgent(rt.tools, agentType); filtered != nil {
		toolExec = filtered
	}

	subRt := &ConversationRuntime{
		provider:     rt.provider,
		tools:        toolExec,
		session:      NewSession(),
		policy:       rt.policy,
		hooks:        rt.hooks,
		usage:        NewUsageTracker(rt.model),
		model:        rt.model,
		maxIter:      maxIterations,
		systemPrompt: rt.systemPrompt,
	}

	outputs, _, err := subRt.RunTurn(ctx, prompt)
	if err != nil {
		return "", err
	}

	var textParts []string
	for _, out := range outputs {
		if out.Type == "text" && out.Text != "" {
			textParts = append(textParts, out.Text)
		}
	}
	return joinStrings(textParts), nil
}
