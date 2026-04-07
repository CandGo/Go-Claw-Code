package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-claw/claw/internal/api"
)

// mockProvider implements api.Provider for testing.
type mockProvider struct {
	responses []mockResponse
	callIndex int
}

type mockResponse struct {
	text      string
	toolCalls []mockToolCall
	usage     api.Usage
}

type mockToolCall struct {
	id   string
	name string
	args map[string]interface{}
}

func (m *mockProvider) SendMessage(ctx context.Context, req *api.MessageRequest) (*api.MessageResponse, error) {
	return nil, nil
}

func (m *mockProvider) StreamMessage(ctx context.Context, req *api.MessageRequest) (<-chan api.SSEFrame, error) {
	ch := make(chan api.SSEFrame, 64)
	go func() {
		defer close(ch)
		if m.callIndex >= len(m.responses) {
			// No more responses, send empty text
			ch <- api.SSEFrame{Event: "event", Data: `{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","usage":{"input_tokens":10,"output_tokens":0}}}`}
			ch <- api.SSEFrame{Event: "event", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`}
			ch <- api.SSEFrame{Event: "event", Data: `{"type":"content_block_stop","index":0}`}
			ch <- api.SSEFrame{Event: "event", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`}
			ch <- api.SSEFrame{Event: "event", Data: `{"type":"message_stop"}`}
			return
		}

		resp := m.responses[m.callIndex]
		m.callIndex++

		// message_start
		ch <- api.SSEFrame{Event: "event", Data: `{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","usage":{"input_tokens":` + itoa(resp.usage.InputTokens) + `,"output_tokens":0}}}`}

		blockIdx := 0

		// Text block
		if resp.text != "" {
			ch <- api.SSEFrame{Event: "event", Data: `{"type":"content_block_start","index":` + itoa(blockIdx) + `,"content_block":{"type":"text","text":""}}`}
			escaped := strings.ReplaceAll(resp.text, `"`, `\"`)
			escaped = strings.ReplaceAll(escaped, "\n", `\n`)
			ch <- api.SSEFrame{Event: "event", Data: `{"type":"content_block_delta","index":` + itoa(blockIdx) + `,"delta":{"type":"text_delta","text":"` + escaped + `"}}`}
			ch <- api.SSEFrame{Event: "event", Data: `{"type":"content_block_stop","index":` + itoa(blockIdx) + `}`}
			blockIdx++
		}

		// Tool calls
		for _, tc := range resp.toolCalls {
			ch <- api.SSEFrame{Event: "event", Data: `{"type":"content_block_start","index":` + itoa(blockIdx) + `,"content_block":{"type":"tool_use","id":"` + tc.id + `","name":"` + tc.name + `","input":{}}}`}
			argsJSON := marshalHookJSON(tc.args)
			ch <- api.SSEFrame{Event: "event", Data: `{"type":"content_block_delta","index":` + itoa(blockIdx) + `,"delta":{"type":"input_json_delta","partial_json":"` + strings.ReplaceAll(argsJSON, `"`, `\\\"`) + `"}}`}
			ch <- api.SSEFrame{Event: "event", Data: `{"type":"content_block_stop","index":` + itoa(blockIdx) + `}`}
			blockIdx++
		}

		stopReason := "end_turn"
		if len(resp.toolCalls) > 0 {
			stopReason = "tool_use"
		}
		ch <- api.SSEFrame{Event: "event", Data: `{"type":"message_delta","delta":{"stop_reason":"` + stopReason + `"},"usage":{"output_tokens":` + itoa(resp.usage.OutputTokens) + `}}`}
		ch <- api.SSEFrame{Event: "event", Data: `{"type":"message_stop"}`}
	}()
	return ch, nil
}

// mockToolExecutor implements ToolExecutor for testing.
type mockToolExecutor struct {
	results map[string]string
	errors  map[string]error
}

func (m *mockToolExecutor) Execute(toolName string, input map[string]interface{}) (string, error) {
	if err, ok := m.errors[toolName]; ok {
		return "", err
	}
	if result, ok := m.results[toolName]; ok {
		return result, nil
	}
	return "mock result", nil
}

func (m *mockToolExecutor) AvailableTools() []api.ToolDefinition {
	return []api.ToolDefinition{
		{Name: "Read", Description: "Read file"},
		{Name: "Bash", Description: "Run bash"},
		{Name: "Write", Description: "Write file"},
	}
}

// --- Tests ---

func TestRunTurnSimpleText(t *testing.T) {
	provider := &mockProvider{
		responses: []mockResponse{
			{text: "Hello! I can help.", usage: api.Usage{InputTokens: 100, OutputTokens: 50}},
		},
	}
	tools := &mockToolExecutor{}
	rt := NewConversationRuntime(provider, tools, "test-model")
	rt.maxIter = 5

	outputs, usage, err := rt.RunTurn(context.Background(), "hi")
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}

	if len(outputs) == 0 {
		t.Fatal("expected outputs")
	}
	foundText := false
	for _, out := range outputs {
		if out.Type == "text" && strings.Contains(out.Text, "Hello!") {
			foundText = true
		}
	}
	if !foundText {
		t.Errorf("expected text output containing 'Hello!', got %v", outputs)
	}
	if usage.InputTokens == 0 {
		t.Error("expected non-zero input tokens")
	}
}

func TestRunTurnWithToolCall(t *testing.T) {
	provider := &mockProvider{
		responses: []mockResponse{
			{
				text: "Let me read that file.",
				toolCalls: []mockToolCall{
					{id: "tool_1", name: "Read", args: map[string]interface{}{"path": "/tmp/test.txt"}},
				},
				usage: api.Usage{InputTokens: 100, OutputTokens: 50},
			},
			{
				text:  "The file contains 'hello world'.",
				usage: api.Usage{InputTokens: 200, OutputTokens: 30},
			},
		},
	}
	tools := &mockToolExecutor{
		results: map[string]string{"Read": "file content: hello world"},
	}
	rt := NewConversationRuntime(provider, tools, "test-model")
	rt.maxIter = 5

	outputs, _, err := rt.RunTurn(context.Background(), "read /tmp/test.txt")
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}

	// Should have: text, tool_use, tool_result, text
	foundToolUse := false
	foundToolResult := false
	foundFinalText := false
	for _, out := range outputs {
		if out.Type == "tool_use" && out.ToolName == "Read" {
			foundToolUse = true
		}
		if out.Type == "tool_result" && out.ToolName == "Read" {
			foundToolResult = true
		}
		if out.Type == "text" && strings.Contains(out.Text, "hello world") {
			foundFinalText = true
		}
	}
	if !foundToolUse {
		t.Error("expected tool_use output")
	}
	if !foundToolResult {
		t.Error("expected tool_result output")
	}
	if !foundFinalText {
		t.Error("expected final text output")
	}
}

func TestRunTurnContextCancellation(t *testing.T) {
	provider := &mockProvider{
		responses: []mockResponse{
			{text: "ok", usage: api.Usage{InputTokens: 10, OutputTokens: 5}},
		},
	}
	tools := &mockToolExecutor{}
	rt := NewConversationRuntime(provider, tools, "test-model")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, _, err := rt.RunTurn(ctx, "test")
	// Should still work since the mock doesn't actually block,
	// but context should be detected
	if err != nil {
		// Context cancelled error is acceptable
		if !strings.Contains(err.Error(), "context") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestExecuteSubAgent(t *testing.T) {
	provider := &mockProvider{
		responses: []mockResponse{
			{text: "Sub-agent result: found 3 items", usage: api.Usage{InputTokens: 50, OutputTokens: 20}},
		},
	}
	tools := &mockToolExecutor{}
	rt := NewConversationRuntime(provider, tools, "test-model")

	result, err := rt.ExecuteSubAgent(context.Background(), "search for items", 3, "Explore", "")
	if err != nil {
		t.Fatalf("ExecuteSubAgent failed: %v", err)
	}
	if !strings.Contains(result, "Sub-agent result") {
		t.Errorf("unexpected sub-agent result: %s", result)
	}
}

func TestFilterToolsForAgent(t *testing.T) {
	tools := &mockToolExecutor{}

	tests := []struct {
		agentType      string
		expectBash     bool
		ExpectReadFile bool
		expectWrite    bool
	}{
		{"Explore", false, true, false},
		{"Plan", false, true, false},
		{"Verification", true, true, false},
		{"claude-code-guide", false, true, false},
		{"statusline-setup", true, true, true},
		{"general-purpose", true, true, true},
	}

	for _, tt := range tests {
		filtered := filterToolsForAgent(tools, tt.agentType)
		if filtered == nil {
			if !tt.expectBash || !tt.ExpectReadFile {
				t.Errorf("agentType %s: expected filtered tools, got nil", tt.agentType)
			}
			continue
		}
		names := make(map[string]bool)
		for _, t := range filtered.AvailableTools() {
			names[t.Name] = true
		}
		if names["Bash"] != tt.expectBash {
			t.Errorf("agentType %s: bash=%v, want %v", tt.agentType, names["Bash"], tt.expectBash)
		}
		if names["Read"] != tt.ExpectReadFile {
			t.Errorf("agentType %s: read_file=%v, want %v", tt.agentType, names["Read"], tt.ExpectReadFile)
		}
		if names["Write"] != tt.expectWrite {
			t.Errorf("agentType %s: write_file=%v, want %v", tt.agentType, names["Write"], tt.expectWrite)
		}
	}
}

func TestRunTurnTurnSummary(t *testing.T) {
	provider := &mockProvider{
		responses: []mockResponse{
			{text: "done", usage: api.Usage{InputTokens: 100, OutputTokens: 50}},
		},
	}
	tools := &mockToolExecutor{}
	rt := NewConversationRuntime(provider, tools, "test-model")

	rt.RunTurn(context.Background(), "test")

	session := rt.GetSession()
	if len(session.TurnSummaries) != 1 {
		t.Fatalf("expected 1 turn summary, got %d", len(session.TurnSummaries))
	}
	summary := session.TurnSummaries[0]
	if summary.TurnNumber != 1 {
		t.Errorf("TurnNumber = %d, want 1", summary.TurnNumber)
	}
	if summary.TokenUsage.InputTokens == 0 {
		t.Error("expected non-zero input tokens in summary")
	}
}

// TestRunTurn_NoTools verifies that RunTurn returns a simple text response
// when the provider returns text with no tool calls.
func TestRunTurn_NoTools(t *testing.T) {
	provider := &mockProvider{
		responses: []mockResponse{
			{text: "The answer is 42.", usage: api.Usage{InputTokens: 50, OutputTokens: 20}},
		},
	}
	tools := &mockToolExecutor{}
	rt := NewConversationRuntime(provider, tools, "test-model")

	outputs, usage, err := rt.RunTurn(context.Background(), "what is the answer?")
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}
	if len(outputs) == 0 {
		t.Fatal("expected at least one output")
	}

	found := false
	for _, out := range outputs {
		if out.Type == "text" && out.Text == "The answer is 42." {
			found = true
		}
	}
	if !found {
		t.Errorf("expected text output 'The answer is 42.', got outputs: %+v", outputs)
	}
	if usage.InputTokens != 50 {
		t.Errorf("usage.InputTokens = %d, want 50", usage.InputTokens)
	}
	if usage.OutputTokens != 20 {
		t.Errorf("usage.OutputTokens = %d, want 20", usage.OutputTokens)
	}
}

// TestRunTurn_ToolUse verifies that a tool_use response triggers tool execution
// and the output includes both tool and text results.
func TestRunTurn_ToolUse(t *testing.T) {
	provider := &mockProvider{
		responses: []mockResponse{
			{
				toolCalls: []mockToolCall{
					{id: "tool_1", name: "Bash", args: map[string]interface{}{"command": "echo hello"}},
				},
				usage: api.Usage{InputTokens: 100, OutputTokens: 30},
			},
			{
				text:  "The command output 'hello'.",
				usage: api.Usage{InputTokens: 80, OutputTokens: 25},
			},
		},
	}
	tools := &mockToolExecutor{
		results: map[string]string{"Bash": "hello"},
	}
	rt := NewConversationRuntime(provider, tools, "test-model")
	// Bash requires full-access permission; set to "allow" so it is not denied.
	rt.SetPermissionMode("allow")

	outputs, _, err := rt.RunTurn(context.Background(), "run echo hello")
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}

	foundToolUse := false
	foundToolResult := false
	foundText := false
	for _, out := range outputs {
		if out.Type == "tool_use" && out.ToolName == "Bash" {
			foundToolUse = true
		}
		if out.Type == "tool_result" && out.ToolName == "Bash" && !out.IsError {
			foundToolResult = true
		}
		if out.Type == "text" && strings.Contains(out.Text, "command output") {
			foundText = true
		}
	}
	if !foundToolUse {
		t.Error("expected tool_use output for bash")
	}
	if !foundToolResult {
		t.Error("expected tool_result output for bash")
	}
	if !foundText {
		t.Error("expected final text output")
	}
}

// TestRunTurn_MultipleTools verifies that two tool calls in a single response
// are both executed.
func TestRunTurn_MultipleTools(t *testing.T) {
	provider := &mockProvider{
		responses: []mockResponse{
			{
				toolCalls: []mockToolCall{
					{id: "tool_1", name: "Read", args: map[string]interface{}{"path": "/tmp/a.txt"}},
					{id: "tool_2", name: "Bash", args: map[string]interface{}{"command": "ls /tmp"}},
				},
				usage: api.Usage{InputTokens: 100, OutputTokens: 50},
			},
			{
				text:  "Here are the results.",
				usage: api.Usage{InputTokens: 200, OutputTokens: 20},
			},
		},
	}

	executedTools := []string{}
	tools := &mockToolExecutorWithTracker{
		results: map[string]string{
			"Read": "file content A",
			"Bash":      "a.txt b.txt",
		},
		tracker: &executedTools,
	}

	rt := NewConversationRuntime(provider, tools, "test-model")
	// bash requires full-access permission
	rt.SetPermissionMode("allow")
	outputs, _, err := rt.RunTurn(context.Background(), "read and list")
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}

	// Both tools should have been called
	if len(executedTools) != 2 {
		t.Errorf("expected 2 tool executions, got %d: %v", len(executedTools), executedTools)
	}

	// Both tool_use and tool_result outputs should be present
	toolUseCount := 0
	toolResultCount := 0
	for _, out := range outputs {
		if out.Type == "tool_use" {
			toolUseCount++
		}
		if out.Type == "tool_result" && !out.IsError {
			toolResultCount++
		}
	}
	if toolUseCount != 2 {
		t.Errorf("expected 2 tool_use outputs, got %d", toolUseCount)
	}
	if toolResultCount != 2 {
		t.Errorf("expected 2 tool_result outputs, got %d", toolResultCount)
	}
}

// mockToolExecutorWithTracker tracks which tools were called.
type mockToolExecutorWithTracker struct {
	results map[string]string
	tracker *[]string
}

func (m *mockToolExecutorWithTracker) Execute(toolName string, input map[string]interface{}) (string, error) {
	*m.tracker = append(*m.tracker, toolName)
	if r, ok := m.results[toolName]; ok {
		return r, nil
	}
	return "mock result", nil
}

func (m *mockToolExecutorWithTracker) AvailableTools() []api.ToolDefinition {
	return []api.ToolDefinition{
		{Name: "Read", Description: "Read file"},
		{Name: "Bash", Description: "Run bash"},
	}
}

// TestCompaction verifies that ShouldCompact detects a large session and
// Compact reduces the message count.
func TestCompaction(t *testing.T) {
	rt := NewConversationRuntime(
		&mockProvider{responses: []mockResponse{{text: "ok"}}},
		&mockToolExecutor{},
		"test-model",
	)

	// Fill the session with many messages to exceed the token threshold
	for i := 0; i < 20; i++ {
		rt.session.AddUserMessage(strings.Repeat("This is a long user message about implementing feature number "+itoa(i)+". ", 50))
		rt.session.AddAssistantMessage([]ContentBlock{
			&TextBlock{Text: strings.Repeat("Assistant response discussing the implementation details for feature "+itoa(i)+". ", 50)},
		}, nil)
	}

	cfg := CompactionConfig{
		PreserveRecentMessages: 4,
		MaxEstimatedTokens:     500,
	}

	msgCountBefore := rt.MessageCount()
	if !rt.ShouldCompact(cfg) {
		t.Fatalf("ShouldCompact should return true with %d messages", msgCountBefore)
	}

	result := rt.Compact(cfg)
	if result.MessagesBefore <= result.MessagesAfter {
		t.Errorf("compaction should reduce messages: before=%d after=%d", result.MessagesBefore, result.MessagesAfter)
	}
	if rt.MessageCount() >= msgCountBefore {
		t.Errorf("MessageCount() should be less after compaction: before=%d after=%d", msgCountBefore, rt.MessageCount())
	}
	// First message should now be a system summary
	if len(rt.Session().Messages) == 0 {
		t.Fatal("session should have messages after compaction")
	}
	if rt.Session().Messages[0].Role != MsgRoleSystem {
		t.Errorf("first message role after compaction = %q, want system", rt.Session().Messages[0].Role)
	}
}

// TestPermissionDeny verifies that a tool requiring elevated permissions
// is denied when the policy is set to read-only.
func TestPermissionDeny(t *testing.T) {
	provider := &mockProvider{
		responses: []mockResponse{
			{
				toolCalls: []mockToolCall{
					{id: "tool_1", name: "Write", args: map[string]interface{}{"path": "/tmp/test.txt", "content": "data"}},
				},
				usage: api.Usage{InputTokens: 50, OutputTokens: 20},
			},
			{
				text:  "I understand.",
				usage: api.Usage{InputTokens: 30, OutputTokens: 10},
			},
		},
	}
	tools := &mockToolExecutor{
		results: map[string]string{"Write": "ok"},
	}
	rt := NewConversationRuntime(provider, tools, "test-model")
	// Set read-only permission mode
	rt.SetPermissionMode("read-only")

	outputs, _, err := rt.RunTurn(context.Background(), "write a file")
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}

	// Find the tool_result for write_file and verify it was denied
	foundDenied := false
	for _, out := range outputs {
		if out.Type == "tool_result" && out.ToolName == "Write" && out.IsError {
			foundDenied = true
			if out.Text == "" {
				t.Error("denied tool result should have a deny reason")
			}
		}
	}
	if !foundDenied {
		t.Error("expected write_file to be denied due to read-only policy")
	}
}

// TestUndo verifies that removing the last assistant turn works correctly.
func TestUndo(t *testing.T) {
	provider := &mockProvider{
		responses: []mockResponse{
			{text: "First response.", usage: api.Usage{InputTokens: 10, OutputTokens: 5}},
		},
	}
	tools := &mockToolExecutor{}
	rt := NewConversationRuntime(provider, tools, "test-model")

	_, _, err := rt.RunTurn(context.Background(), "do something")
	if err != nil {
		t.Fatalf("RunTurn failed: %v", err)
	}

	msgCountAfterTurn := rt.MessageCount()
	if msgCountAfterTurn == 0 {
		t.Fatal("expected messages after RunTurn")
	}

	// Undo: remove the last assistant message and its preceding user message
	rt.session.Messages = rt.session.Messages[:len(rt.session.Messages)-2]

	msgCountAfterUndo := rt.MessageCount()
	if msgCountAfterUndo >= msgCountAfterTurn {
		t.Errorf("expected fewer messages after undo: before=%d after=%d", msgCountAfterTurn, msgCountAfterUndo)
	}

	// Verify the last message is not the assistant's response
	if len(rt.session.Messages) > 0 {
		last := rt.session.Messages[len(rt.session.Messages)-1]
		if last.Role == MsgRoleAssistant {
			t.Error("last message should not be an assistant message after undo")
		}
	}
}

// --- Helpers ---

func itoa(n int) string { return fmt.Sprintf("%d", n) }
