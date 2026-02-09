package resilience

import "time"

// TimeoutConfig centralises all timeout values used across the system.
// Values are organised by subsystem for easy auditing and tuning.
type TimeoutConfig struct {
	HTTP   HTTPTimeouts   `yaml:"http"`
	Redis  RedisTimeouts  `yaml:"redis"`
	ES     ESTimeouts     `yaml:"elasticsearch"`
	Kafka  KafkaTimeouts  `yaml:"kafka"`
	WS     WSTimeouts     `yaml:"websocket"`
}

// HTTPTimeouts configures outbound HTTP client behaviour.
type HTTPTimeouts struct {
	// ConnectTimeout is the maximum time to establish a TCP connection.
	ConnectTimeout time.Duration `yaml:"connect_timeout"`
	// RequestTimeout is the overall request deadline (connect + read).
	RequestTimeout time.Duration `yaml:"request_timeout"`
	// IdleConnTimeout is how long idle keep-alive connections survive.
	IdleConnTimeout time.Duration `yaml:"idle_conn_timeout"`
	// TLSHandshakeTimeout limits TLS negotiation.
	TLSHandshakeTimeout time.Duration `yaml:"tls_handshake_timeout"`
	// ResponseHeaderTimeout limits waiting for response headers.
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"`
}

// RedisTimeouts configures Redis operation deadlines.
type RedisTimeouts struct {
	// DialTimeout is the timeout for establishing new connections.
	DialTimeout time.Duration `yaml:"dial_timeout"`
	// ReadTimeout is the timeout per read operation.
	ReadTimeout time.Duration `yaml:"read_timeout"`
	// WriteTimeout is the timeout per write operation.
	WriteTimeout time.Duration `yaml:"write_timeout"`
	// PoolTimeout is how long to wait for an available pool connection.
	PoolTimeout time.Duration `yaml:"pool_timeout"`
}

// ESTimeouts configures Elasticsearch operation deadlines.
type ESTimeouts struct {
	// IndexTimeout is the deadline for indexing a document.
	IndexTimeout time.Duration `yaml:"index_timeout"`
	// SearchTimeout is the deadline for search requests.
	SearchTimeout time.Duration `yaml:"search_timeout"`
	// BulkTimeout is the deadline for bulk index operations.
	BulkTimeout time.Duration `yaml:"bulk_timeout"`
	// HealthCheckTimeout limits ES health checks.
	HealthCheckTimeout time.Duration `yaml:"health_check_timeout"`
}

// KafkaTimeouts configures Kafka client deadlines.
type KafkaTimeouts struct {
	// SessionTimeout is the consumer group session timeout.
	SessionTimeout time.Duration `yaml:"session_timeout"`
	// HeartbeatInterval is how often the consumer sends heartbeats.
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	// RequestTimeout is the timeout for individual Kafka requests.
	RequestTimeout time.Duration `yaml:"request_timeout"`
	// WriteTimeout is the producer write timeout.
	WriteTimeout time.Duration `yaml:"write_timeout"`
	// ReadTimeout is the consumer fetch timeout.
	ReadTimeout time.Duration `yaml:"read_timeout"`
}

// WSTimeouts configures WebSocket deadlines.
type WSTimeouts struct {
	// ReadDeadline is the maximum time between client messages before
	// the connection is considered stale.
	ReadDeadline time.Duration `yaml:"read_deadline"`
	// WriteDeadline is the deadline for writing a single frame to the client.
	WriteDeadline time.Duration `yaml:"write_deadline"`
	// PingInterval is how often the server pings the client.
	PingInterval time.Duration `yaml:"ping_interval"`
	// PongWait is how long to wait for a pong after a ping.
	PongWait time.Duration `yaml:"pong_wait"`
}

// DefaultTimeoutConfig returns production-safe defaults for all subsystems.
func DefaultTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		HTTP: HTTPTimeouts{
			ConnectTimeout:        5 * time.Second,
			RequestTimeout:        10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
		},
		Redis: RedisTimeouts{
			DialTimeout:  5 * time.Second,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			PoolTimeout:  6 * time.Second,
		},
		ES: ESTimeouts{
			IndexTimeout:       30 * time.Second,
			SearchTimeout:      10 * time.Second,
			BulkTimeout:        60 * time.Second,
			HealthCheckTimeout: 5 * time.Second,
		},
		Kafka: KafkaTimeouts{
			SessionTimeout:    30 * time.Second,
			HeartbeatInterval: 10 * time.Second,
			RequestTimeout:    30 * time.Second,
			WriteTimeout:      10 * time.Second,
			ReadTimeout:       10 * time.Second,
		},
		WS: WSTimeouts{
			ReadDeadline:  60 * time.Second,
			WriteDeadline: 10 * time.Second,
			PingInterval:  30 * time.Second,
			PongWait:      60 * time.Second,
		},
	}
}
