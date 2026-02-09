package resilience

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Retryable error interface
// ---------------------------------------------------------------------------

// RetryableError is implemented by errors that know whether a retry is
// worthwhile.
type RetryableError interface {
	error
	// ShouldRetry returns true if the operation should be retried.
	ShouldRetry() bool
}

// retryableErr wraps any error with a retry flag.
type retryableErr struct {
	err       error
	retryable bool
}

func (e *retryableErr) Error() string   { return e.err.Error() }
func (e *retryableErr) Unwrap() error   { return e.err }
func (e *retryableErr) ShouldRetry() bool { return e.retryable }

// NewRetryableError wraps err marking it as retryable.
func NewRetryableError(err error) error {
	return &retryableErr{err: err, retryable: true}
}

// NewNonRetryableError wraps err marking it as non-retryable.
func NewNonRetryableError(err error) error {
	return &retryableErr{err: err, retryable: false}
}

// IsRetryable checks whether an error is retryable.
// If the error does not implement RetryableError, it defaults to true
// (optimistic — network errors are usually transient).
func IsRetryable(err error) bool {
	var re RetryableError
	if errors.As(err, &re) {
		return re.ShouldRetry()
	}
	// Default: treat unknown errors as retryable.
	return true
}

// ---------------------------------------------------------------------------
// RetryConfig
// ---------------------------------------------------------------------------

// RetryConfig holds parameters for RetryWithBackoff.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts (including the first).
	// 0 means use default (3).
	MaxAttempts int
	// InitialDelay is the base delay before the first retry (default: 1s).
	InitialDelay time.Duration
	// MaxDelay caps the backoff (default: 30s).
	MaxDelay time.Duration
	// Multiplier controls exponential growth (default: 2.0).
	Multiplier float64
	// JitterFraction is the maximum fraction of the delay to add as random
	// jitter (default: 0.1 = 10%).
	JitterFraction float64
	// Logger is optional structured logger.
	Logger *zerolog.Logger
	// OperationName is used in log messages.
	OperationName string
}

func (c *RetryConfig) setDefaults() {
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 3
	}
	if c.InitialDelay == 0 {
		c.InitialDelay = 1 * time.Second
	}
	if c.MaxDelay == 0 {
		c.MaxDelay = 30 * time.Second
	}
	if c.Multiplier == 0 {
		c.Multiplier = 2.0
	}
	if c.JitterFraction == 0 {
		c.JitterFraction = 0.1
	}
	if c.OperationName == "" {
		c.OperationName = "operation"
	}
}

// ---------------------------------------------------------------------------
// RetryWithBackoff
// ---------------------------------------------------------------------------

// RetryWithBackoff executes fn up to MaxAttempts times with exponential
// backoff + jitter. It respects context cancellation and the RetryableError
// interface.
func RetryWithBackoff(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) error) error {
	cfg.setDefaults()

	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Check context before each attempt.
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s: context cancelled after %d attempts: %w", cfg.OperationName, attempt-1, err)
		}

		lastErr = fn(ctx)
		if lastErr == nil {
			if attempt > 1 && cfg.Logger != nil {
				cfg.Logger.Info().
					Str("operation", cfg.OperationName).
					Int("attempt", attempt).
					Msg("Retry succeeded")
			}
			return nil
		}

		// Check if the error says no retry.
		if !IsRetryable(lastErr) {
			if cfg.Logger != nil {
				cfg.Logger.Warn().
					Err(lastErr).
					Str("operation", cfg.OperationName).
					Int("attempt", attempt).
					Msg("Non-retryable error, aborting")
			}
			return lastErr
		}

		// Don't sleep after the last attempt.
		if attempt == cfg.MaxAttempts {
			break
		}

		delay := computeDelay(attempt, cfg)

		if cfg.Logger != nil {
			cfg.Logger.Warn().
				Err(lastErr).
				Str("operation", cfg.OperationName).
				Int("attempt", attempt).
				Int("max_attempts", cfg.MaxAttempts).
				Dur("next_delay", delay).
				Msg("Retrying after error")
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: context cancelled during backoff: %w", cfg.OperationName, ctx.Err())
		case <-time.After(delay):
		}
	}

	return fmt.Errorf("%s: all %d attempts failed: %w", cfg.OperationName, cfg.MaxAttempts, lastErr)
}

// computeDelay returns the backoff delay for the given attempt number.
func computeDelay(attempt int, cfg RetryConfig) time.Duration {
	// Exponential: initialDelay * multiplier^(attempt-1)
	delay := float64(cfg.InitialDelay) * math.Pow(cfg.Multiplier, float64(attempt-1))

	// Cap at MaxDelay.
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}

	// Add jitter: ±jitterFraction of delay.
	jitter := delay * cfg.JitterFraction * (2*rand.Float64() - 1)
	delay += jitter

	if delay < 0 {
		delay = float64(cfg.InitialDelay)
	}

	return time.Duration(delay)
}

// ---------------------------------------------------------------------------
// Convenience wrappers
// ---------------------------------------------------------------------------

// Retry is a simplified wrapper using sensible defaults.
func Retry(ctx context.Context, maxAttempts int, fn func(ctx context.Context) error) error {
	return RetryWithBackoff(ctx, RetryConfig{
		MaxAttempts: maxAttempts,
	}, fn)
}

// RetryForever retries until ctx is cancelled, with a capped backoff.
func RetryForever(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) error) error {
	cfg.setDefaults()
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attempt++
		if err := fn(ctx); err != nil {
			if !IsRetryable(err) {
				return err
			}
			delay := computeDelay(attempt, cfg)
			if cfg.Logger != nil {
				cfg.Logger.Warn().
					Err(err).
					Str("operation", cfg.OperationName).
					Int("attempt", attempt).
					Dur("next_delay", delay).
					Msg("Retrying (forever mode)")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}
		return nil
	}
}
