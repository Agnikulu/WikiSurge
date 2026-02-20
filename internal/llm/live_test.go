// +build integration

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// These tests call the REAL OpenAI API. They are gated behind:
//   LLM_API_KEY env var must be set
//   go test -tags integration ./internal/llm/...
//
// Run with:
//   source .env && go test -v -tags integration -run TestLive ./internal/llm/...

func skipIfNoKey(t *testing.T) string {
	key := os.Getenv("LLM_API_KEY")
	if key == "" {
		t.Skip("LLM_API_KEY not set – skipping live test")
	}
	return key
}

func liveClient(t *testing.T) *Client {
	key := skipIfNoKey(t)
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	return NewClient(Config{
		Provider:    ProviderOpenAI,
		APIKey:      key,
		Model:       model,
		MaxTokens:   512,
		Temperature: 0.3,
		Timeout:     30 * time.Second,
	}, zerolog.New(zerolog.NewTestWriter(t)))
}

// ──────────────────────────────────────────────────────────────
// Test 1: Raw LLM completion — does the API key work at all?
// ──────────────────────────────────────────────────────────────

func TestLive_RawCompletion(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()

	resp, err := client.Complete(ctx,
		"You are a helpful assistant. Reply in one sentence.",
		"What is Wikipedia?",
	)
	if err != nil {
		t.Fatalf("LLM call failed: %v", err)
	}
	t.Logf("Raw response: %s", resp)

	if len(resp) < 10 {
		t.Error("Response suspiciously short")
	}
	lower := strings.ToLower(resp)
	if !strings.Contains(lower, "encyclopedia") && !strings.Contains(lower, "wiki") && !strings.Contains(lower, "online") {
		t.Errorf("Response doesn't seem to be about Wikipedia: %s", resp)
	}
}

// ──────────────────────────────────────────────────────────────
// Test 2: Political edit war (Israel-Palestine)
// ──────────────────────────────────────────────────────────────

func TestLive_PoliticalEditWar(t *testing.T) {
	client := liveClient(t)
	mr, _ := miniredis.Run()
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := NewAnalysisService(client, rdb, 5*time.Minute, zerolog.New(zerolog.NewTestWriter(t)))

	ctx := context.Background()
	page := "Israel-Palestine_conflict"
	key := fmt.Sprintf("editwar:timeline:%s", page)

	edits := []EditTimelineEntry{
		{User: "EditorNeutral", Comment: "Updated civilian casualty figures per UN OCHA report Q3 2024", ByteChange: 850, Timestamp: time.Now().Add(-5 * time.Hour).Unix()},
		{User: "Zionist_Scholar", Comment: "Reverted – OCHA numbers are contested by Israeli MFA, adding IDF figures for balance", ByteChange: -620, Timestamp: time.Now().Add(-4 * time.Hour).Unix()},
		{User: "EditorNeutral", Comment: "OCHA is a reliable source per WP:RS, restoring and adding footnotes", ByteChange: 900, Timestamp: time.Now().Add(-3 * time.Hour).Unix()},
		{User: "PalestineAdvocate", Comment: "Expanded civilian impact section with UNRWA testimony", ByteChange: 1200, Timestamp: time.Now().Add(-2 * time.Hour).Unix()},
		{User: "Zionist_Scholar", Comment: "Trimmed undue weight, WP:NPOV requires both perspectives", ByteChange: -1100, Timestamp: time.Now().Add(-1 * time.Hour).Unix()},
		{User: "PalestineAdvocate", Comment: "Restored per talk page consensus, RFC supports inclusion", ByteChange: 1050, Timestamp: time.Now().Add(-30 * time.Minute).Unix()},
	}
	for _, e := range edits {
		b, _ := json.Marshal(e)
		rdb.RPush(ctx, key, string(b))
	}

	analysis, err := svc.Analyze(ctx, page)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	t.Logf("═══ POLITICAL EDIT WAR ═══")
	t.Logf("Summary: %s", analysis.Summary)
	t.Logf("Content Area: %s", analysis.ContentArea)
	for i, side := range analysis.Sides {
		t.Logf("Side %d: %s (%d editors)", i+1, side.Position, len(side.Editors))
	}
	t.Logf("Edit count: %d | Generated: %s | CacheHit: %v", analysis.EditCount, analysis.GeneratedAt, analysis.CacheHit)

	// Accuracy checks
	if len(analysis.Summary) < 50 {
		t.Error("Summary too short for a meaningful conflict explanation")
	}
	if len(analysis.Sides) < 2 {
		t.Errorf("Expected at least 2 opposing sides, got %d", len(analysis.Sides))
	}
	// Should detect it's about casualties/sourcing
	combined := strings.ToLower(analysis.Summary + " " + analysis.ContentArea)
	if !containsAny(combined, "casualt", "civilian", "source", "npov", "neutral", "conflict", "ocha", "idf") {
		t.Errorf("Analysis doesn't seem to capture the core dispute about casualties/sourcing: %s", combined)
	}
}

// ──────────────────────────────────────────────────────────────
// Test 3: US Election edit war
// ──────────────────────────────────────────────────────────────

func TestLive_USElection(t *testing.T) {
	client := liveClient(t)
	mr, _ := miniredis.Run()
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := NewAnalysisService(client, rdb, 5*time.Minute, zerolog.New(zerolog.NewTestWriter(t)))

	ctx := context.Background()
	page := "2024_United_States_presidential_election"
	key := fmt.Sprintf("editwar:timeline:%s", page)

	edits := []EditTimelineEntry{
		{User: "TrumpSupporter42", Comment: "Added section on widespread voter fraud evidence per Breitbart", ByteChange: 1500, Timestamp: time.Now().Add(-6 * time.Hour).Unix()},
		{User: "FactChecker99", Comment: "Reverted – Breitbart is not RS for factual claims per WP:RSP, fraud claims rejected by 60+ courts", ByteChange: -1500, Timestamp: time.Now().Add(-5 * time.Hour).Unix()},
		{User: "TrumpSupporter42", Comment: "Restored, free speech, you can't censor legitimate sources", ByteChange: 1450, Timestamp: time.Now().Add(-4 * time.Hour).Unix()},
		{User: "ElectionExpert", Comment: "Reverted per WP:FRINGE – these claims have been debunked by AP, Reuters, and every state's election board", ByteChange: -1400, Timestamp: time.Now().Add(-3 * time.Hour).Unix()},
		{User: "TrumpSupporter42", Comment: "This is bias, adding back with Fox News and Epoch Times sources", ByteChange: 1600, Timestamp: time.Now().Add(-2 * time.Hour).Unix()},
		{User: "FactChecker99", Comment: "Reverted again – Epoch Times rated 'questionable' on RSP, Fox News retracted election claims after Dominion lawsuit", ByteChange: -1600, Timestamp: time.Now().Add(-1 * time.Hour).Unix()},
		{User: "Admin_Mediator", Comment: "Page protected, all parties please discuss on talk page before further edits", ByteChange: 0, Timestamp: time.Now().Add(-30 * time.Minute).Unix()},
	}
	for _, e := range edits {
		b, _ := json.Marshal(e)
		rdb.RPush(ctx, key, string(b))
	}

	analysis, err := svc.Analyze(ctx, page)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	t.Logf("═══ US ELECTION EDIT WAR ═══")
	t.Logf("Summary: %s", analysis.Summary)
	t.Logf("Content Area: %s", analysis.ContentArea)
	for i, side := range analysis.Sides {
		t.Logf("Side %d: %s (%d editors)", i+1, side.Position, len(side.Editors))
	}

	if len(analysis.Sides) < 2 {
		t.Errorf("Expected at least 2 sides, got %d", len(analysis.Sides))
	}
	combined := strings.ToLower(analysis.Summary + " " + analysis.ContentArea)
	if !containsAny(combined, "fraud", "election", "source", "reliab", "voter", "claim") {
		t.Errorf("Analysis doesn't capture the voter fraud / source reliability dispute")
	}
}

// ──────────────────────────────────────────────────────────────
// Test 4: BLP (biography of living person) — Elon Musk
// ──────────────────────────────────────────────────────────────

func TestLive_BLPDispute(t *testing.T) {
	client := liveClient(t)
	mr, _ := miniredis.Run()
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := NewAnalysisService(client, rdb, 5*time.Minute, zerolog.New(zerolog.NewTestWriter(t)))

	ctx := context.Background()
	page := "Elon_Musk"
	key := fmt.Sprintf("editwar:timeline:%s", page)

	edits := []EditTimelineEntry{
		{User: "NewsHound", Comment: "Added controversy section re: SEC investigation per WSJ and NYT reports", ByteChange: 980, Timestamp: time.Now().Add(-4 * time.Hour).Unix()},
		{User: "MuskFan2024", Comment: "Removed – this is WP:BLP violation, unproven allegations", ByteChange: -980, Timestamp: time.Now().Add(-3 * time.Hour).Unix()},
		{User: "NewsHound", Comment: "WSJ and NYT are top-tier RS, SEC investigation is public record, restoring", ByteChange: 1000, Timestamp: time.Now().Add(-2 * time.Hour).Unix()},
		{User: "MuskFan2024", Comment: "Undue weight per WP:NPOV, SEC investigation is routine, removing again", ByteChange: -950, Timestamp: time.Now().Add(-1 * time.Hour).Unix()},
		{User: "BLPPatrol", Comment: "Adding back with careful wording per WP:BLPCRIME — sourced, significant, due weight", ByteChange: 850, Timestamp: time.Now().Add(-30 * time.Minute).Unix()},
	}
	for _, e := range edits {
		b, _ := json.Marshal(e)
		rdb.RPush(ctx, key, string(b))
	}

	analysis, err := svc.Analyze(ctx, page)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	t.Logf("═══ BLP DISPUTE ═══")
	t.Logf("Summary: %s", analysis.Summary)
	t.Logf("Content Area: %s", analysis.ContentArea)
	for i, side := range analysis.Sides {
		t.Logf("Side %d: %s (%d editors)", i+1, side.Position, len(side.Editors))
	}

	if len(analysis.Sides) < 2 {
		t.Errorf("Expected at least 2 sides, got %d", len(analysis.Sides))
	}
}

// ──────────────────────────────────────────────────────────────
// Test 5: Vandalism vs legitimate editing
// ──────────────────────────────────────────────────────────────

func TestLive_VandalismVsLegit(t *testing.T) {
	client := liveClient(t)
	mr, _ := miniredis.Run()
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := NewAnalysisService(client, rdb, 5*time.Minute, zerolog.New(zerolog.NewTestWriter(t)))

	ctx := context.Background()
	page := "Climate_change"
	key := fmt.Sprintf("editwar:timeline:%s", page)

	edits := []EditTimelineEntry{
		{User: "IP_192.168.1.1", Comment: "lol climate change is fake", ByteChange: -5000, Timestamp: time.Now().Add(-3 * time.Hour).Unix()},
		{User: "ClimateBot", Comment: "Reverted vandalism by IP_192.168.1.1", ByteChange: 5000, Timestamp: time.Now().Add(-2*time.Hour - 50*time.Minute).Unix()},
		{User: "IP_192.168.1.1", Comment: "", ByteChange: -4800, Timestamp: time.Now().Add(-2 * time.Hour).Unix()},
		{User: "ClimateBot", Comment: "Reverted vandalism", ByteChange: 4800, Timestamp: time.Now().Add(-1*time.Hour - 55*time.Minute).Unix()},
		{User: "ScientistEditor", Comment: "Updated IPCC AR6 synthesis report data, added 2024 temperature records", ByteChange: 650, Timestamp: time.Now().Add(-1 * time.Hour).Unix()},
	}
	for _, e := range edits {
		b, _ := json.Marshal(e)
		rdb.RPush(ctx, key, string(b))
	}

	analysis, err := svc.Analyze(ctx, page)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	t.Logf("═══ VANDALISM VS LEGIT ═══")
	t.Logf("Summary: %s", analysis.Summary)
	t.Logf("Content Area: %s", analysis.ContentArea)
	for i, side := range analysis.Sides {
		t.Logf("Side %d: %s (%d editors)", i+1, side.Position, len(side.Editors))
	}

	// The LLM should recognize this as vandalism, not a content dispute
	combined := strings.ToLower(analysis.Summary)
	if !containsAny(combined, "vandal", "disruptive", "bad faith", "troll", "blanking", "removal") {
		t.Logf("WARNING: LLM may not have correctly identified vandalism pattern")
	}
}

// ──────────────────────────────────────────────────────────────
// Test 6: Caching — second call should be instant & cached
// ──────────────────────────────────────────────────────────────

func TestLive_Caching(t *testing.T) {
	client := liveClient(t)
	mr, _ := miniredis.Run()
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := NewAnalysisService(client, rdb, 5*time.Minute, zerolog.New(zerolog.NewTestWriter(t)))

	ctx := context.Background()
	page := "Test_Caching_Page"
	key := fmt.Sprintf("editwar:timeline:%s", page)

	edits := []EditTimelineEntry{
		{User: "Alice", Comment: "Added info", ByteChange: 500, Timestamp: time.Now().Add(-2 * time.Hour).Unix()},
		{User: "Bob", Comment: "Reverted", ByteChange: -500, Timestamp: time.Now().Add(-1 * time.Hour).Unix()},
	}
	for _, e := range edits {
		b, _ := json.Marshal(e)
		rdb.RPush(ctx, key, string(b))
	}

	// First call — hits LLM
	start1 := time.Now()
	a1, err := svc.Analyze(ctx, page)
	if err != nil {
		t.Fatalf("First analysis failed: %v", err)
	}
	dur1 := time.Since(start1)
	t.Logf("First call: %v (CacheHit=%v)", dur1, a1.CacheHit)

	if a1.CacheHit {
		t.Error("First call should NOT be a cache hit")
	}

	// Second call — should be cached
	start2 := time.Now()
	a2, err := svc.Analyze(ctx, page)
	if err != nil {
		t.Fatalf("Second analysis failed: %v", err)
	}
	dur2 := time.Since(start2)
	t.Logf("Second call: %v (CacheHit=%v)", dur2, a2.CacheHit)

	if !a2.CacheHit {
		t.Error("Second call SHOULD be a cache hit")
	}
	if dur2 > 50*time.Millisecond {
		t.Errorf("Cached response too slow: %v (expected <50ms)", dur2)
	}
	if a1.Summary != a2.Summary {
		t.Error("Cached summary doesn't match original")
	}
}

// ──────────────────────────────────────────────────────────────
// Test 7: JSON schema compliance — does LLM return valid JSON?
// ──────────────────────────────────────────────────────────────

func TestLive_JSONSchemaCompliance(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()

	systemPrompt := `You are a Wikipedia edit war analyst. Respond ONLY with valid JSON matching this exact schema:
{
  "summary": "string — 2-3 sentence explanation",
  "sides": [
    {"position": "string — what this side wants", "editors": [{"user": "string", "edit_count": 0, "role": "string"}]}
  ],
  "content_area": "string — topic area"
}
Do NOT include any text outside the JSON object.`

	userPrompt := `Analyze this edit war on page "Artificial_intelligence":
1. [AIResearcher] "Updated capabilities section with GPT-4 benchmarks" (+800 bytes)
2. [AISkeptic] "Reverted – undue weight on one model, WP:PROMO" (-750 bytes)
3. [AIResearcher] "Not promo – GPT-4 benchmarks are from peer-reviewed papers" (+780 bytes)
4. [TechEditor] "Trimmed to key benchmarks only, compromise" (+200 bytes)`

	resp, err := client.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		t.Fatalf("LLM call failed: %v", err)
	}
	t.Logf("Raw LLM response:\n%s", resp)

	// Try to parse as JSON
	var result struct {
		Summary     string `json:"summary"`
		Sides       []Side `json:"sides"`
		ContentArea string `json:"content_area"`
	}

	// Strip markdown wrapping if present
	cleaned := resp
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		if len(lines) > 2 {
			cleaned = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		t.Fatalf("LLM did not return valid JSON: %v\nRaw: %s", err, resp)
	}

	t.Logf("Parsed summary: %s", result.Summary)
	t.Logf("Parsed sides: %v", result.Sides)
	t.Logf("Parsed content_area: %s", result.ContentArea)

	if result.Summary == "" {
		t.Error("Summary field is empty")
	}
	if len(result.Sides) == 0 {
		t.Error("No sides returned")
	}
	if result.ContentArea == "" {
		t.Error("ContentArea is empty")
	}
}

// ──── helpers ────

func containsAny(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
