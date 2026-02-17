package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/Agnikulu/WikiSurge/internal/storage"
)

// ---------------------------------------------------------------------------
// Health — moved to health.go for enhanced implementation
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Trending
// ---------------------------------------------------------------------------

// TrendingPageResponse represents a single trending page.
type TrendingPageResponse struct {
	Title     string  `json:"title"`
	Score     float64 `json:"score"`
	Edits1h   int64   `json:"edits_1h"`
	LastEdit  string  `json:"last_edit"`
	Rank      int     `json:"rank"`
	Language  string  `json:"language,omitempty"`
	ServerURL string  `json:"server_url,omitempty"`
}

func (s *APIServer) handleGetTrending(w http.ResponseWriter, r *http.Request) {
	params, verr := ParseTrendingParams(r)
	if verr != nil {
		writeValidationError(w, r, verr)
		return
	}

	if s.trending == nil {
		writeAPIError(w, r, http.StatusServiceUnavailable,
			"Trending service not available", ErrCodeServiceUnavailable, "")
		return
	}

	entries, err := s.trending.GetTopTrending(params.Limit)
	if err != nil {
		s.logger.Error().Err(err).
			Str("request_id", GetRequestID(r.Context())).
			Msg("failed to get trending pages")
		writeAPIError(w, r, http.StatusInternalServerError,
			"Failed to retrieve trending pages", ErrCodeInternalError, "")
		return
	}

	ctx := r.Context()
	results := make([]TrendingPageResponse, 0, len(entries))

	for i, e := range entries {
		// Detect language from server_url or page title convention
		lang := extractLanguageFromURL(e.ServerURL)
		if lang == "" {
			lang = extractLanguage(e.PageTitle)
		}

		if params.Language != "" && lang != params.Language {
			continue
		}

		// Enrich with edit count from hot page tracker
		var edits1h int64
		var hotServerURL string
		if s.hotPages != nil {
			stats, err := s.hotPages.GetPageStats(ctx, e.PageTitle)
			if err == nil && stats != nil {
				edits1h = stats.EditsLastHour
				if stats.ServerURL != "" {
					hotServerURL = stats.ServerURL
				}
			}
		}

		// Prefer trending entry's server_url, fall back to hot page's
		serverURL := e.ServerURL
		if serverURL == "" {
			serverURL = hotServerURL
		}

		lastEdit := ""
		if e.LastUpdated > 0 {
			lastEdit = time.Unix(e.LastUpdated, 0).UTC().Format(time.RFC3339)
		}

		results = append(results, TrendingPageResponse{
			Title:     e.PageTitle,
			Score:     e.CurrentScore,
			Edits1h:   edits1h,
			LastEdit:  lastEdit,
			Rank:      i + 1,
			Language:  lang,
			ServerURL: serverURL,
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
	EditsToday     int                `json:"edits_today"`
	HotPagesCount  int                `json:"hot_pages_count"`
	TrendingCount  int                `json:"trending_count"`
	ActiveAlerts   int64              `json:"active_alerts"`
	Uptime         int64              `json:"uptime"`
	TopLanguage    string             `json:"top_language,omitempty"`
	TopLanguages   []LanguageStat     `json:"top_languages"`
	EditsByType    *EditsByType       `json:"edits_by_type,omitempty"`
}

// EditsByType tracks human vs bot edit counts.
type EditsByType struct {
	Human int `json:"human"`
	Bot   int `json:"bot"`
}

// LanguageStat is a single language count.
type LanguageStat struct {
	Language   string  `json:"language"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

func (s *APIServer) handleGetStats(w http.ResponseWriter, r *http.Request) {
	// Return cached result if fresh (< 10 seconds) - matches frontend poll interval
	s.statsMu.RLock()
	if s.statsCache != nil && time.Since(s.statsCacheTime) < 10*time.Second {
		cached := *s.statsCache
		s.statsMu.RUnlock()
		w.Header().Set("Cache-Control", "max-age=10")
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

	// Trending count — use a fast ZCARD instead of fetching all entries.
	var trendingCount int
	if s.trending != nil {
		count, err := s.redis.ZCard(ctx, "trending:global").Result()
		if err == nil {
			trendingCount = int(count)
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
		TopLanguages:  []LanguageStat{},
	}

	// Compute edits_today from stats tracker counter (not KEYS scan)
	if s.statsTracker != nil {
		todayCount, _ := s.statsTracker.GetDailyEditCount(ctx)
		resp.EditsToday = int(todayCount)
	}

	// Compute edits_per_second: use activity count / uptime as approximation.
	if resp.Uptime > 0 && resp.EditsToday > 0 {
		resp.EditsPerSecond = float64(resp.EditsToday) / float64(resp.Uptime)
		// Round to 1 decimal
		resp.EditsPerSecond = math.Round(resp.EditsPerSecond*10) / 10
	}

	// Compute real per-language stats from Redis (tracked by processor).
	if s.statsTracker != nil {
		langCounts, _, langErr := s.statsTracker.GetLanguageCounts(ctx)
		if langErr == nil && len(langCounts) > 0 {
			totalLangEdits := int64(0)
			for _, lc := range langCounts {
				totalLangEdits += lc.Count
			}
			for _, lc := range langCounts {
				pct := 0.0
				if totalLangEdits > 0 {
					pct = math.Round(float64(lc.Count) / float64(totalLangEdits) * 1000) / 10
				}
				resp.TopLanguages = append(resp.TopLanguages, LanguageStat{
					Language:   lc.Language,
					Count:      int(lc.Count),
					Percentage: pct,
				})
			}
			if len(resp.TopLanguages) > 0 {
				resp.TopLanguage = resp.TopLanguages[0].Language
			}
		}

		// Get real human vs bot counts.
		human, bot, typeErr := s.statsTracker.GetEditTypes(ctx)
		if typeErr == nil && (human > 0 || bot > 0) {
			resp.EditsByType = &EditsByType{
				Human: int(human),
				Bot:   int(bot),
			}
		}
	}

	// Fallback: if no real language data yet, use configured languages with weights.
	if len(resp.TopLanguages) == 0 && s.config != nil && len(s.config.Ingestor.AllowedLanguages) > 0 {
		langs := s.config.Ingestor.AllowedLanguages
		totalEdits := resp.EditsToday
		if totalEdits == 0 {
			totalEdits = 1
		}
		weights := map[string]float64{"en": 0.55, "es": 0.15, "fr": 0.15, "de": 0.15}
		for _, lang := range langs {
			w, ok := weights[lang]
			if !ok {
				w = 1.0 / float64(len(langs))
			}
			count := int(math.Round(float64(totalEdits) * w))
			if count < 1 {
				count = 1
			}
			pct := math.Round(w * 1000) / 10
			resp.TopLanguages = append(resp.TopLanguages, LanguageStat{
				Language:   lang,
				Count:      count,
				Percentage: pct,
			})
		}
		sort.Slice(resp.TopLanguages, func(i, j int) bool {
			return resp.TopLanguages[i].Count > resp.TopLanguages[j].Count
		})
		if len(resp.TopLanguages) > 0 {
			resp.TopLanguage = resp.TopLanguages[0].Language
		}
	}

	// Cache
	s.statsMu.Lock()
	s.statsCache = &resp
	s.statsCacheTime = time.Now()
	s.statsMu.Unlock()

	w.Header().Set("Cache-Control", "max-age=10, stale-while-revalidate=5")
	respondJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Timeline
// ---------------------------------------------------------------------------

func (s *APIServer) handleGetTimeline(w http.ResponseWriter, r *http.Request) {
	// Parse duration parameter (default: 24h)
	durationStr := r.URL.Query().Get("duration")
	if durationStr == "" {
		durationStr = "24h"
	}
	
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		writeAPIError(w, r, http.StatusBadRequest,
			"Invalid duration parameter", ErrCodeInvalidParameter, "field: duration")
		return
	}
	
	// Limit to max 24 hours
	if duration > 24*time.Hour {
		duration = 24 * time.Hour
	}
	
	ctx := r.Context()
	
	// Get timeline data from Redis
	points, err := s.statsTracker.GetTimeline(ctx, duration)
	if err != nil {
		s.logger.Error().Err(err).
			Str("request_id", GetRequestID(ctx)).
			Msg("failed to get timeline points")
		writeAPIError(w, r, http.StatusInternalServerError,
			"Failed to retrieve timeline data", ErrCodeInternalError, "")
		return
	}
	
	// Convert to response format (edits per minute)
	response := make([]map[string]interface{}, len(points))
	for i, p := range points {
		response[i] = map[string]interface{}{
			"timestamp": p.Timestamp * 1000, // Convert to milliseconds for JS
			"value":     p.Count,
		}
	}
	
	w.Header().Set("Cache-Control", "max-age=10")
	respondJSON(w, http.StatusOK, response)
}

// ---------------------------------------------------------------------------
// Alerts
// ---------------------------------------------------------------------------

func (s *APIServer) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	params, verr := ParseAndValidateAlertParams(r)
	if verr != nil {
		writeValidationError(w, r, verr)
		return
	}

	validTypes := map[string]string{
		"spike":    "spikes",
		"edit_war": "editwars",
		"spikes":   "spikes",
		"editwars": "editwars",
	}

	// Check cache
	ck := cacheKey("alerts", params.AlertType, params.Severity, params.Since.Format(time.RFC3339),
		strconv.Itoa(params.Limit), strconv.Itoa(params.Offset))
	if cached, ok := s.cache.Get(ck); ok {
		metrics.APICacheHitsTotal.WithLabelValues().Inc()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "max-age=5")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		w.Write(cached)
		return
	}
	metrics.APICacheMissesTotal.WithLabelValues().Inc()

	if s.alerts == nil {
		respondJSON(w, http.StatusOK, AlertsResponse{
			Alerts: []AlertEntry{},
			Total:  0,
			Pagination: PaginationInfo{
				Total: 0, Limit: params.Limit, Offset: params.Offset, HasMore: false,
			},
		})
		return
	}

	ctx := r.Context()

	// Decide which streams to query
	streams := []string{}
	if params.AlertType == "" {
		streams = []string{"spikes", "editwars"}
	} else {
		streams = []string{validTypes[params.AlertType]}
	}

	// Fetch with reasonable buffer for filtering
	fetchCount := int64(params.Limit + params.Offset + 50)
	var allAlerts []storage.Alert

	for _, stream := range streams {
		alerts, err := s.alerts.GetAlertsSince(ctx, stream, params.Since, params.Severity, fetchCount)
		if err != nil {
			s.logger.Error().Err(err).Str("stream", stream).
				Str("request_id", GetRequestID(ctx)).
				Msg("failed to get alerts")
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
	start := params.Offset
	if start > len(allAlerts) {
		start = len(allAlerts)
	}
	end := start + params.Limit
	if end > len(allAlerts) {
		end = len(allAlerts)
	}

	for _, a := range allAlerts[start:end] {
		entry := AlertEntry{
			Type:      a.Type,
			Timestamp: a.Timestamp.Format(time.RFC3339),
			Severity:  storage.DeriveSeverity(a),
		}

		// Handle both field naming conventions (title vs page_title)
		if title, ok := a.Data["page_title"].(string); ok {
			entry.PageTitle = title
		} else if title, ok := a.Data["title"].(string); ok {
			entry.PageTitle = title
		}
		if wiki, ok := a.Data["wiki"].(string); ok {
			entry.Wiki = wiki
		}
		if ratio, ok := a.Data["spike_ratio"].(float64); ok {
			entry.SpikeRatio = ratio
		}
		// Handle both field naming conventions (edits_5min vs edit_count)
		if editCount, ok := a.Data["edits_5min"].(float64); ok {
			entry.Edits5Min = int(editCount)
		} else if editCount, ok := a.Data["edit_count"].(float64); ok {
			entry.Edits5Min = int(editCount)
		}
		// Handle both field naming conventions (unique_editors vs num_editors)
		if numEditors, ok := a.Data["unique_editors"].(float64); ok {
			entry.EditorCount = int(numEditors)
		} else if numEditors, ok := a.Data["num_editors"].(float64); ok {
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
			Limit:   params.Limit,
			Offset:  params.Offset,
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
		writeAPIError(w, r, http.StatusBadRequest,
			"Invalid 'limit' parameter (must be 1-100)", ErrCodeInvalidParameter, "field: limit")
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
			s.logger.Error().Err(err).
				Str("request_id", GetRequestID(ctx)).
				Msg("failed to scan active edit wars")
			writeAPIError(w, r, http.StatusInternalServerError,
				"Failed to retrieve active edit wars", ErrCodeInternalError, "")
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
			if st, ok := w["start_time"].(string); ok {
				entry.StartTime = st
			}
			if le, ok := w["last_edit"].(string); ok {
				entry.LastEdit = le
			}
			results = append(results, entry)
		}

		respondJSON(w, http.StatusOK, results)
		return
	}

	// Historical: read from the alerts:editwars stream
	since := time.Now().Add(-7 * 24 * time.Hour) // last 7 days
	historicalWars, err := s.alerts.GetEditWarAlertsSince(ctx, since, int64(limit*2)) // Fetch more to account for filtering
	if err != nil {
		s.logger.Error().Err(err).
			Str("request_id", GetRequestID(ctx)).
			Msg("failed to get historical edit wars")
		writeAPIError(w, r, http.StatusInternalServerError,
			"Failed to retrieve edit war history", ErrCodeInternalError, "")
		return
	}

	// Get currently active wars to exclude them from history
	activeWars, err := s.alerts.GetActiveEditWars(ctx, 1000) // Get all active wars
	if err != nil {
		s.logger.Warn().Err(err).Msg("failed to get active wars for filtering, continuing without filter")
		activeWars = []map[string]interface{}{} // Continue with empty filter
	}

	// Build set of active page titles
	activeTitles := make(map[string]bool)
	for _, w := range activeWars {
		if pt, ok := w["page_title"].(string); ok {
			activeTitles[pt] = true
		}
	}

	results := make([]EditWarEntry, 0, len(historicalWars))
	for _, w := range historicalWars {
		entry := EditWarEntry{Active: false}
		if pt, ok := w["page_title"].(string); ok {
			entry.PageTitle = pt
			// Skip if this war is currently active
			if activeTitles[pt] {
				continue
			}
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
		if le, ok := w["last_edit"].(string); ok {
			entry.LastEdit = le
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
		
		// Stop if we've reached the requested limit
		if len(results) >= limit {
			break
		}
	}

	respondJSON(w, http.StatusOK, results)
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

func (s *APIServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	params, parseErr := ParseSearchParams(r)
	if parseErr != nil {
		writeValidationError(w, r, parseErr)
		return
	}
	if verr := ValidateSearchParams(params); verr != nil {
		writeValidationError(w, r, verr)
		return
	}

	if s.es == nil || !s.config.Elasticsearch.Enabled {
		writeAPIError(w, r, http.StatusServiceUnavailable,
			"Search is not available (Elasticsearch disabled)", ErrCodeServiceUnavailable, "")
		return
	}

	// Check cache
	ck := cacheKey("search", params.Query, params.From.Format(time.RFC3339), params.To.Format(time.RFC3339),
		strconv.Itoa(params.Limit), strconv.Itoa(params.Offset), params.Language, params.Bot)
	if cached, ok := s.cache.Get(ck); ok {
		metrics.APICacheHitsTotal.WithLabelValues().Inc()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "max-age=10")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		w.Write(cached)
		return
	}
	metrics.APICacheMissesTotal.WithLabelValues().Inc()

	// Build Elasticsearch query
	searchQuery := s.buildSearchQuery(params.Query, params.From, params.To, params.Limit, params.Offset, params.Language, params.Bot)

	result, err := s.es.Search(searchQuery, "wikipedia-edits-*")
	if err != nil {
		if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "context deadline exceeded") {
			writeAPIError(w, r, http.StatusGatewayTimeout,
				"Search timed out", ErrCodeTimeout, "")
			return
		}
		s.logger.Error().Err(err).Str("query", params.Query).
			Str("request_id", GetRequestID(r.Context())).
			Msg("Elasticsearch search failed")
		writeAPIError(w, r, http.StatusInternalServerError,
			"Search failed", ErrCodeInternalError, "")
		return
	}

	// Parse ES response
	resp := s.parseSearchResponse(result, params.Query, params.Limit, params.Offset)

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
						// Derive server_url from wiki field (e.g., "zhwiki" -> "https://zh.wikipedia.org")
						if lang := strings.TrimSuffix(v, "wiki"); lang != "" {
							hit.ServerURL = fmt.Sprintf("https://%s.wikipedia.org", lang)
						}
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

// extractLanguageFromURL extracts a language code from a Wikipedia server URL.
// For example, "https://en.wikipedia.org" returns "en", "https://zh.wikipedia.org" returns "zh".
func extractLanguageFromURL(serverURL string) string {
	if serverURL == "" {
		return ""
	}
	// Strip protocol
	u := strings.TrimPrefix(serverURL, "https://")
	u = strings.TrimPrefix(u, "http://")
	// Extract subdomain before ".wikipedia.org"
	if idx := strings.Index(u, ".wikipedia.org"); idx > 0 {
		return u[:idx]
	}
	return ""
}
