package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

// HealthResponse is returned by GET /health.
type HealthResponse struct {
	Status        string `json:"status"`
	Redis         string `json:"redis"`
	Elasticsearch string `json:"elasticsearch"`
	Uptime        int64  `json:"uptime"`
}

func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	redisStatus := "connected"
	esStatus := "connected"

	// Check Redis
	if err := s.redis.Ping(ctx).Err(); err != nil {
		redisStatus = fmt.Sprintf("error: %v", err)
	}

	// Check Elasticsearch
	if s.es != nil {
		esStatus = "connected"
		// ES client doesn't expose a simple ping in our wrapper; mark connected if configured
	} else if s.config.Elasticsearch.Enabled {
		esStatus = "not_initialized"
	} else {
		esStatus = "disabled"
	}

	overall := "ok"
	if redisStatus != "connected" {
		overall = "degraded"
	}
	if redisStatus != "connected" && esStatus != "connected" && esStatus != "disabled" {
		overall = "error"
	}

	status := http.StatusOK
	if overall != "ok" {
		status = http.StatusServiceUnavailable
	}

	respondJSON(w, status, HealthResponse{
		Status:        overall,
		Redis:         redisStatus,
		Elasticsearch: esStatus,
		Uptime:        int64(time.Since(s.startTime).Seconds()),
	})
}

// ---------------------------------------------------------------------------
// Trending
// ---------------------------------------------------------------------------

// TrendingPageResponse represents a single trending page.
type TrendingPageResponse struct {
	Title    string  `json:"title"`
	Score    float64 `json:"score"`
	Edits1h int64   `json:"edits_1h"`
	LastEdit string  `json:"last_edit"`
	Rank     int     `json:"rank"`
	Language string  `json:"language,omitempty"`
}

func (s *APIServer) handleGetTrending(w http.ResponseWriter, r *http.Request) {
	limit, err := parseIntQuery(r, "limit", 20, 100)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid 'limit' parameter (must be 1-100)", "INVALID_PARAM")
		return
	}
	langFilter := r.URL.Query().Get("language")

	if s.trending == nil {
		respondError(w, http.StatusServiceUnavailable, "Trending service not available", "SERVICE_UNAVAILABLE")
		return
	}

	entries, err := s.trending.GetTopTrending(limit)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get trending pages")
		respondError(w, http.StatusInternalServerError, "Failed to retrieve trending pages", "INTERNAL_ERROR")
		return
	}

	ctx := r.Context()
	results := make([]TrendingPageResponse, 0, len(entries))

	for i, e := range entries {
		// Detect language from page title convention (e.g. "en.wikipedia.org:Page")
		lang := extractLanguage(e.PageTitle)

		if langFilter != "" && lang != langFilter {
			continue
		}

		// Enrich with edit count from hot page tracker
		var edits1h int64
		if s.hotPages != nil {
			stats, err := s.hotPages.GetPageStats(ctx, e.PageTitle)
			if err == nil && stats != nil {
				edits1h = stats.EditsLastHour
			}
		}

		lastEdit := ""
		if e.LastUpdated > 0 {
			lastEdit = time.Unix(e.LastUpdated, 0).UTC().Format(time.RFC3339)
		}

		results = append(results, TrendingPageResponse{
			Title:    e.PageTitle,
			Score:    e.CurrentScore,
			Edits1h:  edits1h,
			LastEdit: lastEdit,
			Rank:     i + 1,
			Language: lang,
		})
	}

	respondJSON(w, http.StatusOK, results)
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

// StatsResponse is returned by GET /api/stats.
type StatsResponse struct {
	EditsPerSecond float64            `json:"edits_per_second"`
	HotPagesCount  int                `json:"hot_pages_count"`
	TrendingCount  int                `json:"trending_count"`
	ActiveAlerts   int64              `json:"active_alerts"`
	Uptime         int64              `json:"uptime"`
	TopLanguages   []LanguageStat     `json:"top_languages"`
}

// LanguageStat is a single language count.
type LanguageStat struct {
	Language string `json:"language"`
	Count    int    `json:"count"`
}

func (s *APIServer) handleGetStats(w http.ResponseWriter, r *http.Request) {
	// Return cached result if fresh (< 5 seconds)
	s.statsMu.RLock()
	if s.statsCache != nil && time.Since(s.statsCacheTime) < 5*time.Second {
		cached := *s.statsCache
		s.statsMu.RUnlock()
		respondJSON(w, http.StatusOK, cached)
		return
	}
	s.statsMu.RUnlock()

	ctx := r.Context()

	// Hot pages count
	var hotCount int
	if s.hotPages != nil {
		hotCount, _ = s.hotPages.GetHotPagesCount(ctx)
	}

	// Trending count
	var trendingCount int
	if s.trending != nil {
		entries, err := s.trending.GetTopTrending(1000)
		if err == nil {
			trendingCount = len(entries)
		}
	}

	// Active alerts
	var activeAlerts int64
	if s.alerts != nil {
		alertTypes := []string{"spikes", "editwars", "trending", "vandalism"}
		for _, t := range alertTypes {
			streamName := fmt.Sprintf("alerts:%s", t)
			length, err := s.redis.XLen(ctx, streamName).Result()
			if err == nil {
				activeAlerts += length
			}
		}
	}

	resp := StatsResponse{
		HotPagesCount: hotCount,
		TrendingCount: trendingCount,
		ActiveAlerts:  activeAlerts,
		Uptime:        int64(time.Since(s.startTime).Seconds()),
		TopLanguages:  []LanguageStat{}, // filled when language tracking is available
	}

	// Cache
	s.statsMu.Lock()
	s.statsCache = &resp
	s.statsCacheTime = time.Now()
	s.statsMu.Unlock()

	respondJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Alerts
// ---------------------------------------------------------------------------

func (s *APIServer) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	limit, err := parseIntQuery(r, "limit", 20, 200)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid 'limit' parameter (must be 1-200)", "INVALID_PARAM")
		return
	}
	alertType := r.URL.Query().Get("type")
	if alertType == "" {
		alertType = "spikes" // default
	}

	validTypes := map[string]bool{"spikes": true, "editwars": true, "trending": true, "vandalism": true}
	if !validTypes[alertType] {
		respondError(w, http.StatusBadRequest,
			fmt.Sprintf("Invalid alert type '%s'; valid types: spikes, editwars, trending, vandalism", alertType),
			"INVALID_PARAM")
		return
	}

	if s.alerts == nil {
		respondJSON(w, http.StatusOK, []interface{}{})
		return
	}

	ctx := r.Context()
	alerts, err := s.alerts.GetRecentAlerts(ctx, alertType, int64(limit))
	if err != nil {
		s.logger.Error().Err(err).Str("type", alertType).Msg("failed to get alerts")
		respondError(w, http.StatusInternalServerError, "Failed to retrieve alerts", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusOK, alerts)
}

// ---------------------------------------------------------------------------
// Edit Wars
// ---------------------------------------------------------------------------

func (s *APIServer) handleGetEditWars(w http.ResponseWriter, r *http.Request) {
	limit, err := parseIntQuery(r, "limit", 20, 100)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid 'limit' parameter (must be 1-100)", "INVALID_PARAM")
		return
	}

	if s.alerts == nil {
		respondJSON(w, http.StatusOK, []interface{}{})
		return
	}

	ctx := r.Context()
	alerts, err := s.alerts.GetRecentAlerts(ctx, "editwars", int64(limit))
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get edit war alerts")
		respondError(w, http.StatusInternalServerError, "Failed to retrieve edit wars", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusOK, alerts)
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

func (s *APIServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		respondError(w, http.StatusBadRequest, "Missing required 'q' parameter", "INVALID_PARAM")
		return
	}

	limit, err := parseIntQuery(r, "limit", 20, 100)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid 'limit' parameter", "INVALID_PARAM")
		return
	}

	if s.es == nil || !s.config.Elasticsearch.Enabled {
		respondError(w, http.StatusServiceUnavailable, "Search is not available (Elasticsearch disabled)", "SERVICE_UNAVAILABLE")
		return
	}

	searchQuery := map[string]interface{}{
		"size": limit,
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":  query,
				"fields": []string{"title^3", "user", "comment"},
			},
		},
		"sort": []map[string]interface{}{
			{"timestamp": map[string]string{"order": "desc"}},
		},
	}

	result, err := s.es.Search(searchQuery, "wikipedia-edits-*")
	if err != nil {
		s.logger.Error().Err(err).Str("query", query).Msg("Elasticsearch search failed")
		respondError(w, http.StatusInternalServerError, "Search failed", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// extractLanguage guesses a language code from the page title or wiki URL convention.
func extractLanguage(pageTitle string) string {
	// Convention: some pages are stored with a "wiki:" prefix like "enwiki:Page"
	if idx := strings.Index(pageTitle, "wiki:"); idx > 0 {
		prefix := pageTitle[:idx]
		if len(prefix) >= 2 {
			return prefix[:2]
		}
	}
	// Default: unknown
	return ""
}
