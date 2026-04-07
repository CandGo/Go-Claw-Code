package runtime

import (
	"fmt"
	"strings"
	"sync"
)

const (
	defaultInputCostPerMillion          = 15.0
	defaultOutputCostPerMillion         = 75.0
	defaultCacheCreationCostPerMillion  = 18.75
	defaultCacheReadCostPerMillion      = 1.5
)

// ModelPricing holds per-model pricing information (USD per million tokens).
type ModelPricing struct {
	InputCostPerMillion          float64
	OutputCostPerMillion         float64
	CacheCreationCostPerMillion  float64
	CacheReadCostPerMillion      float64
}

// DefaultSonnetPricing returns the default Sonnet-tier pricing.
func DefaultSonnetPricing() ModelPricing {
	return ModelPricing{
		InputCostPerMillion:         defaultInputCostPerMillion,
		OutputCostPerMillion:        defaultOutputCostPerMillion,
		CacheCreationCostPerMillion: defaultCacheCreationCostPerMillion,
		CacheReadCostPerMillion:     defaultCacheReadCostPerMillion,
	}
}

// PricingForModel returns the pricing for a known model, or nil if unknown.
func PricingForModel(model string) *ModelPricing {
	normalized := strings.ToLower(model)
	switch {
	case strings.Contains(normalized, "haiku"):
		return &ModelPricing{
			InputCostPerMillion:         1.0,
			OutputCostPerMillion:        5.0,
			CacheCreationCostPerMillion: 1.25,
			CacheReadCostPerMillion:     0.1,
		}
	case strings.Contains(normalized, "opus"):
		return &ModelPricing{
			InputCostPerMillion:         15.0,
			OutputCostPerMillion:        75.0,
			CacheCreationCostPerMillion: 18.75,
			CacheReadCostPerMillion:     1.5,
		}
	case strings.Contains(normalized, "sonnet"):
		p := DefaultSonnetPricing()
		return &p
	default:
		return nil
	}
}

// UsageCostEstimate holds a cost breakdown.
type UsageCostEstimate struct {
	InputCostUSD         float64
	OutputCostUSD        float64
	CacheCreationCostUSD float64
	CacheReadCostUSD     float64
}

// TotalCostUSD returns the sum of all cost components.
func (e UsageCostEstimate) TotalCostUSD() float64 {
	return e.InputCostUSD + e.OutputCostUSD + e.CacheCreationCostUSD + e.CacheReadCostUSD
}

// CostForTokens computes the USD cost for a number of tokens at a given rate.
func CostForTokens(tokens int, usdPerMillion float64) float64 {
	return float64(tokens) / 1_000_000.0 * usdPerMillion
}

// FormatUSD formats a float as a USD string.
func FormatUSD(amount float64) string {
	return fmt.Sprintf("$%.4f", amount)
}

// EstimateCost computes the cost estimate using the given pricing.
func (t TokenUsage) EstimateCost(pricing ModelPricing) UsageCostEstimate {
	return UsageCostEstimate{
		InputCostUSD:         CostForTokens(t.InputTokens, pricing.InputCostPerMillion),
		OutputCostUSD:        CostForTokens(t.OutputTokens, pricing.OutputCostPerMillion),
		CacheCreationCostUSD: CostForTokens(t.CacheCreationInputTokens, pricing.CacheCreationCostPerMillion),
		CacheReadCostUSD:     CostForTokens(t.CacheReadInputTokens, pricing.CacheReadCostPerMillion),
	}
}

// EstimateCostDefault computes cost using default Sonnet-tier pricing.
func (t TokenUsage) EstimateCostDefault() UsageCostEstimate {
	return t.EstimateCost(DefaultSonnetPricing())
}

// SummaryLines returns human-readable usage summary lines.
func (t TokenUsage) SummaryLines(label string) []string {
	return t.SummaryLinesForModel(label, "")
}

// SummaryLinesForModel returns summary lines with model-specific pricing.
func (t TokenUsage) SummaryLinesForModel(label, model string) []string {
	pricing := PricingForModel(model)
	var cost UsageCostEstimate
	if pricing != nil {
		cost = t.EstimateCost(*pricing)
	} else {
		cost = t.EstimateCostDefault()
	}

	modelSuffix := ""
	if model != "" {
		modelSuffix = " model=" + model
	}
	pricingSuffix := ""
	if pricing == nil && model != "" {
		pricingSuffix = " pricing=estimated-default"
	}

	line1 := fmt.Sprintf(
		"%s: total_tokens=%d input=%d output=%d cache_write=%d cache_read=%d estimated_cost=%s%s%s",
		label,
		t.InputTokens+t.OutputTokens+t.CacheCreationInputTokens+t.CacheReadInputTokens,
		t.InputTokens, t.OutputTokens,
		t.CacheCreationInputTokens, t.CacheReadInputTokens,
		FormatUSD(cost.TotalCostUSD()),
		modelSuffix, pricingSuffix,
	)
	line2 := fmt.Sprintf(
		"  cost breakdown: input=%s output=%s cache_write=%s cache_read=%s",
		FormatUSD(cost.InputCostUSD),
		FormatUSD(cost.OutputCostUSD),
		FormatUSD(cost.CacheCreationCostUSD),
		FormatUSD(cost.CacheReadCostUSD),
	)
	return []string{line1, line2}
}

// UsageTracker accumulates token usage across conversation turns.
type UsageTracker struct {
	mu               sync.Mutex
	model            string
	latestTurn       TokenUsage
	cumulative       TokenUsage
	turns            []TokenUsage
}

// NewUsageTracker creates a new usage tracker for the given model.
func NewUsageTracker(model string) *UsageTracker {
	return &UsageTracker{model: model}
}

// FromSession reconstructs usage from session messages.
func NewUsageTrackerFromSession(session *Session) *UsageTracker {
	tracker := &UsageTracker{}
	if session != nil {
		for _, msg := range session.Messages {
			if msg.Usage != nil {
				tracker.Record(*msg.Usage)
			}
		}
	}
	return tracker
}

// Record adds a turn's usage to the tracker.
func (t *UsageTracker) Record(u TokenUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.latestTurn = u
	t.cumulative.InputTokens += u.InputTokens
	t.cumulative.OutputTokens += u.OutputTokens
	t.cumulative.CacheCreationInputTokens += u.CacheCreationInputTokens
	t.cumulative.CacheReadInputTokens += u.CacheReadInputTokens
	t.turns = append(t.turns, u)
}

// CurrentTurnUsage returns the usage from the most recent turn.
func (t *UsageTracker) CurrentTurnUsage() TokenUsage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.latestTurn
}

// CumulativeUsage returns the total accumulated usage.
func (t *UsageTracker) CumulativeUsage() TokenUsage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cumulative
}

// Total returns total input and output tokens (backwards-compatible).
func (t *UsageTracker) Total() (input, output int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cumulative.InputTokens + t.cumulative.CacheCreationInputTokens + t.cumulative.CacheReadInputTokens,
		t.cumulative.OutputTokens
}

// EstimatedCost returns the estimated cost in USD using model pricing.
func (t *UsageTracker) EstimatedCost() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	pricing := PricingForModel(t.model)
	if pricing == nil {
		return 0
	}
	return t.cumulative.EstimateCost(*pricing).TotalCostUSD()
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
		len(t.turns), t.cumulative.InputTokens, t.cumulative.OutputTokens,
		t.cumulative.CacheCreationInputTokens, t.cumulative.CacheReadInputTokens,
		t.formatCostLocked())
}

// formatCostLocked computes formatted cost assuming the mutex is already held.
func (t *UsageTracker) formatCostLocked() string {
	pricing := PricingForModel(t.model)
	if pricing == nil {
		return "$0.00"
	}
	cost := t.cumulative.EstimateCost(*pricing).TotalCostUSD()
	if cost < 0.01 && cost > 0 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}
