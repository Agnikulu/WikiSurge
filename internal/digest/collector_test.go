package digest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func setupTestCollector(t *testing.T) (*Collector, *redis.Client, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rc.Close() })

	trendingCfg := &config.TrendingConfig{
		Enabled: true, MaxPages: 100, HalfLifeMinutes: 30, PruneInterval: 5 * time.Minute,
	}
	hotPagesCfg := &config.HotPages{
		MaxTracked: 100, PromotionThreshold: 3, WindowDuration: 15 * time.Minute,
		MaxMembersPerPage: 50, HotThreshold: 2, CleanupInterval: 5 * time.Minute,
	}

	trending := storage.NewTrendingScorerForTest(rc, trendingCfg)
	hotPages := storage.NewHotPageTracker(rc, hotPagesCfg)
	alerts := storage.NewRedisAlerts(rc)
	stats := storage.NewStatsTracker(rc)

	logger := zerolog.Nop()
	collector := NewCollector(trending, alerts, hotPages, stats, logger)

	t.Cleanup(func() {
		trending.Stop()
		hotPages.Shutdown()
	})

	return collector, rc, mr
}

// seedSpikeAlert publishes a spike alert to the test Redis instance.
func seedSpikeAlert(t *testing.T, rc *redis.Client, title string, spikeRatio float64, editCount int) {
	t.Helper()
	ctx := context.Background()

	alert := storage.Alert{
		ID:        "spike-" + title,
		Type:      "spike",
		Timestamp: time.Now().UTC(),
		Data: map[string]interface{}{
			"title":       title,
			"spike_ratio": spikeRatio,
			"edit_count":  editCount,
			"server_url":  "https://en.wikipedia.org",
		},
	}
	alertJSON, _ := json.Marshal(alert)

	rc.XAdd(ctx, &redis.XAddArgs{
		Stream: "alerts:spikes",
		Values: map[string]interface{}{
			"alert_data": string(alertJSON),
			"type":       "spike",
			"timestamp":  alert.Timestamp.Unix(),
		},
	})
}

// seedEditWarAlert publishes an edit war alert to the test Redis instance.
func seedEditWarAlert(t *testing.T, rc *redis.Client, title string, changeVolume int) {
	t.Helper()
	ctx := context.Background()

	alert := storage.Alert{
		ID:        "editwar-" + title,
		Type:      "edit_war",
		Timestamp: time.Now().UTC(),
		Data: map[string]interface{}{
			"title":      title,
			"page_title": title,
			"edit_count": changeVolume,
			"server_url": "https://en.wikipedia.org",
		},
	}
	alertJSON, _ := json.Marshal(alert)

	rc.XAdd(ctx, &redis.XAddArgs{
		Stream: "alerts:editwars",
		Values: map[string]interface{}{
			"alert_data": string(alertJSON),
			"type":       "edit_war",
			"timestamp":  alert.Timestamp.Unix(),
		},
	})
}

// seedStats populates some language/edit stats.
func seedStats(t *testing.T, rc *redis.Client) {
	t.Helper()
	ctx := context.Background()

	dateStr := time.Now().UTC().Format("2006-01-02")
	langKey := "stats:languages:" + dateStr
	rc.HSet(ctx, langKey, "en", 50000)
	rc.HSet(ctx, langKey, "es", 8000)
	rc.HSet(ctx, langKey, "ja", 6000)
	rc.HSet(ctx, langKey, "__total__", 64000)
}

// seedPageEdits populates per-page daily counters for the given page across days.
func seedPageEdits(t *testing.T, rc *redis.Client, title string, editsPerDay []int) {
	t.Helper()
	ctx := context.Background()

	for i, edits := range editsPerDay {
		d := time.Now().UTC().Add(-time.Duration(i) * 24 * time.Hour)
		dateStr := d.Format("2006-01-02")
		key := "stats:page:" + title + ":" + dateStr
		rc.HSet(ctx, key, "edits", edits)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCollectGlobal_Empty(t *testing.T) {
	collector, _, _ := setupTestCollector(t)
	ctx := context.Background()

	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if data.Period != "daily" {
		t.Errorf("period = %q, want daily", data.Period)
	}
	if data.PeriodEnd.Before(data.PeriodStart) {
		t.Error("period_end should be after period_start")
	}
	// Empty Redis → no highlights, no stats (zero values)
	if len(data.GlobalHighlights) != 0 {
		t.Errorf("expected 0 highlights, got %d", len(data.GlobalHighlights))
	}
}

func TestCollectGlobal_InvalidPeriod(t *testing.T) {
	collector, _, _ := setupTestCollector(t)
	ctx := context.Background()

	_, err := collector.CollectGlobal(ctx, "hourly")
	if err == nil {
		t.Error("expected error for invalid period")
	}
}

func TestCollectGlobal_WithEditWarsMultiple(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	// Seed edit war alerts
	seedEditWarAlert(t, rc, "Bitcoin", 200)
	seedEditWarAlert(t, rc, "Ethereum", 150)
	seedEditWarAlert(t, rc, "OpenAI", 500)

	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if len(data.GlobalHighlights) == 0 {
		t.Fatal("expected at least 1 highlight")
	}
	if len(data.GlobalHighlights) > 5 {
		t.Errorf("expected at most 5 highlights, got %d", len(data.GlobalHighlights))
	}

	// Top highlight should be OpenAI (highest edit count)
	top := data.GlobalHighlights[0]
	if top.Title != "OpenAI" {
		t.Errorf("top highlight = %q, want OpenAI", top.Title)
	}
	if top.Rank != 1 {
		t.Errorf("top rank = %d, want 1", top.Rank)
	}
	if top.EventType != "edit_war" {
		t.Errorf("top event_type = %q, want edit_war", top.EventType)
	}
}

func TestCollectGlobal_WithEditWars(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	seedEditWarAlert(t, rc, "Bitcoin", 50)

	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if len(data.GlobalHighlights) == 0 {
		t.Fatal("expected at least 1 highlight")
	}

	// Bitcoin should appear as edit_war
	found := false
	for _, h := range data.GlobalHighlights {
		if h.Title == "Bitcoin" {
			found = true
			if h.EventType != "edit_war" {
				t.Errorf("Bitcoin event_type = %q, want edit_war", h.EventType)
			}
		}
	}
	if !found {
		t.Error("Bitcoin not found in highlights")
	}
}

func TestCollectGlobal_WithStats(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	seedStats(t, rc)

	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if data.Stats.TotalEdits != 64000 {
		t.Errorf("total edits = %d, want 64000", data.Stats.TotalEdits)
	}
	if len(data.Stats.TopLanguages) == 0 {
		t.Fatal("expected language stats")
	}
	if data.Stats.TopLanguages[0].Language != "en" {
		t.Errorf("top language = %q, want en", data.Stats.TopLanguages[0].Language)
	}
}

func TestPersonalizeForUser_EmptyWatchlist(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	seedEditWarAlert(t, rc, "Bitcoin", 200)
	global, _ := collector.CollectGlobal(ctx, "daily")

	user := &models.User{
		ID:            "user-1",
		Watchlist:     []string{},
		DigestContent: models.DigestContentAll,
	}

	personalized := collector.PersonalizeForUser(ctx, global, user)
	if len(personalized.WatchlistEvents) != 0 {
		t.Errorf("expected 0 watchlist events, got %d", len(personalized.WatchlistEvents))
	}
	// Global highlights should still be there
	if len(personalized.GlobalHighlights) != len(global.GlobalHighlights) {
		t.Error("personalized should include global highlights")
	}
}

func TestPersonalizeForUser_WatchlistMatchesAlert(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	seedEditWarAlert(t, rc, "Bitcoin", 200)
	seedEditWarAlert(t, rc, "Dogecoin", 100)
	global, _ := collector.CollectGlobal(ctx, "daily")

	user := &models.User{
		ID:             "user-1",
		Watchlist:      []string{"Bitcoin", "Taylor Swift"},
		DigestContent:  models.DigestContentAll,
		SpikeThreshold: 2.0,
	}

	personalized := collector.PersonalizeForUser(ctx, global, user)
	if len(personalized.WatchlistEvents) != 2 {
		t.Fatalf("expected 2 watchlist events, got %d", len(personalized.WatchlistEvents))
	}

	// Bitcoin should be notable (matched edit war alert)
	btcFound := false
	for _, ev := range personalized.WatchlistEvents {
		if ev.Title == "Bitcoin" {
			btcFound = true
			if !ev.IsNotable {
				t.Error("Bitcoin should be notable")
			}
			if ev.EventType != "edit_war" {
				t.Errorf("Bitcoin event_type = %q, want edit_war", ev.EventType)
			}
		}
	}
	if !btcFound {
		t.Error("Bitcoin not found in watchlist events")
	}
}

func TestShouldSendToUser_GlobalContent(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	seedEditWarAlert(t, rc, "Bitcoin", 200)
	global, _ := collector.CollectGlobal(ctx, "daily")

	user := &models.User{
		DigestContent:  models.DigestContentGlobal,
		SpikeThreshold: 2.0,
	}

	personalized := collector.PersonalizeForUser(ctx, global, user)
	if !collector.ShouldSendToUser(personalized, user) {
		t.Error("should send when there are global highlights and user wants global content")
	}
}

func TestShouldSendToUser_WatchlistThreshold(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	seedEditWarAlert(t, rc, "Bitcoin", 10)
	global, _ := collector.CollectGlobal(ctx, "daily")

	user := &models.User{
		Watchlist:      []string{"Bitcoin"},
		DigestContent:  models.DigestContentWatchlist,
		SpikeThreshold: 3.0, // threshold higher than spike ratio (edit wars have 0 spike ratio)
	}

	personalized := collector.PersonalizeForUser(ctx, global, user)
	if collector.ShouldSendToUser(personalized, user) {
		t.Error("should NOT send when spike ratio is below user's threshold and user only wants watchlist")
	}
}

func TestShouldSendToUser_EmptyData(t *testing.T) {
	collector, _, _ := setupTestCollector(t)
	ctx := context.Background()

	global, _ := collector.CollectGlobal(ctx, "daily")

	user := &models.User{
		DigestContent:  models.DigestContentAll,
		SpikeThreshold: 2.0,
	}

	personalized := collector.PersonalizeForUser(ctx, global, user)
	if collector.ShouldSendToUser(personalized, user) {
		t.Error("should NOT send when there's no data at all")
	}
}

func TestHighlightsCappedAtFive(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	// Seed many edit war alerts
	for i := 0; i < 10; i++ {
		seedEditWarAlert(t, rc, time.Now().Format("Page")+string(rune('A'+i)), (i+1)*100)
	}

	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if len(data.GlobalHighlights) > 5 {
		t.Errorf("highlights should be capped at 5, got %d", len(data.GlobalHighlights))
	}
}

func TestWeeklyPeriod(t *testing.T) {
	collector, _, _ := setupTestCollector(t)
	ctx := context.Background()

	data, err := collector.CollectGlobal(ctx, "weekly")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	expectedDuration := 7 * 24 * time.Hour
	actualDuration := data.PeriodEnd.Sub(data.PeriodStart)

	// Allow 1 second tolerance
	if actualDuration < expectedDuration-time.Second || actualDuration > expectedDuration+time.Second {
		t.Errorf("weekly period duration = %v, want ~%v", actualDuration, expectedDuration)
	}
}

// ---------------------------------------------------------------------------
// Watchlist per-page daily counter tests
// ---------------------------------------------------------------------------

func TestWatchlist_PageCountersActive(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	// Seed page edits: 25 edits today → should be "active" (> 10)
	seedPageEdits(t, rc, "Bitcoin", []int{25})

	global, _ := collector.CollectGlobal(ctx, "daily")

	user := &models.User{
		ID:            "user-1",
		Watchlist:     []string{"Bitcoin"},
		DigestContent: models.DigestContentAll,
	}

	personalized := collector.PersonalizeForUser(ctx, global, user)
	if len(personalized.WatchlistEvents) != 1 {
		t.Fatalf("expected 1 watchlist event, got %d", len(personalized.WatchlistEvents))
	}

	ev := personalized.WatchlistEvents[0]
	if ev.Title != "Bitcoin" {
		t.Errorf("title = %q, want Bitcoin", ev.Title)
	}
	if ev.EditCount != 25 {
		t.Errorf("edit_count = %d, want 25", ev.EditCount)
	}
	if !ev.IsNotable {
		t.Error("should be notable (25 edits > 10)")
	}
	if ev.EventType != "active" {
		t.Errorf("event_type = %q, want active", ev.EventType)
	}
}

func TestWatchlist_PageCountersQuiet(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	// Seed 3 edits today — should be "quiet" (< 10)
	seedPageEdits(t, rc, "Knitting", []int{3})

	global, _ := collector.CollectGlobal(ctx, "daily")

	user := &models.User{
		ID:            "user-1",
		Watchlist:     []string{"Knitting"},
		DigestContent: models.DigestContentAll,
	}

	personalized := collector.PersonalizeForUser(ctx, global, user)
	if len(personalized.WatchlistEvents) != 1 {
		t.Fatalf("expected 1 watchlist event, got %d", len(personalized.WatchlistEvents))
	}

	ev := personalized.WatchlistEvents[0]
	if ev.IsNotable {
		t.Error("should NOT be notable (3 edits)")
	}
	if ev.EventType != "quiet" {
		t.Errorf("event_type = %q, want quiet", ev.EventType)
	}
	if ev.EditCount != 3 {
		t.Errorf("edit_count = %d, want 3", ev.EditCount)
	}
}

func TestWatchlist_PageCountersNoActivity(t *testing.T) {
	collector, _, _ := setupTestCollector(t)
	ctx := context.Background()

	// No data seeded for "Obscure_Article"
	global, _ := collector.CollectGlobal(ctx, "daily")

	user := &models.User{
		ID:            "user-1",
		Watchlist:     []string{"Obscure_Article"},
		DigestContent: models.DigestContentAll,
	}

	personalized := collector.PersonalizeForUser(ctx, global, user)
	if len(personalized.WatchlistEvents) != 1 {
		t.Fatalf("expected 1 watchlist event, got %d", len(personalized.WatchlistEvents))
	}

	ev := personalized.WatchlistEvents[0]
	if ev.EditCount != 0 {
		t.Errorf("edit_count = %d, want 0", ev.EditCount)
	}
	if ev.Summary != "No recent activity" {
		t.Errorf("summary = %q, want 'No recent activity'", ev.Summary)
	}
}

func TestWatchlist_WeeklyAggregatesMultipleDays(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	// Seed 7 days of edits: [10, 20, 15, 5, 30, 10, 5] from today backward
	seedPageEdits(t, rc, "Climate_change", []int{10, 20, 15, 5, 30, 10, 5})

	global, _ := collector.CollectGlobal(ctx, "weekly")

	user := &models.User{
		ID:            "user-1",
		Watchlist:     []string{"Climate_change"},
		DigestContent: models.DigestContentAll,
	}

	personalized := collector.PersonalizeForUser(ctx, global, user)
	if len(personalized.WatchlistEvents) != 1 {
		t.Fatalf("expected 1 watchlist event, got %d", len(personalized.WatchlistEvents))
	}

	ev := personalized.WatchlistEvents[0]
	// Total = 10+20+15+5+30+10+5 = 95
	if ev.EditCount != 95 {
		t.Errorf("edit_count = %d, want 95 (sum of 7 days)", ev.EditCount)
	}
	if !ev.IsNotable {
		t.Error("should be notable (95 edits > 10)")
	}
}

func TestWatchlist_GlobalHighlightTakesPrecedence(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	// Bitcoin appears as edit war AND has page counters
	seedEditWarAlert(t, rc, "Bitcoin", 200)
	seedPageEdits(t, rc, "Bitcoin", []int{50})

	global, _ := collector.CollectGlobal(ctx, "daily")

	user := &models.User{
		ID:            "user-1",
		Watchlist:     []string{"Bitcoin"},
		DigestContent: models.DigestContentAll,
	}

	personalized := collector.PersonalizeForUser(ctx, global, user)
	if len(personalized.WatchlistEvents) != 1 {
		t.Fatalf("expected 1 watchlist event, got %d", len(personalized.WatchlistEvents))
	}

	ev := personalized.WatchlistEvents[0]
	// Should use the global highlight data, NOT the page counter
	if ev.EventType != "edit_war" {
		t.Errorf("event_type = %q, want edit_war (from global highlight)", ev.EventType)
	}
	if ev.EditCount != 200 {
		t.Errorf("edit_count = %d, want 200 (from global highlight, not page counter)", ev.EditCount)
	}
}

func TestWatchlist_MixedPagesSort(t *testing.T) {
	collector, rc, _ := setupTestCollector(t)
	ctx := context.Background()

	// One page is edit war (notable from global), one is active, one is quiet
	seedEditWarAlert(t, rc, "Bitcoin", 200)
	seedPageEdits(t, rc, "Ethereum", []int{50})
	seedPageEdits(t, rc, "Knitting", []int{2})

	global, _ := collector.CollectGlobal(ctx, "daily")

	user := &models.User{
		ID:            "user-1",
		Watchlist:     []string{"Knitting", "Bitcoin", "Ethereum"},
		DigestContent: models.DigestContentAll,
	}

	personalized := collector.PersonalizeForUser(ctx, global, user)
	if len(personalized.WatchlistEvents) != 3 {
		t.Fatalf("expected 3 watchlist events, got %d", len(personalized.WatchlistEvents))
	}

	// Notable pages first (Bitcoin edit_war=200, Ethereum active=50), then quiet (Knitting=2)
	if personalized.WatchlistEvents[0].Title != "Bitcoin" {
		t.Errorf("first event = %q, want Bitcoin (notable, highest)", personalized.WatchlistEvents[0].Title)
	}
	if personalized.WatchlistEvents[1].Title != "Ethereum" {
		t.Errorf("second event = %q, want Ethereum (notable, active)", personalized.WatchlistEvents[1].Title)
	}
	if personalized.WatchlistEvents[2].Title != "Knitting" {
		t.Errorf("third event = %q, want Knitting (quiet)", personalized.WatchlistEvents[2].Title)
	}
}
