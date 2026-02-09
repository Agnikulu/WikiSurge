package resilience

import (
	"time"
)

// -----------------------------------------------------------------------
// Connection pool configuration
// -----------------------------------------------------------------------

// RedisPoolConfig holds optimized Redis connection pool settings.
type RedisPoolConfig struct {
	// PoolSize is the maximum number of connections in the pool.
	PoolSize int `yaml:"pool_size"`
	// MinIdleConns is the minimum number of idle connections maintained.
	MinIdleConns int `yaml:"min_idle_conns"`
	// MaxIdleConns is the maximum number of idle connections kept open.
	MaxIdleConns int `yaml:"max_idle_conns"`
	// ConnMaxIdleTime is how long idle connections can remain before closing.
	ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time"`
	// ConnMaxLifetime is the maximum lifetime of a connection.
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
	// MaxRetries is the number of retries before giving up.
	MaxRetries int `yaml:"max_retries"`
	// MinRetryBackoff is the minimum backoff between retries.
	MinRetryBackoff time.Duration `yaml:"min_retry_backoff"`
	// MaxRetryBackoff is the maximum backoff between retries.
	MaxRetryBackoff time.Duration `yaml:"max_retry_backoff"`
	// DialTimeout is the timeout for establishing new connections.
	DialTimeout time.Duration `yaml:"dial_timeout"`
	// ReadTimeout is the timeout for individual read operations.
	ReadTimeout time.Duration `yaml:"read_timeout"`
	// WriteTimeout is the timeout for individual write operations.
	WriteTimeout time.Duration `yaml:"write_timeout"`
	// PoolTimeout is how long to wait for a connection from the pool.
	PoolTimeout time.Duration `yaml:"pool_timeout"`
	// HealthCheckInterval configures periodic ping checks for idle conns.
	HealthCheckInterval time.Duration `yaml:"health_check_interval"`
}

// DefaultRedisPoolConfig returns production-optimized defaults.
func DefaultRedisPoolConfig() RedisPoolConfig {
	return RedisPoolConfig{
		PoolSize:            20,
		MinIdleConns:        5,
		MaxIdleConns:        10,
		ConnMaxIdleTime:     5 * time.Minute,
		ConnMaxLifetime:     30 * time.Minute,
		MaxRetries:          3,
		MinRetryBackoff:     8 * time.Millisecond,
		MaxRetryBackoff:     512 * time.Millisecond,
		DialTimeout:         5 * time.Second,
		ReadTimeout:         5 * time.Second,
		WriteTimeout:        5 * time.Second,
		PoolTimeout:         6 * time.Second,
		HealthCheckInterval: 30 * time.Second,
	}
}

// ElasticsearchPoolConfig holds optimized ES connection settings.
type ElasticsearchPoolConfig struct {
	// MaxConns is the maximum number of connections per host.
	MaxConns int `yaml:"max_conns"`
	// MaxIdleConns is the max idle connections per host.
	MaxIdleConns int `yaml:"max_idle_conns"`
	// MaxIdleConnsPerHost limits idle conns per host.
	MaxIdleConnsPerHost int `yaml:"max_idle_conns_per_host"`
	// IdleConnTimeout is how long idle conns survive.
	IdleConnTimeout time.Duration `yaml:"idle_conn_timeout"`
	// KeepAlive enables HTTP keep-alive.
	KeepAlive bool `yaml:"keep_alive"`
	// KeepAliveInterval sets the TCP keep-alive interval.
	KeepAliveInterval time.Duration `yaml:"keep_alive_interval"`
	// Sniff whether to sniff cluster topology (false for single-node).
	Sniff bool `yaml:"sniff"`
	// MaxRetries for ES requests.
	MaxRetries int `yaml:"max_retries"`
	// RetryOnStatus is the set of HTTP status codes that trigger a retry.
	RetryOnStatus []int `yaml:"retry_on_status"`
	// RetryInitialWait is the starting backoff for retries.
	RetryInitialWait time.Duration `yaml:"retry_initial_wait"`
}

// DefaultElasticsearchPoolConfig returns production-optimized defaults.
func DefaultElasticsearchPoolConfig() ElasticsearchPoolConfig {
	return ElasticsearchPoolConfig{
		MaxConns:            10,
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		KeepAlive:           true,
		KeepAliveInterval:   30 * time.Second,
		Sniff:               false,
		MaxRetries:          3,
		RetryOnStatus:       []int{502, 503, 504, 429},
		RetryInitialWait:    100 * time.Millisecond,
	}
}
