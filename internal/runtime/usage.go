package runtime

import (
	"fmt"
	"sync"

	"github.com/go-claw/claw/internal/api"
)

// UsageTracker accumulates token usage across conversation turns.
type UsageTracker struct {
	mu               sync.Mutex
	model            string
	turns            []TurnUsage
	totalInput       int
	totalOutput      int
	totalCacheCreate int
	totalCacheRead   int
}

// TurnUsage records usage for a single turn.
type TurnUsage struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

// NewUsageTracker creates a new usage tracker for the given model.
func NewUsageTracker(model string) *UsageTracker {
	return &UsageTracker{model: model}
}

// Record adds a turn's usage to the tracker.
func (t *UsageTracker) Record(u TokenUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turns = append(t.turns, TurnUsage{
		InputTokens:              u.InputTokens,
		OutputTokens:             u.OutputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens,
	})
	t.totalInput += u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	t.totalOutput += u.OutputTokens
	t.totalCacheCreate += u.CacheCreationInputTokens
	t.totalCacheRead += u.CacheReadInputTokens
}

// Total returns total input and output tokens.
func (t *UsageTracker) Total() (input, output int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totalInput, t.totalOutput
}

// EstimatedCost returns the estimated cost in USD using the model registry.
func (t *UsageTracker) EstimatedCost() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	var inputTokens, outputTokens, cacheWrite, cacheRead int
	for _, turn := range t.turns {
		inputTokens += turn.InputTokens
		outputTokens += turn.OutputTokens
		cacheWrite += turn.CacheCreationInputTokens
		cacheRead += turn.CacheReadInputTokens
	}
	return api.EstimateCost(t.model, inputTokens, outputTokens, cacheWrite, cacheRead)
}

// FormatCost returns a formatted cost string.
func (t *UsageTracker) FormatCost() string {
	cost := t.EstimatedCost()
	if cost < 0.01 && cost > 0 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

// Turns returns the number of recorded turns.
func (t *UsageTracker) Turns() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.turns)
}

// Summary returns a human-readable usage summary.
func (t *UsageTracker) Summary() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return fmt.Sprintf("Turns: %d | Tokens: in=%d out=%d (cache: write=%d read=%d) | Cost: %s",
		len(t.turns), t.totalInput, t.totalOutput, t.totalCacheCreate, t.totalCacheRead,
		t.FormatCost())
}
