package resilience

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

// CircuitState represents the current state of a circuit breaker.
type CircuitState int

const (
	// StateClosed — requests flow through normally.
	StateClosed CircuitState = iota
	// StateOpen — requests are immediately rejected.
	StateOpen
	// StateHalfOpen — a single probe request is allowed.
	StateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreakerConfig holds tunables for a circuit breaker.
type CircuitBreakerConfig struct {
	// Name is a human-readable label (used in logs and metrics).
	Name string
	// FailureThreshold is the number of consecutive failures before the
	// circuit opens (default: 5).
	FailureThreshold int
	// ResetTimeout is how long to wait in the open state before transitioning
	// to half-open (default: 30s).
	ResetTimeout time.Duration
	// HalfOpenMaxCalls limits the number of probe calls in half-open state
	// (default: 1).
	HalfOpenMaxCalls int
}

func (c *CircuitBreakerConfig) setDefaults() {
	if c.FailureThreshold == 0 {
		c.FailureThreshold = 5
	}
	if c.ResetTimeout == 0 {
		c.ResetTimeout = 30 * time.Second
	}
	if c.HalfOpenMaxCalls == 0 {
		c.HalfOpenMaxCalls = 1
	}
}

// CircuitBreaker implements the circuit-breaker pattern.
type CircuitBreaker struct {
	mu               sync.Mutex
	name             string
	state            CircuitState
	failureCount     int
	successCount     int
	failureThreshold int
	resetTimeout     time.Duration
	halfOpenMaxCalls int
	lastFailureTime  time.Time
	logger           zerolog.Logger
	metrics          *circuitBreakerMetrics

	// onStateChange is an optional callback fired on state transitions.
	onStateChange func(name string, from, to CircuitState)
}

type circuitBreakerMetrics struct {
	stateGauge  prometheus.Gauge
	failures    prometheus.Counter
	successes   prometheus.Counter
	rejections  prometheus.Counter
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration.
func NewCircuitBreaker(cfg CircuitBreakerConfig, logger zerolog.Logger) *CircuitBreaker {
	cfg.setDefaults()

	cb := &CircuitBreaker{
		name:             cfg.Name,
		state:            StateClosed,
		failureThreshold: cfg.FailureThreshold,
		resetTimeout:     cfg.ResetTimeout,
		halfOpenMaxCalls: cfg.HalfOpenMaxCalls,
		logger:           logger.With().Str("component", "circuit-breaker").Str("breaker", cfg.Name).Logger(),
	}

	// Best-effort metric registration.
	cb.metrics = &circuitBreakerMetrics{
		stateGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "circuit_breaker_state",
			Help:        "Current state of the circuit breaker (0=closed, 1=open, 2=half-open)",
			ConstLabels: prometheus.Labels{"breaker": cfg.Name},
		}),
		failures: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "circuit_breaker_failures_total",
			Help:        "Total number of failures recorded by the circuit breaker",
			ConstLabels: prometheus.Labels{"breaker": cfg.Name},
		}),
		successes: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "circuit_breaker_successes_total",
			Help:        "Total number of successes recorded by the circuit breaker",
			ConstLabels: prometheus.Labels{"breaker": cfg.Name},
		}),
		rejections: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "circuit_breaker_rejections_total",
			Help:        "Total number of calls rejected due to open circuit",
			ConstLabels: prometheus.Labels{"breaker": cfg.Name},
		}),
	}
	prometheus.Register(cb.metrics.stateGauge)
	prometheus.Register(cb.metrics.failures)
	prometheus.Register(cb.metrics.successes)
	prometheus.Register(cb.metrics.rejections)

	return cb
}

// OnStateChange registers a callback that fires whenever the breaker changes
// state.
func (cb *CircuitBreaker) OnStateChange(fn func(name string, from, to CircuitState)) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onStateChange = fn
}

// Call executes fn if the circuit breaker allows it.
//   - Closed: fn runs; failures are counted.
//   - Open: returns ErrCircuitOpen immediately.
//   - HalfOpen: one probe call is allowed; success closes, failure re-opens.
func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()

	switch cb.state {
	case StateOpen:
		// Check if the reset timeout has elapsed.
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			cb.transitionTo(StateHalfOpen)
			cb.successCount = 0
		} else {
			cb.mu.Unlock()
			cb.metrics.rejections.Inc()
			return ErrCircuitOpen
		}

	case StateHalfOpen:
		// Allow a limited number of probe calls.
		if cb.successCount >= cb.halfOpenMaxCalls {
			cb.mu.Unlock()
			cb.metrics.rejections.Inc()
			return ErrCircuitOpen
		}
	}

	// State is now either Closed or HalfOpen — allow the call.
	cb.mu.Unlock()

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

// GetState returns the current state as a human-readable string.
func (cb *CircuitBreaker) GetState() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check for automatic transition on read.
	if cb.state == StateOpen && time.Since(cb.lastFailureTime) > cb.resetTimeout {
		cb.transitionTo(StateHalfOpen)
	}
	return cb.state.String()
}

// Reset manually resets the circuit breaker to the closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.transitionTo(StateClosed)
	cb.failureCount = 0
	cb.successCount = 0

	cb.logger.Info().Msg("Circuit breaker manually reset")
}

// ConsecutiveFailures returns the current failure count.
func (cb *CircuitBreaker) ConsecutiveFailures() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failureCount
}

// --- internal helpers ---

func (cb *CircuitBreaker) recordFailure() {
	cb.failureCount++
	cb.lastFailureTime = time.Now()
	cb.metrics.failures.Inc()

	cb.logger.Warn().
		Int("consecutive_failures", cb.failureCount).
		Int("threshold", cb.failureThreshold).
		Msg("Circuit breaker recorded failure")

	if cb.failureCount >= cb.failureThreshold {
		cb.transitionTo(StateOpen)
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.metrics.successes.Inc()

	switch cb.state {
	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.halfOpenMaxCalls {
			cb.transitionTo(StateClosed)
			cb.failureCount = 0
			cb.successCount = 0
		}
	case StateClosed:
		cb.failureCount = 0 // reset streak on success
	}
}

func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	if cb.state == newState {
		return
	}
	old := cb.state
	cb.state = newState
	cb.metrics.stateGauge.Set(float64(newState))

	cb.logger.Info().
		Str("from", old.String()).
		Str("to", newState.String()).
		Msg("Circuit breaker state transition")

	if cb.onStateChange != nil {
		go cb.onStateChange(cb.name, old, newState)
	}
}

// ---------------------------------------------------------------------------
// CircuitBreakerRegistry — convenience for managing multiple breakers
// ---------------------------------------------------------------------------

// CircuitBreakerRegistry is a named collection of circuit breakers.
type CircuitBreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	logger   zerolog.Logger
}

// NewCircuitBreakerRegistry creates a new registry.
func NewCircuitBreakerRegistry(logger zerolog.Logger) *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
		logger:   logger.With().Str("component", "circuit-breaker-registry").Logger(),
	}
}

// Register adds a new circuit breaker to the registry.
func (r *CircuitBreakerRegistry) Register(cfg CircuitBreakerConfig) *CircuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()

	cb := NewCircuitBreaker(cfg, r.logger)
	r.breakers[cfg.Name] = cb
	return cb
}

// Get retrieves a circuit breaker by name.
func (r *CircuitBreakerRegistry) Get(name string) (*CircuitBreaker, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cb, ok := r.breakers[name]
	if !ok {
		return nil, fmt.Errorf("circuit breaker %q not found", name)
	}
	return cb, nil
}

// Snapshot returns a map of breaker name → current state string.
func (r *CircuitBreakerRegistry) Snapshot() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]string, len(r.breakers))
	for name, cb := range r.breakers {
		out[name] = cb.GetState()
	}
	return out
}

// ResetAll manually resets every breaker in the registry.
func (r *CircuitBreakerRegistry) ResetAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, cb := range r.breakers {
		cb.Reset()
	}
}
