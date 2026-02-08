package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/storage"
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
	limit, err := parseIntQuery(r, "limit", 20, 100)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid 'limit' parameter (must be 1-100)", "INVALID_PARAM")
		return
	}

	offset, err := parseIntQuery(r, "offset", 0, 10000)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid 'offset' parameter", "INVALID_PARAM")
		return
	}

	// Parse 'since' (default: 24h ago)
	since, err := parseTimeQuery(r, "since", time.Now().Add(-24*time.Hour))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid 'since' parameter (use RFC3339 or Unix timestamp)", "INVALID_PARAM")
		return
	}

	severity := r.URL.Query().Get("severity")
	if severity != "" {
		validSeverities := map[string]bool{"low": true, "medium": true, "high": true, "critical": true}
		if !validSeverities[severity] {
			respondError(w, http.StatusBadRequest,
				fmt.Sprintf("Invalid severity '%s'; valid values: low, medium, high, critical", severity),
				"INVALID_PARAM")
			return
		}
	}

	alertType := r.URL.Query().Get("type")
	validTypes := map[string]string{
		"spike":    "spikes",
		"edit_war": "editwars",
		"spikes":   "spikes",
		"editwars": "editwars",
	}

	if alertType != "" {
		if _, ok := validTypes[alertType]; !ok {
			respondError(w, http.StatusBadRequest,
				fmt.Sprintf("Invalid alert type '%s'; valid types: spike, edit_war", alertType),
				"INVALID_PARAM")
			return
		}
	}

	// Check cache
	ck := cacheKey("alerts", alertType, severity, since.Format(time.RFC3339), strconv.Itoa(limit), strconv.Itoa(offset))
	if cached, ok := s.cache.Get(ck); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "max-age=5")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		w.Write(cached)
		return
	}

	if s.alerts == nil {
		respondJSON(w, http.StatusOK, AlertsResponse{
			Alerts: []AlertEntry{},
			Total:  0,
			Pagination: PaginationInfo{
				Total: 0, Limit: limit, Offset: offset, HasMore: false,
			},
		})
		return
	}

	ctx := r.Context()

	// Decide which streams to query
	streams := []string{}
	if alertType == "" {
		streams = []string{"spikes", "editwars"}
	} else {
		streams = []string{validTypes[alertType]}
	}

	// Fetch a larger batch to allow for filtering + offset
	fetchCount := int64(limit + offset + 100)
	var allAlerts []storage.Alert

	for _, stream := range streams {
		alerts, err := s.alerts.GetAlertsSince(ctx, stream, since, severity, fetchCount)
		if err != nil {
			s.logger.Error().Err(err).Str("stream", stream).Msg("failed to get alerts")
			continue
		}
		allAlerts = append(allAlerts, alerts...)
	}

	// Sort all combined alerts by timestamp descending
	sort.Slice(allAlerts, func(i, j int) bool {
		return allAlerts[i].Timestamp.After(allAlerts[j].Timestamp)
	})

	// Transform to response format
	total := len(allAlerts)
	entries := make([]AlertEntry, 0)

	// Apply offset
	start := offset
	if start > len(allAlerts) {
		start = len(allAlerts)
	}
	end := start + limit
	if end > len(allAlerts) {
		end = len(allAlerts)
	}

	for _, a := range allAlerts[start:end] {
		entry := AlertEntry{
			Type:      a.Type,
			Timestamp: a.Timestamp.Format(time.RFC3339),
			Severity:  storage.DeriveSeverity(a),
		}

		if title, ok := a.Data["title"].(string); ok {
			entry.PageTitle = title
		}
		if wiki, ok := a.Data["wiki"].(string); ok {
			entry.Wiki = wiki
		}
		if ratio, ok := a.Data["spike_ratio"].(float64); ok {
			entry.SpikeRatio = ratio
		}
		if editCount, ok := a.Data["edit_count"].(float64); ok {
			entry.Edits5Min = int(editCount)
		}
		if numEditors, ok := a.Data["num_editors"].(float64); ok {
			entry.EditorCount = int(numEditors)
		}
		if participants, ok := a.Data["participants"].([]interface{}); ok {
			eds := make([]string, 0, len(participants))
			for _, p := range participants {
				if s, ok := p.(string); ok {
					eds = append(eds, s)
				}
			}
			entry.Editors = eds
		}

		entries = append(entries, entry)
	}

	resp := AlertsResponse{
		Alerts: entries,
		Total:  total,
		Pagination: PaginationInfo{
			Total:   int64(total),
			Limit:   limit,
			Offset:  offset,
			HasMore: end < total,
		},
	}

	// Cache the response
	respBytes, _ := json.Marshal(resp)
	s.cache.Set(ck, respBytes, 5*time.Second)

	w.Header().Set("Cache-Control", "max-age=5")
	w.Header().Set("X-Cache", "MISS")
	respondJSON(w, http.StatusOK, resp)
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

	active := parseBoolQuery(r, "active", true)

	if s.alerts == nil {
		respondJSON(w, http.StatusOK, []EditWarEntry{})
		return
	}

	ctx := r.Context()

	if active {
		// SCAN for editwar:editors:* keys to find currently active wars
		activeWars, err := s.alerts.GetActiveEditWars(ctx, limit)
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to scan active edit wars")
			respondError(w, http.StatusInternalServerError, "Failed to retrieve active edit wars", "INTERNAL_ERROR")
			return
		}

		results := make([]EditWarEntry, 0, len(activeWars))
		for _, w := range activeWars {
			entry := EditWarEntry{Active: true}
			if pt, ok := w["page_title"].(string); ok {
				entry.PageTitle = pt
			}
			if ec, ok := w["editor_count"].(int); ok {
				entry.EditorCount = ec
			}
			if edc, ok := w["edit_count"].(int); ok {
				entry.EditCount = edc
			}
			if rc, ok := w["revert_count"].(int); ok {
				entry.RevertCount = rc
			}
			if sev, ok := w["severity"].(string); ok {
				entry.Severity = sev
			}
			if eds, ok := w["editors"].([]string); ok {
				entry.Editors = eds
			}
			results = append(results, entry)
		}

		respondJSON(w, http.StatusOK, results)
		return
	}

	// Historical: read from the alerts:editwars stream
	since := time.Now().Add(-7 * 24 * time.Hour) // last 7 days
	historicalWars, err := s.alerts.GetEditWarAlertsSince(ctx, since, int64(limit))
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get historical edit wars")
		respondError(w, http.StatusInternalServerError, "Failed to retrieve edit war history", "INTERNAL_ERROR")
		return
	}

	results := make([]EditWarEntry, 0, len(historicalWars))
	for _, w := range historicalWars {
		entry := EditWarEntry{Active: false}
		if pt, ok := w["page_title"].(string); ok {
			entry.PageTitle = pt
		}
		if ec, ok := w["editor_count"].(float64); ok {
			entry.EditorCount = int(ec)
		}
		if edc, ok := w["edit_count"].(float64); ok {
			entry.EditCount = int(edc)
		}
		if rc, ok := w["revert_count"].(float64); ok {
			entry.RevertCount = int(rc)
		}
		if sev, ok := w["severity"].(string); ok {
			entry.Severity = sev
		}
		if ts, ok := w["start_time"].(string); ok {
			entry.StartTime = ts
		}
		if eds, ok := w["editors"].([]interface{}); ok {
			names := make([]string, 0, len(eds))
			for _, e := range eds {
				if s, ok := e.(string); ok {
					names = append(names, s)
				}
			}
			entry.Editors = names
		}
		results = append(results, entry)
	}

	respondJSON(w, http.StatusOK, results)
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

	limit, err := parseIntQuery(r, "limit", 50, 100)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid 'limit' parameter (must be 1-100)", "INVALID_PARAM")
		return
	}

	offset, err := parseIntQuery(r, "offset", 0, 10000)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid 'offset' parameter", "INVALID_PARAM")
		return
	}

	// Parse time range
	defaultFrom := time.Now().Add(-7 * 24 * time.Hour) // 7 days ago
	from, err := parseTimeQuery(r, "from", defaultFrom)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid 'from' parameter (use RFC3339 or Unix timestamp)", "INVALID_PARAM")
		return
	}

	to, err := parseTimeQuery(r, "to", time.Now())
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid 'to' parameter (use RFC3339 or Unix timestamp)", "INVALID_PARAM")
		return
	}

	if from.After(to) {
		respondError(w, http.StatusBadRequest, "'from' must be before 'to'", "INVALID_PARAM")
		return
	}

	if s.es == nil || !s.config.Elasticsearch.Enabled {
		respondError(w, http.StatusServiceUnavailable, "Search is not available (Elasticsearch disabled)", "SERVICE_UNAVAILABLE")
		return
	}

	// Optional filters
	language := r.URL.Query().Get("language")
	botFilter := r.URL.Query().Get("bot")

	// Check cache
	ck := cacheKey("search", query, from.Format(time.RFC3339), to.Format(time.RFC3339),
		strconv.Itoa(limit), strconv.Itoa(offset), language, botFilter)
	if cached, ok := s.cache.Get(ck); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "max-age=10")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		w.Write(cached)
		return
	}

	// Build Elasticsearch query
	searchQuery := s.buildSearchQuery(query, from, to, limit, offset, language, botFilter)

	result, err := s.es.Search(searchQuery, "wikipedia-edits-*")
	if err != nil {
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "context deadline exceeded") {
			respondError(w, http.StatusGatewayTimeout, "Search timed out", "TIMEOUT")
			return
		}
		s.logger.Error().Err(err).Str("query", query).Msg("Elasticsearch search failed")
		respondError(w, http.StatusInternalServerError, "Search failed", "INTERNAL_ERROR")
		return
	}

	// Parse ES response
	resp := s.parseSearchResponse(result, query, limit, offset)

	// Cache the response
	respBytes, _ := json.Marshal(resp)
	s.cache.Set(ck, respBytes, 10*time.Second)

	w.Header().Set("Cache-Control", "max-age=10")
	w.Header().Set("X-Cache", "MISS")
	respondJSON(w, http.StatusOK, resp)
}

// buildSearchQuery constructs an Elasticsearch bool query DSL.
func (s *APIServer) buildSearchQuery(
	query string,
	from, to time.Time,
	limit, offset int,
	language, botFilter string,
) map[string]interface{} {
	// Build the multi_match must clause
	multiMatch := map[string]interface{}{
		"query":     query,
		"fields":    []string{"title^2", "comment", "user"},
		"fuzziness": "AUTO",
	}

	// Detect phrase queries (quoted strings)
	if strings.HasPrefix(query, "\"") && strings.HasSuffix(query, "\"") {
		multiMatch["type"] = "phrase"
		multiMatch["query"] = strings.Trim(query, "\"")
		delete(multiMatch, "fuzziness")
	}

	must := []interface{}{
		map[string]interface{}{
			"multi_match": multiMatch,
		},
	}

	// Build filters
	filters := []interface{}{
		map[string]interface{}{
			"range": map[string]interface{}{
				"timestamp": map[string]interface{}{
					"gte": from.Format("2006-01-02T15:04:05.000Z"),
					"lte": to.Format("2006-01-02T15:04:05.000Z"),
				},
			},
		},
	}

	// Language filter
	if language != "" {
		filters = append(filters, map[string]interface{}{
			"term": map[string]interface{}{
				"language": language,
			},
		})
	}

	// Bot filter
	if botFilter != "" {
		isBot := botFilter == "true" || botFilter == "1"
		filters = append(filters, map[string]interface{}{
			"term": map[string]interface{}{
				"bot": isBot,
			},
		})
	}

	esQuery := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must":   must,
				"filter": filters,
			},
		},
		"size": limit,
		"from": offset,
		"sort": []map[string]interface{}{
			{"timestamp": map[string]string{"order": "desc"}},
		},
		"highlight": map[string]interface{}{
			"fields": map[string]interface{}{
				"title":   map[string]interface{}{},
				"comment": map[string]interface{}{},
			},
		},
	}

	return esQuery
}

// parseSearchResponse transforms the raw ES response into our SearchResponse.
func (s *APIServer) parseSearchResponse(result map[string]interface{}, query string, limit, offset int) SearchResponse {
	resp := SearchResponse{
		Hits:  make([]SearchHit, 0),
		Query: query,
	}

	// Extract total
	if hitsObj, ok := result["hits"].(map[string]interface{}); ok {
		// Total count
		if totalObj, ok := hitsObj["total"].(map[string]interface{}); ok {
			if val, ok := totalObj["value"].(float64); ok {
				resp.Total = int64(val)
			}
		}

		// Extract individual hits
		if hitsArr, ok := hitsObj["hits"].([]interface{}); ok {
			for _, h := range hitsArr {
				hitMap, ok := h.(map[string]interface{})
				if !ok {
					continue
				}

				hit := SearchHit{}

				// Score
				if score, ok := hitMap["_score"].(float64); ok {
					hit.Score = score
				}

				// Source fields
				if source, ok := hitMap["_source"].(map[string]interface{}); ok {
					if v, ok := source["title"].(string); ok {
						hit.Title = v
					}
					if v, ok := source["user"].(string); ok {
						hit.User = v
					}
					if v, ok := source["comment"].(string); ok {
						hit.Comment = v
					}
					if v, ok := source["wiki"].(string); ok {
						hit.Wiki = v
					}
					if v, ok := source["language"].(string); ok {
						hit.Language = v
					}
					if v, ok := source["timestamp"].(string); ok {
						hit.Timestamp = v
					}
					if v, ok := source["byte_change"].(float64); ok {
						hit.ByteChange = int(v)
					}
				}

				resp.Hits = append(resp.Hits, hit)
			}
		}
	}

	resp.Pagination = PaginationInfo{
		Total:   resp.Total,
		Limit:   limit,
		Offset:  offset,
		HasMore: int64(offset+limit) < resp.Total,
	}

	return resp
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
