package api

import "strings"

// ModelEntry describes a known model with metadata.
type ModelEntry struct {
	ID              string
	Provider        string  // "anthropic", "openai", "xai"
	MaxTokens       int
	InputPrice      float64 // per million tokens (USD)
	OutputPrice     float64 // per million tokens (USD)
	CacheWritePrice  float64 // per million tokens (cache write)
	CacheReadPrice   float64 // per million tokens (cache read)
	Aliases         []string
}

// ModelRegistry contains known model definitions.
var ModelRegistry = []ModelEntry{
	// Anthropic
	{ID: "claude-opus-4-6", Provider: "anthropic", MaxTokens: 32000, InputPrice: 15, OutputPrice: 75, CacheWritePrice: 18.75, CacheReadPrice: 1.50, Aliases: []string{"opus"}},
	{ID: "claude-sonnet-4-6", Provider: "anthropic", MaxTokens: 64000, InputPrice: 3, OutputPrice: 15, CacheWritePrice: 3.75, CacheReadPrice: 0.30, Aliases: []string{"sonnet"}},
	{ID: "claude-haiku-4-5-20251001", Provider: "anthropic", MaxTokens: 64000, InputPrice: 0.80, OutputPrice: 4, CacheWritePrice: 1.00, CacheReadPrice: 0.08, Aliases: []string{"haiku"}},
	{ID: "claude-opus-4-20250116", Provider: "anthropic", MaxTokens: 32000, InputPrice: 15, OutputPrice: 75},
	{ID: "claude-sonnet-4-20250514", Provider: "anthropic", MaxTokens: 64000, InputPrice: 3, OutputPrice: 15},

	// OpenAI-compatible (various providers)
	{ID: "gpt-4o", Provider: "openai", MaxTokens: 16384, InputPrice: 2.50, OutputPrice: 10, Aliases: []string{"gpt4o"}},
	{ID: "gpt-4o-mini", Provider: "openai", MaxTokens: 16384, InputPrice: 0.15, OutputPrice: 0.60},
	{ID: "o1", Provider: "openai", MaxTokens: 100000, InputPrice: 15, OutputPrice: 60},
	{ID: "o1-mini", Provider: "openai", MaxTokens: 65536, InputPrice: 3, OutputPrice: 12},
	{ID: "o3-mini", Provider: "openai", MaxTokens: 65536, InputPrice: 1.10, OutputPrice: 4.40},

	// xAI
	{ID: "grok-3", Provider: "xai", MaxTokens: 32768, InputPrice: 3, OutputPrice: 15, Aliases: []string{"grok", "grok3"}},
	{ID: "grok-3-mini", Provider: "xai", MaxTokens: 32768, InputPrice: 0.40, OutputPrice: 2, Aliases: []string{"grok-mini", "grok3mini"}},
	{ID: "grok-2", Provider: "xai", MaxTokens: 32768, InputPrice: 2, OutputPrice: 10, Aliases: []string{"grok2"}},

	// DeepSeek
	{ID: "deepseek-chat", Provider: "openai", MaxTokens: 65536, InputPrice: 0.27, OutputPrice: 1.10, Aliases: []string{"deepseek"}},
	{ID: "deepseek-reasoner", Provider: "openai", MaxTokens: 65536, InputPrice: 0.55, OutputPrice: 2.19, Aliases: []string{"deepseek-r1"}},

	// GLM (Zhipu AI)
	{ID: "glm-5.1", Provider: "openai", MaxTokens: 65536, InputPrice: 0.50, OutputPrice: 2, Aliases: []string{"glm", "glm5"}},
}

// lookupModel finds a model entry by ID or alias.
func lookupModel(model string) *ModelEntry {
	lower := strings.ToLower(model)
	for i := range ModelRegistry {
		entry := &ModelRegistry[i]
		if strings.EqualFold(entry.ID, model) {
			return entry
		}
		for _, alias := range entry.Aliases {
			if lower == alias {
				return entry
			}
		}
	}
	return nil
}

// providerFromString maps provider names to ProviderKind values.
func providerFromString(p string) ProviderKind {
	switch p {
	case "openai":
		return ProviderOpenAI
	case "xai":
		return ProviderXAI
	default:
		return ProviderAnthropic
	}
}

// EstimateCost calculates the estimated cost for given token usage.
func EstimateCost(model string, inputTokens, outputTokens, cacheWriteTokens, cacheReadTokens int) float64 {
	entry := lookupModel(model)
	if entry == nil {
		return 0
	}
	return float64(inputTokens)/1_000_000*entry.InputPrice +
		float64(outputTokens)/1_000_000*entry.OutputPrice +
		float64(cacheWriteTokens)/1_000_000*entry.CacheWritePrice +
		float64(cacheReadTokens)/1_000_000*entry.CacheReadPrice
}

// ListModels returns all known model IDs grouped by provider.
func ListModels() map[string][]string {
	result := make(map[string][]string)
	for _, entry := range ModelRegistry {
		result[entry.Provider] = append(result[entry.Provider], entry.ID)
	}
	return result
}
