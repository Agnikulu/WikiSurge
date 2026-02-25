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
		configPath = flag.String("config", "", "Path to configuration file")
		verbose    = flag.Bool("verbose", false, "Enable debug logging")
	)
	flag.Parse()

	// Determine config path: flag > env var > default
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = os.Getenv("CONFIG_PATH")
	}
	if cfgPath == "" {
		cfgPath = "configs/config.dev.yaml"
	}

	// Load configuration
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		log.Fatal().Err(err).Str("path", cfgPath).Msg("Failed to load configuration")
	}

	// Initialize logger
	logger := setupLogger(cfg, *verbose)
	logger.Info().
		Str("config_path", cfgPath).
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

	// Set up signal handling early so we can catch SIGTERM during connect retries
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for Wikipedia SSE to become reachable.  The endpoint may be
	// temporarily down (503) during Wikimedia maintenance windows, so we
	// retry with exponential backoff instead of crashing the process.
	{
		delay := cfg.Ingestor.ReconnectDelay
		for attempt := 1; ; attempt++ {
			if err := client.Connect(); err == nil {
				break
			} else {
				logger.Warn().
					Err(err).
					Int("attempt", attempt).
					Dur("retry_in", delay).
					Msg("Wikipedia SSE not reachable, retrying")

				select {
				case <-time.After(delay):
				case sig := <-sigChan:
					logger.Info().Str("signal", sig.String()).Msg("Shutdown during connect retry")
					os.Exit(0)
				}

				delay *= 2
				if delay > cfg.Ingestor.MaxReconnectDelay {
					delay = cfg.Ingestor.MaxReconnectDelay
				}
			}
		}
	}

	// Start the client
	if err := client.Start(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to start SSE client")
	}

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