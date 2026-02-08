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

	ProduceAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "produce_attempts_total",
			Help: "Total attempts to produce messages to Kafka",
		},
		[]string{},
	)

	MessagesProducedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "messages_produced_total",
			Help: "Total messages successfully produced to Kafka",
		},
		[]string{},
	)

	MessagesDroppedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "messages_dropped_total",
			Help: "Total messages dropped due to buffer full or other reasons",
		},
		[]string{"reason"},
	)

	ProduceErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "produce_errors_total",
			Help: "Total Kafka production errors by type",
		},
		[]string{"type"},
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

	WebSocketMessagesBroadcast = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "websocket_messages_broadcast_total",
			Help: "Total messages broadcast to WebSocket clients",
		},
		[]string{"type"},
	)

	WebSocketMessagesDropped = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "websocket_messages_dropped_total",
			Help: "Messages dropped due to full buffers",
		},
		[]string{},
	)

	RateLimitHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rate_limit_hits_total",
			Help: "Rate limiter hits",
		},
		[]string{},
	)

	SSEReconnectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sse_reconnections_total",
			Help: "SSE client reconnection attempts",
		},
		[]string{},
	)

	// API-specific counters (Task 17.8)
	APIErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_errors_total",
			Help: "API errors by error code",
		},
		[]string{"error_code"},
	)

	APICacheHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_cache_hits_total",
			Help: "API response cache hits",
		},
		[]string{},
	)

	APICacheMissesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "api_cache_misses_total",
			Help: "API response cache misses",
		},
		[]string{},
	)

	WebSocketMessagesSentTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "websocket_messages_sent_total",
			Help: "WebSocket messages sent to clients",
		},
		[]string{},
	)

	WebSocketMessagesReceivedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "websocket_messages_received_total",
			Help: "WebSocket messages received from clients",
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

	ActivityCounterTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "activity_counter_total",
			Help: "Total activity counter increments",
		},
		[]string{},
	)

	HotPagesPromotedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hot_pages_promoted_total",
			Help: "Total pages promoted to hot tracking",
		},
		[]string{},
	)

	PromotionRejectedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "promotion_rejected_total",
			Help: "Total promotions rejected due to circuit breaker",
		},
		[]string{},
	)

	HotPagesExpiredTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hot_pages_expired_total",
			Help: "Total hot pages expired and cleaned up",
		},
		[]string{},
	)

	CleanupRunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cleanup_runs_total",
			Help: "Total cleanup operations completed",
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

	APIResponseSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "api_response_size_bytes",
			Help:    "API response size in bytes",
			Buckets: []float64{100, 500, 1000, 5000, 10000, 50000, 100000, 500000},
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

	prometheus.MustRegister(ProduceAttemptsTotal)
	metricsRegistry["produce_attempts_total"] = ProduceAttemptsTotal

	prometheus.MustRegister(MessagesProducedTotal)
	metricsRegistry["messages_produced_total"] = MessagesProducedTotal

	prometheus.MustRegister(MessagesDroppedTotal)
	metricsRegistry["messages_dropped_total"] = MessagesDroppedTotal

	prometheus.MustRegister(ProduceErrorsTotal)
	metricsRegistry["produce_errors_total"] = ProduceErrorsTotal

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

	prometheus.MustRegister(WebSocketMessagesBroadcast)
	metricsRegistry["websocket_messages_broadcast_total"] = WebSocketMessagesBroadcast

	prometheus.MustRegister(WebSocketMessagesDropped)
	metricsRegistry["websocket_messages_dropped_total"] = WebSocketMessagesDropped

	prometheus.MustRegister(RateLimitHitsTotal)
	metricsRegistry["rate_limit_hits_total"] = RateLimitHitsTotal

	prometheus.MustRegister(SSEReconnectionsTotal)
	metricsRegistry["sse_reconnections_total"] = SSEReconnectionsTotal

	prometheus.MustRegister(APIErrorsTotal)
	metricsRegistry["api_errors_total"] = APIErrorsTotal

	prometheus.MustRegister(APICacheHitsTotal)
	metricsRegistry["api_cache_hits_total"] = APICacheHitsTotal

	prometheus.MustRegister(APICacheMissesTotal)
	metricsRegistry["api_cache_misses_total"] = APICacheMissesTotal

	prometheus.MustRegister(WebSocketMessagesSentTotal)
	metricsRegistry["websocket_messages_sent_total"] = WebSocketMessagesSentTotal

	prometheus.MustRegister(WebSocketMessagesReceivedTotal)
	metricsRegistry["websocket_messages_received_total"] = WebSocketMessagesReceivedTotal

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

	prometheus.MustRegister(ActivityCounterTotal)
	metricsRegistry["activity_counter_total"] = ActivityCounterTotal

	prometheus.MustRegister(HotPagesPromotedTotal)
	metricsRegistry["hot_pages_promoted_total"] = HotPagesPromotedTotal

	prometheus.MustRegister(PromotionRejectedTotal)
	metricsRegistry["promotion_rejected_total"] = PromotionRejectedTotal

	prometheus.MustRegister(HotPagesExpiredTotal)
	metricsRegistry["hot_pages_expired_total"] = HotPagesExpiredTotal

	prometheus.MustRegister(CleanupRunsTotal)
	metricsRegistry["cleanup_runs_total"] = CleanupRunsTotal

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

	prometheus.MustRegister(APIResponseSizeBytes)
	metricsRegistry["api_response_size_bytes"] = APIResponseSizeBytes

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