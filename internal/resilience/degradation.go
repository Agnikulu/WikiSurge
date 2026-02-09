package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

// DegradationLevel represents how degraded the system currently is.
type DegradationLevel int

const (
	// DegradationNone — everything is operational.
	DegradationNone DegradationLevel = iota
	// DegradationPartial — some non-critical features disabled.
	DegradationPartial
	// DegradationSevere — most features disabled, only core processing.
	DegradationSevere
)

func (d DegradationLevel) String() string {
	switch d {
	case DegradationNone:
		return "none"
	case DegradationPartial:
		return "partial"
	case DegradationSevere:
		return "severe"
	default:
		return "unknown"
	}
}

// DegradationManager coordinates graceful degradation across the system.
// It monitors component health states and automatically adjusts feature flags
// and resource limits to keep the core pipeline running.
type DegradationManager struct {
	mu            sync.RWMutex
	features      *config.FeatureFlags
	cfg           *config.Config
	logger        zerolog.Logger
	level         DegradationLevel
	components    map[string]ComponentState
	metrics       *degradationMetrics
	actions       []DegradationAction

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ComponentState tracks the health of an infrastructure component.
type ComponentState struct {
	Name      string    `json:"name"`
	Healthy   bool      `json:"healthy"`
	Message   string    `json:"message"`
	LastCheck time.Time `json:"last_check"`
}

// DegradationAction records an automatic degradation action taken.
type DegradationAction struct {
	Timestamp time.Time `json:"timestamp"`
	Component string    `json:"component"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
}

type degradationMetrics struct {
	level       prometheus.Gauge
	actionsTotal prometheus.Counter
}

// NewDegradationManager creates a new degradation manager.
func NewDegradationManager(
	features *config.FeatureFlags,
	cfg *config.Config,
	logger zerolog.Logger,
) *DegradationManager {
	ctx, cancel := context.WithCancel(context.Background())

	dm := &DegradationManager{
		features:   features,
		cfg:        cfg,
		logger:     logger.With().Str("component", "degradation-manager").Logger(),
		level:      DegradationNone,
		components: make(map[string]ComponentState),
		ctx:        ctx,
		cancel:     cancel,
	}

	dm.metrics = &degradationMetrics{
		level: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "system_degradation_level",
			Help: "Current degradation level (0=none, 1=partial, 2=severe)",
		}),
		actionsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "degradation_actions_total",
			Help: "Total automatic degradation actions taken",
		}),
	}
	prometheus.Register(dm.metrics.level)
	prometheus.Register(dm.metrics.actionsTotal)

	return dm
}

// Level returns the current degradation level.
func (dm *DegradationManager) Level() DegradationLevel {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.level
}

// ComponentHealth returns the current health summary.
func (dm *DegradationManager) ComponentHealth() map[string]ComponentState {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	out := make(map[string]ComponentState, len(dm.components))
	for k, v := range dm.components {
		out[k] = v
	}
	return out
}

// RecentActions returns the last N degradation actions.
func (dm *DegradationManager) RecentActions() []DegradationAction {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	out := make([]DegradationAction, len(dm.actions))
	copy(out, dm.actions)
	return out
}

// HealthCheckResponse is the enhanced health response with degradation info.
type HealthCheckResponse struct {
	Status     string                    `json:"status"`
	Level      string                    `json:"degradation_level"`
	Components map[string]ComponentState `json:"components"`
	Actions    []DegradationAction       `json:"recent_actions,omitempty"`
}

// HealthCheck performs a full health check and returns the result.
func (dm *DegradationManager) HealthCheck() HealthCheckResponse {
	dm.mu.RLock()
	level := dm.level
	components := make(map[string]ComponentState, len(dm.components))
	for k, v := range dm.components {
		components[k] = v
	}
	actions := make([]DegradationAction, len(dm.actions))
	copy(actions, dm.actions)
	dm.mu.RUnlock()

	status := "healthy"
	if level == DegradationPartial {
		status = "degraded"
	} else if level == DegradationSevere {
		status = "critical"
	}

	return HealthCheckResponse{
		Status:     status,
		Level:      level.String(),
		Components: components,
		Actions:    actions,
	}
}

// -----------------------------------------------------------------------
// Scenario handlers
// -----------------------------------------------------------------------

// HandleElasticsearchUnavailable applies Scenario 1: ES is down.
//   - Disable indexing
//   - Trending and alerts continue
//   - Search returns error
func (dm *DegradationManager) HandleElasticsearchUnavailable(reason string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.components["elasticsearch"] = ComponentState{
		Name: "elasticsearch", Healthy: false,
		Message: reason, LastCheck: time.Now(),
	}

	dm.features.DisableFeature(config.FeatureElasticsearchIndexing, reason)
	dm.recordAction("elasticsearch", "disabled indexing", reason)

	dm.recalcLevel()
	dm.logger.Warn().Str("reason", reason).Msg("Elasticsearch unavailable — indexing disabled")
}

// HandleElasticsearchRecovered reverts Scenario 1.
func (dm *DegradationManager) HandleElasticsearchRecovered() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.components["elasticsearch"] = ComponentState{
		Name: "elasticsearch", Healthy: true,
		Message: "recovered", LastCheck: time.Now(),
	}

	dm.features.EnableFeature(config.FeatureElasticsearchIndexing)
	dm.recordAction("elasticsearch", "re-enabled indexing", "recovered")

	dm.recalcLevel()
	dm.logger.Info().Msg("Elasticsearch recovered — indexing re-enabled")
}

// HandleRedisMemoryLimit applies Scenario 2: Redis memory pressure.
//   - Reduce hot pages to reducedLimit (e.g. 100)
//   - Disable trending if still insufficient
func (dm *DegradationManager) HandleRedisMemoryLimit(reducedLimit int) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.components["redis"] = ComponentState{
		Name: "redis", Healthy: false,
		Message: fmt.Sprintf("memory pressure — hot page limit reduced to %d", reducedLimit),
		LastCheck: time.Now(),
	}

	// Reduce hot page limit.
	dm.cfg.Redis.HotPages.MaxTracked = reducedLimit
	dm.recordAction("redis", fmt.Sprintf("reduced hot page limit to %d", reducedLimit), "memory pressure")

	dm.recalcLevel()
	dm.logger.Warn().Int("hot_page_limit", reducedLimit).Msg("Redis memory pressure — hot page limit reduced")
}

// HandleRedisMemoryCritical applies the second stage of Scenario 2.
func (dm *DegradationManager) HandleRedisMemoryCritical() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.features.DisableFeature(config.FeatureTrendingTracking, "redis memory critical")
	dm.recordAction("redis", "disabled trending tracking", "redis memory critical")

	dm.recalcLevel()
	dm.logger.Warn().Msg("Redis memory critical — trending disabled")
}

// HandleRedisRecovered reverts Scenario 2.
func (dm *DegradationManager) HandleRedisRecovered(normalLimit int) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.components["redis"] = ComponentState{
		Name: "redis", Healthy: true,
		Message: "recovered", LastCheck: time.Now(),
	}

	dm.cfg.Redis.HotPages.MaxTracked = normalLimit
	dm.features.EnableFeature(config.FeatureTrendingTracking)
	dm.recordAction("redis", "restored normal operation", "recovered")

	dm.recalcLevel()
	dm.logger.Info().Msg("Redis recovered — normal limits restored")
}

// HandleHighKafkaLag applies Scenario 3: Kafka lag is high.
//   - Temporarily pause ES indexing
//   - Prioritize real-time updates
func (dm *DegradationManager) HandleHighKafkaLag() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.components["kafka"] = ComponentState{
		Name: "kafka", Healthy: false,
		Message: "high consumer lag", LastCheck: time.Now(),
	}

	dm.features.DisableFeature(config.FeatureElasticsearchIndexing, "high kafka lag — pausing indexing")
	dm.recordAction("kafka", "paused ES indexing", "high kafka lag")

	dm.recalcLevel()
	dm.logger.Warn().Msg("High Kafka lag — ES indexing paused")
}

// HandleKafkaLagRecovered reverts Scenario 3.
func (dm *DegradationManager) HandleKafkaLagRecovered() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.components["kafka"] = ComponentState{
		Name: "kafka", Healthy: true,
		Message: "lag recovered", LastCheck: time.Now(),
	}

	dm.features.EnableFeature(config.FeatureElasticsearchIndexing)
	dm.recordAction("kafka", "resumed ES indexing", "lag recovered")

	dm.recalcLevel()
	dm.logger.Info().Msg("Kafka lag recovered — ES indexing resumed")
}

// -----------------------------------------------------------------------
// Internal helpers
// -----------------------------------------------------------------------

func (dm *DegradationManager) recordAction(component, action, reason string) {
	a := DegradationAction{
		Timestamp: time.Now(),
		Component: component,
		Action:    action,
		Reason:    reason,
	}
	dm.actions = append(dm.actions, a)
	if len(dm.actions) > 50 {
		dm.actions = dm.actions[len(dm.actions)-50:]
	}
	dm.metrics.actionsTotal.Inc()
}

// recalcLevel recomputes the degradation level based on component states.
// Must be called with dm.mu held.
func (dm *DegradationManager) recalcLevel() {
	unhealthy := 0
	for _, cs := range dm.components {
		if !cs.Healthy {
			unhealthy++
		}
	}

	old := dm.level
	switch {
	case unhealthy == 0:
		dm.level = DegradationNone
	case unhealthy == 1:
		dm.level = DegradationPartial
	default:
		dm.level = DegradationSevere
	}

	if dm.level != old {
		dm.metrics.level.Set(float64(dm.level))
		dm.logger.Info().
			Str("from", old.String()).
			Str("to", dm.level.String()).
			Int("unhealthy_components", unhealthy).
			Msg("Degradation level changed")
	}
}

// Stop shuts down the manager.
func (dm *DegradationManager) Stop() {
	dm.cancel()
	dm.wg.Wait()
}
