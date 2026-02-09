package config

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestFeatureFlags_AllEnabledByDefault(t *testing.T) {
	ff := NewFeatureFlags(zerolog.Nop())

	for _, f := range AllFeatures() {
		assert.True(t, ff.IsEnabled(f), "feature %s should be enabled by default", f)
	}
}

func TestFeatureFlags_DisableAndEnable(t *testing.T) {
	ff := NewFeatureFlags(zerolog.Nop())

	ff.DisableFeature(FeatureElasticsearchIndexing, "testing")
	assert.False(t, ff.IsEnabled(FeatureElasticsearchIndexing))
	assert.Equal(t, "testing", ff.DisableReason(FeatureElasticsearchIndexing))

	ff.EnableFeature(FeatureElasticsearchIndexing)
	assert.True(t, ff.IsEnabled(FeatureElasticsearchIndexing))
	assert.Empty(t, ff.DisableReason(FeatureElasticsearchIndexing))
}

func TestFeatureFlags_UnknownFeatureDisabled(t *testing.T) {
	ff := NewFeatureFlags(zerolog.Nop())
	assert.False(t, ff.IsEnabled("nonexistent"))
}

func TestFeatureFlags_SafeExecute_Enabled(t *testing.T) {
	ff := NewFeatureFlags(zerolog.Nop())
	executed := false

	err := ff.SafeExecute(FeatureTrendingTracking, func() error {
		executed = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, executed)
}

func TestFeatureFlags_SafeExecute_Disabled(t *testing.T) {
	ff := NewFeatureFlags(zerolog.Nop())
	ff.DisableFeature(FeatureTrendingTracking, "test")
	executed := false

	err := ff.SafeExecute(FeatureTrendingTracking, func() error {
		executed = true
		return nil
	})

	assert.NoError(t, err)
	assert.False(t, executed, "function should not have been called")
}

func TestFeatureFlags_SafeExecute_PanicRecovery(t *testing.T) {
	ff := NewFeatureFlags(zerolog.Nop())

	err := ff.SafeExecute(FeatureWebsocketBroadcast, func() error {
		panic("kaboom")
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic")
}

func TestFeatureFlags_Snapshot(t *testing.T) {
	ff := NewFeatureFlags(zerolog.Nop())
	ff.DisableFeature(FeatureEditWarDetection, "test")

	snap := ff.Snapshot()
	assert.True(t, snap[FeatureElasticsearchIndexing])
	assert.False(t, snap[FeatureEditWarDetection])
}

func TestFeatureFlags_FromConfig(t *testing.T) {
	cfg := &Features{
		ElasticsearchIndexing: true,
		Trending:             false,
		EditWars:             true,
		Websockets:           false,
	}

	ff := NewFeatureFlagsFromConfig(cfg, zerolog.Nop())
	assert.True(t, ff.IsEnabled(FeatureElasticsearchIndexing))
	assert.False(t, ff.IsEnabled(FeatureTrendingTracking))
	assert.True(t, ff.IsEnabled(FeatureEditWarDetection))
	assert.False(t, ff.IsEnabled(FeatureWebsocketBroadcast))
}
