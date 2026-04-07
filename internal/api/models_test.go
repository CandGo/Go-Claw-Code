package api

import (
	"testing"
)

func TestModelRegistryLookup(t *testing.T) {
	knownModels := []string{
		"claude-sonnet-4-6",
		"claude-opus-4-6",
		"claude-haiku-4-5-20251001",
	}
	for _, m := range knownModels {
		entry := lookupModel(m)
		if entry == nil {
			t.Errorf("lookupModel(%q) returned nil, expected entry", m)
			continue
		}
		if entry.ID == "" {
			t.Errorf("lookupModel(%q).ID is empty", m)
		}
		if entry.MaxTokens <= 0 {
			t.Errorf("lookupModel(%q).MaxTokens = %d, want > 0", m, entry.MaxTokens)
		}
	}

	entry := lookupModel("nonexistent-model-xyz-12345")
	if entry != nil {
		t.Errorf("lookupModel(unknown) returned %+v, expected nil", entry)
	}
}

func TestModelAliases(t *testing.T) {
	aliasTests := []struct {
		alias    string
		wantID   string
	}{
		{"opus", "claude-opus-4-6"},
		{"sonnet", "claude-sonnet-4-6"},
		{"haiku", "claude-haiku-4-5-20251001"},
		{"grok", "grok-3"},
		{"deepseek", "deepseek-chat"},
		{"glm", "glm-5.1"},
		{"gpt4o", "gpt-4o"},
	}
	for _, tt := range aliasTests {
		entry := lookupModel(tt.alias)
		if entry == nil {
			t.Errorf("lookupModel(alias %q) returned nil", tt.alias)
			continue
		}
		if entry.ID != tt.wantID {
			t.Errorf("lookupModel(%q).ID = %q, want %q", tt.alias, entry.ID, tt.wantID)
		}
	}
}

func TestProviderFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected ProviderKind
	}{
		{"anthropic", ProviderAnthropic},
		{"openai", ProviderOpenAI},
		{"xai", ProviderXAI},
		{"", ProviderAnthropic},
		{"unknown", ProviderAnthropic},
	}
	for _, tt := range tests {
		result := providerFromString(tt.input)
		if result != tt.expected {
			t.Errorf("providerFromString(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestEstimateCost(t *testing.T) {
	cost := EstimateCost("claude-sonnet-4-6", 1_000_000, 1_000_000, 0, 0)
	if cost <= 0 {
		t.Errorf("EstimateCost for sonnet should be > 0, got %f", cost)
	}

	// Zero usage → zero cost
	cost = EstimateCost("claude-sonnet-4-6", 0, 0, 0, 0)
	if cost != 0 {
		t.Errorf("EstimateCost with zero usage = %f, want 0", cost)
	}

	// Unknown model → zero cost
	cost = EstimateCost("unknown-model", 1000, 1000, 0, 0)
	if cost != 0 {
		t.Errorf("EstimateCost for unknown model should be 0, got %f", cost)
	}
}

func TestEstimateCostWithCache(t *testing.T) {
	// Test cache token pricing
	costNoCache := EstimateCost("claude-sonnet-4-6", 1000, 500, 0, 0)
	costWithCache := EstimateCost("claude-sonnet-4-6", 1000, 500, 1000, 1000)
	if costWithCache <= costNoCache {
		t.Errorf("cost with cache tokens (%f) should be > cost without (%f)", costWithCache, costNoCache)
	}
}

func TestListModels(t *testing.T) {
	models := ListModels()
	if len(models) == 0 {
		t.Error("ListModels() returned empty map")
	}
	// Should have at least anthropic, openai, xai providers
	for _, provider := range []string{"anthropic", "openai", "xai"} {
		if len(models[provider]) == 0 {
			t.Errorf("ListModels()[%q] is empty", provider)
		}
	}
}
