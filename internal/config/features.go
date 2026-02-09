package config

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// FeatureFlags provides runtime feature toggles for the system.
// All operations are goroutine-safe.
type FeatureFlags struct {
	mu    sync.RWMutex
	flags map[string]bool
	// reasons tracks why a feature was disabled (most recent reason)
	reasons map[string]string
	logger  zerolog.Logger
	metrics *featureFlagMetrics
}

type featureFlagMetrics struct {
	disableEvents *prometheus.CounterVec
	featureState  *prometheus.GaugeVec
}

// Well-known feature names.
const (
	FeatureElasticsearchIndexing = "elasticsearch_indexing"
	FeatureTrendingTracking      = "trending_tracking"
	FeatureEditWarDetection      = "edit_war_detection"
	FeatureWebsocketBroadcast    = "websocket_broadcast"
)

// AllFeatures returns the list of known feature names.
func AllFeatures() []string {
	return []string{
		FeatureElasticsearchIndexing,
		FeatureTrendingTracking,
		FeatureEditWarDetection,
		FeatureWebsocketBroadcast,
	}
}

// NewFeatureFlags creates a new FeatureFlags instance with all features enabled
// by default and registers Prometheus metrics.
func NewFeatureFlags(logger zerolog.Logger) *FeatureFlags {
	ff := &FeatureFlags{
		flags:   make(map[string]bool),
		reasons: make(map[string]string),
		logger:  logger.With().Str("component", "feature-flags").Logger(),
	}

	// Enable all known features by default
	for _, f := range AllFeatures() {
		ff.flags[f] = true
	}

	// Metrics (best-effort; ignore duplicate registration)
	ff.metrics = &featureFlagMetrics{
		disableEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "feature_flag_disable_total",
			Help: "Number of times a feature flag was disabled",
		}, []string{"feature"}),
		featureState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "feature_flag_enabled",
			Help: "Current state of feature flags (1=enabled, 0=disabled)",
		}, []string{"feature"}),
	}
	prometheus.Register(ff.metrics.disableEvents)
	prometheus.Register(ff.metrics.featureState)

	// Publish initial state
	for _, f := range AllFeatures() {
		ff.metrics.featureState.WithLabelValues(f).Set(1)
	}

	return ff
}

// NewFeatureFlagsFromConfig creates FeatureFlags pre-populated from the
// existing config.Features section.
func NewFeatureFlagsFromConfig(cfg *Features, logger zerolog.Logger) *FeatureFlags {
	ff := NewFeatureFlags(logger)

	ff.mu.Lock()
	defer ff.mu.Unlock()

	ff.flags[FeatureElasticsearchIndexing] = cfg.ElasticsearchIndexing
	ff.flags[FeatureTrendingTracking] = cfg.Trending
	ff.flags[FeatureEditWarDetection] = cfg.EditWars
	ff.flags[FeatureWebsocketBroadcast] = cfg.Websockets

	for _, f := range AllFeatures() {
		val := float64(0)
		if ff.flags[f] {
			val = 1
		}
		ff.metrics.featureState.WithLabelValues(f).Set(val)
	}

	return ff
}

// IsEnabled returns whether a feature is currently enabled.
func (ff *FeatureFlags) IsEnabled(feature string) bool {
	ff.mu.RLock()
	defer ff.mu.RUnlock()
	enabled, ok := ff.flags[feature]
	if !ok {
		// Unknown features are treated as disabled.
		return false
	}
	return enabled
}

// EnableFeature enables a feature at runtime.
func (ff *FeatureFlags) EnableFeature(feature string) {
	ff.mu.Lock()
	defer ff.mu.Unlock()

	ff.flags[feature] = true
	delete(ff.reasons, feature)

	ff.logger.Info().
		Str("feature", feature).
		Msg("Feature enabled")

	ff.metrics.featureState.WithLabelValues(feature).Set(1)
}

// DisableFeature disables a feature and records the reason.
// It logs a warning, updates metrics, and marks the feature as disabled.
func (ff *FeatureFlags) DisableFeature(feature, reason string) {
	ff.mu.Lock()
	defer ff.mu.Unlock()

	ff.flags[feature] = false
	ff.reasons[feature] = reason

	ff.logger.Warn().
		Str("feature", feature).
		Str("reason", reason).
		Msg("Feature disabled")

	ff.metrics.disableEvents.WithLabelValues(feature).Inc()
	ff.metrics.featureState.WithLabelValues(feature).Set(0)
}

// DisableReason returns the most recent reason a feature was disabled. If the
// feature is enabled or has never been disabled, it returns an empty string.
func (ff *FeatureFlags) DisableReason(feature string) string {
	ff.mu.RLock()
	defer ff.mu.RUnlock()
	return ff.reasons[feature]
}

// SafeExecute runs fn only when the given feature is enabled.
// If the feature is disabled it is a no-op and returns nil.
// Panics inside fn are recovered and returned as errors.
func (ff *FeatureFlags) SafeExecute(feature string, fn func() error) error {
	if !ff.IsEnabled(feature) {
		ff.logger.Debug().
			Str("feature", feature).
			Msg("Skipping execution — feature disabled")
		return nil
	}

	// Recover panics so a single bad feature doesn't bring down the system.
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic in feature %s: %v", feature, r)
				ff.logger.Error().
					Str("feature", feature).
					Interface("panic", r).
					Msg("Panic recovered in SafeExecute")
			}
		}()
		err = fn()
	}()
	return err
}

// Snapshot returns a point-in-time copy of all feature states.
func (ff *FeatureFlags) Snapshot() map[string]bool {
	ff.mu.RLock()
	defer ff.mu.RUnlock()

	out := make(map[string]bool, len(ff.flags))
	for k, v := range ff.flags {
		out[k] = v
	}
	return out
}

// DefaultFeatureFlags returns a globally usable FeatureFlags instance.
// Intended for packages that don't have access to DI.
var DefaultFeatureFlags = NewFeatureFlags(log.Logger)
