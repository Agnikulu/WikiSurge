package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Global metric variables are non-nil
// ---------------------------------------------------------------------------

func TestMetricVariables_Counters(t *testing.T) {
	counters := map[string]*prometheus.CounterVec{
		"EditsIngestedTotal":             EditsIngestedTotal,
		"EditsFilteredTotal":             EditsFilteredTotal,
		"KafkaProduceErrorsTotal":        KafkaProduceErrorsTotal,
		"ProduceAttemptsTotal":           ProduceAttemptsTotal,
		"MessagesProducedTotal":          MessagesProducedTotal,
		"MessagesDroppedTotal":           MessagesDroppedTotal,
		"ProduceErrorsTotal":             ProduceErrorsTotal,
		"EditsProcessedTotal":            EditsProcessedTotal,
		"ProcessingErrorsTotal":          ProcessingErrorsTotal,
		"DocsIndexedTotal":               DocsIndexedTotal,
		"IndexErrorsTotal":               IndexErrorsTotal,
		"SpikesDetectedTotal":            SpikesDetectedTotal,
		"EditWarsDetectedTotal":          EditWarsDetectedTotal,
		"APIRequestsTotal":               APIRequestsTotal,
		"WebSocketConnectionsTotal":      WebSocketConnectionsTotal,
		"WebSocketDisconnectionsTotal":   WebSocketDisconnectionsTotal,
		"WebSocketMessagesBroadcast":     WebSocketMessagesBroadcast,
		"WebSocketMessagesDropped":       WebSocketMessagesDropped,
		"RateLimitHitsTotal":             RateLimitHitsTotal,
		"SSEReconnectionsTotal":          SSEReconnectionsTotal,
		"APIErrorsTotal":                 APIErrorsTotal,
		"APICacheHitsTotal":              APICacheHitsTotal,
		"APICacheMissesTotal":            APICacheMissesTotal,
		"WebSocketMessagesSentTotal":     WebSocketMessagesSentTotal,
		"WebSocketMessagesReceivedTotal": WebSocketMessagesReceivedTotal,
		"ActivityCounterTotal":           ActivityCounterTotal,
		"HotPagesPromotedTotal":          HotPagesPromotedTotal,
		"PromotionRejectedTotal":         PromotionRejectedTotal,
		"HotPagesExpiredTotal":           HotPagesExpiredTotal,
		"CleanupRunsTotal":               CleanupRunsTotal,
	}
	for name, c := range counters {
		assert.NotNilf(t, c, "counter %s should not be nil", name)
	}
}

func TestMetricVariables_Gauges(t *testing.T) {
	gauges := map[string]*prometheus.GaugeVec{
		"KafkaConsumerLag":            KafkaConsumerLag,
		"RedisMemoryBytes":            RedisMemoryBytes,
		"RedisKeysTotal":              RedisKeysTotal,
		"ElasticsearchDocsTotal":      ElasticsearchDocsTotal,
		"ElasticsearchIndexSizeBytes": ElasticsearchIndexSizeBytes,
		"HotPagesTracked":             HotPagesTracked,
		"TrendingPagesTotal":          TrendingPagesTotal,
		"WebSocketConnectionsActive":  WebSocketConnectionsActive,
		"APIRequestsInFlight":         APIRequestsInFlight,
	}
	for name, g := range gauges {
		assert.NotNilf(t, g, "gauge %s should not be nil", name)
	}
}

func TestMetricVariables_Histograms(t *testing.T) {
	histograms := map[string]*prometheus.HistogramVec{
		"KafkaProduceLatency":          KafkaProduceLatency,
		"ProcessingDuration":           ProcessingDuration,
		"APIRequestDuration":           APIRequestDuration,
		"APIResponseSizeBytes":         APIResponseSizeBytes,
		"ElasticsearchQueryDuration":   ElasticsearchQueryDuration,
	}
	for name, h := range histograms {
		assert.NotNilf(t, h, "histogram %s should not be nil", name)
	}
}

// ---------------------------------------------------------------------------
// Helper functions: IncrementCounter, SetGauge, ObserveHistogram, GetMetric
// ---------------------------------------------------------------------------

func seedTestRegistry() func() {
	registryMu.Lock()
	defer registryMu.Unlock()

	old := make(map[string]prometheus.Collector)
	for k, v := range metricsRegistry {
		old[k] = v
	}

	testCounter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_helper_counter",
	}, []string{"label"})
	testGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "test_helper_gauge",
	}, []string{"label"})
	testHist := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "test_helper_histogram",
	}, []string{"label"})

	metricsRegistry["test_counter"] = testCounter
	metricsRegistry["test_gauge"] = testGauge
	metricsRegistry["test_histogram"] = testHist

	return func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		delete(metricsRegistry, "test_counter")
		delete(metricsRegistry, "test_gauge")
		delete(metricsRegistry, "test_histogram")
		for k, v := range old {
			metricsRegistry[k] = v
		}
	}
}

func TestIncrementCounter_Existing(t *testing.T) {
	cleanup := seedTestRegistry()
	defer cleanup()

	// Should not panic
	IncrementCounter("test_counter", map[string]string{"label": "val"})
}

func TestIncrementCounter_Missing(t *testing.T) {
	// Should be a no-op, not panic
	IncrementCounter("nonexistent_metric", map[string]string{})
}

func TestIncrementCounter_WrongType(t *testing.T) {
	cleanup := seedTestRegistry()
	defer cleanup()

	// test_gauge is a gauge, not a counter — should be a no-op
	IncrementCounter("test_gauge", map[string]string{"label": "v"})
}

func TestSetGauge_Existing(t *testing.T) {
	cleanup := seedTestRegistry()
	defer cleanup()

	SetGauge("test_gauge", 42.5, map[string]string{"label": "v"})
}

func TestSetGauge_Missing(t *testing.T) {
	SetGauge("nonexistent_metric", 1.0, map[string]string{})
}

func TestSetGauge_WrongType(t *testing.T) {
	cleanup := seedTestRegistry()
	defer cleanup()

	// test_counter is a counter — should be a no-op
	SetGauge("test_counter", 10, map[string]string{"label": "v"})
}

func TestObserveHistogram_Existing(t *testing.T) {
	cleanup := seedTestRegistry()
	defer cleanup()

	ObserveHistogram("test_histogram", 0.123, map[string]string{"label": "v"})
}

func TestObserveHistogram_Missing(t *testing.T) {
	ObserveHistogram("nonexistent_metric", 1.0, map[string]string{})
}

func TestObserveHistogram_WrongType(t *testing.T) {
	cleanup := seedTestRegistry()
	defer cleanup()

	ObserveHistogram("test_counter", 1.0, map[string]string{"label": "v"})
}

func TestGetMetric_Existing(t *testing.T) {
	cleanup := seedTestRegistry()
	defer cleanup()

	m := GetMetric("test_counter")
	require.NotNil(t, m)
}

func TestGetMetric_Missing(t *testing.T) {
	m := GetMetric("totally_missing_metric_xyz")
	assert.Nil(t, m)
}

// ---------------------------------------------------------------------------
// Registry map is initialised
// ---------------------------------------------------------------------------

func TestRegistryMapInitialized(t *testing.T) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	assert.NotNil(t, metricsRegistry)
}

// ---------------------------------------------------------------------------
// Counter label combinations
// ---------------------------------------------------------------------------

func TestCounterVec_WithLabels(t *testing.T) {
	// EditsFilteredTotal has label "reason"
	c, err := EditsFilteredTotal.GetMetricWithLabelValues("bot")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestCounterVec_APIRequests(t *testing.T) {
	c, err := APIRequestsTotal.GetMetricWithLabelValues("/trending", "GET")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestGaugeVec_RedisKeys(t *testing.T) {
	g, err := RedisKeysTotal.GetMetricWithLabelValues("string")
	require.NoError(t, err)
	assert.NotNil(t, g)
}

func TestHistogramVec_ProcessingDuration(t *testing.T) {
	h, err := ProcessingDuration.GetMetricWithLabelValues("main")
	require.NoError(t, err)
	assert.NotNil(t, h)
}
