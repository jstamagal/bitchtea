package llm

import (
	"context"
	"math/rand"
	"time"
)

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts int           // Maximum retry attempts (default: 3)
	BaseDelay   time.Duration // Initial delay (default: 1s)
	MaxDelay    time.Duration // Maximum delay cap (default: 30s)
}

// DefaultRetryConfig returns sensible retry defaults
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Second,
		MaxDelay:   30 * time.Second,
	}
}

// IsRateLimited checks if an HTTP status code indicates rate limiting
func IsRateLimited(statusCode int) bool {
	return statusCode == 429
}

// IsServerError checks if an HTTP status code indicates a retryable server error
func IsServerError(statusCode int) bool {
	return statusCode >= 500 && statusCode < 600
}

// IsRetryable checks if an error is retryable (rate limit or server error)
func IsRetryable(statusCode int) bool {
	return IsRateLimited(statusCode) || IsServerError(statusCode)
}

// RetryWithBackoff executes a function with exponential backoff retry.
// Returns the number of attempts made.
func RetryWithBackoff(ctx context.Context, cfg RetryConfig, fn func() (bool, error)) (int, error) {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}

	var attempt int
	for attempt = 1; attempt <= cfg.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return attempt, ctx.Err()
		default:
		}

		shouldRetry, err := fn()
		if err != nil {
			return attempt, err
		}
		if !shouldRetry {
			return attempt, nil
		}

		if attempt < cfg.MaxAttempts {
			delay := calculateBackoff(attempt, cfg)
			select {
			case <-ctx.Done():
				return attempt, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return attempt, nil
}

// calculateBackoff calculates delay with exponential increase and jitter
func calculateBackoff(attempt int, cfg RetryConfig) time.Duration {
	// Exponential backoff: base * 2^(attempt-1)
	baseNs := float64(cfg.BaseDelay)
	exponent := 1 << uint(attempt-1)
	delay := baseNs * float64(exponent)

	// Cap at max delay
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}

	// Add jitter: +/- 25%
	jitter := delay * 0.25 * (2*rand.Float64() - 1)
	delay += jitter

	return time.Duration(delay)
}
