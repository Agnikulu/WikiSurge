package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/segmentio/kafka-go"
)

// ResourceMonitor continuously checks the health and resource usage of all
// infrastructure dependencies and publishes Prometheus metrics + triggers
// auto-scaling actions when thresholds are breached.
type ResourceMonitor struct {
	redis      *redis.Client
	esClient   *elasticsearch.Client
	kafkaCfg   *config.Kafka
	config     *config.Config
	logger     zerolog.Logger
	metrics    *resourceMetrics

	mu         sync.RWMutex
	thresholds Thresholds
	status     ResourceStatus
	alerts     []ResourceAlert

	// Callbacks for auto-scaling actions.
	onRedisHighMem    func()
	onESDiskHigh      func()
	onKafkaLagHigh    func()
	onKafkaLagRecover func()

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Thresholds defines when auto-scaling actions should trigger.
type Thresholds struct {
	RedisMemoryPercent float64 // default 80
	ESDiskPercent      float64 // default 80
	KafkaLagMessages   int64   // default 1000
}

// ResourceStatus holds current resource readings.
type ResourceStatus struct {
	RedisMemoryPercent float64
	RedisMemoryUsedMB  float64
	RedisMemoryMaxMB   float64
	ESDiskPercent      float64
	ESDiskUsedGB       float64
	ESDiskTotalGB      float64
	KafkaLag           int64
	LastCheck          time.Time
}

// ResourceAlert represents a triggered alert.
type ResourceAlert struct {
	Resource  string    `json:"resource"`
	Message   string    `json:"message"`
	Value     float64   `json:"value"`
	Threshold float64   `json:"threshold"`
	Timestamp time.Time `json:"timestamp"`
}

type resourceMetrics struct {
	redisMemory  prometheus.Gauge
	esDisk       prometheus.Gauge
	kafkaLag     prometheus.Gauge
	alertsTotal  *prometheus.CounterVec
}

// NewResourceMonitor creates a resource monitor. esClient may be nil if ES is
// not in use.
func NewResourceMonitor(
	redisClient *redis.Client,
	esClient *elasticsearch.Client,
	cfg *config.Config,
	logger zerolog.Logger,
) *ResourceMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	rm := &ResourceMonitor{
		redis:    redisClient,
		esClient: esClient,
		kafkaCfg: &cfg.Kafka,
		config:   cfg,
		logger:   logger.With().Str("component", "resource-monitor").Logger(),
		ctx:      ctx,
		cancel:   cancel,
		thresholds: Thresholds{
			RedisMemoryPercent: 80,
			ESDiskPercent:      80,
			KafkaLagMessages:   1000,
		},
	}

	rm.metrics = &resourceMetrics{
		redisMemory: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "redis_memory_usage_percent",
			Help: "Current Redis memory usage as a percentage of maxmemory",
		}),
		esDisk: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "elasticsearch_disk_usage_percent",
			Help: "Current Elasticsearch disk usage percentage",
		}),
		kafkaLag: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kafka_consumer_lag_seconds",
			Help: "Current Kafka consumer lag in estimated seconds",
		}),
		alertsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "resource_monitor_alerts_total",
			Help: "Total resource monitor alerts fired",
		}, []string{"resource"}),
	}
	prometheus.Register(rm.metrics.redisMemory)
	prometheus.Register(rm.metrics.esDisk)
	prometheus.Register(rm.metrics.kafkaLag)
	prometheus.Register(rm.metrics.alertsTotal)

	return rm
}

// SetThresholds overrides the default thresholds.
func (rm *ResourceMonitor) SetThresholds(t Thresholds) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.thresholds = t
}

// OnRedisHighMemory registers a callback when Redis memory exceeds threshold.
func (rm *ResourceMonitor) OnRedisHighMemory(fn func()) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onRedisHighMem = fn
}

// OnESDiskHigh registers a callback when ES disk exceeds threshold.
func (rm *ResourceMonitor) OnESDiskHigh(fn func()) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onESDiskHigh = fn
}

// OnKafkaLagHigh registers a callback when Kafka lag exceeds threshold.
func (rm *ResourceMonitor) OnKafkaLagHigh(fn func()) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onKafkaLagHigh = fn
}

// OnKafkaLagRecover registers a callback when Kafka lag drops below threshold.
func (rm *ResourceMonitor) OnKafkaLagRecover(fn func()) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.onKafkaLagRecover = fn
}

// Start begins the monitoring loops.
func (rm *ResourceMonitor) Start() {
	rm.logger.Info().Msg("Starting resource monitor")

	rm.wg.Add(3)
	go rm.monitorRedis()
	go rm.monitorES()
	go rm.monitorKafka()
}

// Stop gracefully shuts down the monitor.
func (rm *ResourceMonitor) Stop() {
	rm.logger.Info().Msg("Stopping resource monitor")
	rm.cancel()
	rm.wg.Wait()
}

// Status returns a snapshot of the current resource readings.
func (rm *ResourceMonitor) Status() ResourceStatus {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.status
}

// RecentAlerts returns alerts recorded during the current session.
func (rm *ResourceMonitor) RecentAlerts() []ResourceAlert {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	out := make([]ResourceAlert, len(rm.alerts))
	copy(out, rm.alerts)
	return out
}

// -----------------------------------------------------------------------
// Redis monitoring (every 30s)
// -----------------------------------------------------------------------

func (rm *ResourceMonitor) monitorRedis() {
	defer rm.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Run once immediately.
	rm.checkRedis()

	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			rm.checkRedis()
		}
	}
}

func (rm *ResourceMonitor) checkRedis() {
	ctx, cancel := context.WithTimeout(rm.ctx, 5*time.Second)
	defer cancel()

	info, err := rm.redis.Info(ctx, "memory").Result()
	if err != nil {
		rm.logger.Error().Err(err).Msg("Failed to get Redis memory info")
		return
	}

	usedMem := parseRedisInfoInt(info, "used_memory")
	maxMem := parseRedisInfoInt(info, "maxmemory")

	var pct float64
	if maxMem > 0 {
		pct = float64(usedMem) / float64(maxMem) * 100
	}

	rm.mu.Lock()
	rm.status.RedisMemoryPercent = pct
	rm.status.RedisMemoryUsedMB = float64(usedMem) / 1024 / 1024
	rm.status.RedisMemoryMaxMB = float64(maxMem) / 1024 / 1024
	rm.status.LastCheck = time.Now()
	thresholds := rm.thresholds
	rm.mu.Unlock()

	rm.metrics.redisMemory.Set(pct)

	if pct > thresholds.RedisMemoryPercent {
		rm.fireAlert("redis", fmt.Sprintf("Redis memory at %.1f%% (limit %.1f%%)", pct, thresholds.RedisMemoryPercent), pct, thresholds.RedisMemoryPercent)
		rm.mu.RLock()
		cb := rm.onRedisHighMem
		rm.mu.RUnlock()
		if cb != nil {
			cb()
		}
	}
}

// -----------------------------------------------------------------------
// Elasticsearch monitoring (every 60s)
// -----------------------------------------------------------------------

func (rm *ResourceMonitor) monitorES() {
	defer rm.wg.Done()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	rm.checkES()

	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			rm.checkES()
		}
	}
}

func (rm *ResourceMonitor) checkES() {
	if rm.esClient == nil {
		return
	}

	ctx, cancel := context.WithTimeout(rm.ctx, 10*time.Second)
	defer cancel()

	res, err := rm.esClient.Cluster.Stats(
		rm.esClient.Cluster.Stats.WithContext(ctx),
	)
	if err != nil {
		rm.logger.Error().Err(err).Msg("Failed to get ES cluster stats")
		return
	}
	defer res.Body.Close()

	if res.IsError() {
		rm.logger.Error().Str("status", res.Status()).Msg("ES cluster stats returned error")
		return
	}

	// Parse minimal fields from the response.
	var body struct {
		Nodes struct {
			FS struct {
				TotalInBytes     int64 `json:"total_in_bytes"`
				AvailableInBytes int64 `json:"available_in_bytes"`
			} `json:"fs"`
		} `json:"nodes"`
	}

	if err := decodeJSON(res.Body, &body); err != nil {
		rm.logger.Error().Err(err).Msg("Failed to parse ES cluster stats")
		return
	}

	total := body.Nodes.FS.TotalInBytes
	avail := body.Nodes.FS.AvailableInBytes
	var pct float64
	if total > 0 {
		used := total - avail
		pct = float64(used) / float64(total) * 100
	}

	rm.mu.Lock()
	rm.status.ESDiskPercent = pct
	rm.status.ESDiskUsedGB = float64(total-avail) / 1024 / 1024 / 1024
	rm.status.ESDiskTotalGB = float64(total) / 1024 / 1024 / 1024
	rm.status.LastCheck = time.Now()
	thresholds := rm.thresholds
	rm.mu.Unlock()

	rm.metrics.esDisk.Set(pct)

	if pct > thresholds.ESDiskPercent {
		rm.fireAlert("elasticsearch", fmt.Sprintf("ES disk at %.1f%% (limit %.1f%%)", pct, thresholds.ESDiskPercent), pct, thresholds.ESDiskPercent)
		rm.mu.RLock()
		cb := rm.onESDiskHigh
		rm.mu.RUnlock()
		if cb != nil {
			cb()
		}
	}
}

// -----------------------------------------------------------------------
// Kafka monitoring (every 15s)
// -----------------------------------------------------------------------

func (rm *ResourceMonitor) monitorKafka() {
	defer rm.wg.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	rm.checkKafka()
	wasHigh := false

	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			lag := rm.checkKafka()
			rm.mu.RLock()
			threshold := rm.thresholds.KafkaLagMessages
			rm.mu.RUnlock()

			isHigh := lag > threshold
			if isHigh && !wasHigh {
				rm.fireAlert("kafka", fmt.Sprintf("Kafka lag at %d (limit %d)", lag, threshold), float64(lag), float64(threshold))
				rm.mu.RLock()
				cb := rm.onKafkaLagHigh
				rm.mu.RUnlock()
				if cb != nil {
					cb()
				}
			} else if !isHigh && wasHigh {
				rm.logger.Info().Int64("lag", lag).Msg("Kafka lag recovered")
				rm.mu.RLock()
				cb := rm.onKafkaLagRecover
				rm.mu.RUnlock()
				if cb != nil {
					cb()
				}
			}
			wasHigh = isHigh
		}
	}
}

func (rm *ResourceMonitor) checkKafka() int64 {
	if len(rm.kafkaCfg.Brokers) == 0 {
		return 0
	}

	conn, err := kafka.Dial("tcp", rm.kafkaCfg.Brokers[0])
	if err != nil {
		rm.logger.Error().Err(err).Msg("Failed to connect to Kafka broker")
		return 0
	}
	defer conn.Close()

	partitions, err := conn.ReadPartitions("wikipedia.edits")
	if err != nil {
		rm.logger.Error().Err(err).Msg("Failed to read Kafka partitions")
		return 0
	}

	var totalLag int64
	for _, p := range partitions {
		pConn, err := kafka.DialLeader(rm.ctx, "tcp", rm.kafkaCfg.Brokers[0], p.Topic, p.ID)
		if err != nil {
			continue
		}
		first, last, err := pConn.ReadOffsets()
		pConn.Close()
		if err != nil {
			continue
		}
		totalLag += last - first
	}

	rm.mu.Lock()
	rm.status.KafkaLag = totalLag
	rm.status.LastCheck = time.Now()
	rm.mu.Unlock()

	rm.metrics.kafkaLag.Set(float64(totalLag))

	return totalLag
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

func (rm *ResourceMonitor) fireAlert(resource, msg string, value, threshold float64) {
	alert := ResourceAlert{
		Resource:  resource,
		Message:   msg,
		Value:     value,
		Threshold: threshold,
		Timestamp: time.Now(),
	}

	rm.mu.Lock()
	rm.alerts = append(rm.alerts, alert)
	// Keep last 100 alerts.
	if len(rm.alerts) > 100 {
		rm.alerts = rm.alerts[len(rm.alerts)-100:]
	}
	rm.mu.Unlock()

	rm.metrics.alertsTotal.WithLabelValues(resource).Inc()
	rm.logger.Warn().
		Str("resource", resource).
		Float64("value", value).
		Float64("threshold", threshold).
		Msg(msg)
}

// parseRedisInfoInt extracts an integer field from Redis INFO output.
func parseRedisInfoInt(info, key string) int64 {
	for _, line := range strings.Split(info, "\r\n") {
		if strings.HasPrefix(line, key+":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				v, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
				return v
			}
		}
	}
	return 0
}

// decodeJSON decodes from an io.Reader.
func decodeJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}
