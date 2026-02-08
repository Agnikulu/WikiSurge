package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/kafka"
	"github.com/Agnikulu/WikiSurge/internal/processor"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	kafkago "github.com/segmentio/kafka-go"
)

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
	logger.Info().Str("config", configPath).Msg("Starting WikiSurge Processor (Spike Detection)")

	// Initialize Redis client
	redisClient, err := initRedis(cfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize Redis client")
	}
	defer redisClient.Close()

	// Test Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to connect to Redis")
	}
	logger.Info().Msg("Connected to Redis")

	// Initialize HotPageTracker
	hotPageTracker := storage.NewHotPageTracker(redisClient, &cfg.Redis.HotPages)
	logger.Info().Msg("Initialized HotPageTracker")

	// Initialize TrendingScorer
	trendingScorer := storage.NewTrendingScorer(redisClient, &cfg.Redis.Trending)
	logger.Info().Msg("Initialized TrendingScorer")
	
	// Start trending pruning
	trendingScorer.StartPruning()
	logger.Info().Msg("Started trending pruning scheduler")

	// Initialize SpikeDetector
	spikeDetector := processor.NewSpikeDetector(hotPageTracker, redisClient, cfg, logger)
	logger.Info().Msg("Initialized SpikeDetector")

	// Initialize EditWarDetector
	editWarDetector := processor.NewEditWarDetector(hotPageTracker, redisClient, cfg, logger)
	logger.Info().Msg("Initialized EditWarDetector")
	
	// Initialize TrendingAggregator
	trendingAggregator := processor.NewTrendingAggregator(trendingScorer, cfg, logger)
	logger.Info().Msg("Initialized TrendingAggregator")

	// Initialize IndexingStrategy
	indexingStrategy := storage.NewIndexingStrategy(
		&cfg.Elasticsearch.SelectiveCriteria,
		redisClient,
		trendingScorer,
		hotPageTracker,
	)
	logger.Info().Msg("Initialized IndexingStrategy")

	// Initialize Elasticsearch client (if enabled)
	var esClient *storage.ElasticsearchClient
	var selectiveIndexer *processor.SelectiveIndexer
	if cfg.Elasticsearch.Enabled {
		var esErr error
		esClient, esErr = storage.NewElasticsearchClient(&cfg.Elasticsearch)
		if esErr != nil {
			logger.Warn().Err(esErr).Msg("Failed to create Elasticsearch client, indexing disabled")
		} else {
			logger.Info().Msg("Connected to Elasticsearch")
			esClient.StartBulkProcessor()

			// Initialize SelectiveIndexer
			selectiveIndexer = processor.NewSelectiveIndexer(esClient, indexingStrategy, cfg, logger)
			selectiveIndexer.Start()
			logger.Info().Msg("Initialized SelectiveIndexer")
		}
	}

	// Initialize Kafka consumer for spike detection
	spikeConsumerCfg := kafka.ConsumerConfig{
		Brokers:        cfg.Kafka.Brokers,
		Topic:          "wikipedia.edits",
		GroupID:        "spike-detector",
		StartOffset:    kafkago.FirstOffset, // Start from earliest on first run
		MinBytes:       1024,                // 1KB
		MaxBytes:       10 * 1024 * 1024,    // 10MB
		CommitInterval: time.Second,
		MaxWait:        500 * time.Millisecond,
	}

	spikeConsumer, err := kafka.NewConsumer(cfg, spikeConsumerCfg, spikeDetector, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create spike detection Kafka consumer")
	}

	// Initialize Kafka consumer for trending aggregation
	trendingConsumerCfg := kafka.ConsumerConfig{
		Brokers:        cfg.Kafka.Brokers,
		Topic:          "wikipedia.edits",
		GroupID:        "trending-aggregator", // Different consumer group
		StartOffset:    kafkago.FirstOffset, 
		MinBytes:       1024,                
		MaxBytes:       10 * 1024 * 1024,    
		CommitInterval: time.Second,
		MaxWait:        500 * time.Millisecond,
	}

	trendingConsumer, err := kafka.NewConsumer(cfg, trendingConsumerCfg, trendingAggregator, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create trending aggregation Kafka consumer")
	}

	// Initialize Kafka consumer for edit war detection
	editWarConsumerCfg := kafka.ConsumerConfig{
		Brokers:        cfg.Kafka.Brokers,
		Topic:          "wikipedia.edits",
		GroupID:        "edit-war-detector",
		StartOffset:    kafkago.FirstOffset,
		MinBytes:       1024,
		MaxBytes:       10 * 1024 * 1024,
		CommitInterval: time.Second,
		MaxWait:        500 * time.Millisecond,
	}

	editWarConsumer, err := kafka.NewConsumer(cfg, editWarConsumerCfg, editWarDetector, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create edit war detection Kafka consumer")
	}

	// Initialize Kafka consumer for elasticsearch indexing (if enabled)
	var indexerConsumer *kafka.Consumer
	if selectiveIndexer != nil {
		indexerConsumerCfg := kafka.ConsumerConfig{
			Brokers:        cfg.Kafka.Brokers,
			Topic:          "wikipedia.edits",
			GroupID:        "elasticsearch-indexer",
			StartOffset:    kafkago.FirstOffset,
			MinBytes:       1024,
			MaxBytes:       10 * 1024 * 1024,
			CommitInterval: time.Second,
			MaxWait:        500 * time.Millisecond,
		}

		var indexerErr error
		indexerConsumer, indexerErr = kafka.NewConsumer(cfg, indexerConsumerCfg, selectiveIndexer, logger)
		if indexerErr != nil {
			logger.Fatal().Err(indexerErr).Msg("Failed to create elasticsearch indexer Kafka consumer")
		}
	}

	// Start metrics server
	metricsPort := 2112 // Default metrics port for processor
	if cfg.Ingestor.MetricsPort != 0 {
		metricsPort = cfg.Ingestor.MetricsPort
	}
	
	metricsServer := startMetricsServer(metricsPort, logger)
	logger.Info().Int("port", metricsPort).Msg("Metrics server started")

	// Start consumers
	if err := spikeConsumer.Start(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start spike detection Kafka consumer")
	}
	logger.Info().Msg("Spike detection Kafka consumer started")
	
	if err := trendingConsumer.Start(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start trending aggregation Kafka consumer")
	}
	logger.Info().Msg("Trending aggregation Kafka consumer started")

	if err := editWarConsumer.Start(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start edit war detection Kafka consumer")
	}
	logger.Info().Msg("Edit war detection Kafka consumer started")

	// Start indexer consumer (if enabled)
	if indexerConsumer != nil {
		if err := indexerConsumer.Start(); err != nil {
			logger.Fatal().Err(err).Msg("Failed to start elasticsearch indexer Kafka consumer")
		}
		logger.Info().Msg("Elasticsearch indexer Kafka consumer started")
	}

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Info().Str("signal", sig.String()).Msg("Received shutdown signal")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop consumers
	logger.Info().Msg("Stopping Kafka consumers...")
	if err := spikeConsumer.Stop(); err != nil {
		logger.Error().Err(err).Msg("Error stopping spike detection Kafka consumer")
	} else {
		logger.Info().Msg("Spike detection Kafka consumer stopped")
	}
	
	if err := trendingConsumer.Stop(); err != nil {
		logger.Error().Err(err).Msg("Error stopping trending aggregation Kafka consumer")
	} else {
		logger.Info().Msg("Trending aggregation Kafka consumer stopped")
	}

	if err := editWarConsumer.Stop(); err != nil {
		logger.Error().Err(err).Msg("Error stopping edit war detection Kafka consumer")
	} else {
		logger.Info().Msg("Edit war detection Kafka consumer stopped")
	}

	// Stop indexer consumer
	if indexerConsumer != nil {
		if err := indexerConsumer.Stop(); err != nil {
			logger.Error().Err(err).Msg("Error stopping elasticsearch indexer Kafka consumer")
		} else {
			logger.Info().Msg("Elasticsearch indexer Kafka consumer stopped")
		}
	}

	// Stop selective indexer
	if selectiveIndexer != nil {
		logger.Info().Msg("Stopping selective indexer...")
		selectiveIndexer.Stop()
		logger.Info().Msg("Selective indexer stopped")
	}

	// Stop ES client
	if esClient != nil {
		logger.Info().Msg("Stopping Elasticsearch client...")
		esClient.Stop()
		logger.Info().Msg("Elasticsearch client stopped")
	}
	
	// Stop trending scorer
	logger.Info().Msg("Stopping trending scorer...")
	trendingScorer.Stop()
	logger.Info().Msg("Trending scorer stopped")

	// Stop metrics server
	logger.Info().Msg("Stopping metrics server...")
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("Error stopping metrics server")
	} else {
		logger.Info().Msg("Metrics server stopped")
	}

	// Close Redis connection
	if err := redisClient.Close(); err != nil {
		logger.Error().Err(err).Msg("Error closing Redis connection")
	} else {
		logger.Info().Msg("Redis connection closed")
	}

	logger.Info().Msg("WikiSurge Processor shutdown complete")
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

// startMetricsServer starts the Prometheus metrics HTTP server
func startMetricsServer(port int, logger zerolog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy","service":"wikisurge-processor"}`))
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("Metrics server failed")
		}
	}()

	return server
}