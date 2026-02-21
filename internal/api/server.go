package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/auth"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/llm"
	"github.com/Agnikulu/WikiSurge/internal/models"
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
	statsTracker   *storage.StatsTracker
	config         *config.Config
	logger         zerolog.Logger
	startTime      time.Time
	cache          *responseCache
	rateLimiter    *RateLimiter
	wsHub          *WebSocketHub
	alertHub       *AlertHub
	analysisService *llm.AnalysisService
	userStore       *storage.UserStore
	jwtService      *auth.JWTService
	version        string

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
	userStore *storage.UserStore,
	jwtSvc *auth.JWTService,
	cfg *config.Config,
	logger zerolog.Logger,
) *APIServer {
	s := &APIServer{
		router:       http.NewServeMux(),
		redis:        redisClient,
		es:           es,
		trending:     trending,
		hotPages:     hotPages,
		alerts:       alerts,
		statsTracker: storage.NewStatsTracker(redisClient),
		config:       cfg,
		logger:       logger.With().Str("component", "api").Logger(),
		startTime:    time.Now(),
		cache:        newResponseCache(),
		userStore:    userStore,
		jwtService:   jwtSvc,
		version:      "1.0.0",
	}

	// Initialise Redis-backed rate limiter.
	if cfg.API.RateLimiting.Enabled {
		s.rateLimiter = NewRateLimiter(redisClient, cfg.API.RateLimiting, s.logger)
		s.logger.Info().Msg("Redis sliding-window rate limiter enabled")
	}

	// WebSocket hub.
	s.wsHub = NewWebSocketHub(s.logger)
	go s.wsHub.Run()

	// Alert hub — single shared Redis subscription for all alert WS clients.
	s.alertHub = NewAlertHub(alerts, s.logger)
	go s.alertHub.Run()

	// LLM analysis service for edit war conflict summaries.
	if cfg.LLM.Enabled {
		llmClient := llm.NewClient(llm.Config{
			Provider:    llm.Provider(cfg.LLM.Provider),
			APIKey:      cfg.LLM.APIKey,
			Model:       cfg.LLM.Model,
			BaseURL:     cfg.LLM.BaseURL,
			MaxTokens:   cfg.LLM.MaxTokens,
			Temperature: cfg.LLM.Temperature,
			Timeout:     cfg.LLM.Timeout,
		}, s.logger)
		s.analysisService = llm.NewAnalysisService(llmClient, redisClient, cfg.LLM.CacheTTL, s.logger)
		s.logger.Info().Str("provider", cfg.LLM.Provider).Str("model", cfg.LLM.Model).Msg("LLM analysis service enabled")
	} else {
		// Even without LLM, provide heuristic fallback
		llmClient := llm.NewClient(llm.Config{}, s.logger)
		s.analysisService = llm.NewAnalysisService(llmClient, redisClient, cfg.LLM.CacheTTL, s.logger)
		s.logger.Info().Msg("LLM not configured — edit war analysis will use heuristic fallback")
	}

	s.setupRoutes()
	return s
}

// setupRoutes registers all REST endpoints.
func (s *APIServer) setupRoutes() {
	// Health (no /api prefix)
	s.router.HandleFunc("GET /health", s.handleHealth)
	s.router.HandleFunc("GET /health/live", s.handleLiveness)
	s.router.HandleFunc("GET /health/ready", s.handleReadiness)

	// API routes
	s.router.HandleFunc("GET /api/trending", s.handleGetTrending)
	s.router.HandleFunc("GET /api/stats", s.handleGetStats)
	s.router.HandleFunc("GET /api/alerts", s.handleGetAlerts)
	s.router.HandleFunc("GET /api/edit-wars", s.handleGetEditWars)
	s.router.HandleFunc("GET /api/edit-wars/analysis", s.handleGetEditWarAnalysis)
	s.router.HandleFunc("GET /api/edit-wars/timeline", s.handleGetEditWarTimeline)
	s.router.HandleFunc("GET /api/timeline", s.handleGetTimeline)
	s.router.HandleFunc("GET /api/search", s.handleSearch)

	// Documentation routes
	s.router.HandleFunc("GET /api/docs", s.handleAPIDocs)
	s.router.HandleFunc("GET /api/docs/openapi.yaml", s.handleOpenAPISpec)

	// WebSocket routes
	s.router.HandleFunc("/ws/feed", s.WebSocketFeed)
	s.router.HandleFunc("/ws/alerts", s.WebSocketAlerts)

	// Auth + user routes (only if user store is configured)
	if s.userStore != nil && s.jwtService != nil {
		// Auth routes (public)
		s.router.HandleFunc("POST /api/auth/register", s.handleRegister)
		s.router.HandleFunc("POST /api/auth/login", s.handleLogin)

		// Unsubscribe (public — token-based from email)
		s.router.HandleFunc("GET /api/digest/unsubscribe", s.handleUnsubscribe)

		// User routes (protected by JWT auth middleware)
		authMw := auth.Middleware(s.jwtService)
		s.router.Handle("GET /api/user/profile", authMw(http.HandlerFunc(s.handleGetProfile)))
		s.router.Handle("GET /api/user/preferences", authMw(http.HandlerFunc(s.handleGetPreferences)))
		s.router.Handle("PUT /api/user/preferences", authMw(http.HandlerFunc(s.handleUpdatePreferences)))
		s.router.Handle("GET /api/user/watchlist", authMw(http.HandlerFunc(s.handleGetWatchlist)))
		s.router.Handle("PUT /api/user/watchlist", authMw(http.HandlerFunc(s.handleUpdateWatchlist)))
	}
}

// Handler returns the full middleware-wrapped HTTP handler.
func (s *APIServer) Handler() http.Handler {
	var h http.Handler = s.router

	// Wrap in middleware (innermost first)
	h = MetricsMiddleware(h)

	// Rate limiting: prefer Redis sliding-window when available.
	if s.rateLimiter != nil {
		h = s.rateLimiter.Middleware(h)
	} else {
		h = RateLimitMiddleware(s.config.API.RateLimit, h)
	}

	h = RequestValidationMiddleware(h)
	h = SecurityHeadersMiddleware(h)
	h = CORSMiddleware(h)
	h = ETagMiddleware(h)
	h = GzipMiddleware(h)
	h = RecoveryMiddleware(s.logger, h)
	h = RequestIDMiddleware(s.logger, h)
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

// Hub returns the WebSocket hub for external integration (e.g., processor broadcasting).
func (s *APIServer) Hub() *WebSocketHub {
	return s.wsHub
}

// StartEditRelay subscribes to the Redis pub/sub channel where the processor
// publishes live edits, and feeds them into the API's WebSocket hub so that
// connected dashboard clients receive real-time updates.
func (s *APIServer) StartEditRelay(redisClient *redis.Client) {
	go func() {
		ctx := context.Background()
		sub := redisClient.Subscribe(ctx, "wikisurge:edits:live")
		ch := sub.Channel()
		s.logger.Info().Msg("Edit relay started — subscribing to Redis pub/sub for live edits")

		for msg := range ch {
			var edit models.WikipediaEdit
			if err := json.Unmarshal([]byte(msg.Payload), &edit); err != nil {
				s.logger.Warn().Err(err).Msg("Failed to unmarshal relayed edit")
				continue
			}
			s.wsHub.BroadcastEditFiltered(&edit)
		}
	}()
}

// Shutdown performs graceful shutdown of API-specific resources.
func (s *APIServer) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("API server shutting down")
	if s.wsHub != nil {
		s.wsHub.Stop()
	}
	return nil
}
