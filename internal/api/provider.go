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
	ProviderXAI
)

// Provider is the interface for LLM API backends.
type Provider interface {
	// SendMessage sends a non-streaming request.
	SendMessage(ctx context.Context, req *MessageRequest) (*MessageResponse, error)
	// StreamMessage sends a streaming request, returning SSE frames via channel.
	StreamMessage(ctx context.Context, req *MessageRequest) (<-chan SSEFrame, error)
}

// ResolveModelAlias maps short aliases to full model names using the model registry.
func ResolveModelAlias(model string) string {
	if entry := lookupModel(model); entry != nil {
		return entry.ID
	}
	return model
}

// DetectProviderKind returns the provider kind based on the model name.
func DetectProviderKind(model string) ProviderKind {
	// Check registry first
	if entry := lookupModel(model); entry != nil {
		return providerFromString(entry.Provider)
	}

	// Fallback heuristics for unknown models
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "gpt"), strings.Contains(m, "o1-"), strings.Contains(m, "o3-"):
		return ProviderOpenAI
	case strings.Contains(m, "grok"):
		return ProviderXAI
	case strings.Contains(m, "deepseek"), strings.Contains(m, "qwen"), strings.Contains(m, "glm"):
		return ProviderOpenAI // OpenAI-compatible
	default:
		return ProviderAnthropic
	}
}

// MaxTokensForModel returns the default max output tokens for a model.
func MaxTokensForModel(model string) int {
	if entry := lookupModel(model); entry != nil {
		return entry.MaxTokens
	}
	return 4096 // safe default
}

// NewProvider creates the appropriate provider based on model name and config.
// URL takes priority: if the URL is an Anthropic-compatible endpoint, use AnthropicClient
// regardless of model name (e.g. GLM via /api/anthropic).
func NewProvider(model string, auth *AuthSource, baseURL string) Provider {
	// Cloud providers use Anthropic protocol with custom auth
	if auth != nil && (auth.Method == AuthAWSSigV4 || auth.Method == AuthGoogleADC || auth.Method == AuthAzureToken) {
		return NewAnthropicClient(baseURL, auth, model)
	}

	// URL-based detection first — URL determines the protocol
	lowerURL := strings.ToLower(baseURL)
	if strings.Contains(lowerURL, "anthropic") {
		return NewAnthropicClient(baseURL, auth, model)
	}

	// Model-based detection for non-Anthropic URLs
	kind := DetectProviderKind(model)
	switch kind {
	case ProviderOpenAI, ProviderXAI:
		return NewOpenAICompatClient(baseURL, auth, model)
	default:
		return NewAnthropicClient(baseURL, auth, model)
	}
}
