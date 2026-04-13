package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/CandGo/Go-Claw-Code/internal/api"
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
	systemPrompt       string
	systemPromptBuilder *SystemPromptBuilder // for cached prompt with cache_control
	settings           map[string]string
	permissionPrompter PermissionPrompter
	reflectionEnabled  bool // when true, adds self-evaluation prompt after agent responds
}

// NewConversationRuntime creates a new conversation runtime.
func NewConversationRuntime(provider api.Provider, tools ToolExecutor, model string) *ConversationRuntime {
	return &ConversationRuntime{
		provider:     provider,
		tools:        tools,
		session:      NewSession(),
		policy:       DefaultPermissionPolicy(),
		hooks:        NewHookRunner(nil),
		usage:        NewUsageTracker(model),
		model:        model,
		maxIter:      50,
		systemPrompt: DefaultSystemPrompt(model),
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

// GetSession returns a copy of the current session.
func (rt *ConversationRuntime) GetSession() *Session {
	return rt.session
}

// Session returns the current session (direct access).
func (rt *ConversationRuntime) Session() *Session {
	return rt.session
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
func (rt *ConversationRuntime) Compact(cfg CompactionConfig) CompactionResult {
	return rt.session.Compact(cfg)
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

		// Retry StreamMessage on transient errors
		var eventsCh <-chan api.SSEFrame
		var streamErr error
		for attempt := 0; attempt < 3; attempt++ {
			eventsCh, streamErr = rt.provider.StreamMessage(ctx, req)
			if streamErr == nil {
				break
			}
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * time.Second)
			}
		}
		if streamErr != nil {
			return allOutputs, &totalUsage, fmt.Errorf("stream error (after 3 retries): %w", streamErr)
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
		if len(assistantBlocks) == 0 {
			assistantBlocks = append(assistantBlocks, &TextBlock{Text: ""})
		}

		rt.session.Messages = append(rt.session.Messages, ConversationMessage{
			Role:    MsgRoleAssistant,
			Content: assistantBlocks,
			Usage:   &TokenUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens},
		})

		// If no tool calls, we're done
		if len(toolCalls) == 0 {
			allOutputs = append(allOutputs, TurnOutput{Type: "text", Text: joinStrings(textParts)})

			// Reflection pattern: inject self-evaluation after agent work to improve quality
			if rt.reflectionEnabled && i > 0 && len(textParts) > 0 {
				reflectionPrompt := "Before finalizing, review your work: " +
					"1) Did you complete all parts of the user's request? " +
					"2) Are there any errors or edge cases you missed? " +
					"3) Is the code/test change correct and complete? " +
					"If you find issues, use the appropriate tools to fix them. " +
					"If everything looks good, no further action needed."
				rt.session.Messages = append(rt.session.Messages, ConversationMessage{
					Role:    MsgRoleUser,
					Content: []ContentBlock{&TextBlock{Text: reflectionPrompt}},
				})
			}
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
				Type:      "tool_use",
				ToolName:  tc.Name,
				ToolID:    tc.ID,
				ToolInput: tc.Input,
			})

			// Permission policy check (only when a prompter is configured for interactive decisions)
				if rt.permissionPrompter != nil {
					outcome := rt.policy.Authorize(tc.Name, "", rt.permissionPrompter)
					if outcome == OutcomeDeny {
						denyMsg := rt.policy.DenyReason(tc.Name)
						toolResultBlocks = append(toolResultBlocks, &ToolResultBlock{
							ToolUseID: tc.ID,
							Content:   denyMsg,
							IsError:   true,
						})
						allOutputs = append(allOutputs, TurnOutput{
							Type:     "tool_result",
							ToolName: tc.Name,
							ToolID:   tc.ID,
							Text:     denyMsg,
							IsError:  true,
						})
						continue
					}
				} else {
					// Non-interactive: check if the policy would deny without prompting
					currentMode := rt.policy.ActiveMode()
					requiredMode := rt.policy.RequiredModeFor(tc.Name)
					if currentMode < requiredMode && currentMode != PermAllow && currentMode != PermDontAsk {
						if currentMode == PermReadOnly && requiredMode > PermReadOnly {
							denyMsg := rt.policy.DenyReason(tc.Name)
							toolResultBlocks = append(toolResultBlocks, &ToolResultBlock{
								ToolUseID: tc.ID,
								Content:   denyMsg,
								IsError:   true,
							})
							allOutputs = append(allOutputs, TurnOutput{
								Type:     "tool_result",
								ToolName: tc.Name,
								ToolID:   tc.ID,
								Text:     denyMsg,
								IsError:  true,
							})
							continue
						}
					}
				}

				// Pre-tool hook: check if tool use is allowed
			preResult := rt.hooks.RunPreToolUseMap(tc.Name, tc.Input)
			if preResult.IsDenied() {
				blockedMsg := fmt.Sprintf("blocked by hook: %s", strings.Join(preResult.Messages(), ", "))
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
				if result == "" {
					result = fmt.Sprintf("error: %v", err)
				} else {
					result = result + "\n[error: " + err.Error() + "]"
				}
			}

			// Post-tool hook
			postResult := rt.hooks.RunPostToolUseMap(tc.Name, tc.Input, result, isErr)
			if strings.Join(postResult.Messages(), ", ") != "" {
				result += "\n[hook: " + strings.Join(postResult.Messages(), ", ") + "]"
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

	// Record turn summary
	var toolsCalled []string
	for _, o := range allOutputs {
		if o.Type == "tool_use" && o.ToolName != "" {
			toolsCalled = append(toolsCalled, o.ToolName)
		}
	}
	rt.session.AddTurnSummary(TurnSummary{
		TurnNumber: rt.session.TurnCount() + 1,
		TokenUsage: totalUsage,
		ToolsCalled: toolsCalled,
	})

	return allOutputs, &totalUsage, nil
}

func (rt *ConversationRuntime) buildRequest() *api.MessageRequest {
	msgs := make([]api.InputMessage, 0, len(rt.session.Messages))
	var systemFromMessages string
	for _, m := range rt.session.Messages {
		if m.Role == MsgRoleSystem {
			for _, b := range m.Content {
				if tb, ok := b.(*TextBlock); ok {
					systemFromMessages += tb.Text + "\n"
				}
			}
			continue
		}
		im := api.InputMessage{Role: api.MessageRole(m.Role)}
		for _, b := range m.Content {
			switch v := b.(type) {
			case *TextBlock:
				im.Content = append(im.Content, api.InputContentBlock{
					Type: "text",
					Text: v.Text,
				})
			case *ToolUseBlock:
				input := v.Input
				if input == nil {
					input = map[string]interface{}{}
				}
				im.Content = append(im.Content, api.InputContentBlock{
					Type:  "tool_use",
					ID:    v.ID,
					Name:  v.Name,
					Input: input,
				})
			case *ToolResultBlock:
				content := v.Content
				if content == "" {
					content = " "
				}
				im.Content = append(im.Content, api.InputContentBlock{
					Type:      "tool_result",
					ToolUseID: v.ToolUseID,
					IsError:   v.IsError,
					Content:   []api.ToolResultContentBlock{{Type: "text", Text: content}},
				})
			}
		}
		if len(im.Content) == 0 {
			im.Content = []api.InputContentBlock{{Type: "text", Text: " "}}
		}
		msgs = append(msgs, im)
	}

	combinedSystem := rt.systemPrompt
	if systemFromMessages != "" {
		combinedSystem += "\n\n" + systemFromMessages
	}

	// Use cached system prompt with cache_control breakpoints for Anthropic prompt caching.
	// Falls back to flat string if systemPromptBuilder is not available.
	var sysPrompt json.RawMessage
	if rt.systemPromptBuilder != nil {
		cachedBlocks := rt.systemPromptBuilder.BuildCachedSystem()
		if systemFromMessages != "" {
			// Append extra system content as a final cached block
			cachedBlocks = append(cachedBlocks, CachedSystemBlock{
				Type: "text",
				Text: systemFromMessages,
				CacheControl: &struct {
					Type string `json:"type"`
				}{Type: "ephemeral"},
			})
		}
		sysPrompt, _ = json.Marshal(cachedBlocks)
	} else {
		sysPrompt, _ = json.Marshal(combinedSystem)
	}
	toolDefs := rt.tools.AvailableTools()
	toolDefs = renameToolsForPrompt(toolDefs)

	return &api.MessageRequest{
		Model:      rt.model,
		MaxTokens:  api.MaxTokensForModel(rt.model),
		Messages:   msgs,
		System:     sysPrompt,
		Tools:      toolDefs,
		ToolChoice: api.AutoToolChoice(),
	}
}

// toolRenameMap maps registered tool names to the names used in the system prompt.
// The system prompt references "Read", "Write", "Edit", "Bash" etc., but the tools
// are registered as "read_file", "write_file", "edit_file", "bash". This mapping
// ensures the API request uses names the model can find.
var toolRenameMap = map[string]string{
	"read_file":  "Read",
	"write_file": "Write",
	"edit_file":  "Edit",
	"bash":       "Bash",
	"glob":       "Glob",
	"grep":       "Grep",
}

// toolsToFilter are tools that confuse non-Claude models (e.g., GLM keeps
// calling ToolSearch in a loop instead of using tools directly).
var toolsToFilter = map[string]bool{
	"ToolSearch": true,
}

// renameToolsForPrompt renames tool definitions to match system prompt naming
// and filters out tools that confuse non-Claude models.
func renameToolsForPrompt(tools []api.ToolDefinition) []api.ToolDefinition {
	filtered := make([]api.ToolDefinition, 0, len(tools))
	for _, t := range tools {
		if toolsToFilter[t.Name] {
			continue
		}
		if newName, ok := toolRenameMap[t.Name]; ok {
			t.Name = newName
		}
		filtered = append(filtered, t)
	}
	return filtered
}

// filterToolsForAgent returns a filtered ToolExecutor based on agent type.
func filterToolsForAgent(te ToolExecutor, agentType string) ToolExecutor {
	readOnlyTools := map[string]bool{
		"Read": true, "Glob": true, "Grep": true,
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
		readOnlyTools["Bash"] = true
		readOnlyTools["bash"] = true
		readOnlyTools["PowerShell"] = true
	case "claude-code-guide":
		// read-only + SendUserMessage (already included)
	case "statusline-setup":
		readOnlyTools = map[string]bool{
			"Bash": true, "bash": true,
			"Read": true, "read_file": true,
			"Write": true, "write_file": true,
			"Edit": true, "edit_file": true,
			"Glob": true, "glob": true,
			"Grep": true, "grep": true,
			"ToolSearch": true,
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
func (rt *ConversationRuntime) ExecuteSubAgent(ctx context.Context, prompt string, maxIterations int, agentType string, modelOverride string) (string, error) {
	// Apply tool filtering based on agent type
	toolExec := rt.tools
	if filtered := filterToolsForAgent(rt.tools, agentType); filtered != nil {
		toolExec = filtered
	}

	modelUsed := rt.model
		if modelOverride != "" {
			modelUsed = modelOverride
		}
	subRt := &ConversationRuntime{
		provider:     rt.provider,
		tools:        toolExec,
		session:      NewSession(),
		policy:       rt.policy,
		hooks:        rt.hooks,
		usage:        NewUsageTracker(modelUsed),
		model:        modelUsed,
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

// RunTurnStreaming executes one user turn with TRUE streaming output.
// Text deltas are emitted to outCh as they arrive from the API,
// not buffered until the entire response completes.
func (rt *ConversationRuntime) RunTurnStreaming(ctx context.Context, prompt string, outCh chan<- TurnOutput, usageCh chan<- TokenUsage, errCh chan<- error) {
	defer close(outCh)
	defer close(usageCh)
	defer close(errCh)

	// Add user message
	rt.session.Messages = append(rt.session.Messages, ConversationMessage{
		Role:    MsgRoleUser,
		Content: []ContentBlock{&TextBlock{Text: prompt}},
	})

	var totalUsage TokenUsage

	for i := 0; i < rt.maxIter; i++ {
		req := rt.buildRequest()

		// Get SSE stream with retry
		var eventsCh <-chan api.SSEFrame
		var streamErr error
		for attempt := 0; attempt < 3; attempt++ {
			eventsCh, streamErr = rt.provider.StreamMessage(ctx, req)
			if streamErr == nil {
				break
			}
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * time.Second)
			}
		}
		if streamErr != nil {
			errCh <- fmt.Errorf("stream error (after 3 retries): %w", streamErr)
			return
		}

		// Process SSE frames incrementally — emit text deltas immediately
		streamEvents := api.StreamEventsIncremental(eventsCh)

		var textParts []string
		var toolCalls []ToolUseBlock

		for se := range streamEvents {
			switch se.Type {
			case "text_delta":
				// Emit text delta immediately — this is TRUE streaming
				outCh <- TurnOutput{Type: "text_delta", Text: se.Text}
				textParts = append(textParts, se.Text)

			case "tool_use":
				toolCalls = append(toolCalls, ToolUseBlock{
					ID:    se.ToolID,
					Name:  se.ToolName,
					Input: se.ToolInput,
				})

			case "usage":
				totalUsage.InputTokens += se.Usage.InputTokens
				totalUsage.OutputTokens += se.Usage.OutputTokens
				totalUsage.CacheCreationInputTokens += se.Usage.CacheCreationInputTokens
				totalUsage.CacheReadInputTokens += se.Usage.CacheReadInputTokens
				rt.usage.Record(TokenUsage{
					InputTokens:              se.Usage.InputTokens,
					OutputTokens:             se.Usage.OutputTokens,
					CacheCreationInputTokens: se.Usage.CacheCreationInputTokens,
					CacheReadInputTokens:     se.Usage.CacheReadInputTokens,
				})

			case "error":
				errCh <- fmt.Errorf("stream error: %s", se.Text)
				return

			case "message_stop":
				// This iteration is done
			}
		}

		// Build assistant message for session history
		var assistantBlocks []ContentBlock
		joinedText := joinStrings(textParts)
		if joinedText != "" {
			assistantBlocks = append(assistantBlocks, &TextBlock{Text: joinedText})
		}
		for _, tc := range toolCalls {
			assistantBlocks = append(assistantBlocks, &tc)
		}
		if len(assistantBlocks) == 0 {
			assistantBlocks = append(assistantBlocks, &TextBlock{Text: ""})
		}

		rt.session.Messages = append(rt.session.Messages, ConversationMessage{
			Role:    MsgRoleAssistant,
			Content: assistantBlocks,
			Usage:   &TokenUsage{InputTokens: totalUsage.InputTokens, OutputTokens: totalUsage.OutputTokens},
		})

		// If no tool calls, we're done
		if len(toolCalls) == 0 {
			outCh <- TurnOutput{Type: "done"}
			usageCh <- totalUsage
			return
		}

		// Execute tools
		var toolResultBlocks []ContentBlock
		for _, tc := range toolCalls {
			outCh <- TurnOutput{
				Type:      "tool_use",
				ToolName:  tc.Name,
				ToolID:    tc.ID,
				ToolInput: tc.Input,
			}

			// Permission check
			if rt.permissionPrompter != nil {
				outcome := rt.policy.Authorize(tc.Name, "", rt.permissionPrompter)
				if outcome == OutcomeDeny {
					denyMsg := rt.policy.DenyReason(tc.Name)
					toolResultBlocks = append(toolResultBlocks, &ToolResultBlock{
						ToolUseID: tc.ID, Content: denyMsg, IsError: true,
					})
					outCh <- TurnOutput{
						Type: "tool_result", ToolName: tc.Name, ToolID: tc.ID,
						Text: denyMsg, IsError: true,
					}
					continue
				}
			}

			// Pre-tool hook
			preResult := rt.hooks.RunPreToolUseMap(tc.Name, tc.Input)
			if preResult.IsDenied() {
				blockedMsg := fmt.Sprintf("blocked by hook: %s", strings.Join(preResult.Messages(), ", "))
				toolResultBlocks = append(toolResultBlocks, &ToolResultBlock{
					ToolUseID: tc.ID, Content: blockedMsg, IsError: true,
				})
				outCh <- TurnOutput{
					Type: "tool_result", ToolName: tc.Name, ToolID: tc.ID,
					Text: blockedMsg, IsError: true,
				}
				continue
			}

			// Execute the tool
			result, err := rt.tools.Execute(tc.Name, tc.Input)
			isErr := err != nil
			if isErr {
				if result == "" {
					result = fmt.Sprintf("error: %v", err)
				} else {
					result = result + "\n[error: " + err.Error() + "]"
				}
			}

			// Post-tool hook
			postResult := rt.hooks.RunPostToolUseMap(tc.Name, tc.Input, result, isErr)
			if strings.Join(postResult.Messages(), ", ") != "" {
				result += "\n[hook: " + strings.Join(postResult.Messages(), ", ") + "]"
			}

			toolResultBlocks = append(toolResultBlocks, &ToolResultBlock{
				ToolUseID: tc.ID, Content: result, IsError: isErr,
			})
			outCh <- TurnOutput{
				Type: "tool_result", ToolName: tc.Name, ToolID: tc.ID,
				Text: result, IsError: isErr,
			}
		}

		// Add tool results as next user message
		rt.session.Messages = append(rt.session.Messages, ConversationMessage{
			Role:    MsgRoleUser,
			Content: toolResultBlocks,
		})
	}

	// Max iterations reached
	outCh <- TurnOutput{Type: "done"}
	usageCh <- totalUsage
}

// PermissionMode returns the current permission mode as a string.
func (rt *ConversationRuntime) PermissionMode() string {
	return rt.policy.ActiveMode().String()
}

// SetPermissionMode sets the permission mode from a string.
func (rt *ConversationRuntime) SetPermissionMode(mode string) {
	parsed := PermissionModeFromString(mode)
	def := DefaultPermissionPolicy()
	rt.policy = NewPermissionPolicy(parsed)
	for tool, req := range def.toolRequirements {
		rt.policy.toolRequirements[tool] = req
	}
}

// SetSetting stores a key-value setting.
func (rt *ConversationRuntime) SetSetting(key, value string) {
	// Settings are stored in the session metadata
	if rt.settings == nil {
		rt.settings = make(map[string]string)
	}
	rt.settings[key] = value
}

// Setting retrieves a setting value.
func (rt *ConversationRuntime) Setting(key string) string {
	if rt.settings == nil {
		return ""
	}
	return rt.settings[key]
}

// SetModel changes the active model.
func (rt *ConversationRuntime) SetModel(model string) {
	rt.model = model
}

// SetSystemPrompt sets the system prompt for the conversation.
func (rt *ConversationRuntime) SetSystemPrompt(prompt string) {
	rt.systemPrompt = prompt
}

// SetSystemPromptBuilder sets the system prompt builder for cached prompt generation.
func (rt *ConversationRuntime) SetSystemPromptBuilder(builder *SystemPromptBuilder) {
	rt.systemPromptBuilder = builder
	if builder != nil {
		rt.systemPrompt = builder.Render()
	}
}

// SetPermissionPrompter sets the interactive permission prompter.
func (rt *ConversationRuntime) SetPermissionPrompter(prompter PermissionPrompter) {
	rt.policy = rt.policy.WithPrompter(prompter)
}

// SetReflectionEnabled enables or disables the reflection pattern.
// When enabled, a self-evaluation prompt is injected after the agent's
// final response to encourage quality checking.
func (rt *ConversationRuntime) SetReflectionEnabled(enabled bool) {
	rt.reflectionEnabled = enabled
}

