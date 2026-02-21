package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/api"
	"github.com/Agnikulu/WikiSurge/internal/auth"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/digest"
	"github.com/Agnikulu/WikiSurge/internal/email"
	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func main() {
	// ---- Load .env (if present) ----
	_ = godotenv.Load() // silently ignore if .env doesn't exist

	// ---- Flags ----
	configPath := flag.String("config", "", "Path to configuration file")
	portOverride := flag.Int("port", 0, "Override API port (default from config)")
	flag.Parse()

	// Determine config path: flag > env var > default
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = os.Getenv("CONFIG_PATH")
	}
	if cfgPath == "" {
		cfgPath = "configs/config.dev.yaml"
	}

	// ---- Configuration ----
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	if *portOverride > 0 {
		cfg.API.Port = *portOverride
	}

	// ---- Logger ----
	level, _ := zerolog.ParseLevel(cfg.Logging.Level)
	logger := zerolog.New(os.Stdout).With().Timestamp().Str("service", "wikisurge-api").Logger().Level(level)
	logger.Info().Str("config", cfgPath).Int("port", cfg.API.Port).Msg("Starting WikiSurge API Server")

	// ---- Metrics ----
	metrics.InitMetrics()
	apiMetricsPort := cfg.Ingestor.MetricsPort + 2 // avoid collision with ingestor (+0) and processor (+1)
	metricsServer := metrics.NewServer(apiMetricsPort)
	if err := metricsServer.Start(); err != nil {
		logger.Warn().Err(err).Int("port", apiMetricsPort).Msg("Metrics server failed to start (non-fatal)")
	} else {
		logger.Info().Int("port", apiMetricsPort).Msg("Metrics server started")
	}

	// ---- Redis ----
	redisAddr := strings.TrimPrefix(cfg.Redis.URL, "redis://")
	redisClient := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     50,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Warn().Err(err).Msg("Redis not reachable at startup (will retry on requests)")
	} else {
		logger.Info().Str("addr", redisAddr).Msg("Connected to Redis")
	}
	cancel()

	// ---- Elasticsearch (optional) ----
	var esClient *storage.ElasticsearchClient
	if cfg.Elasticsearch.Enabled {
		// Retry connecting to Elasticsearch for a short period to handle startup races
		maxAttempts := 30
		attempt := 0
		for {
			attempt++
			esClient, err = storage.NewElasticsearchClient(&cfg.Elasticsearch)
			if err == nil {
				logger.Info().Str("url", cfg.Elasticsearch.URL).Msg("Connected to Elasticsearch")
				break
			}
			logger.Warn().Err(err).Int("attempt", attempt).Msg("Elasticsearch not ready, retrying")
			if attempt >= maxAttempts {
				logger.Warn().Err(err).Msg("Failed to connect to Elasticsearch after retries (search disabled)")
				esClient = nil
				break
			}
			time.Sleep(2 * time.Second)
		}
	}

	// ---- Storage components ----
	hotPageTracker := storage.NewHotPageTracker(redisClient, &cfg.Redis.HotPages)
	trendingScorer := storage.NewTrendingScorer(redisClient, &cfg.Redis.Trending)
	alerts := storage.NewRedisAlerts(redisClient)

	// ---- SQLite User Store ----
	// Ensure the data directory exists
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0755); err != nil {
		logger.Fatal().Err(err).Str("path", cfg.Database.Path).Msg("Failed to create database directory")
	}
	userStore, err := storage.NewUserStore(cfg.Database.Path)
	if err != nil {
		logger.Fatal().Err(err).Str("path", cfg.Database.Path).Msg("Failed to open user database")
	}
	logger.Info().Str("path", cfg.Database.Path).Msg("User database ready")

	// ---- JWT Service ----
	jwtSvc := auth.NewJWTService(cfg.Auth.JWTSecret, cfg.Auth.JWTExpiry)
	logger.Info().Dur("expiry", cfg.Auth.JWTExpiry).Msg("JWT auth service initialized")

	// ---- API Server ----
	apiServer := api.NewAPIServer(redisClient, esClient, trendingScorer, hotPageTracker, alerts, userStore, jwtSvc, cfg, logger)
	addr := fmt.Sprintf(":%d", cfg.API.Port)
	httpServer := apiServer.ListenAndServe(addr)

	// ---- Digest Scheduler ----
	var digestScheduler *digest.Scheduler
	if cfg.Email.Enabled {
		statsTracker := storage.NewStatsTracker(redisClient)
		collector := digest.NewCollector(trendingScorer, alerts, hotPageTracker, statsTracker, logger)

		var emailSender digest.EmailSender
		switch cfg.Email.Provider {
		case "resend":
			emailSender = email.NewResendSender(cfg.Email.APIKey, cfg.Email.FromAddress, cfg.Email.FromName, logger)
		case "smtp":
			emailSender = email.NewSMTPSender(cfg.Email.SMTPHost, fmt.Sprintf("%d", cfg.Email.SMTPPort), cfg.Email.SMTPUser, cfg.Email.SMTPPass, cfg.Email.FromAddress, cfg.Email.FromName, logger)
		default:
			emailSender = email.NewLogSender(logger)
		}

		digestScheduler = digest.NewScheduler(collector, emailSender, userStore, digest.SchedulerConfig{
			DailySendHour:      cfg.Email.DailySendHour,
			WeeklySendDay:      cfg.Email.WeeklySendDay,
			WeeklySendHour:     cfg.Email.WeeklySendHour,
			MaxConcurrentSends: cfg.Email.MaxConcurrentSends,
			DashboardURL:       cfg.Email.DashboardURL,
			Enabled:            true,
		}, logger)
		digestScheduler.Start()
		logger.Info().Str("provider", cfg.Email.Provider).Msg("Digest email scheduler started")
	} else {
		logger.Info().Msg("Digest email scheduler disabled (email.enabled = false)")
	}

	// Start HTTP server
	go func() {
		logger.Info().Str("addr", addr).Msg("API server listening")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("API server failed")
		}
	}()

	// ---- Start Redis pub/sub relay for live edits to WebSocket clients ----
	apiServer.StartEditRelay(redisClient)

	// ---- Wait for shutdown signal ----
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	logger.Info().Str("signal", sig.String()).Msg("Shutdown signal received")

	// ---- Graceful shutdown ----
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop accepting new requests, wait for in-flight
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("HTTP server shutdown error")
	}

	// Stop digest scheduler
	if digestScheduler != nil {
		digestScheduler.Stop()
	}

	// Stop metrics server
	if err := metricsServer.Stop(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("Metrics server shutdown error")
	}

	// Stop storage components
	hotPageTracker.Shutdown()
	trendingScorer.Stop()

	// Close Redis
	if err := redisClient.Close(); err != nil {
		logger.Error().Err(err).Msg("Redis close error")
	}

	// Close user database
	if err := userStore.Close(); err != nil {
		logger.Error().Err(err).Msg("User database close error")
	}

	// Close ES
	if esClient != nil {
		esClient.Stop()
	}

	_ = apiServer.Shutdown(shutdownCtx)

	logger.Info().Msg("WikiSurge API Server stopped")
}