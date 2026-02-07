package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/ingestor"
	"github.com/Agnikulu/WikiSurge/internal/kafka"
	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Parse command line flags
	var (
		configPath = flag.String("config", "configs/config.dev.yaml", "Path to configuration file")
		verbose    = flag.Bool("verbose", false, "Enable debug logging")
	)
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Str("path", *configPath).Msg("Failed to load configuration")
	}

	// Initialize logger
	logger := setupLogger(cfg, *verbose)
	logger.Info().
		Str("config_path", *configPath).
		Bool("verbose", *verbose).
		Msg("Starting Wikipedia ingestor")

	// Initialize metrics
	metrics.InitMetrics()
	logger.Info().Msg("Metrics initialized")

	// Start metrics server
	metricsServer := metrics.NewServer(cfg.Ingestor.MetricsPort)
	var wg sync.WaitGroup
	
	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info().
			Int("port", cfg.Ingestor.MetricsPort).
			Msg("Starting metrics server")
		
		if err := metricsServer.Start(); err != nil {
			logger.Error().Err(err).Msg("Metrics server failed")
		}
	}()

	// Create Kafka producer
	producer, err := kafka.NewProducer(cfg.Kafka.Brokers, "wikipedia.edits", cfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create Kafka producer")
	}

	// Start the Kafka producer
	if err := producer.Start(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start Kafka producer")
	}

	// Create Wikipedia SSE client
	client := ingestor.NewWikiStreamClient(cfg, logger, producer)

	// Test connection first
	if err := client.Connect(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to connect to Wikipedia SSE stream")
	}

	// Start the client
	if err := client.Start(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start SSE client")
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logger.Info().Msg("Wikipedia ingestor started successfully")
	logger.Info().
		Str("metrics_url", fmt.Sprintf("http://localhost:%d/metrics", cfg.Ingestor.MetricsPort)).
		Msg("Metrics available at endpoint")

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Info().
		Str("signal", sig.String()).
		Msg("Shutdown signal received")

	// Graceful shutdown
	logger.Info().Msg("Initiating graceful shutdown")
	
	// Stop the SSE client
	client.Stop()
	
	// Stop the metrics server with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := metricsServer.Stop(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("Error stopping metrics server")
	}
	
	// Wait for metrics server to finish
	wg.Wait()
	
	logger.Info().Msg("Shutdown completed successfully")
}

// setupLogger configures structured logging based on configuration
func setupLogger(cfg *config.Config, verbose bool) zerolog.Logger {
	// Set log level
	logLevel := zerolog.InfoLevel
	if verbose {
		logLevel = zerolog.DebugLevel
	} else {
		switch cfg.Logging.Level {
		case "debug":
			logLevel = zerolog.DebugLevel
		case "info":
			logLevel = zerolog.InfoLevel
		case "warn":
			logLevel = zerolog.WarnLevel
		case "error":
			logLevel = zerolog.ErrorLevel
		}
	}

	zerolog.SetGlobalLevel(logLevel)

	// Configure output format
	var logger zerolog.Logger
	if cfg.Logging.Format == "json" {
		logger = zerolog.New(os.Stdout).With().
			Timestamp().
			Str("component", "ingestor").
			Logger()
	} else {
		// Use console writer for human-readable logs
		output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006-01-02 15:04:05"}
		logger = zerolog.New(output).With().
			Timestamp().
			Str("component", "ingestor").
			Logger()
	}

	return logger
}