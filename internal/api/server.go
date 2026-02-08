package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// APIServer is the main HTTP API server for WikiSurge.
type APIServer struct {
	router         *http.ServeMux
	redis          *redis.Client
	es             *storage.ElasticsearchClient
	trending       *storage.TrendingScorer
	hotPages       *storage.HotPageTracker
	alerts         *storage.RedisAlerts
	config         *config.Config
	logger         zerolog.Logger
	startTime      time.Time

	// Stats cache
	statsMu        sync.RWMutex
	statsCache     *StatsResponse
	statsCacheTime time.Time
}

// NewAPIServer creates and configures a new API server with all middleware and routes.
func NewAPIServer(
	redisClient *redis.Client,
	es *storage.ElasticsearchClient,
	trending *storage.TrendingScorer,
	hotPages *storage.HotPageTracker,
	alerts *storage.RedisAlerts,
	cfg *config.Config,
	logger zerolog.Logger,
) *APIServer {
	s := &APIServer{
		router:    http.NewServeMux(),
		redis:     redisClient,
		es:        es,
		trending:  trending,
		hotPages:  hotPages,
		alerts:    alerts,
		config:    cfg,
		logger:    logger.With().Str("component", "api").Logger(),
		startTime: time.Now(),
	}

	s.setupRoutes()
	return s
}

// setupRoutes registers all REST endpoints.
func (s *APIServer) setupRoutes() {
	// Health (no /api prefix)
	s.router.HandleFunc("GET /health", s.handleHealth)

	// API routes
	s.router.HandleFunc("GET /api/trending", s.handleGetTrending)
	s.router.HandleFunc("GET /api/stats", s.handleGetStats)
	s.router.HandleFunc("GET /api/alerts", s.handleGetAlerts)
	s.router.HandleFunc("GET /api/edit-wars", s.handleGetEditWars)
	s.router.HandleFunc("GET /api/search", s.handleSearch)
}

// Handler returns the full middleware-wrapped HTTP handler.
func (s *APIServer) Handler() http.Handler {
	var h http.Handler = s.router

	// Wrap in middleware (innermost first)
	h = MetricsMiddleware(h)
	h = RateLimitMiddleware(s.config.API.RateLimit, h)
	h = CORSMiddleware(h)
	h = RecoveryMiddleware(s.logger, h)
	h = LoggerMiddleware(s.logger, h)

	return h
}

// ListenAndServe starts the API server with the given address.
func (s *APIServer) ListenAndServe(addr string) *http.Server {
	if addr == "" {
		addr = fmt.Sprintf(":%d", s.config.API.Port)
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      s.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return srv
}

// Shutdown performs graceful shutdown of API-specific resources.
func (s *APIServer) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("API server shutting down")
	return nil
}
