package resilience

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// RedisPoolConfig defaults
// ---------------------------------------------------------------------------

func TestDefaultRedisPoolConfig(t *testing.T) {
	cfg := DefaultRedisPoolConfig()

	assert.Equal(t, 20, cfg.PoolSize)
	assert.Equal(t, 5, cfg.MinIdleConns)
	assert.Equal(t, 10, cfg.MaxIdleConns)
	assert.Equal(t, 5*time.Minute, cfg.ConnMaxIdleTime)
	assert.Equal(t, 30*time.Minute, cfg.ConnMaxLifetime)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, 8*time.Millisecond, cfg.MinRetryBackoff)
	assert.Equal(t, 512*time.Millisecond, cfg.MaxRetryBackoff)
	assert.Equal(t, 5*time.Second, cfg.DialTimeout)
	assert.Equal(t, 5*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 5*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 6*time.Second, cfg.PoolTimeout)
	assert.Equal(t, 30*time.Second, cfg.HealthCheckInterval)
}

// ---------------------------------------------------------------------------
// ElasticsearchPoolConfig defaults
// ---------------------------------------------------------------------------

func TestDefaultElasticsearchPoolConfig(t *testing.T) {
	cfg := DefaultElasticsearchPoolConfig()

	assert.Equal(t, 10, cfg.MaxConns)
	assert.Equal(t, 10, cfg.MaxIdleConns)
	assert.Equal(t, 10, cfg.MaxIdleConnsPerHost)
	assert.Equal(t, 90*time.Second, cfg.IdleConnTimeout)
	assert.True(t, cfg.KeepAlive)
	assert.Equal(t, 30*time.Second, cfg.KeepAliveInterval)
	assert.False(t, cfg.Sniff)
	assert.Equal(t, 3, cfg.MaxRetries)
	assert.Equal(t, []int{502, 503, 504, 429}, cfg.RetryOnStatus)
	assert.Equal(t, 100*time.Millisecond, cfg.RetryInitialWait)
}

// ---------------------------------------------------------------------------
// TimeoutConfig defaults
// ---------------------------------------------------------------------------

func TestDefaultTimeoutConfig(t *testing.T) {
	cfg := DefaultTimeoutConfig()

	// HTTP
	assert.Equal(t, 5*time.Second, cfg.HTTP.ConnectTimeout)
	assert.Equal(t, 10*time.Second, cfg.HTTP.RequestTimeout)
	assert.Equal(t, 90*time.Second, cfg.HTTP.IdleConnTimeout)
	assert.Equal(t, 5*time.Second, cfg.HTTP.TLSHandshakeTimeout)
	assert.Equal(t, 5*time.Second, cfg.HTTP.ResponseHeaderTimeout)

	// Redis
	assert.Equal(t, 5*time.Second, cfg.Redis.DialTimeout)
	assert.Equal(t, 5*time.Second, cfg.Redis.ReadTimeout)
	assert.Equal(t, 5*time.Second, cfg.Redis.WriteTimeout)
	assert.Equal(t, 6*time.Second, cfg.Redis.PoolTimeout)

	// Elasticsearch
	assert.Equal(t, 30*time.Second, cfg.ES.IndexTimeout)
	assert.Equal(t, 10*time.Second, cfg.ES.SearchTimeout)
	assert.Equal(t, 60*time.Second, cfg.ES.BulkTimeout)
	assert.Equal(t, 5*time.Second, cfg.ES.HealthCheckTimeout)

	// Kafka
	assert.Equal(t, 30*time.Second, cfg.Kafka.SessionTimeout)
	assert.Equal(t, 10*time.Second, cfg.Kafka.HeartbeatInterval)
	assert.Equal(t, 30*time.Second, cfg.Kafka.RequestTimeout)
	assert.Equal(t, 10*time.Second, cfg.Kafka.WriteTimeout)
	assert.Equal(t, 10*time.Second, cfg.Kafka.ReadTimeout)

	// WebSocket
	assert.Equal(t, 60*time.Second, cfg.WS.ReadDeadline)
	assert.Equal(t, 10*time.Second, cfg.WS.WriteDeadline)
	assert.Equal(t, 30*time.Second, cfg.WS.PingInterval)
	assert.Equal(t, 60*time.Second, cfg.WS.PongWait)
}

// ---------------------------------------------------------------------------
// Zero values are overridden by defaults
// ---------------------------------------------------------------------------

func TestDefaultRedisPoolConfig_AllNonZero(t *testing.T) {
	cfg := DefaultRedisPoolConfig()
	assert.Greater(t, cfg.PoolSize, 0)
	assert.Greater(t, cfg.MinIdleConns, 0)
	assert.Greater(t, cfg.MaxIdleConns, 0)
	assert.Greater(t, cfg.ConnMaxIdleTime, time.Duration(0))
	assert.Greater(t, cfg.ConnMaxLifetime, time.Duration(0))
}

func TestDefaultTimeoutConfig_AllNonZero(t *testing.T) {
	cfg := DefaultTimeoutConfig()
	assert.Greater(t, cfg.HTTP.ConnectTimeout, time.Duration(0))
	assert.Greater(t, cfg.Redis.DialTimeout, time.Duration(0))
	assert.Greater(t, cfg.ES.IndexTimeout, time.Duration(0))
	assert.Greater(t, cfg.Kafka.SessionTimeout, time.Duration(0))
	assert.Greater(t, cfg.WS.ReadDeadline, time.Duration(0))
}
