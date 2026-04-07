package runtime

import (
	"strings"
	"testing"
)

func TestUsageTrackerRecord(t *testing.T) {
	tracker := NewUsageTracker("claude-sonnet-4-6")

	tracker.Record(TokenUsage{
		InputTokens:  100,
		OutputTokens: 50,
	})
	tracker.Record(TokenUsage{
		InputTokens:              200,
		OutputTokens:             100,
		CacheCreationInputTokens: 50,
		CacheReadInputTokens:     25,
	})

	inT, outT := tracker.Total()
	if inT == 0 {
		t.Error("total input should be > 0")
	}
	if outT == 0 {
		t.Error("total output should be > 0")
	}
}

func TestUsageTrackerTurns(t *testing.T) {
	tracker := NewUsageTracker("claude-sonnet-4-6")
	if tracker.Turns() != 0 {
		t.Error("initial turns should be 0")
	}

	tracker.Record(TokenUsage{InputTokens: 100, OutputTokens: 50})
	tracker.Record(TokenUsage{InputTokens: 200, OutputTokens: 100})

	if tracker.Turns() != 2 {
		t.Errorf("turns = %d, want 2", tracker.Turns())
	}
}

func TestUsageTrackerCost(t *testing.T) {
	tracker := NewUsageTracker("claude-sonnet-4-6")
	tracker.Record(TokenUsage{
		InputTokens:  1_000_000,
		OutputTokens: 500_000,
	})

	cost := tracker.EstimatedCost()
	if cost <= 0 {
		t.Errorf("EstimatedCost should be > 0, got %f", cost)
	}
}

func TestUsageTrackerFormatCost(t *testing.T) {
	tracker := NewUsageTracker("claude-sonnet-4-6")
	tracker.Record(TokenUsage{InputTokens: 100, OutputTokens: 50})

	formatted := tracker.FormatCost()
	if formatted == "" {
		t.Error("FormatCost should not be empty")
	}
	if formatted[0] != '$' {
		t.Errorf("FormatCost = %q, should start with $", formatted)
	}
}

func TestUsageTrackerSummary(t *testing.T) {
	tracker := NewUsageTracker("claude-sonnet-4-6")
	tracker.Record(TokenUsage{InputTokens: 100, OutputTokens: 50})

	summary := tracker.Summary()
	if summary == "" {
		t.Error("Summary should not be empty")
	}
	if !strings.Contains(summary, "Turns:") {
		t.Errorf("Summary missing 'Turns:': %q", summary)
	}
	if !strings.Contains(summary, "Tokens:") {
		t.Errorf("Summary missing 'Tokens:': %q", summary)
	}
}

func TestUsageTrackerUnknownModel(t *testing.T) {
	tracker := NewUsageTracker("unknown-model")
	tracker.Record(TokenUsage{InputTokens: 100, OutputTokens: 50})
	cost := tracker.EstimatedCost()
	if cost != 0 {
		t.Errorf("unknown model cost should be 0, got %f", cost)
	}
}
