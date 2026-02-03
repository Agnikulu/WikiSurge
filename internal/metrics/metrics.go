package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// Counters
	EditsIngestedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "edits_ingested_total",
			Help: "Total edits received from Wikipedia SSE",
		},
		[]string{},
	)

	EditsFilteredTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "edits_filtered_total",
			Help: "Edits filtered out (bots, languages)",
		},
		[]string{"reason"},
	)

	KafkaProduceErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kafka_produce_errors_total",
			Help: "Kafka production failures",
		},
		[]string{},
	)

	EditsProcessedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "edits_processed_total",
			Help: "Edits processed per consumer",
		},
		[]string{"consumer"},
	)

	ProcessingErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "processing_errors_total",
			Help: "Processing errors",
		},
		[]string{"consumer"},
	)

	DocsIndexedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "docs_indexed_total",
			Help: "Documents indexed to Elasticsearch",
		},
		[]string{},
	)

	IndexErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "index_errors_total",
			Help: "Elasticsearch indexing errors",
		},
		[]string{},
	)

	SpikesDetectedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "spikes_detected_total",
			Help: "Spike alerts generated",
		},
		[]string{},
	)

	EditWarsDetectedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "edit_wars_detected_total",
			Help: "Edit war alerts generated",
		},
		[]string{},
	)

	APIRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_requests_total",
			Help: "API requests",
		},
		[]string{"endpoint", "method"},
	)

	WebSocketConnectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "websocket_connections_total",
			Help: "WebSocket connections established",
		},
		[]string{},
	)

	WebSocketDisconnectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "websocket_disconnections_total",
			Help: "WebSocket disconnections",
		},
		[]string{},
	)

	// Gauges
	KafkaConsumerLag = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kafka_consumer_lag",
			Help: "Current lag in messages",
		},
		[]string{"consumer"},
	)

	RedisMemoryBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redis_memory_bytes",
			Help: "Current Redis memory usage",
		},
		[]string{},
	)

	RedisKeysTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "redis_keys_total",
			Help: "Redis key counts by type",
		},
		[]string{"type"},
	)

	ElasticsearchDocsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "elasticsearch_docs_total",
			Help: "Total documents in ES",
		},
		[]string{},
	)

	ElasticsearchIndexSizeBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "elasticsearch_index_size_bytes",
			Help: "Total index size",
		},
		[]string{},
	)

	HotPagesTracked = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "hot_pages_tracked",
			Help: "Current hot pages being tracked",
		},
		[]string{},
	)

	TrendingPagesTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "trending_pages_total",
			Help: "Pages in trending set",
		},
		[]string{},
	)

	WebSocketConnectionsActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "websocket_connections_active",
			Help: "Currently active WebSocket connections",
		},
		[]string{},
	)

	APIRequestsInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "api_requests_in_flight",
			Help: "Concurrent API requests",
		},
		[]string{},
	)

	// Histograms
	KafkaProduceLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kafka_produce_latency_seconds",
			Help:    "Kafka produce operation duration",
			Buckets: prometheus.DefBuckets,
		},
		[]string{},
	)

	ProcessingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "processing_duration_seconds",
			Help:    "Processing time per edit",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"consumer"},
	)

	APIRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "api_request_duration_seconds",
			Help:    "API request duration",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"endpoint"},
	)

	ElasticsearchQueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "elasticsearch_query_duration_seconds",
			Help:    "ES query duration",
			Buckets: prometheus.DefBuckets,
		},
		[]string{},
	)

	// Registry for all metrics
	metricsRegistry = make(map[string]prometheus.Collector)
	registryMu      sync.RWMutex
)

// InitMetrics registers all metrics with Prometheus
func InitMetrics() {
	registryMu.Lock()
	defer registryMu.Unlock()

	// Register all counters
	prometheus.MustRegister(EditsIngestedTotal)
	metricsRegistry["edits_ingested_total"] = EditsIngestedTotal

	prometheus.MustRegister(EditsFilteredTotal)
	metricsRegistry["edits_filtered_total"] = EditsFilteredTotal

	prometheus.MustRegister(KafkaProduceErrorsTotal)
	metricsRegistry["kafka_produce_errors_total"] = KafkaProduceErrorsTotal

	prometheus.MustRegister(EditsProcessedTotal)
	metricsRegistry["edits_processed_total"] = EditsProcessedTotal

	prometheus.MustRegister(ProcessingErrorsTotal)
	metricsRegistry["processing_errors_total"] = ProcessingErrorsTotal

	prometheus.MustRegister(DocsIndexedTotal)
	metricsRegistry["docs_indexed_total"] = DocsIndexedTotal

	prometheus.MustRegister(IndexErrorsTotal)
	metricsRegistry["index_errors_total"] = IndexErrorsTotal

	prometheus.MustRegister(SpikesDetectedTotal)
	metricsRegistry["spikes_detected_total"] = SpikesDetectedTotal

	prometheus.MustRegister(EditWarsDetectedTotal)
	metricsRegistry["edit_wars_detected_total"] = EditWarsDetectedTotal

	prometheus.MustRegister(APIRequestsTotal)
	metricsRegistry["api_requests_total"] = APIRequestsTotal

	prometheus.MustRegister(WebSocketConnectionsTotal)
	metricsRegistry["websocket_connections_total"] = WebSocketConnectionsTotal

	prometheus.MustRegister(WebSocketDisconnectionsTotal)
	metricsRegistry["websocket_disconnections_total"] = WebSocketDisconnectionsTotal

	// Register all gauges
	prometheus.MustRegister(KafkaConsumerLag)
	metricsRegistry["kafka_consumer_lag"] = KafkaConsumerLag

	prometheus.MustRegister(RedisMemoryBytes)
	metricsRegistry["redis_memory_bytes"] = RedisMemoryBytes

	prometheus.MustRegister(RedisKeysTotal)
	metricsRegistry["redis_keys_total"] = RedisKeysTotal

	prometheus.MustRegister(ElasticsearchDocsTotal)
	metricsRegistry["elasticsearch_docs_total"] = ElasticsearchDocsTotal

	prometheus.MustRegister(ElasticsearchIndexSizeBytes)
	metricsRegistry["elasticsearch_index_size_bytes"] = ElasticsearchIndexSizeBytes

	prometheus.MustRegister(HotPagesTracked)
	metricsRegistry["hot_pages_tracked"] = HotPagesTracked

	prometheus.MustRegister(TrendingPagesTotal)
	metricsRegistry["trending_pages_total"] = TrendingPagesTotal

	prometheus.MustRegister(WebSocketConnectionsActive)
	metricsRegistry["websocket_connections_active"] = WebSocketConnectionsActive

	prometheus.MustRegister(APIRequestsInFlight)
	metricsRegistry["api_requests_in_flight"] = APIRequestsInFlight

	// Register all histograms
	prometheus.MustRegister(KafkaProduceLatency)
	metricsRegistry["kafka_produce_latency_seconds"] = KafkaProduceLatency

	prometheus.MustRegister(ProcessingDuration)
	metricsRegistry["processing_duration_seconds"] = ProcessingDuration

	prometheus.MustRegister(APIRequestDuration)
	metricsRegistry["api_request_duration_seconds"] = APIRequestDuration

	prometheus.MustRegister(ElasticsearchQueryDuration)
	metricsRegistry["elasticsearch_query_duration_seconds"] = ElasticsearchQueryDuration
}

// Helper functions for easy metric operations

// IncrementCounter increments a counter metric with labels
func IncrementCounter(name string, labels map[string]string) {
	registryMu.RLock()
	metric, exists := metricsRegistry[name]
	registryMu.RUnlock()
	
	if !exists {
		return
	}
	
	if counterVec, ok := metric.(*prometheus.CounterVec); ok {
		counterVec.With(labels).Inc()
	}
}

// SetGauge sets a gauge metric value with labels
func SetGauge(name string, value float64, labels map[string]string) {
	registryMu.RLock()
	metric, exists := metricsRegistry[name]
	registryMu.RUnlock()
	
	if !exists {
		return
	}
	
	if gaugeVec, ok := metric.(*prometheus.GaugeVec); ok {
		gaugeVec.With(labels).Set(value)
	}
}

// ObserveHistogram observes a histogram metric with labels
func ObserveHistogram(name string, value float64, labels map[string]string) {
	registryMu.RLock()
	metric, exists := metricsRegistry[name]
	registryMu.RUnlock()
	
	if !exists {
		return
	}
	
	if histogramVec, ok := metric.(*prometheus.HistogramVec); ok {
		histogramVec.With(labels).Observe(value)
	}
}

// GetMetric retrieves a metric by name for external use
func GetMetric(name string) prometheus.Collector {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return metricsRegistry[name]
}