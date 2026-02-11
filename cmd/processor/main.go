package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/api"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/kafka"
	"github.com/Agnikulu/WikiSurge/internal/processor"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	kafkago "github.com/segmentio/kafka-go"
)

// componentHealth tracks health status of each component
type componentHealth struct {
	name           string
	healthy        atomic.Bool
	failureCount   atomic.Int64
	lastError      atomic.Value // stores string
	lastCheckTime  atomic.Value // stores time.Time
	disabled       atomic.Bool
}

// processorOrchestrator manages all consumers and shared components
type processorOrchestrator struct {
	cfg              *config.Config
	logger           zerolog.Logger
	wg               sync.WaitGroup

	// Shared infrastructure
	redisClient      *redis.Client
	esClient         *storage.ElasticsearchClient
	hotPageTracker   *storage.HotPageTracker
	trendingScorer   *storage.TrendingScorer

	// Processors
	spikeDetector      *processor.SpikeDetector
	editWarDetector    *processor.EditWarDetector
	trendingAggregator *processor.TrendingAggregator
	selectiveIndexer   *processor.SelectiveIndexer
	wsForwarder        *processor.WebSocketForwarder

	// WebSocket hub
	wsHub              *api.WebSocketHub

	// Consumers
	spikeConsumer    *kafka.Consumer
	trendingConsumer *kafka.Consumer
	editWarConsumer  *kafka.Consumer
	indexerConsumer  *kafka.Consumer
	wsConsumer       *kafka.Consumer

	// Health monitoring
	components       []*componentHealth
	healthCheckStop  chan struct{}

	// Metrics server
	metricsServer    *http.Server

	// Health metrics
	componentFailures *prometheus.CounterVec
	componentHealthy  *prometheus.GaugeVec
}

func main() {
	// Parse command line flags
	var configPath string
	flag.StringVar(&configPath, "config", "configs/config.dev.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Initialize logger
	logger := initLogger(cfg)
	logger.Info().Str("config", configPath).Msg("Starting WikiSurge Processor")

	// Create orchestrator
	orch := &processorOrchestrator{
		cfg:             cfg,
		logger:          logger,
		healthCheckStop: make(chan struct{}),
	}

	// Initialize health metrics (safe to register once)
	orch.componentFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "processor_component_failures_total",
			Help: "Total health check failures per component",
		},
		[]string{"component"},
	)
	orch.componentHealthy = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "processor_component_healthy",
			Help: "Whether a component is healthy (1) or not (0)",
		},
		[]string{"component"},
	)
	prometheus.MustRegister(orch.componentFailures, orch.componentHealthy)

	// Step 1: Initialize shared infrastructure
	if err := orch.initInfrastructure(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize infrastructure")
	}

	// Step 2: Initialize WebSocket hub
	orch.wsHub = api.NewWebSocketHub(logger)
	go orch.wsHub.Run()
	logger.Info().Msg("WebSocket hub started")

	// Step 3: Initialize all processors
	orch.initProcessors()

	// Step 4: Create all Kafka consumers
	if err := orch.createConsumers(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to create consumers")
	}

	// Step 5: Start metrics server (use port 2113 to avoid conflict with ingestor on 2112)
	metricsPort := 2113
	if cfg.Ingestor.MetricsPort != 0 {
		metricsPort = cfg.Ingestor.MetricsPort + 1
	}
	orch.metricsServer = orch.startMetricsServer(metricsPort)
	logger.Info().Int("port", metricsPort).Msg("Metrics server started")

	// Step 6: Start all consumers in parallel goroutines
	if err := orch.startAllConsumers(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start consumers")
	}

	// Step 7: Start health monitoring
	orch.startHealthMonitoring()

	logger.Info().Msg("All consumers started successfully — processor is running")

	// Step 8: Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	logger.Info().Str("signal", sig.String()).Msg("Received shutdown signal")

	// Step 9: Graceful shutdown
	orch.gracefulShutdown()

	logger.Info().Msg("WikiSurge Processor shutdown complete")
	os.Exit(0)
}

// initInfrastructure initializes all shared components: Redis, ES, HotPageTracker, TrendingScorer
func (o *processorOrchestrator) initInfrastructure() error {
	// Initialize Redis client (shared)
	var err error
	o.redisClient, err = initRedis(o.cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize Redis: %w", err)
	}

	// Test Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := o.redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	o.logger.Info().Msg("Connected to Redis")
	o.registerComponent("redis")

	// Initialize HotPageTracker (shared)
	o.hotPageTracker = storage.NewHotPageTracker(o.redisClient, &o.cfg.Redis.HotPages)
	o.logger.Info().Msg("Initialized HotPageTracker")

	// Initialize TrendingScorer (shared)
	o.trendingScorer = storage.NewTrendingScorer(o.redisClient, &o.cfg.Redis.Trending)
	o.trendingScorer.StartPruning()
	o.logger.Info().Msg("Initialized TrendingScorer with pruning")

	// Initialize Elasticsearch client (shared, if enabled)
	if o.cfg.Elasticsearch.Enabled {
		// Retry connecting to Elasticsearch to tolerate startup ordering
		maxAttempts := 30
		attempt := 0
		for {
			attempt++
			esClient, esErr := storage.NewElasticsearchClient(&o.cfg.Elasticsearch)
			if esErr == nil {
				o.esClient = esClient
				o.esClient.StartBulkProcessor()
				o.logger.Info().Msg("Connected to Elasticsearch")
				o.registerComponent("elasticsearch")
				break
			}
			o.logger.Warn().Err(esErr).Int("attempt", attempt).Msg("Elasticsearch not ready, retrying")
			if attempt >= maxAttempts {
				o.logger.Warn().Err(esErr).Msg("Failed to create Elasticsearch client after retries, indexing disabled")
				break
			}
			time.Sleep(2 * time.Second)
		}
	}

	return nil
}

// initProcessors initializes all processor components
func (o *processorOrchestrator) initProcessors() {
	// Spike Detector
	o.spikeDetector = processor.NewSpikeDetector(o.hotPageTracker, o.redisClient, o.cfg, o.logger)
	o.logger.Info().Msg("Initialized SpikeDetector")
	o.registerComponent("spike-detector")

	// Edit War Detector
	o.editWarDetector = processor.NewEditWarDetector(o.hotPageTracker, o.redisClient, o.cfg, o.logger)
	o.logger.Info().Msg("Initialized EditWarDetector")
	o.registerComponent("edit-war-detector")

	// Trending Aggregator
	statsTracker := storage.NewStatsTracker(o.redisClient)
	o.trendingAggregator = processor.NewTrendingAggregator(o.trendingScorer, statsTracker, o.cfg, o.logger)
	o.logger.Info().Msg("Initialized TrendingAggregator with StatsTracker")
	o.registerComponent("trending-aggregator")

	// Selective Indexer (if ES is available)
	if o.esClient != nil {
		indexingStrategy := storage.NewIndexingStrategy(
			&o.cfg.Elasticsearch.SelectiveCriteria,
			o.redisClient,
			o.trendingScorer,
			o.hotPageTracker,
		)
		o.selectiveIndexer = processor.NewSelectiveIndexer(o.esClient, indexingStrategy, o.cfg, o.logger)
		o.selectiveIndexer.Start()
		o.logger.Info().Msg("Initialized SelectiveIndexer")
		o.registerComponent("selective-indexer")
	}

	// WebSocket Forwarder
	if o.wsHub != nil {
		o.wsForwarder = processor.NewWebSocketForwarder(o.wsHub, o.redisClient, o.logger)
		o.logger.Info().Msg("Initialized WebSocketForwarder")
		o.registerComponent("websocket-forwarder")
	}
}

// createConsumers creates all Kafka consumers with separate consumer groups
func (o *processorOrchestrator) createConsumers() error {
	baseConsumerCfg := func(groupID string) kafka.ConsumerConfig {
		return kafka.ConsumerConfig{
			Brokers:        o.cfg.Kafka.Brokers,
			Topic:          "wikipedia.edits",
			GroupID:        groupID,
			StartOffset:    kafkago.FirstOffset,
			MinBytes:       1024,
			MaxBytes:       10 * 1024 * 1024,
			CommitInterval: time.Second,
			MaxWait:        500 * time.Millisecond,
		}
	}

	var err error

	// Spike detection consumer
	o.spikeConsumer, err = kafka.NewConsumer(o.cfg, baseConsumerCfg("spike-detector"), o.spikeDetector, o.logger)
	if err != nil {
		return fmt.Errorf("failed to create spike detection consumer: %w", err)
	}

	// Trending aggregation consumer
	o.trendingConsumer, err = kafka.NewConsumer(o.cfg, baseConsumerCfg("trending-aggregator"), o.trendingAggregator, o.logger)
	if err != nil {
		return fmt.Errorf("failed to create trending aggregation consumer: %w", err)
	}

	// Edit war detection consumer
	o.editWarConsumer, err = kafka.NewConsumer(o.cfg, baseConsumerCfg("edit-war-detector"), o.editWarDetector, o.logger)
	if err != nil {
		return fmt.Errorf("failed to create edit war detection consumer: %w", err)
	}

	// Elasticsearch indexer consumer (if ES enabled)
	if o.selectiveIndexer != nil {
		o.indexerConsumer, err = kafka.NewConsumer(o.cfg, baseConsumerCfg("elasticsearch-indexer"), o.selectiveIndexer, o.logger)
		if err != nil {
			return fmt.Errorf("failed to create elasticsearch indexer consumer: %w", err)
		}
	}

	// WebSocket forwarder consumer
	if o.wsForwarder != nil {
		o.wsConsumer, err = kafka.NewConsumer(o.cfg, baseConsumerCfg("websocket-forwarder"), o.wsForwarder, o.logger)
		if err != nil {
			return fmt.Errorf("failed to create websocket forwarder consumer: %w", err)
		}
	}

	return nil
}

// startAllConsumers starts all consumers in parallel goroutines
func (o *processorOrchestrator) startAllConsumers() error {
	type consumerEntry struct {
		name     string
		consumer *kafka.Consumer
	}

	consumers := []consumerEntry{
		{"spike-detector", o.spikeConsumer},
		{"trending-aggregator", o.trendingConsumer},
		{"edit-war-detector", o.editWarConsumer},
	}

	if o.indexerConsumer != nil {
		consumers = append(consumers, consumerEntry{"elasticsearch-indexer", o.indexerConsumer})
	}

	if o.wsConsumer != nil {
		consumers = append(consumers, consumerEntry{"websocket-forwarder", o.wsConsumer})
	}

	for _, c := range consumers {
		if err := c.consumer.Start(); err != nil {
			return fmt.Errorf("failed to start %s consumer: %w", c.name, err)
		}
		o.logger.Info().Str("consumer", c.name).Msg("Kafka consumer started")
	}

	return nil
}

// registerComponent registers a component for health monitoring
func (o *processorOrchestrator) registerComponent(name string) {
	ch := &componentHealth{name: name}
	ch.healthy.Store(true)
	ch.lastCheckTime.Store(time.Now())
	ch.lastError.Store("")
	o.components = append(o.components, ch)
}

// startHealthMonitoring starts periodic health checks for all components
func (o *processorOrchestrator) startHealthMonitoring() {
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				o.performHealthChecks()
			case <-o.healthCheckStop:
				return
			}
		}
	}()
	o.logger.Info().Msg("Health monitoring started (30s interval)")
}

// performHealthChecks checks all components and updates health status
func (o *processorOrchestrator) performHealthChecks() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check Redis
	o.checkRedisHealth(ctx)

	// Check Elasticsearch
	if o.esClient != nil {
		o.checkESHealth()
	}

	// Check consumer lag
	o.checkConsumerLag()

	// Log overall status
	allHealthy := true
	for _, c := range o.components {
		healthy := c.healthy.Load()
		o.componentHealthy.WithLabelValues(c.name).Set(boolToFloat(healthy))
		if !healthy {
			allHealthy = false
			errStr, _ := c.lastError.Load().(string)
			o.logger.Warn().
				Str("component", c.name).
				Int64("failures", c.failureCount.Load()).
				Str("last_error", errStr).
				Bool("disabled", c.disabled.Load()).
				Msg("Component unhealthy")
		}
	}

	if allHealthy {
		o.logger.Debug().Msg("All components healthy")
	}
}

// checkRedisHealth checks if Redis is responding
func (o *processorOrchestrator) checkRedisHealth(ctx context.Context) {
	ch := o.findComponent("redis")
	if ch == nil {
		return
	}

	if err := o.redisClient.Ping(ctx).Err(); err != nil {
		ch.healthy.Store(false)
		failures := ch.failureCount.Add(1)
		ch.lastError.Store(err.Error())
		o.componentFailures.WithLabelValues("redis").Inc()

		// Graceful degradation: if Redis fails repeatedly, disable hot page tracking
		if failures >= 5 {
			o.logger.Error().
				Int64("failures", failures).
				Msg("Redis repeatedly failing — consider graceful degradation")
		}
	} else {
		ch.healthy.Store(true)
		ch.failureCount.Store(0)
		ch.lastError.Store("")
	}
	ch.lastCheckTime.Store(time.Now())
}

// checkESHealth checks if Elasticsearch is responding
func (o *processorOrchestrator) checkESHealth() {
	ch := o.findComponent("elasticsearch")
	if ch == nil {
		return
	}

	// We just check if the selective indexer buffer isn't continually growing
	if o.selectiveIndexer != nil {
		bufLen := o.selectiveIndexer.BufferLen()
		if bufLen > 800 { // 80% of default 1000 buffer
			ch.healthy.Store(false)
			ch.failureCount.Add(1)
			ch.lastError.Store(fmt.Sprintf("index buffer near full: %d/1000", bufLen))
			o.componentFailures.WithLabelValues("elasticsearch").Inc()
		} else {
			ch.healthy.Store(true)
			ch.failureCount.Store(0)
			ch.lastError.Store("")
		}
	}
	ch.lastCheckTime.Store(time.Now())
}

// checkConsumerLag checks Kafka consumer lag for all consumers
func (o *processorOrchestrator) checkConsumerLag() {
	type consumerEntry struct {
		name     string
		consumer *kafka.Consumer
	}

	consumers := []consumerEntry{
		{"spike-detector", o.spikeConsumer},
		{"trending-aggregator", o.trendingConsumer},
		{"edit-war-detector", o.editWarConsumer},
	}
	if o.indexerConsumer != nil {
		consumers = append(consumers, consumerEntry{"selective-indexer", o.indexerConsumer})
	}

	for _, c := range consumers {
		ch := o.findComponent(c.name)
		if ch == nil {
			continue
		}

		stats := c.consumer.GetStats()
		lag := stats.Lag

		if lag > 1000 {
			ch.healthy.Store(false)
			ch.failureCount.Add(1)
			ch.lastError.Store(fmt.Sprintf("high consumer lag: %d", lag))
			o.componentFailures.WithLabelValues(c.name).Inc()
			o.logger.Warn().Str("consumer", c.name).Int64("lag", lag).Msg("High consumer lag detected")
		} else {
			ch.healthy.Store(true)
			ch.failureCount.Store(0)
		}
		ch.lastCheckTime.Store(time.Now())
	}
}

// findComponent finds a component by name
func (o *processorOrchestrator) findComponent(name string) *componentHealth {
	for _, c := range o.components {
		if c.name == name {
			return c
		}
	}
	return nil
}

// gracefulShutdown performs ordered shutdown of all components
func (o *processorOrchestrator) gracefulShutdown() {
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	o.logger.Info().Msg("Starting graceful shutdown...")

	// 1. Stop health monitoring
	close(o.healthCheckStop)
	o.logger.Info().Msg("Health monitoring stopped")

	// 2. Stop all consumers (stop accepting new messages)
	o.logger.Info().Msg("Stopping Kafka consumers...")
	var consumerWg sync.WaitGroup

	stopConsumer := func(name string, c *kafka.Consumer) {
		defer consumerWg.Done()
		if err := c.Stop(); err != nil {
			o.logger.Error().Err(err).Str("consumer", name).Msg("Error stopping consumer")
		} else {
			o.logger.Info().Str("consumer", name).Msg("Consumer stopped")
		}
	}

	consumerWg.Add(3)
	go stopConsumer("spike-detector", o.spikeConsumer)
	go stopConsumer("trending-aggregator", o.trendingConsumer)
	go stopConsumer("edit-war-detector", o.editWarConsumer)

	if o.indexerConsumer != nil {
		consumerWg.Add(1)
		go stopConsumer("elasticsearch-indexer", o.indexerConsumer)
	}

	if o.wsConsumer != nil {
		consumerWg.Add(1)
		go stopConsumer("websocket-forwarder", o.wsConsumer)
	}

	consumerWg.Wait()
	o.logger.Info().Msg("All Kafka consumers stopped")

	// 3. Flush all buffers
	if o.selectiveIndexer != nil {
		o.logger.Info().Msg("Flushing selective indexer buffer...")
		o.selectiveIndexer.Stop()
		o.logger.Info().Msg("Selective indexer stopped")
	}

	if o.esClient != nil {
		o.logger.Info().Msg("Flushing Elasticsearch bulk buffer...")
		o.esClient.Stop()
		o.logger.Info().Msg("Elasticsearch client stopped")
	}

	// 4. Stop trending scorer (flush pruning)
	o.logger.Info().Msg("Stopping trending scorer...")
	o.trendingScorer.Stop()
	o.logger.Info().Msg("Trending scorer stopped")

	// 5. Stop metrics server
	o.logger.Info().Msg("Stopping metrics server...")
	if err := o.metricsServer.Shutdown(shutdownCtx); err != nil {
		o.logger.Error().Err(err).Msg("Error stopping metrics server")
	} else {
		o.logger.Info().Msg("Metrics server stopped")
	}

	// 6. Close connections
	if o.wsHub != nil {
		o.logger.Info().Msg("Stopping WebSocket hub...")
		o.wsHub.Stop()
		o.logger.Info().Msg("WebSocket hub stopped")
	}

	if err := o.redisClient.Close(); err != nil {
		o.logger.Error().Err(err).Msg("Error closing Redis connection")
	} else {
		o.logger.Info().Msg("Redis connection closed")
	}

	// 7. Wait for all goroutines to finish
	o.wg.Wait()
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// initLogger initializes the logger based on configuration
func initLogger(cfg *config.Config) zerolog.Logger {
	// Set log level
	level := zerolog.InfoLevel
	switch cfg.Logging.Level {
	case "debug":
		level = zerolog.DebugLevel
	case "warn":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	}
	zerolog.SetGlobalLevel(level)

	// Configure output format
	if cfg.Logging.Format == "pretty" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	return log.Logger.With().
		Str("service", "wikisurge-processor").
		Str("version", "1.0.0").
		Logger()
}

// initRedis initializes Redis client
func initRedis(cfg *config.Config) (*redis.Client, error) {
	opt, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	client := redis.NewClient(opt)
	return client, nil
}

// startMetricsServer starts the Prometheus metrics HTTP server with detailed health endpoint
func (o *processorOrchestrator) startMetricsServer(port int) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	// Basic health endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		allHealthy := true
		componentStatus := make(map[string]interface{})
		for _, c := range o.components {
			healthy := c.healthy.Load()
			if !healthy {
				allHealthy = false
			}
			errStr, _ := c.lastError.Load().(string)
			lastCheck, _ := c.lastCheckTime.Load().(time.Time)
			componentStatus[c.name] = map[string]interface{}{
				"healthy":      healthy,
				"failures":     c.failureCount.Load(),
				"last_error":   errStr,
				"last_check":   lastCheck.Format(time.RFC3339),
				"disabled":     c.disabled.Load(),
			}
		}

		status := "healthy"
		statusCode := http.StatusOK
		if !allHealthy {
			status = "degraded"
			statusCode = http.StatusServiceUnavailable
		}

		resp := map[string]interface{}{
			"status":     status,
			"service":    "wikisurge-processor",
			"components": componentStatus,
			"timestamp":  time.Now().Format(time.RFC3339),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(resp)
	})

	// Readiness probe
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		// Check if critical components are ready
		redisOK := true
		if ch := o.findComponent("redis"); ch != nil {
			redisOK = ch.healthy.Load()
		}

		if redisOK {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ready":true}`))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"ready":false,"reason":"redis_unavailable"}`))
		}
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			o.logger.Fatal().Err(err).Msg("Metrics server failed")
		}
	}()

	return server
}