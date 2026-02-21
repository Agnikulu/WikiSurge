package digest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
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

// Collector gathers digest data from Redis storage layers.
type Collector struct {
	trending *storage.TrendingScorer
	alerts   *storage.RedisAlerts
	hotPages *storage.HotPageTracker
	stats    *storage.StatsTracker
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
		if existing, ok := seen[title]; ok {
			// Upgrade to edit_war type if it was just a spike
			existing.EventType = "edit_war"
			existing.Summary = "Edit war detected"
		} else {
			seen[title] = &GlobalHighlight{
				Title:     title,
				EditCount: intFromData(a.Data, "change_volume"),
				EventType: "edit_war",
				Summary:   "Edit war detected",
				ServerURL: stringFromData(a.Data, "server_url"),
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

	// Cap at top 5
	if len(highlights) > 5 {
		highlights = highlights[:5]
	}

	// Assign ranks
	for i := range highlights {
		highlights[i].Rank = i + 1
	}

	return highlights, nil
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

	// Count edit wars from alert streams
	alertStats, err := c.alerts.GetAlertStats(ctx, []string{"editwars"})
	if err != nil {
		c.logger.Warn().Err(err).Msg("could not get alert stats")
	} else {
		if s, ok := alertStats["editwars"]; ok {
			stats.EditWars = int(s.Length)
		}
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

		// Otherwise check hot page stats
		if c.hotPages != nil {
			stats, err := c.hotPages.GetPageStats(ctx, pageTitle)
			if err == nil && stats != nil {
				ev.EditCount = int(stats.EditsLastHour)
				// Consider "notable" if > 10 edits/hour (simple heuristic)
				if stats.EditsLastHour > 10 {
					ev.IsNotable = true
					ev.EventType = "active"
					ev.Summary = fmt.Sprintf("%d edits in the last hour", stats.EditsLastHour)
				} else {
					ev.Summary = fmt.Sprintf("%d edits (quiet)", stats.EditsLastHour)
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
