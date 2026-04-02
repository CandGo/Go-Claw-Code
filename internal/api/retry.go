package api

import (
	"math"
	"math/rand"
	"time"
)

// RetryPolicy controls retry behavior for API calls.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// DefaultRetryPolicy returns the default retry configuration.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   200 * time.Millisecond,
		MaxDelay:    5 * time.Second,
	}
}

// Delay returns the backoff delay for the given attempt (0-indexed).
func (p RetryPolicy) Delay(attempt int) time.Duration {
	delay := float64(p.BaseDelay) * math.Pow(2, float64(attempt))
	jitter := rand.Float64() * 0.3 * delay
	delay += jitter
	if delay > float64(p.MaxDelay) {
		delay = float64(p.MaxDelay)
	}
	return time.Duration(delay)
}
