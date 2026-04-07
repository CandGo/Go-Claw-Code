package api

import (
	"strings"
	"testing"
)

// TestSSEMultiDataFields tests that multi-line data fields are joined with newlines.
func TestSSEMultiDataFields(t *testing.T) {
	input := "event: event\ndata: line1\ndata: line2\n\n"
	ch := ParseSSEStream(strings.NewReader(input))
	frame, ok := <-ch
	if !ok {
		t.Fatal("expected frame")
	}
	if !strings.Contains(frame.Data, "line1") || !strings.Contains(frame.Data, "line2") {
		t.Errorf("Data = %q, should contain both data lines", frame.Data)
	}
}

// TestCollectStreamEventsTextOnly tests collecting a simple text response.
func TestCollectStreamEventsTextOnly(t *testing.T) {
	var input strings.Builder
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","usage":{"input_tokens":50,"output_tokens":0}}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello world"}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_stop","index":0}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_stop"}`)
	input.WriteString("\n\n")

	ch := ParseSSEStream(strings.NewReader(input.String()))
	events, usage, err := CollectStreamEvents(ch)
	if err != nil {
		t.Fatalf("CollectStreamEvents failed: %v", err)
	}
	if usage.InputTokens != 50 {
		t.Errorf("InputTokens = %d, want 50", usage.InputTokens)
	}
	if usage.OutputTokens != 10 {
		t.Errorf("OutputTokens = %d, want 10", usage.OutputTokens)
	}
	foundText := false
	for _, e := range events {
		if e.Type == "text" && e.Text == "Hello world" {
			foundText = true
		}
	}
	if !foundText {
		t.Errorf("expected text event 'Hello world', got events: %+v", events)
	}
}

// TestCollectStreamEventsToolUse tests collecting a tool_use response.
func TestCollectStreamEventsToolUse(t *testing.T) {
	var input strings.Builder
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","usage":{"input_tokens":100,"output_tokens":0}}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"Read","input":{}}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"/tmp/test.txt\"}"}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_stop","index":0}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":30}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_stop"}`)
	input.WriteString("\n\n")

	ch := ParseSSEStream(strings.NewReader(input.String()))
	events, _, err := CollectStreamEvents(ch)
	if err != nil {
		t.Fatalf("CollectStreamEvents failed: %v", err)
	}
	foundTool := false
	for _, e := range events {
		if e.Type == "tool_use" && e.ToolName == "Read" && e.ToolID == "tool_1" {
			if e.ToolInput["path"] == "/tmp/test.txt" {
				foundTool = true
			}
		}
	}
	if !foundTool {
		t.Errorf("expected tool_use event for read_file, got: %+v", events)
	}
}

// TestCollectStreamEventsThinking tests collecting thinking blocks.
func TestCollectStreamEventsThinking(t *testing.T) {
	var input strings.Builder
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_start","message":{"id":"msg_3","type":"message","role":"assistant","usage":{"input_tokens":200,"output_tokens":0}}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"I should analyze this carefully."}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_123"}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_stop","index":0}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"The answer is 42."}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"content_block_stop","index":1}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":50}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_stop"}`)
	input.WriteString("\n\n")

	ch := ParseSSEStream(strings.NewReader(input.String()))
	events, usage, err := CollectStreamEvents(ch)
	if err != nil {
		t.Fatalf("CollectStreamEvents failed: %v", err)
	}

	foundThinking := false
	foundSignature := false
	foundText := false
	for _, e := range events {
		if e.Type == "thinking" && e.Thinking == "I should analyze this carefully." {
			foundThinking = true
		}
		if e.Type == "thinking" && e.Signature == "sig_123" {
			foundSignature = true
		}
		if e.Type == "text" && e.Text == "The answer is 42." {
			foundText = true
		}
	}
	if !foundThinking {
		t.Error("expected thinking event")
	}
	if !foundSignature {
		t.Error("expected signature event")
	}
	if !foundText {
		t.Error("expected text event")
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", usage.OutputTokens)
	}
}

// TestCollectStreamEventsError tests that error events are returned as errors.
func TestCollectStreamEventsError(t *testing.T) {
	var input strings.Builder
	input.WriteString("event: error\ndata: ")
	input.WriteString(`{"type":"error","error":{"type":"overloaded","message":"Server is busy"}}`)
	input.WriteString("\n\n")

	ch := ParseSSEStream(strings.NewReader(input.String()))
	_, _, err := CollectStreamEvents(ch)
	if err == nil {
		t.Fatal("expected error from error event")
	}
	if !strings.Contains(err.Error(), "overloaded") {
		t.Errorf("error = %v, should mention overloaded", err)
	}
}

// TestCollectStreamEventsWithCache tests cache token parsing.
func TestCollectStreamEventsWithCache(t *testing.T) {
	var input strings.Builder
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_start","message":{"id":"msg_4","type":"message","role":"assistant","usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":100,"cache_read_input_tokens":200}}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`)
	input.WriteString("\n\n")
	input.WriteString("event: event\ndata: ")
	input.WriteString(`{"type":"message_stop"}`)
	input.WriteString("\n\n")

	ch := ParseSSEStream(strings.NewReader(input.String()))
	_, usage, err := CollectStreamEvents(ch)
	if err != nil {
		t.Fatalf("CollectStreamEvents failed: %v", err)
	}
	if usage.CacheCreationInputTokens != 100 {
		t.Errorf("CacheCreationInputTokens = %d, want 100", usage.CacheCreationInputTokens)
	}
	if usage.CacheReadInputTokens != 200 {
		t.Errorf("CacheReadInputTokens = %d, want 200", usage.CacheReadInputTokens)
	}
}

// TestIsRetryableStatus tests retry logic for HTTP status codes.
func TestIsRetryableStatus(t *testing.T) {
	retryable := []int{408, 409, 429, 500, 502, 503, 504}
	for _, code := range retryable {
		if !isRetryableStatus(code) {
			t.Errorf("expected status %d to be retryable", code)
		}
	}
	nonRetryable := []int{200, 201, 400, 401, 403, 404, 405, 422}
	for _, code := range nonRetryable {
		if isRetryableStatus(code) {
			t.Errorf("expected status %d to NOT be retryable", code)
		}
	}
}

// TestSSEFrameWithCommentLines tests that SSE comment lines are ignored.
func TestSSEFrameWithCommentLines(t *testing.T) {
	input := ": this is a comment\nevent: ping\ndata: {}\n\n"
	ch := ParseSSEStream(strings.NewReader(input))
	frame, ok := <-ch
	if !ok {
		t.Fatal("expected frame after comment")
	}
	if frame.Event != "ping" {
		t.Errorf("Event = %q, want ping", frame.Event)
	}
}

// TestInputMessageConstructors tests message construction helpers.
func TestInputMessageConstructors(t *testing.T) {
	msg := UserTextMessage("hello")
	if msg.Role != RoleUser {
		t.Errorf("Role = %q, want user", msg.Role)
	}
	if len(msg.Content) != 1 || msg.Content[0].Type != "text" || msg.Content[0].Text != "hello" {
		t.Errorf("Content = %+v, want text block with 'hello'", msg.Content)
	}

	toolMsg := UserToolResultMessage("tool_1", "result text", false)
	if toolMsg.Role != RoleUser {
		t.Errorf("Role = %q, want user", toolMsg.Role)
	}
	if len(toolMsg.Content) != 1 || toolMsg.Content[0].Type != "tool_result" {
		t.Errorf("Content = %+v, want tool_result", toolMsg.Content)
	}
	if toolMsg.Content[0].ToolUseID != "tool_1" {
		t.Errorf("ToolUseID = %q, want tool_1", toolMsg.Content[0].ToolUseID)
	}

	toolErr := UserToolResultMessage("tool_2", "error occurred", true)
	if !toolErr.Content[0].IsError {
		t.Error("expected IsError = true")
	}
}

// TestOAuthCredentialsIsExpired tests token expiry detection.
func TestOAuthCredentialsIsExpired(t *testing.T) {
	expired := &OAuthCredentials{ExpiresAt: 1} // long ago
	if !expired.IsExpired() {
		t.Error("expected expired token")
	}

	future := &OAuthCredentials{ExpiresAt: 99999999999} // far future
	if future.IsExpired() {
		t.Error("expected non-expired token")
	}
}

// TestNamedToolChoice tests tool choice constructors.
func TestNamedToolChoice(t *testing.T) {
	tc := NamedToolChoice("Read")
	if tc.Type != "tool" || tc.ToolName != "Read" {
		t.Errorf("NamedToolChoice = %+v, want type=tool, name=read_file", tc)
	}
	tc2 := AnyToolChoice()
	if tc2.Type != "any" {
		t.Errorf("AnyToolChoice.Type = %q, want any", tc2.Type)
	}
}
