package resilience

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------
// Scenario 1: Elasticsearch unavailable
// -----------------------------------------------------------------------

func TestDegradation_ElasticsearchUnavailable(t *testing.T) {
	ff := config.NewFeatureFlags(zerolog.Nop())
	cfg := &config.Config{}
	dm := NewDegradationManager(ff, cfg, zerolog.Nop())
	defer dm.Stop()

	// Verify indexing starts enabled.
	assert.True(t, ff.IsEnabled(config.FeatureElasticsearchIndexing))

	// Simulate ES going down.
	dm.HandleElasticsearchUnavailable("connection refused")

	// Indexing should be disabled.
	assert.False(t, ff.IsEnabled(config.FeatureElasticsearchIndexing))
	// Trending and edit war detection should remain enabled.
	assert.True(t, ff.IsEnabled(config.FeatureTrendingTracking))
	assert.True(t, ff.IsEnabled(config.FeatureEditWarDetection))
	// Degradation level should be partial.
	assert.Equal(t, DegradationPartial, dm.Level())

	hc := dm.HealthCheck()
	assert.Equal(t, "degraded", hc.Status)

	// Simulate recovery.
	dm.HandleElasticsearchRecovered()
	assert.True(t, ff.IsEnabled(config.FeatureElasticsearchIndexing))
	assert.Equal(t, DegradationNone, dm.Level())
}

// -----------------------------------------------------------------------
// Scenario 2: Redis memory limit
// -----------------------------------------------------------------------

func TestDegradation_RedisMemoryLimit(t *testing.T) {
	ff := config.NewFeatureFlags(zerolog.Nop())
	cfg := &config.Config{}
	cfg.Redis.HotPages.MaxTracked = 1000
	dm := NewDegradationManager(ff, cfg, zerolog.Nop())
	defer dm.Stop()

	// Stage 1: Reduce hot page limit.
	dm.HandleRedisMemoryLimit(100)
	assert.Equal(t, 100, cfg.Redis.HotPages.MaxTracked)
	assert.True(t, ff.IsEnabled(config.FeatureTrendingTracking)) // still on

	// Stage 2: Critical — disable trending.
	dm.HandleRedisMemoryCritical()
	assert.False(t, ff.IsEnabled(config.FeatureTrendingTracking))
	assert.Equal(t, DegradationPartial, dm.Level()) // still just redis unhealthy

	// Recovery.
	dm.HandleRedisRecovered(1000)
	assert.Equal(t, 1000, cfg.Redis.HotPages.MaxTracked)
	assert.True(t, ff.IsEnabled(config.FeatureTrendingTracking))
}

// -----------------------------------------------------------------------
// Scenario 3: High Kafka lag
// -----------------------------------------------------------------------

func TestDegradation_HighKafkaLag(t *testing.T) {
	ff := config.NewFeatureFlags(zerolog.Nop())
	cfg := &config.Config{}
	dm := NewDegradationManager(ff, cfg, zerolog.Nop())
	defer dm.Stop()

	dm.HandleHighKafkaLag()
	assert.False(t, ff.IsEnabled(config.FeatureElasticsearchIndexing))
	assert.Equal(t, DegradationPartial, dm.Level())

	dm.HandleKafkaLagRecovered()
	assert.True(t, ff.IsEnabled(config.FeatureElasticsearchIndexing))
	assert.Equal(t, DegradationNone, dm.Level())
}

// -----------------------------------------------------------------------
// Circuit breaker integration with degradation
// -----------------------------------------------------------------------

func TestCircuitBreaker_TriggersOnRedisFailure(t *testing.T) {
	cb := newTestBreaker(t, 3, 100*time.Millisecond)
	redisErr := errors.New("READONLY You can't write against a read only replica")

	// Simulate 3 consecutive Redis failures.
	for i := 0; i < 3; i++ {
		_ = cb.Call(func() error { return redisErr })
	}

	// Circuit should be open.
	assert.Equal(t, "open", cb.GetState())

	// Calls should be rejected.
	err := cb.Call(func() error { return nil })
	assert.ErrorIs(t, err, ErrCircuitOpen)

	// Wait for half-open.
	time.Sleep(120 * time.Millisecond)
	assert.Equal(t, "half-open", cb.GetState())

	// Successful probe closes circuit.
	err = cb.Call(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, "closed", cb.GetState())
}

func TestCircuitBreaker_TriggersOnESTimeout(t *testing.T) {
	cb := newTestBreaker(t, 2, 50*time.Millisecond)
	timeoutErr := context.DeadlineExceeded

	_ = cb.Call(func() error { return timeoutErr })
	_ = cb.Call(func() error { return timeoutErr })

	assert.Equal(t, "open", cb.GetState())
}

// -----------------------------------------------------------------------
// Retry with circuit breaker
// -----------------------------------------------------------------------

func TestRetry_WithCircuitBreaker(t *testing.T) {
	cb := newTestBreaker(t, 5, 30*time.Second)
	ctx := context.Background()

	// The function fails 2 times then succeeds — retry should handle it
	// and the circuit should stay closed.
	var attempt int
	err := RetryWithBackoff(ctx, RetryConfig{
		MaxAttempts:  5,
		InitialDelay: 10 * time.Millisecond,
	}, func(ctx context.Context) error {
		return cb.Call(func() error {
			attempt++
			if attempt <= 2 {
				return errors.New("transient network error")
			}
			return nil
		})
	})

	assert.NoError(t, err)
	assert.Equal(t, "closed", cb.GetState())
}

// -----------------------------------------------------------------------
// Degradation health check
// -----------------------------------------------------------------------

func TestDegradation_HealthCheck_Healthy(t *testing.T) {
	ff := config.NewFeatureFlags(zerolog.Nop())
	cfg := &config.Config{}
	dm := NewDegradationManager(ff, cfg, zerolog.Nop())
	defer dm.Stop()

	hc := dm.HealthCheck()
	assert.Equal(t, "healthy", hc.Status)
	assert.Equal(t, "none", hc.Level)
}

func TestDegradation_HealthCheck_Multiple_Components(t *testing.T) {
	ff := config.NewFeatureFlags(zerolog.Nop())
	cfg := &config.Config{}
	dm := NewDegradationManager(ff, cfg, zerolog.Nop())
	defer dm.Stop()

	dm.HandleElasticsearchUnavailable("timeout")
	dm.HandleHighKafkaLag()

	hc := dm.HealthCheck()
	assert.Equal(t, "critical", hc.Status)
	assert.Equal(t, "severe", hc.Level)
	require.Len(t, hc.Actions, 2)
}

// -----------------------------------------------------------------------
// Recovery verification
// -----------------------------------------------------------------------

func TestDegradation_FullRecovery(t *testing.T) {
	ff := config.NewFeatureFlags(zerolog.Nop())
	cfg := &config.Config{}
	cfg.Redis.HotPages.MaxTracked = 1000
	dm := NewDegradationManager(ff, cfg, zerolog.Nop())
	defer dm.Stop()

	// Break everything.
	dm.HandleElasticsearchUnavailable("down")
	dm.HandleRedisMemoryLimit(100)
	dm.HandleHighKafkaLag()
	assert.Equal(t, DegradationSevere, dm.Level())

	// Recover everything.
	dm.HandleElasticsearchRecovered()
	dm.HandleRedisRecovered(1000)
	dm.HandleKafkaLagRecovered()
	assert.Equal(t, DegradationNone, dm.Level())

	// All features should be back.
	for _, f := range config.AllFeatures() {
		assert.True(t, ff.IsEnabled(f), "feature %s should be re-enabled", f)
	}
}
