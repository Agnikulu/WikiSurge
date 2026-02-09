package resilience

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------
// Circuit Breaker tests
// -----------------------------------------------------------------------

func newTestBreaker(t *testing.T, threshold int, timeout time.Duration) *CircuitBreaker {
	t.Helper()
	return NewCircuitBreaker(CircuitBreakerConfig{
		Name:             t.Name(),
		FailureThreshold: threshold,
		ResetTimeout:     timeout,
		HalfOpenMaxCalls: 1,
	}, zerolog.Nop())
}

func TestCircuitBreaker_ClosedState(t *testing.T) {
	cb := newTestBreaker(t, 5, 30*time.Second)
	assert.Equal(t, "closed", cb.GetState())

	err := cb.Call(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, "closed", cb.GetState())
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := newTestBreaker(t, 3, 30*time.Second)
	boom := errors.New("boom")

	for i := 0; i < 3; i++ {
		_ = cb.Call(func() error { return boom })
	}

	assert.Equal(t, "open", cb.GetState())
	err := cb.Call(func() error { return nil })
	assert.ErrorIs(t, err, ErrCircuitOpen)
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	cb := newTestBreaker(t, 2, 50*time.Millisecond)
	boom := errors.New("boom")

	_ = cb.Call(func() error { return boom })
	_ = cb.Call(func() error { return boom })
	assert.Equal(t, "open", cb.GetState())

	// Wait for reset timeout.
	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, "half-open", cb.GetState())
}

func TestCircuitBreaker_HalfOpenSuccessCloses(t *testing.T) {
	cb := newTestBreaker(t, 2, 50*time.Millisecond)
	boom := errors.New("boom")

	_ = cb.Call(func() error { return boom })
	_ = cb.Call(func() error { return boom })
	time.Sleep(60 * time.Millisecond)

	// Half-open probe should succeed.
	err := cb.Call(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, "closed", cb.GetState())
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb := newTestBreaker(t, 2, 50*time.Millisecond)
	boom := errors.New("boom")

	_ = cb.Call(func() error { return boom })
	_ = cb.Call(func() error { return boom })
	time.Sleep(60 * time.Millisecond)

	_ = cb.Call(func() error { return boom })
	assert.Equal(t, "open", cb.GetState())
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := newTestBreaker(t, 2, 30*time.Second)
	boom := errors.New("boom")

	_ = cb.Call(func() error { return boom })
	_ = cb.Call(func() error { return boom })
	assert.Equal(t, "open", cb.GetState())

	cb.Reset()
	assert.Equal(t, "closed", cb.GetState())
	assert.Equal(t, 0, cb.ConsecutiveFailures())
}

func TestCircuitBreaker_StateChangeCallback(t *testing.T) {
	cb := newTestBreaker(t, 2, 50*time.Millisecond)
	boom := errors.New("boom")

	var transitions []string
	cb.OnStateChange(func(name string, from, to CircuitState) {
		transitions = append(transitions, from.String()+"→"+to.String())
	})

	_ = cb.Call(func() error { return boom })
	_ = cb.Call(func() error { return boom })
	time.Sleep(100 * time.Millisecond) // let async callback run

	assert.Contains(t, transitions, "closed→open")
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cb := newTestBreaker(t, 3, 30*time.Second)
	boom := errors.New("boom")

	_ = cb.Call(func() error { return boom })
	_ = cb.Call(func() error { return boom })
	assert.Equal(t, 2, cb.ConsecutiveFailures())

	// A success should reset the counter.
	_ = cb.Call(func() error { return nil })
	assert.Equal(t, 0, cb.ConsecutiveFailures())
	assert.Equal(t, "closed", cb.GetState())
}

// -----------------------------------------------------------------------
// Registry tests
// -----------------------------------------------------------------------

func TestCircuitBreakerRegistry(t *testing.T) {
	reg := NewCircuitBreakerRegistry(zerolog.Nop())

	cb := reg.Register(CircuitBreakerConfig{Name: "redis", FailureThreshold: 3, ResetTimeout: 10 * time.Second})
	require.NotNil(t, cb)

	got, err := reg.Get("redis")
	require.NoError(t, err)
	assert.Equal(t, cb, got)

	_, err = reg.Get("unknown")
	assert.Error(t, err)

	snap := reg.Snapshot()
	assert.Equal(t, "closed", snap["redis"])
}

// -----------------------------------------------------------------------
// Retry tests
// -----------------------------------------------------------------------

func TestRetryWithBackoff_SucceedsImmediately(t *testing.T) {
	ctx := context.Background()
	var calls int32

	err := RetryWithBackoff(ctx, RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
	}, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestRetryWithBackoff_SucceedsAfterRetries(t *testing.T) {
	ctx := context.Background()
	var calls int32

	err := RetryWithBackoff(ctx, RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
	}, func(ctx context.Context) error {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return errors.New("transient")
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

func TestRetryWithBackoff_ExhaustsAttempts(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("permanent")

	err := RetryWithBackoff(ctx, RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
	}, func(ctx context.Context) error {
		return boom
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "all 3 attempts failed")
}

func TestRetryWithBackoff_RespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := RetryWithBackoff(ctx, RetryConfig{
		MaxAttempts:  5,
		InitialDelay: time.Second,
	}, func(ctx context.Context) error {
		return errors.New("should not be retried")
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled")
}

func TestRetryWithBackoff_NonRetryableError(t *testing.T) {
	ctx := context.Background()
	var calls int32

	err := RetryWithBackoff(ctx, RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
	}, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return NewNonRetryableError(errors.New("bad request"))
	})

	assert.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestIsRetryable(t *testing.T) {
	assert.True(t, IsRetryable(errors.New("unknown")))
	assert.True(t, IsRetryable(NewRetryableError(errors.New("yes"))))
	assert.False(t, IsRetryable(NewNonRetryableError(errors.New("no"))))
}

// -----------------------------------------------------------------------
// Feature flags tests
// -----------------------------------------------------------------------

func TestFeatureFlags_DefaultAllEnabled(t *testing.T) {
	// This test imports the config package indirectly through the
	// resilience package tests; we test separately in config_test.go.
	// Just verify the retry/circuit breaker logic here.
	t.Skip("Feature flags tested in config package")
}
