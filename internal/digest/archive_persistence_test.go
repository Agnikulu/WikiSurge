package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupTestCollectorWithRedis returns a Collector with its Redis client set
// (enabling enrichment), plus the underlying miniredis for direct inspection.
func setupTestCollectorWithRedis(t *testing.T) (*Collector, *redis.Client, *miniredis.Miniredis) {
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

	collector := NewCollectorWithRedis(trending, alerts, hotPages, stats, rc, logger)

	t.Cleanup(func() {
		trending.Stop()
		hotPages.Shutdown()
	})

	return collector, rc, mr
}

// seedAnalysisArchive writes a pre-built LLM analysis directly into the
// digest archive for a given date string (YYYY-MM-DD).
func seedAnalysisArchive(t *testing.T, rc *redis.Client, dateStr, pageTitle string, analysis map[string]interface{}) {
	t.Helper()
	ctx := context.Background()

	data, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("marshal analysis: %v", err)
	}

	dateKey := fmt.Sprintf("digest:war_analyses:%s", dateStr)
	hashKey := dateKey + ":data"

	editCount := 0
	if ec, ok := analysis["edit_count"]; ok {
		if n, ok := ec.(int); ok {
			editCount = n
		}
	}

	pipe := rc.Pipeline()
	pipe.HSet(ctx, hashKey, pageTitle, string(data))
	pipe.Expire(ctx, hashKey, 8*24*time.Hour)
	pipe.ZAdd(ctx, dateKey, redis.Z{Score: float64(editCount), Member: pageTitle})
	pipe.Expire(ctx, dateKey, 8*24*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatalf("seed archive: %v", err)
	}
}

// makeTestAnalysis builds a typical LLM analysis map for tests.
func makeTestAnalysis(summary, contentArea, severity string, editCount int, editors []string) map[string]interface{} {
	sides := []map[string]interface{}{}
	if len(editors) > 0 {
		editorEntries := make([]map[string]interface{}, len(editors))
		for i, e := range editors {
			editorEntries[i] = map[string]interface{}{
				"user":       e,
				"edit_count": 5,
				"role":       "participant",
			}
		}
		sides = append(sides, map[string]interface{}{
			"position": "Side A",
			"editors":  editorEntries,
		})
	}
	return map[string]interface{}{
		"page_title":   "",
		"summary":      summary,
		"content_area": contentArea,
		"severity":     severity,
		"edit_count":   editCount,
		"sides":        sides,
	}
}

// ---------------------------------------------------------------------------
// Test: Daily digest — LLM summary from ephemeral cache
// ---------------------------------------------------------------------------

func TestDailyDigest_LLMSummaryFromEphemeralCache(t *testing.T) {
	collector, rc, _ := setupTestCollectorWithRedis(t)
	ctx := context.Background()

	// Seed an edit war alert
	seedEditWarAlert(t, rc, "Climate_change", 300)

	// Seed ephemeral LLM analysis cache (as if the processor just generated it)
	analysis := makeTestAnalysis(
		"Editors are fighting over temperature attribution data.",
		"Climate attribution",
		"high",
		300,
		[]string{"Alice", "Bob", "Charlie"},
	)
	analysisJSON, _ := json.Marshal(analysis)
	rc.Set(ctx, "editwar:analysis:Climate_change", string(analysisJSON), 25*time.Hour)

	// Collect daily digest
	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if len(data.EditWarHighlights) == 0 {
		t.Fatal("expected at least 1 edit war highlight")
	}

	ew := data.EditWarHighlights[0]
	if ew.LLMSummary == "" {
		t.Error("expected LLM summary from ephemeral cache, got empty")
	}
	if !strings.Contains(ew.LLMSummary, "temperature attribution") {
		t.Errorf("unexpected LLM summary: %s", ew.LLMSummary)
	}
	if ew.Severity != "high" {
		t.Errorf("severity = %q, want high", ew.Severity)
	}
	if ew.ContentArea != "Climate attribution" {
		t.Errorf("content_area = %q, want 'Climate attribution'", ew.ContentArea)
	}
	if ew.EditorCount != 3 {
		t.Errorf("editor_count = %d, want 3", ew.EditorCount)
	}
}

// ---------------------------------------------------------------------------
// Test: Daily digest — ephemeral cache expired, archive has the data
// ---------------------------------------------------------------------------

func TestDailyDigest_FallbackToArchive(t *testing.T) {
	collector, rc, _ := setupTestCollectorWithRedis(t)
	ctx := context.Background()

	// Seed an edit war alert (the alert stream is the source of highlights)
	seedEditWarAlert(t, rc, "Bitcoin", 200)

	// Do NOT seed ephemeral cache — simulate it having expired.
	// Instead, seed the digest archive for today.
	today := time.Now().UTC().Format("2006-01-02")
	seedAnalysisArchive(t, rc, today, "Bitcoin", makeTestAnalysis(
		"Editors disagree on Bitcoin's energy consumption claims.",
		"Energy consumption",
		"moderate",
		200,
		[]string{"Miner1", "Greeny"},
	))

	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if len(data.EditWarHighlights) == 0 {
		t.Fatal("expected at least 1 edit war highlight")
	}

	ew := data.EditWarHighlights[0]

	// The enrichEditWars primary pass found no ephemeral cache, no analyzer,
	// and no editor hash. Then fillFromDigestArchive should have recovered it.
	if ew.LLMSummary == "" {
		t.Error("expected LLM summary recovered from digest archive, got empty")
	}
	if !strings.Contains(ew.LLMSummary, "energy consumption") {
		t.Errorf("unexpected LLM summary: %s", ew.LLMSummary)
	}
	if ew.Severity != "moderate" {
		t.Errorf("severity = %q, want moderate", ew.Severity)
	}
	if ew.EditorCount != 2 {
		t.Errorf("editor_count = %d, want 2", ew.EditorCount)
	}
}

// ---------------------------------------------------------------------------
// Test: Weekly digest — all ephemeral data expired, archive from past days
// ---------------------------------------------------------------------------

func TestWeeklyDigest_ArchiveRecoveryAcrossMultipleDays(t *testing.T) {
	collector, rc, _ := setupTestCollectorWithRedis(t)
	ctx := context.Background()

	// Seed edit war alerts (these live in the Redis stream and survive 7+ days
	// because the stream is MAXLEN-capped, not TTL-based).
	seedEditWarAlert(t, rc, "OpenAI", 500)
	seedEditWarAlert(t, rc, "Bitcoin", 200)
	seedEditWarAlert(t, rc, "Pope_Francis", 350)

	// No ephemeral cache for any of them — simulate that all expired.
	// Seed the digest archive across different past days (as if analyses
	// were generated on different days during the week).
	dayMinus1 := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	dayMinus3 := time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02")
	dayMinus6 := time.Now().UTC().AddDate(0, 0, -6).Format("2006-01-02")

	seedAnalysisArchive(t, rc, dayMinus1, "OpenAI", makeTestAnalysis(
		"The OpenAI page edit war revolves around whether Sam Altman's leadership changes should be described as a 'coup' or a 'governance transition'.",
		"Leadership description",
		"high",
		500,
		[]string{"TechWriter", "AIFan", "NeutralEd"},
	))
	seedAnalysisArchive(t, rc, dayMinus3, "Bitcoin", makeTestAnalysis(
		"Editors are disputing the characterization of Bitcoin's environmental impact.",
		"Environmental impact",
		"moderate",
		200,
		[]string{"CryptoMax", "EcoWarrior"},
	))
	seedAnalysisArchive(t, rc, dayMinus6, "Pope_Francis", makeTestAnalysis(
		"The edit war concerns whether recent papal statements should be categorized as political or theological.",
		"Papal statements classification",
		"low",
		350,
		[]string{"Vatican1", "Secular2", "Historian3"},
	))

	// Collect weekly digest
	data, err := collector.CollectGlobal(ctx, "weekly")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if len(data.EditWarHighlights) < 3 {
		t.Fatalf("expected 3 edit war highlights, got %d", len(data.EditWarHighlights))
	}

	// Check each edit war recovered its LLM summary from the archive.
	found := map[string]bool{}
	for _, ew := range data.EditWarHighlights {
		found[ew.Title] = true
		switch ew.Title {
		case "OpenAI":
			if ew.LLMSummary == "" {
				t.Error("OpenAI: expected LLM summary from archive (day -1)")
			}
			if !strings.Contains(ew.LLMSummary, "Sam Altman") {
				t.Errorf("OpenAI: unexpected summary: %s", ew.LLMSummary)
			}
			if ew.Severity != "high" {
				t.Errorf("OpenAI: severity = %q, want high", ew.Severity)
			}
			if ew.EditorCount != 3 {
				t.Errorf("OpenAI: editor_count = %d, want 3", ew.EditorCount)
			}
		case "Bitcoin":
			if ew.LLMSummary == "" {
				t.Error("Bitcoin: expected LLM summary from archive (day -3)")
			}
			if !strings.Contains(ew.LLMSummary, "environmental") {
				t.Errorf("Bitcoin: unexpected summary: %s", ew.LLMSummary)
			}
		case "Pope_Francis":
			if ew.LLMSummary == "" {
				t.Error("Pope_Francis: expected LLM summary from archive (day -6)")
			}
			if !strings.Contains(ew.LLMSummary, "papal statements") {
				t.Errorf("Pope_Francis: unexpected summary: %s", ew.LLMSummary)
			}
		}
	}

	for _, title := range []string{"OpenAI", "Bitcoin", "Pope_Francis"} {
		if !found[title] {
			t.Errorf("missing edit war: %s", title)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Archive beyond 8 days is NOT recovered
// ---------------------------------------------------------------------------

func TestWeeklyDigest_ArchiveBeyond8DaysNotRecovered(t *testing.T) {
	collector, rc, _ := setupTestCollectorWithRedis(t)
	ctx := context.Background()

	seedEditWarAlert(t, rc, "OldWar", 100)

	// Seed archive 9 days ago — beyond the 8-day scan window
	dayMinus9 := time.Now().UTC().AddDate(0, 0, -9).Format("2006-01-02")
	seedAnalysisArchive(t, rc, dayMinus9, "OldWar", makeTestAnalysis(
		"This analysis is too old to be recovered.",
		"Ancient history",
		"low",
		100,
		[]string{"Dinosaur"},
	))

	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if len(data.EditWarHighlights) == 0 {
		t.Fatal("expected edit war highlight")
	}

	ew := data.EditWarHighlights[0]
	if ew.LLMSummary != "" {
		t.Error("should NOT recover archive beyond 8 days, but got summary")
	}
}

// ---------------------------------------------------------------------------
// Test: On-demand analyzer fills cache and archive on miss
// ---------------------------------------------------------------------------

func TestDailyDigest_OnDemandAnalyzerFillsCacheOnMiss(t *testing.T) {
	collector, rc, _ := setupTestCollectorWithRedis(t)
	ctx := context.Background()

	seedEditWarAlert(t, rc, "AI_Ethics", 250)

	// Attach a mock analyzer that writes an analysis to the ephemeral cache
	// (simulating what the real AnalysisService.Analyze does).
	analyzerCalled := false
	collector.SetAnalyzer(func(ctx context.Context, pageTitle string) error {
		analyzerCalled = true
		// Simulate what AnalysisService.Analyze does: write to cache
		analysis := makeTestAnalysis(
			"The AI Ethics page has a heated debate between deontological and utilitarian perspectives.",
			"Ethical frameworks in AI",
			"moderate",
			250,
			[]string{"EthicsProf", "Pragmatist"},
		)
		data, _ := json.Marshal(analysis)
		rc.Set(ctx, "editwar:analysis:"+pageTitle, string(data), 25*time.Hour)
		return nil
	})

	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if !analyzerCalled {
		t.Error("expected on-demand analyzer to be called when cache is empty")
	}

	if len(data.EditWarHighlights) == 0 {
		t.Fatal("expected edit war highlights")
	}

	ew := data.EditWarHighlights[0]
	if ew.LLMSummary == "" {
		t.Error("expected LLM summary to be populated after on-demand analysis")
	}
	if !strings.Contains(ew.LLMSummary, "deontological") {
		t.Errorf("unexpected summary: %s", ew.LLMSummary)
	}
}

// ---------------------------------------------------------------------------
// Test: On-demand analyzer fails gracefully, falls back to archive
// ---------------------------------------------------------------------------

func TestDailyDigest_AnalyzerFailsFallsBackToArchive(t *testing.T) {
	collector, rc, _ := setupTestCollectorWithRedis(t)
	ctx := context.Background()

	seedEditWarAlert(t, rc, "Quantum_Computing", 180)

	// Attach an analyzer that fails
	collector.SetAnalyzer(func(ctx context.Context, pageTitle string) error {
		return fmt.Errorf("LLM API timeout")
	})

	// But the archive has the data from yesterday
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	seedAnalysisArchive(t, rc, yesterday, "Quantum_Computing", makeTestAnalysis(
		"The edit war is about quantum supremacy claims and their verifiability.",
		"Quantum supremacy verification",
		"moderate",
		180,
		[]string{"PhysicistA", "SkepticB"},
	))

	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if len(data.EditWarHighlights) == 0 {
		t.Fatal("expected edit war highlights")
	}

	ew := data.EditWarHighlights[0]
	if ew.LLMSummary == "" {
		t.Error("expected LLM summary recovered from archive after analyzer failure")
	}
	if !strings.Contains(ew.LLMSummary, "quantum supremacy") {
		t.Errorf("unexpected summary: %s", ew.LLMSummary)
	}
}

// ---------------------------------------------------------------------------
// Test: Mixed — some wars have cache, some need archive, some have nothing
// ---------------------------------------------------------------------------

func TestDailyDigest_MixedEnrichmentSources(t *testing.T) {
	collector, rc, _ := setupTestCollectorWithRedis(t)
	ctx := context.Background()

	seedEditWarAlert(t, rc, "Page_Cached", 400)
	seedEditWarAlert(t, rc, "Page_Archived", 300)
	seedEditWarAlert(t, rc, "Page_Bare", 200)

	// Page_Cached: has ephemeral cache
	cachedAnalysis := makeTestAnalysis("Cached summary for Page_Cached.", "Topic A", "high", 400, []string{"Ed1"})
	data, _ := json.Marshal(cachedAnalysis)
	rc.Set(ctx, "editwar:analysis:Page_Cached", string(data), 25*time.Hour)

	// Page_Archived: only in archive (ephemeral expired)
	today := time.Now().UTC().Format("2006-01-02")
	seedAnalysisArchive(t, rc, today, "Page_Archived", makeTestAnalysis(
		"Archived summary for Page_Archived.", "Topic B", "moderate", 300, []string{"Ed2", "Ed3"},
	))

	// Page_Bare: no cache, no archive, no editor hash → stays without summary

	digest, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if len(digest.EditWarHighlights) < 3 {
		t.Fatalf("expected 3 edit war highlights, got %d", len(digest.EditWarHighlights))
	}

	for _, ew := range digest.EditWarHighlights {
		switch ew.Title {
		case "Page_Cached":
			if !strings.Contains(ew.LLMSummary, "Cached summary") {
				t.Errorf("Page_Cached: expected cached summary, got: %s", ew.LLMSummary)
			}
		case "Page_Archived":
			if !strings.Contains(ew.LLMSummary, "Archived summary") {
				t.Errorf("Page_Archived: expected archived summary, got: %s", ew.LLMSummary)
			}
			if ew.EditorCount != 2 {
				t.Errorf("Page_Archived: editor_count = %d, want 2", ew.EditorCount)
			}
		case "Page_Bare":
			if ew.LLMSummary != "" {
				t.Errorf("Page_Bare: expected no LLM summary, got: %s", ew.LLMSummary)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Full E2E weekly — scheduler sends email with archived LLM summaries
// ---------------------------------------------------------------------------

func TestE2E_WeeklyDigest_ArchivedSummariesInEmail(t *testing.T) {
	// ---- Infrastructure ----
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rc.Close()

	userStore, err := storage.NewUserStore(":memory:")
	if err != nil {
		t.Fatalf("user store: %v", err)
	}
	defer userStore.Close()

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
	defer trending.Stop()
	defer hotPages.Shutdown()

	logger := zerolog.Nop()
	collector := NewCollectorWithRedis(trending, alerts, hotPages, stats, rc, logger)
	sender := &captureSender{}

	scheduler := NewScheduler(collector, sender, userStore, SchedulerConfig{
		DailySendHour:      8,
		WeeklySendDay:      1,
		WeeklySendHour:     8,
		MaxConcurrentSends: 5,
		DashboardURL:       "https://wikisurge.test",
		Enabled:            true,
	}, logger)

	ctx := context.Background()

	// ---- Create a user who wants weekly global content ----
	user, err := userStore.CreateUser("weekly@example.com", "pw")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := userStore.SetVerified(user.ID); err != nil {
		t.Fatalf("verify user: %v", err)
	}
	if err := userStore.UpdatePreferences(user.ID, models.DigestPreferences{
		DigestFreq:     models.DigestFreqWeekly,
		DigestContent:  models.DigestContentGlobal,
		SpikeThreshold: 1.0,
	}); err != nil {
		t.Fatalf("set prefs: %v", err)
	}

	// ---- Seed edit war alerts ----
	seedEditWarAlert(t, rc, "Smiling_Friends", 150)
	seedEditWarAlert(t, rc, "Baby_Keem", 220)

	// ---- Seed language stats (so the email renders fully) ----
	dateStr := time.Now().UTC().Format("2006-01-02")
	rc.HSet(ctx, "stats:languages:"+dateStr, "en", 80000, "__total__", 80000)

	// ---- Seed archive (simulating analyses generated mid-week) ----
	dayMinus2 := time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02")
	dayMinus5 := time.Now().UTC().AddDate(0, 0, -5).Format("2006-01-02")

	seedAnalysisArchive(t, rc, dayMinus2, "Smiling_Friends", makeTestAnalysis(
		"The Smiling Friends page edit war centers on whether the show is definitively ending after season 3. Multiple editors are reverting each others changes about the cancellation announcement, with one group citing executive producer interviews and another group insisting official network press releases should be the only accepted source. The dispute has been escalating with increasingly heated edit summaries over the past 48 hours.",
		"Show continuation status",
		"moderate",
		150,
		[]string{"BaldiBasicsFan", "D3nsebucket"},
	))
	seedAnalysisArchive(t, rc, dayMinus5, "Baby_Keem", makeTestAnalysis(
		"Editors are debating how to reflect Baby Keem's reported death on Feb 25, 2026. One faction insists on treating unverified social media reports as reliable sourcing, while another group is enforcing strict WP:BLP policies. The situation is further complicated by conflicting statements from the artist's management team and several news outlets retracting their initial reports.",
		"Death reporting",
		"high",
		220,
		[]string{"ItsMario97", "Sensitivebore"},
	))

	// ---- Trigger weekly digest ----
	sent, _, errored := scheduler.RunNow(ctx, "weekly")
	if errored != 0 {
		t.Errorf("errored = %d, want 0", errored)
	}
	if sent != 1 {
		t.Fatalf("sent = %d, want 1", sent)
	}

	emails := sender.all()
	if len(emails) != 1 {
		t.Fatalf("expected 1 email, got %d", len(emails))
	}

	html := emails[0].HTML

	// Verify the email contains LLM summaries recovered from the archive
	archiveChecks := []struct {
		label string
		want  string
	}{
		{"Smiling Friends summary", "ending after season 3"},
		{"Baby Keem summary", "Baby Keem"},
		{"Smiling Friends severity or content area", "Show continuation"},
		{"Baby Keem severity or content area", "Death reporting"},
		{"Edit wars section", "Most Popular Edit Wars"},
		{"Read more (expandable)", "Read more"},
		{"Weekly in subject", "Weekly"},
	}

	for _, c := range archiveChecks {
		target := html
		if c.label == "Weekly in subject" {
			target = emails[0].Subject
		}
		if !strings.Contains(target, c.want) {
			t.Errorf("Email missing %s (looking for %q)", c.label, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Archive deduplication — newer analysis for same page overwrites older
// ---------------------------------------------------------------------------

func TestArchive_NewerAnalysisOverwritesOlder(t *testing.T) {
	collector, rc, _ := setupTestCollectorWithRedis(t)
	ctx := context.Background()

	seedEditWarAlert(t, rc, "Dedup_Page", 300)

	today := time.Now().UTC().Format("2006-01-02")

	// First analysis (older version)
	seedAnalysisArchive(t, rc, today, "Dedup_Page", makeTestAnalysis(
		"Old analysis text that should be replaced.",
		"Old topic",
		"low",
		200,
		[]string{"OldEditor"},
	))

	// Second analysis (newer — same day, same page → overwrites)
	seedAnalysisArchive(t, rc, today, "Dedup_Page", makeTestAnalysis(
		"Updated analysis with better information.",
		"New topic",
		"high",
		300,
		[]string{"NewEditor1", "NewEditor2"},
	))

	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	if len(data.EditWarHighlights) == 0 {
		t.Fatal("expected edit war highlights")
	}

	ew := data.EditWarHighlights[0]
	if !strings.Contains(ew.LLMSummary, "Updated analysis") {
		t.Errorf("expected newer analysis, got: %s", ew.LLMSummary)
	}
	if ew.Severity != "high" {
		t.Errorf("severity = %q, want high (from newer analysis)", ew.Severity)
	}
	if ew.EditorCount != 2 {
		t.Errorf("editor_count = %d, want 2 (from newer analysis)", ew.EditorCount)
	}
}

// ---------------------------------------------------------------------------
// Test: Archive recovery fills editors and all metadata, not just summary
// ---------------------------------------------------------------------------

func TestArchive_RecoveryFillsAllMetadata(t *testing.T) {
	collector, rc, _ := setupTestCollectorWithRedis(t)
	ctx := context.Background()

	seedEditWarAlert(t, rc, "Full_Meta", 250)

	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	seedAnalysisArchive(t, rc, yesterday, "Full_Meta", makeTestAnalysis(
		"Comprehensive analysis with all metadata.",
		"Content area test",
		"critical",
		250,
		[]string{"Alpha", "Bravo", "Charlie", "Delta"},
	))

	data, err := collector.CollectGlobal(ctx, "daily")
	if err != nil {
		t.Fatalf("CollectGlobal: %v", err)
	}

	ew := data.EditWarHighlights[0]

	if ew.LLMSummary != "Comprehensive analysis with all metadata." {
		t.Errorf("summary = %q", ew.LLMSummary)
	}
	if ew.Summary != ew.LLMSummary {
		t.Errorf("Summary should be overwritten by LLMSummary: Summary=%q, LLMSummary=%q", ew.Summary, ew.LLMSummary)
	}
	if ew.ContentArea != "Content area test" {
		t.Errorf("content_area = %q", ew.ContentArea)
	}
	if ew.Severity != "critical" {
		t.Errorf("severity = %q", ew.Severity)
	}
	if ew.EditorCount != 4 {
		t.Errorf("editor_count = %d, want 4", ew.EditorCount)
	}
	// Verify editors are sorted
	expected := []string{"Alpha", "Bravo", "Charlie", "Delta"}
	if len(ew.Editors) != len(expected) {
		t.Fatalf("editors = %v, want %v", ew.Editors, expected)
	}
	for i, e := range expected {
		if ew.Editors[i] != e {
			t.Errorf("editors[%d] = %q, want %q", i, ew.Editors[i], e)
		}
	}
}
