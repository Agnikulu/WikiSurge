package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Digest data types
// ---------------------------------------------------------------------------

// DigestData is the complete data payload for one digest email.
type DigestData struct {
	Period           string            `json:"period"` // "daily" or "weekly"
	PeriodStart      time.Time         `json:"period_start"`
	PeriodEnd        time.Time         `json:"period_end"`
	GlobalHighlights []GlobalHighlight `json:"global_highlights"`
	EditWarHighlights  []GlobalHighlight `json:"edit_war_highlights"`
	TrendingHighlights []GlobalHighlight `json:"trending_highlights"`
	WatchlistEvents  []WatchlistEvent  `json:"watchlist_events"`
	Stats            FunStats          `json:"stats"`
}

// GlobalHighlight is a top event visible to all users.
type GlobalHighlight struct {
	Rank       int     `json:"rank"`
	Title      string  `json:"title"`
	EditCount  int     `json:"edit_count"`
	EventType  string  `json:"event_type"` // "spike", "edit_war", "trending"
	SpikeRatio float64 `json:"spike_ratio,omitempty"`
	Summary    string  `json:"summary"`
	ServerURL  string  `json:"server_url,omitempty"`

	// Edit war detail fields (populated from LLM analysis + Redis state)
	EditorCount int      `json:"editor_count,omitempty"`
	Editors     []string `json:"editors,omitempty"`
	RevertCount int      `json:"revert_count,omitempty"`
	Severity    string   `json:"severity,omitempty"`    // "low", "moderate", "high", "critical"
	LLMSummary  string   `json:"llm_summary,omitempty"` // LLM-generated conflict narrative
	ContentArea string   `json:"content_area,omitempty"` // topic area of disagreement
}

// WatchlistEvent is a per-user event for a page they track.
type WatchlistEvent struct {
	Title      string  `json:"title"`
	EditCount  int     `json:"edit_count"`
	IsNotable  bool    `json:"is_notable"`
	SpikeRatio float64 `json:"spike_ratio,omitempty"`
	EventType  string  `json:"event_type"` // "spike", "edit_war", "quiet", "trending"
	Summary    string  `json:"summary"`
}

// FunStats contains aggregate numbers shown at the bottom of the digest.
type FunStats struct {
	TotalEdits   int64          `json:"total_edits"`
	EditWars     int            `json:"edit_wars"`
	TopLanguages []LanguageStat `json:"top_languages"`
}

// LanguageStat is a language with its percentage share.
type LanguageStat struct {
	Language   string  `json:"language"`
	Count      int64   `json:"count"`
	Percentage float64 `json:"percentage"`
}

// ---------------------------------------------------------------------------
// Collector
// ---------------------------------------------------------------------------

// EditWarAnalyzeFunc is a function that generates an edit war analysis on
// demand. The collector only needs the side effect (result cached in Redis);
// the return value is ignored. Assign llm.AnalysisService.Analyze (wrapped)
// to keep the digest package decoupled from the LLM implementation.
type EditWarAnalyzeFunc func(ctx context.Context, pageTitle string) error

// Collector gathers digest data from Redis storage layers.
type Collector struct {
	trending *storage.TrendingScorer
	alerts   *storage.RedisAlerts
	hotPages *storage.HotPageTracker
	stats    *storage.StatsTracker
	redis    *redis.Client
	analyzer EditWarAnalyzeFunc // optional: generates analysis on cache miss
	logger   zerolog.Logger
}

// NewCollector creates a digest data collector.
func NewCollector(
	trending *storage.TrendingScorer,
	alerts *storage.RedisAlerts,
	hotPages *storage.HotPageTracker,
	stats *storage.StatsTracker,
	logger zerolog.Logger,
) *Collector {
	return &Collector{
		trending: trending,
		alerts:   alerts,
		hotPages: hotPages,
		stats:    stats,
		logger:   logger.With().Str("component", "digest-collector").Logger(),
	}
}

// NewCollectorWithRedis creates a digest data collector with direct Redis access
// for fetching cached LLM analysis data for edit war enrichment.
func NewCollectorWithRedis(
	trending *storage.TrendingScorer,
	alerts *storage.RedisAlerts,
	hotPages *storage.HotPageTracker,
	stats *storage.StatsTracker,
	redisClient *redis.Client,
	logger zerolog.Logger,
) *Collector {
	return &Collector{
		trending: trending,
		alerts:   alerts,
		hotPages: hotPages,
		stats:    stats,
		redis:    redisClient,
		logger:   logger.With().Str("component", "digest-collector").Logger(),
	}
}

// SetAnalyzer attaches an optional LLM analysis function so the collector can
// generate fresh analysis on cache misses during digest enrichment.
func (c *Collector) SetAnalyzer(fn EditWarAnalyzeFunc) {
	c.analyzer = fn
}

// CollectGlobal gathers data that is shared across all users in a digest.
// This should be called once per digest run, not per-user.
func (c *Collector) CollectGlobal(ctx context.Context, period string) (*DigestData, error) {
	now := time.Now().UTC()
	var since time.Time
	switch period {
	case "daily":
		since = now.Add(-24 * time.Hour)
	case "weekly":
		since = now.Add(-7 * 24 * time.Hour)
	default:
		return nil, fmt.Errorf("invalid period: %q (want daily or weekly)", period)
	}

	data := &DigestData{
		Period:      period,
		PeriodStart: since,
		PeriodEnd:   now,
	}

	// --- Global highlights ---
	highlights, err := c.collectHighlights(ctx, since)
	if err != nil {
		c.logger.Warn().Err(err).Msg("failed to collect highlights, continuing with empty")
	}
	data.GlobalHighlights = highlights

	// --- Split into edit wars vs trending ---
	for i := range highlights {
		switch highlights[i].EventType {
		case "edit_war":
			data.EditWarHighlights = append(data.EditWarHighlights, highlights[i])
		default:
			data.TrendingHighlights = append(data.TrendingHighlights, highlights[i])
		}
	}

	// --- Enrich edit wars with LLM analysis and editor data ---
	c.enrichEditWars(ctx, data.EditWarHighlights)

	// --- Fun stats ---
	funStats, err := c.collectFunStats(ctx, since)
	if err != nil {
		c.logger.Warn().Err(err).Msg("failed to collect fun stats, continuing with empty")
	}
	data.Stats = funStats

	return data, nil
}

// PersonalizeForUser enriches a DigestData with watchlist-specific events for one user.
// It returns a copy — the original global data is not mutated.
func (c *Collector) PersonalizeForUser(ctx context.Context, global *DigestData, user *models.User) *DigestData {
	personalized := *global // shallow copy
	personalized.WatchlistEvents = c.collectWatchlistEvents(ctx, user, global)
	return &personalized
}

// ShouldSendToUser decides if a digest is worth sending based on user's threshold.
// Returns true if there's notable watchlist activity OR global highlights exist.
func (c *Collector) ShouldSendToUser(data *DigestData, user *models.User) bool {
	// Always send if user wants global content
	if user.DigestContent == models.DigestContentGlobal || user.DigestContent == models.DigestContentAll {
		if len(data.GlobalHighlights) > 0 {
			return true
		}
	}

	// Check watchlist events against threshold
	if user.DigestContent == models.DigestContentWatchlist || user.DigestContent == models.DigestContentAll {
		for _, ev := range data.WatchlistEvents {
			if ev.IsNotable && ev.SpikeRatio >= user.SpikeThreshold {
				return true
			}
		}
	}

	return false
}

// ---------------------------------------------------------------------------
// Internal collection methods
// ---------------------------------------------------------------------------

func (c *Collector) collectHighlights(ctx context.Context, since time.Time) ([]GlobalHighlight, error) {
	// Gather spikes and edit wars from alert streams
	seen := make(map[string]*GlobalHighlight) // dedupe by title

	// 1. Edit war alerts
	editWars, err := c.alerts.GetAlertsSince(ctx, "editwars", since, "", 100)
	if err != nil {
		c.logger.Warn().Err(err).Msg("could not fetch edit war alerts")
	}
	for _, a := range editWars {
		title := stringFromData(a.Data, "title")
		if title == "" {
			title = stringFromData(a.Data, "page_title")
		}
		if title == "" {
			continue
		}
		alertEditorCount := intFromData(a.Data, "editor_count")
		alertRevertCount := intFromData(a.Data, "revert_count")
		alertEditCount := intFromData(a.Data, "edit_count")
		alertSeverity := stringFromData(a.Data, "severity")
		alertLLMSummary := stringFromData(a.Data, "llm_summary")
		alertContentArea := stringFromData(a.Data, "content_area")
		// Extract editors array from alert data
		var alertEditors []string
		if rawEditors, ok := a.Data["editors"]; ok {
			if edArr, ok := rawEditors.([]interface{}); ok {
				for _, e := range edArr {
					if s, ok := e.(string); ok {
						alertEditors = append(alertEditors, s)
					}
				}
			}
		}

		if existing, ok := seen[title]; ok {
			// Upgrade to edit_war type and use highest counts
			existing.EventType = "edit_war"
			existing.Summary = "Edit war detected"
			if alertEditCount > existing.EditCount {
				existing.EditCount = alertEditCount
			}
			if alertEditorCount > existing.EditorCount {
				existing.EditorCount = alertEditorCount
				existing.Editors = alertEditors
			}
			if alertRevertCount > existing.RevertCount {
				existing.RevertCount = alertRevertCount
			}
			if alertSeverity != "" {
				existing.Severity = alertSeverity
			}
			// Prefer LLM data from final snapshot (most recent entry wins)
			if alertLLMSummary != "" {
				existing.LLMSummary = alertLLMSummary
			}
			if alertContentArea != "" {
				existing.ContentArea = alertContentArea
			}
		} else {
			seen[title] = &GlobalHighlight{
				Title:       title,
				EditCount:   alertEditCount,
				EventType:   "edit_war",
				Summary:     "Edit war detected",
				ServerURL:   stringFromData(a.Data, "server_url"),
				EditorCount: alertEditorCount,
				Editors:     alertEditors,
				RevertCount: alertRevertCount,
				Severity:    alertSeverity,
				LLMSummary:  alertLLMSummary,
				ContentArea: alertContentArea,
			}
		}
	}

	// 3. Trending pages (fill remaining slots)
	if c.trending != nil {
		trending, err := c.trending.GetTopTrending(20)
		if err != nil {
			c.logger.Warn().Err(err).Msg("could not fetch trending pages")
		}
		for _, t := range trending {
			if _, exists := seen[t.PageTitle]; !exists {
				// Only add if trending entry is from our period
				if t.LastUpdated >= since.Unix() {
					var edits int64
					if c.hotPages != nil {
						stats, err := c.hotPages.GetPageStats(ctx, t.PageTitle)
						if err == nil && stats != nil {
							edits = stats.EditsLastHour
						}
					}
					seen[t.PageTitle] = &GlobalHighlight{
						Title:     t.PageTitle,
						EditCount: int(edits),
						EventType: "trending",
						Summary:   fmt.Sprintf("Trending (score: %.0f)", t.CurrentScore),
						ServerURL: t.ServerURL,
					}
				}
			}
		}
	}

	// Convert to sorted slice — highest edit count first
	highlights := make([]GlobalHighlight, 0, len(seen))
	for _, h := range seen {
		highlights = append(highlights, *h)
	}
	sort.Slice(highlights, func(i, j int) bool {
		// Edit wars first, then by edit count
		if highlights[i].EventType != highlights[j].EventType {
			priority := map[string]int{"edit_war": 0, "trending": 1}
			return priority[highlights[i].EventType] < priority[highlights[j].EventType]
		}
		return highlights[i].EditCount > highlights[j].EditCount
	})

	// Cap at top 10 (up to 5 edit wars + 5 trending shown in separate sections)
	if len(highlights) > 10 {
		highlights = highlights[:10]
	}

	// Assign ranks
	for i := range highlights {
		highlights[i].Rank = i + 1
	}

	return highlights, nil
}

// enrichEditWars enriches edit war highlights with LLM analysis data, editor
// info, and revert counts from Redis. This adds the detailed information that
// appears on the edit war details page into the digest email.
func (c *Collector) enrichEditWars(ctx context.Context, wars []GlobalHighlight) {
	if c.redis == nil || len(wars) == 0 {
		return
	}

	for i := range wars {
		title := wars[i].Title

		// 1. Fetch cached LLM analysis (or regenerate on miss)
		cacheKey := fmt.Sprintf("editwar:analysis:%s", title)
		cached, err := c.redis.Get(ctx, cacheKey).Result()

		// If cache is empty and we have an analyzer, generate fresh analysis.
		// The Analyze call caches its result in Redis, so re-read afterwards.
		if (err != nil || cached == "") && c.analyzer != nil {
			analyzeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if aErr := c.analyzer(analyzeCtx, title); aErr != nil {
				c.logger.Warn().Err(aErr).Str("page", title).Msg("on-demand LLM analysis failed for digest")
			} else {
				c.logger.Info().Str("page", title).Msg("generated fresh LLM analysis for digest")
				// Re-read the now-populated cache
				cached, err = c.redis.Get(ctx, cacheKey).Result()
			}
			cancel()
		}

		if err == nil && cached != "" {
			var analysis struct {
				Summary     string `json:"summary"`
				ContentArea string `json:"content_area"`
				Severity    string `json:"severity"`
				EditCount   int    `json:"edit_count"`
				Sides       []struct {
					Position string `json:"position"`
					Editors  []struct {
						User      string `json:"user"`
						EditCount int    `json:"edit_count"`
						Role      string `json:"role"`
					} `json:"editors"`
				} `json:"sides"`
			}
			if json.Unmarshal([]byte(cached), &analysis) == nil {
				if analysis.Summary != "" {
					wars[i].LLMSummary = analysis.Summary
				}
				if analysis.ContentArea != "" {
					wars[i].ContentArea = analysis.ContentArea
				}
				if analysis.Severity != "" {
					wars[i].Severity = analysis.Severity
				}
				if analysis.EditCount > 0 && wars[i].EditCount == 0 {
					wars[i].EditCount = analysis.EditCount
				}
				// Collect unique editors from LLM analysis sides and add them
				// to wars[i].Editors so they get merged with hash editors below.
				for _, side := range analysis.Sides {
					for _, ed := range side.Editors {
						wars[i].Editors = append(wars[i].Editors, ed.User)
					}
				}
				// Note: LLM editors are merged with hash editors below
			}
		}

		// 2. Always merge editors from the tracking hash — the hash has every
		// editor who participated, while the LLM analysis typically only
		// mentions a handful of prominent ones.
		editorSet := make(map[string]bool)
		// Start with any editors already set (from alert stream data)
		for _, ed := range wars[i].Editors {
			editorSet[ed] = true
		}
		editorsKey := fmt.Sprintf("editwar:editors:%s", title)
		editorMap, err := c.redis.HGetAll(ctx, editorsKey).Result()
		if err == nil && len(editorMap) > 0 {
			for editor := range editorMap {
				editorSet[editor] = true
			}
		}
		if len(editorSet) > 0 {
			editors := make([]string, 0, len(editorSet))
			for ed := range editorSet {
				editors = append(editors, ed)
			}
			sort.Strings(editors)
			wars[i].Editors = editors
			wars[i].EditorCount = len(editors)
		}

		// 3. Count reverts from changes list using proper alternating-sign
		// pattern detection (matching the edit war detector's algorithm).
		// The existing RevertCount from alert stream data is used as a floor.
		alertRevertCount := wars[i].RevertCount
		changesKey := fmt.Sprintf("editwar:changes:%s", title)
		changes, err := c.redis.LRange(ctx, changesKey, 0, -1).Result()
		if err == nil && len(changes) >= 2 {
			revertCount := countRevertsFromChanges(changes)
			// Use whichever source gives a higher count — the alert stream
			// captured the count at detection time, but the changes list may
			// have accumulated more reverts since then.
			if revertCount > alertRevertCount {
				wars[i].RevertCount = revertCount
			}
		}

		// 4. Update the summary to use LLM summary if available
		if wars[i].LLMSummary != "" {
			wars[i].Summary = wars[i].LLMSummary
		} else if wars[i].EditorCount > 0 {
			wars[i].Summary = fmt.Sprintf("Edit war with %d editors involved", wars[i].EditorCount)
		}

		c.logger.Debug().
			Str("title", title).
			Int("editor_count", wars[i].EditorCount).
			Str("severity", wars[i].Severity).
			Bool("has_llm_summary", wars[i].LLMSummary != "").
			Msg("enriched edit war for digest")
	}

	// 5. Final pass: fill any remaining gaps from the digest archive.
	//    This covers weekly digests where the ephemeral cache AND timeline
	//    have both expired — the archive sorted sets survive for 8 days.
	c.fillFromDigestArchive(ctx, wars)
}

// fillFromDigestArchive looks up the durable digest archive sorted sets for any
// edit war entries that are still missing an LLM summary after the primary
// enrichment pass. This is the safety net for weekly digests where ephemeral
// Redis keys have long expired.
func (c *Collector) fillFromDigestArchive(ctx context.Context, wars []GlobalHighlight) {
	if c.redis == nil {
		return
	}

	// Collect titles that still need enrichment.
	needsLLM := make(map[string]int) // title → index in wars slice
	for i := range wars {
		if wars[i].LLMSummary == "" {
			needsLLM[wars[i].Title] = i
		}
	}
	if len(needsLLM) == 0 {
		return
	}

	// Scan the last 8 days of archive hash keys.
	for d := 0; d < 8; d++ {
		dateStr := time.Now().UTC().AddDate(0, 0, -d).Format("2006-01-02")
		hashKey := fmt.Sprintf("digest:war_analyses:%s:data", dateStr)

		for title, idx := range needsLLM {
			raw, err := c.redis.HGet(ctx, hashKey, title).Result()
			if err != nil || raw == "" {
				continue
			}

			var analysis struct {
				Summary     string `json:"summary"`
				ContentArea string `json:"content_area"`
				Severity    string `json:"severity"`
				EditCount   int    `json:"edit_count"`
				Sides       []struct {
					Position string `json:"position"`
					Editors  []struct {
						User      string `json:"user"`
						EditCount int    `json:"edit_count"`
						Role      string `json:"role"`
					} `json:"editors"`
				} `json:"sides"`
			}
			if json.Unmarshal([]byte(raw), &analysis) != nil {
				continue
			}

			if analysis.Summary != "" {
				wars[idx].LLMSummary = analysis.Summary
				wars[idx].Summary = analysis.Summary
			}
			if analysis.ContentArea != "" && wars[idx].ContentArea == "" {
				wars[idx].ContentArea = analysis.ContentArea
			}
			if analysis.Severity != "" && wars[idx].Severity == "" {
				wars[idx].Severity = analysis.Severity
			}
			if analysis.EditCount > 0 && wars[idx].EditCount == 0 {
				wars[idx].EditCount = analysis.EditCount
			}
			// Fill editors from analysis sides if we have none
			if wars[idx].EditorCount == 0 {
				editorSet := make(map[string]bool)
				for _, side := range analysis.Sides {
					for _, ed := range side.Editors {
						editorSet[ed.User] = true
					}
				}
				if len(editorSet) > 0 {
					editors := make([]string, 0, len(editorSet))
					for ed := range editorSet {
						editors = append(editors, ed)
					}
					sort.Strings(editors)
					wars[idx].Editors = editors
					wars[idx].EditorCount = len(editors)
				}
			}

			c.logger.Info().
				Str("title", title).
				Str("archive_date", dateStr).
				Msg("recovered LLM analysis from digest archive")

			// Found it — remove from needsLLM so we don't keep searching.
			delete(needsLLM, title)
		}

		if len(needsLLM) == 0 {
			break
		}
	}
}

func (c *Collector) collectFunStats(ctx context.Context, since time.Time) (FunStats, error) {
	stats := FunStats{}

	// Total edits for the period (aggregates across all date keys)
	total, err := c.stats.GetEditCountForPeriod(ctx, since)
	if err != nil {
		c.logger.Warn().Err(err).Msg("could not get edit count for period")
	}
	stats.TotalEdits = total

	// Language breakdown for the period
	langCounts, langTotal, err := c.stats.GetLanguageCountsForPeriod(ctx, since)
	if err != nil {
		c.logger.Warn().Err(err).Msg("could not get language counts for period")
	}
	for _, lc := range langCounts {
		pct := 0.0
		if langTotal > 0 {
			pct = math.Round(float64(lc.Count)/float64(langTotal)*1000) / 10 // one decimal
		}
		stats.TopLanguages = append(stats.TopLanguages, LanguageStat{
			Language:   lc.Language,
			Count:      lc.Count,
			Percentage: pct,
		})
	}
	if len(stats.TopLanguages) > 5 {
		stats.TopLanguages = stats.TopLanguages[:5]
	}

	// Count edit wars from alert stream within the digest period
	editWars, err := c.alerts.GetAlertsSince(ctx, "editwars", since, "", 1000)
	if err != nil {
		c.logger.Warn().Err(err).Msg("could not get edit war alerts for stats")
	} else {
		// Deduplicate by title to count unique edit wars
		ewSeen := make(map[string]bool)
		for _, a := range editWars {
			title := stringFromData(a.Data, "title")
			if title == "" {
				title = stringFromData(a.Data, "page_title")
			}
			ewSeen[title] = true
		}
		stats.EditWars = len(ewSeen)
	}

	return stats, nil
}

func (c *Collector) collectWatchlistEvents(ctx context.Context, user *models.User, global *DigestData) []WatchlistEvent {
	if len(user.Watchlist) == 0 {
		return nil
	}

	// Build a set of global highlight titles for cross-referencing
	globalTitles := make(map[string]*GlobalHighlight)
	for i := range global.GlobalHighlights {
		globalTitles[global.GlobalHighlights[i].Title] = &global.GlobalHighlights[i]
	}

	events := make([]WatchlistEvent, 0, len(user.Watchlist))

	for _, pageTitle := range user.Watchlist {
		ev := WatchlistEvent{
			Title:     pageTitle,
			EventType: "quiet",
		}

		// Check if this page appeared in global highlights
		if gh, ok := globalTitles[pageTitle]; ok {
			ev.EditCount = gh.EditCount
			ev.SpikeRatio = gh.SpikeRatio
			ev.EventType = gh.EventType
			ev.IsNotable = true
			ev.Summary = gh.Summary
			events = append(events, ev)
			continue
		}

		// Otherwise query per-page daily edit counters (persistent, 8-day TTL)
		if c.stats != nil {
			editCount, err := c.stats.GetPageEditCount(ctx, pageTitle, global.PeriodStart)
			if err == nil && editCount > 0 {
				ev.EditCount = int(editCount)
				// Consider "notable" if > 10 edits in the period
				if editCount > 10 {
					ev.IsNotable = true
					ev.EventType = "active"
					ev.Summary = fmt.Sprintf("%d edits in the period", editCount)
				} else {
					ev.Summary = fmt.Sprintf("%d edits (quiet)", editCount)
				}
			} else {
				ev.Summary = "No recent activity"
			}
		}

		events = append(events, ev)
	}

	// Sort: notable first, then by edit count
	sort.Slice(events, func(i, j int) bool {
		if events[i].IsNotable != events[j].IsNotable {
			return events[i].IsNotable
		}
		return events[i].EditCount > events[j].EditCount
	})

	return events
}

// ---------------------------------------------------------------------------
// Data extraction helpers (alert.Data is map[string]interface{})
// ---------------------------------------------------------------------------

func stringFromData(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func floatFromData(data map[string]interface{}, key string) float64 {
	if v, ok := data[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
	}
	return 0
}

func intFromData(data map[string]interface{}, key string) int {
	if v, ok := data[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return 0
}

// countRevertsFromChanges detects reversions by analyzing byte change patterns,
// mirroring the algorithm from processor.EditWarDetector.countReverts().
// It looks for consecutive edits with opposite signs and similar magnitudes.
func countRevertsFromChanges(changesStr []string) int {
	if len(changesStr) < 2 {
		return 0
	}

	changes := make([]int, 0, len(changesStr))
	for _, cs := range changesStr {
		val, err := strconv.Atoi(cs)
		if err != nil {
			continue
		}
		changes = append(changes, val)
	}

	if len(changes) < 2 {
		return 0
	}

	revertCount := 0
	for i := 1; i < len(changes); i++ {
		prev := changes[i-1]
		curr := changes[i]

		// Two consecutive zero-byte edits often indicate reverts
		if prev == 0 && curr == 0 {
			revertCount++
			continue
		}

		// Check for opposite signs
		if (prev > 0 && curr < 0) || (prev < 0 && curr > 0) {
			// For very small edits, be lenient
			absPrev := math.Abs(float64(prev))
			absCurr := math.Abs(float64(curr))

			if absPrev < 10 && absCurr < 10 {
				revertCount++
				continue
			}

			// If magnitudes are within 40% of each other, likely a revert
			ratio := absCurr / absPrev
			if ratio > 1 {
				ratio = absPrev / absCurr
			}
			if ratio >= 0.6 {
				revertCount++
			}
		}
	}

	return revertCount
}
