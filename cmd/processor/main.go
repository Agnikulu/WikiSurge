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

	// Initialize SpikeDetector
	spikeDetector := processor.NewSpikeDetector(hotPageTracker, redisClient, cfg, logger)
	logger.Info().Msg("Initialized SpikeDetector")

	// Initialize Kafka consumer
	consumerCfg := kafka.ConsumerConfig{
		Brokers:        cfg.Kafka.Brokers,
		Topic:          "wikipedia.edits",
		GroupID:        "spike-detector",
		StartOffset:    kafkago.FirstOffset, // Start from earliest on first run
		MinBytes:       1024,                // 1KB
		MaxBytes:       10 * 1024 * 1024,    // 10MB
		CommitInterval: time.Second,
		MaxWait:        500 * time.Millisecond,
	}

	consumer, err := kafka.NewConsumer(cfg, consumerCfg, spikeDetector, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create Kafka consumer")
	}

	// Start metrics server
	metricsPort := 2112 // Default metrics port for processor
	if cfg.Ingestor.MetricsPort != 0 {
		metricsPort = cfg.Ingestor.MetricsPort
	}
	
	metricsServer := startMetricsServer(metricsPort, logger)
	logger.Info().Int("port", metricsPort).Msg("Metrics server started")

	// Start consumer
	if err := consumer.Start(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start Kafka consumer")
	}
	logger.Info().Msg("Kafka consumer started")

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Info().Str("signal", sig.String()).Msg("Received shutdown signal")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop consumer
	logger.Info().Msg("Stopping Kafka consumer...")
	if err := consumer.Stop(); err != nil {
		logger.Error().Err(err).Msg("Error stopping Kafka consumer")
	} else {
		logger.Info().Msg("Kafka consumer stopped")
	}

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