package api

import (
	"strings"
	"testing"
)

func TestResolveModelAlias(t *testing.T) {
	// Known alias should resolve
	result := ResolveModelAlias("opus")
	entry := lookupModel("opus")
	if entry != nil && result != entry.ID {
		t.Errorf("ResolveModelAlias(opus) = %q, want %q", result, entry.ID)
	}

	// Unknown model passes through
	result = ResolveModelAlias("my-custom-model")
	if result != "my-custom-model" {
		t.Errorf("ResolveModelAlias(unknown) = %q, want passthrough", result)
	}

	// Full model ID should resolve to itself
	result = ResolveModelAlias("claude-sonnet-4-6")
	if result != "claude-sonnet-4-6" {
		t.Errorf("ResolveModelAlias(full ID) = %q, want claude-sonnet-4-6", result)
	}
}

func TestDetectProviderKind(t *testing.T) {
	tests := []struct {
		model    string
		expected ProviderKind
	}{
		{"claude-sonnet-4-6", ProviderAnthropic},
		{"claude-opus-4-6", ProviderAnthropic},
		{"gpt-4o", ProviderOpenAI},
		{"o1-preview", ProviderOpenAI},
		{"o3-mini", ProviderOpenAI},
		{"grok-2", ProviderXAI},
		{"deepseek-chat", ProviderOpenAI},
		{"qwen-plus", ProviderOpenAI},
		{"glm-5.1", ProviderOpenAI},
		{"unknown-model", ProviderAnthropic},
	}
	for _, tt := range tests {
		result := DetectProviderKind(tt.model)
		if result != tt.expected {
			t.Errorf("DetectProviderKind(%q) = %d, want %d", tt.model, result, tt.expected)
		}
	}
}

func TestMaxTokensForModel(t *testing.T) {
	// Known models should have positive token limits
	if tokens := MaxTokensForModel("claude-sonnet-4-6"); tokens <= 0 {
		t.Errorf("MaxTokensForModel(sonnet) = %d, want > 0", tokens)
	}
	// Unknown model should get default
	if tokens := MaxTokensForModel("nonexistent-model"); tokens != 4096 {
		t.Errorf("MaxTokensForModel(unknown) = %d, want 4096", tokens)
	}
}

func TestNewProviderAnthropicURL(t *testing.T) {
	auth := &AuthSource{APIKey: "test-key"}
	p := NewProvider("claude-sonnet-4-6", auth, "https://api.anthropic.com")
	if p == nil {
		t.Error("NewProvider returned nil for Anthropic URL")
	}
}

func TestNewProviderOpenAICompat(t *testing.T) {
	auth := &AuthSource{APIKey: "test-key"}
	p := NewProvider("gpt-4o", auth, "https://api.openai.com")
	if p == nil {
		t.Error("NewProvider returned nil for OpenAI model")
	}
}

func TestAutoToolChoice(t *testing.T) {
	tc := AutoToolChoice()
	if tc.Type != "auto" {
		t.Errorf("AutoToolChoice().Type = %q, want 'auto'", tc.Type)
	}
}

func TestSSEFrameParsing(t *testing.T) {
	input := "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"
	ch := ParseSSEStream(strings.NewReader(input))
	frame, ok := <-ch
	if !ok {
		t.Fatal("expected SSE frame, channel closed")
	}
	// Event is trimmed (TrimSpace)
	if frame.Event != "message_start" {
		t.Errorf("frame.Event = %q, want 'message_start'", frame.Event)
	}
	// Data preserves content after "data:" prefix (includes leading space)
	if !strings.Contains(frame.Data, `"type":"message_start"`) {
		t.Errorf("frame.Data = %q, should contain message_start data", frame.Data)
	}
}

func TestSSEFrameMultiEvent(t *testing.T) {
	input := "event: ping\ndata: {}\n\nevent: message_stop\ndata: {}\n\n"
	ch := ParseSSEStream(strings.NewReader(input))

	count := 0
	for range ch {
		count++
	}
	if count != 2 {
		t.Errorf("got %d SSE frames, want 2", count)
	}
}

func TestSSEFrameEmpty(t *testing.T) {
	ch := ParseSSEStream(strings.NewReader(""))
	count := 0
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("got %d frames from empty input, want 0", count)
	}
}

func TestAuthSource(t *testing.T) {
	auth := &AuthSource{APIKey: "sk-test", BearerToken: "tok"}
	if auth.APIKey != "sk-test" {
		t.Error("APIKey not set")
	}
	if auth.BearerToken != "tok" {
		t.Error("BearerToken not set")
	}
}

func TestUsageTotalInputTokens(t *testing.T) {
	u := Usage{
		InputTokens:              100,
		CacheCreationInputTokens: 50,
		CacheReadInputTokens:     25,
	}
	if total := u.TotalInputTokens(); total != 175 {
		t.Errorf("TotalInputTokens() = %d, want 175", total)
	}
}
