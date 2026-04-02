package api

import (
	"context"
	"strings"
)

// ProviderKind identifies which LLM provider to use.
type ProviderKind int

const (
	ProviderAnthropic ProviderKind = iota
	ProviderOpenAI
)

// Provider is the interface for LLM API backends.
type Provider interface {
	// SendMessage sends a non-streaming request.
	SendMessage(ctx context.Context, req *MessageRequest) (*MessageResponse, error)
	// StreamMessage sends a streaming request, returning SSE frames via channel.
	StreamMessage(ctx context.Context, req *MessageRequest) (<-chan SSEFrame, error)
}

// ResolveModelAlias maps short aliases to full model names.
func ResolveModelAlias(model string) string {
	aliases := map[string]string{
		"opus":   "claude-opus-4-6",
		"sonnet": "claude-sonnet-4-6",
		"haiku":  "claude-haiku-4-5-20251001",
	}
	if full, ok := aliases[strings.ToLower(model)]; ok {
		return full
	}
	return model
}

// DetectProviderKind returns the provider kind based on the model name.
func DetectProviderKind(model string) ProviderKind {
	m := strings.ToLower(model)
	if strings.Contains(m, "gpt") || strings.Contains(m, "o1") || strings.Contains(m, "o3") ||
		strings.Contains(m, "deepseek") || strings.Contains(m, "qwen") || strings.Contains(m, "glm") {
		return ProviderOpenAI
	}
	return ProviderAnthropic
}

// MaxTokensForModel returns the default max output tokens for a model.
func MaxTokensForModel(model string) int {
	m := strings.ToLower(model)
	if strings.Contains(m, "opus") {
		return 32000
	}
	return 64000
}

// NewProvider creates the appropriate provider based on model name and config.
func NewProvider(model string, auth *AuthSource, baseURL string) Provider {
	// If the base URL suggests Anthropic-compatible API, always use Anthropic client
	lowerURL := strings.ToLower(baseURL)
	if strings.Contains(lowerURL, "anthropic") {
		return NewAnthropicClient(baseURL, auth, model)
	}

	kind := DetectProviderKind(model)
	switch kind {
	case ProviderOpenAI:
		return NewOpenAICompatClient(baseURL, auth, model)
	default:
		return NewAnthropicClient(baseURL, auth, model)
	}
}
