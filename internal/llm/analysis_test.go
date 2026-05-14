package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return client, mr
}

// seedTimeline pushes edit timeline entries into a miniredis instance.
func seedTimeline(t *testing.T, client *redis.Client, pageTitle string, entries []EditTimelineEntry) {
	ctx := context.Background()
	key := "editwar:timeline:" + pageTitle
	for _, e := range entries {
		data, _ := json.Marshal(e)
		client.RPush(ctx, key, string(data))
	}
	client.Expire(ctx, key, 10*time.Minute)
}

// ─── Heuristic fallback tests (no LLM configured) ──────────────────────────

func TestAnalysisService_HeuristicFallback_ClearEditWar(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	// LLM client with no API key → Enabled() = false → heuristic mode
	llmClient := NewClient(Config{Provider: ProviderOpenAI}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	// Simulate a clear political edit war
	pageTitle := "2024_US_Election"
	entries := []EditTimelineEntry{
		{User: "Alice", Comment: "Added section on voter fraud allegations per Fox News", ByteChange: 500, Timestamp: time.Now().Add(-8 * time.Minute).Unix()},
		{User: "Bob", Comment: "Reverted - voter fraud claims are unsubstantiated per AP", ByteChange: -480, Timestamp: time.Now().Add(-7 * time.Minute).Unix()},
		{User: "Alice", Comment: "Restored sourced content, Fox News IS reliable for opinion", ByteChange: 490, Timestamp: time.Now().Add(-6 * time.Minute).Unix()},
		{User: "Bob", Comment: "Removed again, WP:RS says Fox News opinion is not reliable", ByteChange: -495, Timestamp: time.Now().Add(-5 * time.Minute).Unix()},
		{User: "Alice", Comment: "Added back with additional CNN source", ByteChange: 520, Timestamp: time.Now().Add(-4 * time.Minute).Unix()},
		{User: "Bob", Comment: "Revert: CNN source doesn't support the claim made", ByteChange: -510, Timestamp: time.Now().Add(-3 * time.Minute).Unix()},
	}

	seedTimeline(t, redisClient, pageTitle, entries)

	analysis, err := svc.Analyze(context.Background(), pageTitle)
	require.NoError(t, err)
	require.NotNil(t, analysis)

	// Heuristic should identify the editors and revert pattern
	assert.Equal(t, pageTitle, analysis.PageTitle)
	assert.Equal(t, 6, analysis.EditCount)
	assert.Greater(t, len(analysis.Summary), 50, "Summary should be meaningful")
	assert.Contains(t, analysis.Summary, "edit war")
	assert.GreaterOrEqual(t, len(analysis.Sides), 1)
	assert.False(t, analysis.CacheHit)

	// New enriched fields
	assert.NotEmpty(t, analysis.Severity)
	assert.Contains(t, []string{"low", "moderate", "high", "critical"}, analysis.Severity)
	assert.NotEmpty(t, analysis.Recommendation)
	assert.NotEqual(t, "undetermined (LLM not configured)", analysis.ContentArea)

	// Sides should contain Alice and Bob
	var allEditors []string
	for _, side := range analysis.Sides {
		for _, ed := range side.Editors {
			allEditors = append(allEditors, ed.User)
		}
	}
	assert.Contains(t, allEditors, "Alice")
	assert.Contains(t, allEditors, "Bob")

	t.Logf("Heuristic summary: %s", analysis.Summary)
	t.Logf("Severity: %s | Content area: %s", analysis.Severity, analysis.ContentArea)
	t.Logf("Recommendation: %s", analysis.Recommendation)
}

func TestAnalysisService_EmptyTimeline(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{Provider: ProviderOpenAI}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	analysis, err := svc.Analyze(context.Background(), "Nonexistent_Page")
	require.NoError(t, err)
	assert.Equal(t, 0, analysis.EditCount)
	assert.Contains(t, analysis.Summary, "No edit timeline data")
}

func TestAnalysisService_Caching(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{Provider: ProviderOpenAI}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	pageTitle := "Cache_Test_Page"
	entries := []EditTimelineEntry{
		{User: "Ed1", Comment: "Added info", ByteChange: 300, Timestamp: time.Now().Unix()},
		{User: "Ed2", Comment: "Reverted", ByteChange: -290, Timestamp: time.Now().Unix()},
	}
	seedTimeline(t, redisClient, pageTitle, entries)

	// First call — should not be cached
	a1, err := svc.Analyze(context.Background(), pageTitle)
	require.NoError(t, err)
	assert.False(t, a1.CacheHit)

	// Manually seed cache (as heuristic mode doesn't cache by default since
	// it's instant, but the LLM path does)
	cacheKey := "editwar:analysis:" + pageTitle
	data, _ := json.Marshal(a1)
	redisClient.Set(context.Background(), cacheKey, string(data), 5*time.Minute)

	// Second call — should be cache hit
	a2, err := svc.Analyze(context.Background(), pageTitle)
	require.NoError(t, err)
	assert.True(t, a2.CacheHit)
	assert.Equal(t, a1.Summary, a2.Summary)
}

// ─── LLM integration tests (mock server) ───────────────────────────────────

func TestAnalysisService_LLMIntegration_PoliticalConflict(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	// Mock LLM that returns structured JSON
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the prompt contains timeline data
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		messages := req["messages"].([]interface{})
		userMsg := messages[1].(map[string]interface{})["content"].(string)
		assert.Contains(t, userMsg, "Israel-Palestine")
		assert.Contains(t, userMsg, "Adding sourced content about civilian casualties")

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{
							"summary": "This edit war centers on how civilian casualties in the Israel-Palestine conflict should be described. Editor_A wants to include detailed casualty figures from UN sources emphasizing Palestinian casualties, while Editor_B argues the figures are disputed and wants to present both sides equally with Israeli government sources.",
							"sides": [
								{"position": "Prominent inclusion of UN-sourced civilian casualty data, emphasizing the humanitarian impact", "editors": [{"user": "Editor_A", "edit_count": 3, "role": "content adder"}]},
								{"position": "Balanced presentation with Israeli government counter-claims and disputes over casualty counting methodology", "editors": [{"user": "Editor_B", "edit_count": 2, "role": "reverter"}]}
							],
							"content_area": "Israel-Palestine conflict casualty reporting"
						}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close()

	llmClient := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "test-key",
		BaseURL:  mockLLM.URL,
		Model:    "gpt-4o-mini",
	}, zerolog.Nop())

	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	pageTitle := "Israel-Palestine_Conflict"
	entries := []EditTimelineEntry{
		{User: "Editor_A", Comment: "Adding sourced content about civilian casualties from UNRWA report", ByteChange: 850, Timestamp: time.Now().Add(-10 * time.Minute).Unix()},
		{User: "Editor_B", Comment: "Reverted - these figures are disputed, adding IDF response", ByteChange: -800, Timestamp: time.Now().Add(-9 * time.Minute).Unix()},
		{User: "Editor_A", Comment: "Restored UN data, IDF response is already in separate section", ByteChange: 820, Timestamp: time.Now().Add(-8 * time.Minute).Unix()},
		{User: "Editor_B", Comment: "POV pushing, must present both sides per WP:NPOV", ByteChange: -780, Timestamp: time.Now().Add(-7 * time.Minute).Unix()},
		{User: "Editor_A", Comment: "Not POV - UN is default reliable source per WP:RS", ByteChange: 830, Timestamp: time.Now().Add(-6 * time.Minute).Unix()},
	}
	seedTimeline(t, redisClient, pageTitle, entries)

	analysis, err := svc.Analyze(context.Background(), pageTitle)
	require.NoError(t, err)

	// Verify the LLM response was parsed correctly
	assert.Equal(t, pageTitle, analysis.PageTitle)
	assert.Equal(t, 5, analysis.EditCount)
	assert.Contains(t, analysis.Summary, "civilian casualties")
	assert.Contains(t, analysis.ContentArea, "Israel-Palestine")
	assert.False(t, analysis.CacheHit)

	t.Logf("LLM Analysis Summary: %s", analysis.Summary)
	for i, side := range analysis.Sides {
		t.Logf("Side %d: %s (%d editors)", i+1, side.Position, len(side.Editors))
	}
}

func TestAnalysisService_LLMIntegration_BiographyDispute(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{
							"summary": "Editors are disputing whether recent sexual assault allegations against Elon Musk should be included in his biography. One side argues for inclusion citing multiple mainstream news sources, while the other argues it's undue weight on unproven allegations per WP:BLP.",
							"sides": [
								{"position": "Include the allegations section with citations from NYT and WSJ as notable coverage", "editors": [{"user": "BioEditor", "edit_count": 2, "role": "content adder"}]},
								{"position": "Remove per WP:BLP - giving undue weight to unproven allegations in a living person's biography", "editors": [{"user": "BLPPatrol", "edit_count": 2, "role": "reverter"}]}
							],
							"content_area": "biography of living person - allegations"
						}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close()

	llmClient := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "test-key",
		BaseURL:  mockLLM.URL,
	}, zerolog.Nop())

	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	pageTitle := "Elon_Musk"
	entries := []EditTimelineEntry{
		{User: "BioEditor", Comment: "Adding assault allegations section per NYT, WSJ coverage", ByteChange: 1200, Timestamp: time.Now().Add(-15 * time.Minute).Unix()},
		{User: "BLPPatrol", Comment: "Removed per WP:BLP - undue weight on unproven allegations", ByteChange: -1150, Timestamp: time.Now().Add(-14 * time.Minute).Unix()},
		{User: "BioEditor", Comment: "Restored - multiple RS covered this, meets WP:WEIGHT", ByteChange: 1180, Timestamp: time.Now().Add(-12 * time.Minute).Unix()},
		{User: "BLPPatrol", Comment: "Reverted. Take it to talk page. BLP violation.", ByteChange: -1170, Timestamp: time.Now().Add(-11 * time.Minute).Unix()},
	}
	seedTimeline(t, redisClient, pageTitle, entries)

	analysis, err := svc.Analyze(context.Background(), pageTitle)
	require.NoError(t, err)

	assert.Contains(t, analysis.Summary, "allegations")
	assert.Contains(t, analysis.ContentArea, "biography")

	t.Logf("BLP Dispute: %s", analysis.Summary)
}

func TestAnalysisService_LLMFailure_FallsBackToHeuristic(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	// Mock LLM that always fails
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "server error"}`))
	}))
	defer mockLLM.Close()

	llmClient := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "test-key",
		BaseURL:  mockLLM.URL,
	}, zerolog.Nop())

	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	pageTitle := "Fallback_Test"
	entries := []EditTimelineEntry{
		{User: "UserX", Comment: "Adding content", ByteChange: 300, Timestamp: time.Now().Unix()},
		{User: "UserY", Comment: "Reverting vandalism", ByteChange: -280, Timestamp: time.Now().Unix()},
		{User: "UserX", Comment: "Not vandalism, legitimate edit", ByteChange: 290, Timestamp: time.Now().Unix()},
	}
	seedTimeline(t, redisClient, pageTitle, entries)

	// Should not error — should fall back to heuristic
	analysis, err := svc.Analyze(context.Background(), pageTitle)
	require.NoError(t, err)
	assert.Equal(t, 3, analysis.EditCount)
	assert.Contains(t, analysis.Summary, "edit war")
	assert.NotEmpty(t, analysis.Severity)
	assert.NotEmpty(t, analysis.Recommendation)
	assert.GreaterOrEqual(t, len(analysis.Sides), 1)

	t.Logf("Fallback analysis: %s", analysis.Summary)
}

// ─── Prompt building tests ──────────────────────────────────────────────────

func TestAnalysisService_BuildPrompt(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	entries := []EditTimelineEntry{
		{User: "Alice", Comment: "Added climate data from IPCC", ByteChange: 600, Timestamp: 1700000000},
		{User: "Bob", Comment: "Removed - cherry picked data", ByteChange: -580, Timestamp: 1700000060},
		{User: "Alice", Comment: "", ByteChange: 590, Timestamp: 1700000120}, // empty comment
	}

	system, user := svc.buildPrompt("Climate_Change", entries, nil)

	// System prompt should contain instructions
	assert.Contains(t, system, "Wikipedia edit war")
	assert.Contains(t, system, "JSON")

	// User prompt should contain page title and all entries
	assert.Contains(t, user, "Climate_Change")
	assert.Contains(t, user, "Alice")
	assert.Contains(t, user, "Bob")
	assert.Contains(t, user, "Added climate data from IPCC")
	assert.Contains(t, user, "Removed - cherry picked data")
	assert.Contains(t, user, "(no edit summary)")  // empty comment placeholder
	assert.Contains(t, user, "+600 bytes")
	assert.Contains(t, user, "-580 bytes")
	assert.Contains(t, user, "Diff content was unavailable") // no diffs passed
}

func TestAnalysisService_BuildPrompt_WithDiffs(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	entries := []EditTimelineEntry{
		{User: "Alice", Comment: "Added section", ByteChange: 200, Timestamp: 1700000000, RevisionID: 100},
		{User: "Bob", Comment: "Reverted", ByteChange: -200, Timestamp: 1700000060, RevisionID: 101},
	}
	diffs := map[int64]string{
		100: "+ ADDED: A new paragraph about climate policy.",
		101: "- REMOVED: A new paragraph about climate policy.",
	}

	_, user := svc.buildPrompt("Climate_Change", entries, diffs)

	assert.Contains(t, user, "A new paragraph about climate policy")
	assert.Contains(t, user, "Diff:")
	assert.Contains(t, user, "exact text") // user prompt explains diffs show exact text
}

// ─── Response parsing tests ─────────────────────────────────────────────────

func TestParseLLMResponse_ValidJSON(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	response := `{"summary":"Test summary","sides":[{"position":"Side A","editors":[{"user":"Ed1","edit_count":5,"role":"reverter"}]},{"position":"Side B","editors":[]}],"content_area":"testing","severity":"high","recommendation":"Seek mediation"}`
	analysis := svc.parseLLMResponse("TestPage", response, 5)

	assert.Equal(t, "Test summary", analysis.Summary)
	assert.Len(t, analysis.Sides, 2)
	assert.Equal(t, "Side A", analysis.Sides[0].Position)
	assert.Len(t, analysis.Sides[0].Editors, 1)
	assert.Equal(t, "Ed1", analysis.Sides[0].Editors[0].User)
	assert.Equal(t, "testing", analysis.ContentArea)
	assert.Equal(t, "high", analysis.Severity)
	assert.Equal(t, "Seek mediation", analysis.Recommendation)
	assert.Equal(t, 5, analysis.EditCount)
}

func TestParseLLMResponse_MarkdownWrapped(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	// Some LLMs wrap JSON in markdown code blocks
	response := "```json\n{\"summary\":\"Wrapped summary\",\"sides\":[{\"position\":\"X\",\"editors\":[]},{\"position\":\"Y\",\"editors\":[]}],\"content_area\":\"tech\"}\n```"
	analysis := svc.parseLLMResponse("TestPage", response, 3)

	assert.Equal(t, "Wrapped summary", analysis.Summary)
	assert.Len(t, analysis.Sides, 2)
}

func TestParseLLMResponse_PlainText(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	// If LLM returns plain text instead of JSON, use it as summary
	response := "This edit war is about whether to include recent controversy. Side A wants inclusion, Side B opposes."
	analysis := svc.parseLLMResponse("TestPage", response, 4)

	assert.Equal(t, response, analysis.Summary)
	assert.Empty(t, analysis.Sides)
	assert.Equal(t, "unknown", analysis.ContentArea)
	assert.Equal(t, "unknown", analysis.Severity)
}

// ─── Accuracy validation scenario tests ─────────────────────────────────────
// These tests validate that the mock LLM server setup and prompt construction
// produce correctly structured, meaningful analyses for real-world edit war
// patterns. Use these as templates for live LLM accuracy testing.

func TestAccuracy_VandalismVsLegitimateEdit(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		messages := req["messages"].([]interface{})
		userMsg := messages[1].(map[string]interface{})["content"].(string)

		// Verify prompt has the page name and comments
		assert.Contains(t, userMsg, "Barack_Obama")

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{
							"summary": "This dispute involves repeated removal of content about policy criticism. One editor is adding sourced criticism from notable publications, while a protective editor is treating legitimate criticism as vandalism and reverting it.",
							"sides": [
								{"position": "Adding notable sourced criticism of policies from Economist and WSJ", "editors": [{"user": "PolicyCritic", "edit_count": 3, "role": "content adder"}]},
								{"position": "Treating additions as vandalism and reverting to protect page from perceived POV edits", "editors": [{"user": "PageGuardian", "edit_count": 2, "role": "reverter"}]}
							],
							"content_area": "political biography - policy criticism"
						}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close()

	llmClient := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "test-key",
		BaseURL:  mockLLM.URL,
	}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	entries := []EditTimelineEntry{
		{User: "PolicyCritic", Comment: "Added criticism section citing Economist analysis", ByteChange: 900, Timestamp: time.Now().Add(-20 * time.Minute).Unix()},
		{User: "PageGuardian", Comment: "Reverted vandalism", ByteChange: -880, Timestamp: time.Now().Add(-19 * time.Minute).Unix()},
		{User: "PolicyCritic", Comment: "This is NOT vandalism, sourced content per WP:RS", ByteChange: 910, Timestamp: time.Now().Add(-18 * time.Minute).Unix()},
		{User: "PageGuardian", Comment: "Rv again - take to talk page first", ByteChange: -890, Timestamp: time.Now().Add(-17 * time.Minute).Unix()},
		{User: "PolicyCritic", Comment: "Added with WSJ source too, per WP:BRD I am discussing", ByteChange: 920, Timestamp: time.Now().Add(-15 * time.Minute).Unix()},
	}
	seedTimeline(t, redisClient, "Barack_Obama", entries)

	analysis, err := svc.Analyze(context.Background(), "Barack_Obama")
	require.NoError(t, err)

	// Structural correctness
	assert.Equal(t, "Barack_Obama", analysis.PageTitle)
	assert.Equal(t, 5, analysis.EditCount)
	assert.NotEmpty(t, analysis.ContentArea)

	// Content correctness — the analysis should identify this is NOT simple vandalism
	assert.Contains(t, analysis.Summary, "criticism")
	assert.NotEmpty(t, analysis.GeneratedAt)

	t.Logf("Accuracy test - vandalism vs legit:\n  Summary: %s\n  Area: %s",
		analysis.Summary, analysis.ContentArea)
}

func TestAccuracy_NoComments(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{
							"summary": "Multiple editors are making alternating large additions and removals on this page without providing edit summaries, indicating a content dispute. The byte change pattern suggests one side is adding approximately 400 bytes of content that another side repeatedly removes.",
							"sides": [
								{"position": "IP_Editor_1 is repeatedly adding content (+400 bytes each time)", "editors": [{"user": "IP_Editor_1", "edit_count": 2, "role": "content adder"}]},
								{"position": "IP_Editor_2 is repeatedly removing roughly the same amount of content", "editors": [{"user": "IP_Editor_2", "edit_count": 2, "role": "content remover"}]}
							],
							"content_area": "unknown - no edit summaries provided"
						}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close()

	llmClient := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "test-key",
		BaseURL:  mockLLM.URL,
	}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	// No comments at all — LLM should still provide useful analysis
	entries := []EditTimelineEntry{
		{User: "IP_Editor_1", Comment: "", ByteChange: 400, Timestamp: time.Now().Add(-5 * time.Minute).Unix()},
		{User: "IP_Editor_2", Comment: "", ByteChange: -390, Timestamp: time.Now().Add(-4 * time.Minute).Unix()},
		{User: "IP_Editor_1", Comment: "", ByteChange: 410, Timestamp: time.Now().Add(-3 * time.Minute).Unix()},
		{User: "IP_Editor_2", Comment: "", ByteChange: -395, Timestamp: time.Now().Add(-2 * time.Minute).Unix()},
	}
	seedTimeline(t, redisClient, "Mystery_Page", entries)

	analysis, err := svc.Analyze(context.Background(), "Mystery_Page")
	require.NoError(t, err)

	assert.Equal(t, 4, analysis.EditCount)
	assert.NotEmpty(t, analysis.Summary)
	// Should acknowledge the lack of edit summaries
	assert.Contains(t, analysis.Summary, "summar")

	t.Logf("No-comments analysis: %s", analysis.Summary)
}

func TestAccuracy_MultiPartyConflict(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{
							"summary": "A three-way conflict over the 'Controversies' section of a tech company article. One editor pushes for antitrust criticism, another wants to minimize it as undue weight, and a third is adding consumer complaint data that both others want removed for different reasons.",
							"sides": [
								{"position": "Prominent antitrust criticism section", "editors": [{"user": "CriticEditor", "edit_count": 2, "role": "content adder"}]},
								{"position": "Minimize controversies as undue weight", "editors": [{"user": "CompanyFan", "edit_count": 2, "role": "reverter"}]},
								{"position": "Add raw consumer complaint statistics", "editors": [{"user": "DataNerd", "edit_count": 2, "role": "data contributor"}]}
							],
							"content_area": "technology company controversies"
						}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close()

	llmClient := NewClient(Config{Provider: ProviderOpenAI, APIKey: "test-key", BaseURL: mockLLM.URL}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	entries := []EditTimelineEntry{
		{User: "CriticEditor", Comment: "Adding antitrust section per EU ruling", ByteChange: 700, Timestamp: time.Now().Add(-10 * time.Minute).Unix()},
		{User: "CompanyFan", Comment: "Removed, WP:UNDUE - minor regulatory issue", ByteChange: -680, Timestamp: time.Now().Add(-9 * time.Minute).Unix()},
		{User: "DataNerd", Comment: "Adding BBB complaint data table", ByteChange: 500, Timestamp: time.Now().Add(-8 * time.Minute).Unix()},
		{User: "CriticEditor", Comment: "Restored antitrust, removed BBB (not RS)", ByteChange: 200, Timestamp: time.Now().Add(-7 * time.Minute).Unix()},
		{User: "CompanyFan", Comment: "Reverted all controversy additions", ByteChange: -890, Timestamp: time.Now().Add(-6 * time.Minute).Unix()},
		{User: "DataNerd", Comment: "Re-added BBB data with FTC source", ByteChange: 520, Timestamp: time.Now().Add(-5 * time.Minute).Unix()},
	}
	seedTimeline(t, redisClient, "Big_Tech_Company", entries)

	analysis, err := svc.Analyze(context.Background(), "Big_Tech_Company")
	require.NoError(t, err)

	assert.Equal(t, 6, analysis.EditCount)
	assert.Contains(t, analysis.ContentArea, "technolog")

	t.Logf("Multi-party conflict: %s", analysis.Summary)
}

// ─── New field: Headline, WhatIsAtStake, EscalationTrend ────────────────────

// TestHeuristicAnalysis_DramaticWar_NewFields verifies that a clearly dramatic
// edit war (many edits, high revert rate, second half faster) produces
// non-empty headline, what_is_at_stake, and a meaningful escalation_trend.
func TestHeuristicAnalysis_DramaticWar_NewFields(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{Provider: ProviderOpenAI}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	pageTitle := "Ukraine_War_Casualties"
	now := time.Now()

	// 12 edits, second half twice as fast — should trigger "rising" escalation.
	entries := []EditTimelineEntry{
		{User: "Editor_A", Comment: "Updated UN casualty figures per OHCHR report", ByteChange: 900, Timestamp: now.Add(-60 * time.Minute).Unix()},
		{User: "Editor_B", Comment: "Reverted - figures disputed by Russian MOD", ByteChange: -880, Timestamp: now.Add(-52 * time.Minute).Unix()},
		{User: "Editor_A", Comment: "Restored OHCHR data, WP:RS policy applies", ByteChange: 910, Timestamp: now.Add(-44 * time.Minute).Unix()},
		{User: "Editor_B", Comment: "Removed again, both sides should be presented", ByteChange: -895, Timestamp: now.Add(-36 * time.Minute).Unix()},
		{User: "Editor_C", Comment: "Added compromise text citing both OHCHR and MOD", ByteChange: 950, Timestamp: now.Add(-28 * time.Minute).Unix()},
		{User: "Editor_B", Comment: "Compromise rejected, reverted to last stable", ByteChange: -930, Timestamp: now.Add(-20 * time.Minute).Unix()},
		// Second half much faster (roughly 2× pace)
		{User: "Editor_A", Comment: "Re-added OHCHR section verbatim", ByteChange: 905, Timestamp: now.Add(-10 * time.Minute).Unix()},
		{User: "Editor_B", Comment: "Reverted", ByteChange: -900, Timestamp: now.Add(-8 * time.Minute).Unix()},
		{User: "Editor_A", Comment: "Not reverting — adding new sourced content", ByteChange: 915, Timestamp: now.Add(-6 * time.Minute).Unix()},
		{User: "Editor_B", Comment: "Rv", ByteChange: -905, Timestamp: now.Add(-4 * time.Minute).Unix()},
		{User: "Editor_C", Comment: "Tried mediation again, undone within 2 min", ByteChange: 960, Timestamp: now.Add(-2 * time.Minute).Unix()},
		{User: "Editor_B", Comment: "Reverted mediation too - seek consensus first", ByteChange: -940, Timestamp: now.Add(-1 * time.Minute).Unix()},
	}
	seedTimeline(t, redisClient, pageTitle, entries)

	analysis, err := svc.Analyze(context.Background(), pageTitle)
	require.NoError(t, err)
	require.NotNil(t, analysis)

	// Headline must be populated and reference the page
	assert.NotEmpty(t, analysis.Headline, "Headline should be set for a dramatic edit war")
	assert.Contains(t, analysis.Headline, pageTitle,
		"Headline should mention the article name")

	// WhatIsAtStake must be a non-trivial sentence about the contested content
	assert.NotEmpty(t, analysis.WhatIsAtStake,
		"WhatIsAtStake should be populated for a dramatic edit war")
	assert.Greater(t, len(analysis.WhatIsAtStake), 30,
		"WhatIsAtStake should be a real sentence, not a placeholder")

	// Escalation trend: second half edits are twice as fast → should be "rising"
	assert.Equal(t, "rising", analysis.EscalationTrend,
		"escalation_trend should be 'rising' when second half is faster")

	// High revert count (10/12) → severity should be moderate or higher
	assert.Contains(t, []string{"moderate", "high", "critical"}, analysis.Severity,
		"dramatic edit war should not be rated 'low'")

	// Summary should describe the pattern
	assert.Contains(t, strings.ToLower(analysis.Summary), "revert",
		"summary should mention the revert pattern")

	t.Logf("Dramatic war: headline=%q  trend=%s  severity=%s",
		analysis.Headline, analysis.EscalationTrend, analysis.Severity)
}

// TestHeuristicAnalysis_QuietPage_NoDramaFalsePositive verifies that a calm
// page with only two slow, small, positive edits does not get flagged as
// dramatic by the heuristic analysis.
func TestHeuristicAnalysis_QuietPage_NoDramaFalsePositive(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{Provider: ProviderOpenAI}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	pageTitle := "Quiet_Article"
	now := time.Now()

	// Two editors making constructive additions, 45 minutes apart — not an edit war
	entries := []EditTimelineEntry{
		{User: "Ed1", Comment: "Added citation for population figure", ByteChange: 150, Timestamp: now.Add(-90 * time.Minute).Unix()},
		{User: "Ed2", Comment: "Copyedit, fixed grammar", ByteChange: -20, Timestamp: now.Add(-45 * time.Minute).Unix()},
		{User: "Ed1", Comment: "Expanded geography section", ByteChange: 320, Timestamp: now.Add(-10 * time.Minute).Unix()},
	}
	seedTimeline(t, redisClient, pageTitle, entries)

	analysis, err := svc.Analyze(context.Background(), pageTitle)
	require.NoError(t, err)
	require.NotNil(t, analysis)

	// Should be rated low — not a real edit war
	assert.Equal(t, "low", analysis.Severity,
		"slow collaborative editing should be rated 'low' severity")

	// EscalationTrend should not be "rising"
	assert.NotEqual(t, "rising", analysis.EscalationTrend,
		"quiet page should not have rising escalation trend")

	// Headline and WhatIsAtStake are still populated (heuristic always sets them)
	assert.NotEmpty(t, analysis.Headline)
	assert.NotEmpty(t, analysis.WhatIsAtStake)

	t.Logf("Quiet page: headline=%q  trend=%s  severity=%s",
		analysis.Headline, analysis.EscalationTrend, analysis.Severity)
}

// TestHeuristicAnalysis_CoolingWar verifies that when edit frequency drops
// in the second half, the escalation trend is "cooling".
func TestHeuristicAnalysis_CoolingWar(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{Provider: ProviderOpenAI}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	pageTitle := "Cooling_War_Page"
	now := time.Now()

	// First half: 4 edits over 8 minutes (very fast)
	// Second half: 4 edits over 40 minutes (much slower — cooling down)
	entries := []EditTimelineEntry{
		{User: "Alpha", Comment: "Revert", ByteChange: -500, Timestamp: now.Add(-50 * time.Minute).Unix()},
		{User: "Beta", Comment: "Revert", ByteChange: 490, Timestamp: now.Add(-48 * time.Minute).Unix()},
		{User: "Alpha", Comment: "Revert", ByteChange: -495, Timestamp: now.Add(-46 * time.Minute).Unix()},
		{User: "Beta", Comment: "Revert", ByteChange: 488, Timestamp: now.Add(-44 * time.Minute).Unix()},
		// Long gap — dispute cooling
		{User: "Alpha", Comment: "Restored with new source", ByteChange: -492, Timestamp: now.Add(-30 * time.Minute).Unix()},
		{User: "Beta", Comment: "Adjusted wording", ByteChange: 100, Timestamp: now.Add(-20 * time.Minute).Unix()},
		{User: "Alpha", Comment: "Minor tweak", ByteChange: 50, Timestamp: now.Add(-10 * time.Minute).Unix()},
		{User: "Beta", Comment: "Accepted edit", ByteChange: 30, Timestamp: now.Add(-5 * time.Minute).Unix()},
	}
	seedTimeline(t, redisClient, pageTitle, entries)

	analysis, err := svc.Analyze(context.Background(), pageTitle)
	require.NoError(t, err)

	assert.Equal(t, "cooling", analysis.EscalationTrend,
		"edit war that slows down in the second half should be 'cooling'")
	assert.NotEmpty(t, analysis.Headline)
	t.Logf("Cooling war: trend=%s  severity=%s", analysis.EscalationTrend, analysis.Severity)
}

// TestLLMParsing_AllNewFields verifies that when the LLM returns a response
// containing headline, what_is_at_stake, and escalation_trend, all three are
// correctly extracted and set on the Analysis struct.
func TestLLMParsing_AllNewFields(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{
							"headline": "Editors dispute whether Elon Musk's 2022 Twitter acquisition should be called a 'hostile takeover' in the lead section.",
							"what_is_at_stake": "If the first group wins, the article's lead will describe the acquisition as a hostile takeover with forced layoffs. If the second group wins, the article will use neutral language without characterizing intent.",
							"summary": "An edit war over one phrase in the lead section has been running for 90 minutes. One editor repeatedly inserts 'hostile takeover' while another removes it, citing WP:NPOV.",
							"sides": [
								{
									"position": "Describe acquisition as hostile takeover using sources from FT and NYT",
									"editors": [{"user": "CriticalUser", "edit_count": 4, "role": "Has added 'hostile takeover' language four times, citing FT and NYT coverage."}]
								},
								{
									"position": "Use neutral language; characterization of intent is not encyclopedic",
									"editors": [{"user": "NeutralUser", "edit_count": 3, "role": "Has reverted the 'hostile takeover' phrase three times, citing WP:NPOV policy."}]
								}
							],
							"content_area": "lead section characterization of business acquisition",
							"severity": "high",
							"recommendation": "Both editors should take this to the talk page. The phrase 'hostile takeover' is used in reliable sources, so mediation via WP:3O is appropriate.",
							"escalation_trend": "rising"
						}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close()

	llmClient := NewClient(Config{
		Provider: ProviderOpenAI,
		APIKey:   "test-key",
		BaseURL:  mockLLM.URL,
	}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	pageTitle := "Elon_Musk_Twitter"
	now := time.Now()
	entries := []EditTimelineEntry{
		{User: "CriticalUser", Comment: "Added 'hostile takeover' per FT", ByteChange: 30, Timestamp: now.Add(-90 * time.Minute).Unix()},
		{User: "NeutralUser", Comment: "Reverted NPOV violation", ByteChange: -28, Timestamp: now.Add(-85 * time.Minute).Unix()},
		{User: "CriticalUser", Comment: "Restored sourced phrase", ByteChange: 30, Timestamp: now.Add(-70 * time.Minute).Unix()},
		{User: "NeutralUser", Comment: "Removed again - not neutral", ByteChange: -28, Timestamp: now.Add(-60 * time.Minute).Unix()},
		{User: "CriticalUser", Comment: "NYT also uses this term", ByteChange: 30, Timestamp: now.Add(-40 * time.Minute).Unix()},
		{User: "NeutralUser", Comment: "Rv", ByteChange: -28, Timestamp: now.Add(-15 * time.Minute).Unix()},
		{User: "CriticalUser", Comment: "Re-adding, will start 3O", ByteChange: 30, Timestamp: now.Add(-5 * time.Minute).Unix()},
	}
	seedTimeline(t, redisClient, pageTitle, entries)

	analysis, err := svc.Analyze(context.Background(), pageTitle)
	require.NoError(t, err)

	// All three new fields must be populated from the LLM JSON
	assert.Equal(t,
		"Editors dispute whether Elon Musk's 2022 Twitter acquisition should be called a 'hostile takeover' in the lead section.",
		analysis.Headline,
		"Headline should be parsed from LLM JSON")

	assert.Contains(t, analysis.WhatIsAtStake, "hostile takeover",
		"WhatIsAtStake should be parsed from LLM JSON")

	assert.Equal(t, "rising", analysis.EscalationTrend,
		"EscalationTrend should be parsed from LLM JSON")

	// Existing fields should still work
	assert.Equal(t, "high", analysis.Severity)
	assert.Contains(t, analysis.Summary, "edit war")
	assert.Equal(t, 7, analysis.EditCount)
	assert.Contains(t, analysis.ContentArea, "lead section")

	// Role descriptions should be factual sentences, not nicknames
	for _, side := range analysis.Sides {
		for _, ed := range side.Editors {
			assert.NotContains(t, strings.ToLower(ed.Role), "sniper",
				"role should not contain creative nicknames")
			assert.NotContains(t, strings.ToLower(ed.Role), "vigilante",
				"role should not contain creative nicknames")
		}
	}

	t.Logf("New fields — headline: %q", analysis.Headline)
	t.Logf("             what_is_at_stake: %q", analysis.WhatIsAtStake)
	t.Logf("             escalation_trend: %q", analysis.EscalationTrend)
}

// TestParseLLMResponse_NewFieldsInJSON verifies parseLLMResponse directly
// extracts headline, what_is_at_stake, and escalation_trend from raw JSON.
func TestParseLLMResponse_NewFieldsInJSON(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	response := `{
		"headline": "Three editors dispute whether a 2019 court ruling should be described as 'corruption' in the lead.",
		"what_is_at_stake": "If Side A wins, the article will call the ruling corrupt. If Side B wins, it will describe the ruling neutrally without editorial characterization.",
		"summary": "A dispute over the lead section of a political article.",
		"sides": [
			{"position": "Call the ruling corrupt", "editors": [{"user": "A", "edit_count": 3, "role": "Repeatedly adds the word 'corrupt' to the lead."}]},
			{"position": "Keep neutral language", "editors": [{"user": "B", "edit_count": 2, "role": "Removes characterizations citing WP:NPOV."}]}
		],
		"content_area": "political article lead section",
		"severity": "moderate",
		"recommendation": "Request third opinion.",
		"escalation_trend": "steady"
	}`

	analysis := svc.parseLLMResponse("Court_Ruling", response, 5)

	assert.Equal(t, "Three editors dispute whether a 2019 court ruling should be described as 'corruption' in the lead.", analysis.Headline)
	assert.Contains(t, analysis.WhatIsAtStake, "corrupt")
	assert.Equal(t, "steady", analysis.EscalationTrend)
	assert.Equal(t, "moderate", analysis.Severity)
}

// TestParseLLMResponse_NewFieldsMissing verifies that if the LLM omits the
// new fields, the parser handles it gracefully (zero value, no panic).
func TestParseLLMResponse_NewFieldsMissing(t *testing.T) {
	redisClient, _ := setupTestRedis(t)
	defer redisClient.Close()

	llmClient := NewClient(Config{}, zerolog.Nop())
	svc := NewAnalysisService(llmClient, redisClient, 5*time.Minute, zerolog.Nop())

	// Old-style response without new fields
	response := `{
		"summary": "An edit war with no headline provided.",
		"sides": [{"position": "Side A", "editors": [{"user": "X", "edit_count": 2, "role": "reverter"}]}],
		"content_area": "unknown",
		"severity": "low",
		"recommendation": "Monitor the situation."
	}`

	analysis := svc.parseLLMResponse("Old_Format_Page", response, 3)

	// Should not panic; fields default to empty strings
	assert.Equal(t, "An edit war with no headline provided.", analysis.Summary)
	assert.Equal(t, "low", analysis.Severity)
	// New fields absent from JSON → empty string (no crash)
	assert.IsType(t, "", analysis.Headline, "Headline should default to empty string")
	assert.IsType(t, "", analysis.WhatIsAtStake, "WhatIsAtStake should default to empty string")
	assert.IsType(t, "", analysis.EscalationTrend, "EscalationTrend should default to empty string")
}

// ─── Digest archive tests ────────────────────────────────────────────────────

// ─── Stats block injection tests ─────────────────────────────────────────────

// TestBuildPrompt_StatsBlock_AppearsWithMultipleEntries verifies that when
// ≥2 entries are provided the user prompt contains the computed stats header.
func TestBuildPrompt_StatsBlock_AppearsWithMultipleEntries(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	now := time.Now()
	entries := []EditTimelineEntry{
		{User: "Alice", Comment: "Reverted", ByteChange: -500, Timestamp: now.Add(-30 * time.Minute).Unix()},
		{User: "Bob", Comment: "Undid last edit", ByteChange: 490, Timestamp: now.Add(-20 * time.Minute).Unix()},
		{User: "Alice", Comment: "Added back", ByteChange: -495, Timestamp: now.Add(-10 * time.Minute).Unix()},
	}
	_, user := svc.buildPrompt("Stats_Test", entries, nil)

	assert.Contains(t, user, "Computed statistics")
	assert.Contains(t, user, "Total edits:    3")
	assert.Contains(t, user, "Unique editors: 2")
	// "Reverted" and "Undid" should both count
	assert.Contains(t, user, "Detected reverts (from edit summaries): 2")
}

// TestBuildPrompt_StatsBlock_SingleEntry_Absent verifies no stats block when
// only 1 entry exists (need at least 2 to compute a meaningful time span).
func TestBuildPrompt_StatsBlock_SingleEntry_Absent(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	entries := []EditTimelineEntry{
		{User: "Alice", Comment: "Added info", ByteChange: 100, Timestamp: time.Now().Unix()},
	}
	_, user := svc.buildPrompt("Single_Entry", entries, nil)

	assert.NotContains(t, user, "Computed statistics",
		"stats block should be absent for a single-entry timeline")
}

// TestBuildPrompt_StatsBlock_ShowsMinutesForShortWar verifies the time span
// is rendered as "N minutes" when the war spans < 60 minutes.
func TestBuildPrompt_StatsBlock_ShowsMinutesForShortWar(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	now := time.Now()
	entries := []EditTimelineEntry{
		{User: "X", Comment: "Edit", ByteChange: 10, Timestamp: now.Add(-20 * time.Minute).Unix()},
		{User: "Y", Comment: "Revert", ByteChange: -10, Timestamp: now.Unix()},
	}
	_, user := svc.buildPrompt("Short_War", entries, nil)

	assert.Contains(t, user, "minutes", "short war should report time in minutes")
	assert.NotContains(t, user, "hours", "short war should not report time in hours")
}

// TestBuildPrompt_StatsBlock_ShowsHoursForLongWar verifies the time span is
// rendered as "N.N hours" when the war spans ≥ 60 minutes.
func TestBuildPrompt_StatsBlock_ShowsHoursForLongWar(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	now := time.Now()
	entries := []EditTimelineEntry{
		{User: "X", Comment: "Edit", ByteChange: 10, Timestamp: now.Add(-3 * time.Hour).Unix()},
		{User: "Y", Comment: "Revert", ByteChange: -10, Timestamp: now.Unix()},
	}
	_, user := svc.buildPrompt("Long_War", entries, nil)

	assert.Contains(t, user, "hours", "long war should report time in hours")
	assert.NotContains(t, user, "minutes", "long war should not report in minutes")
}

// TestBuildPrompt_StatsBlock_RevertKeywords verifies all revert-signal keywords
// are counted: "revert", "reverted", "undid", "rvv".
func TestBuildPrompt_StatsBlock_RevertKeywords(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	now := time.Now()
	entries := []EditTimelineEntry{
		{User: "A", Comment: "revert vandalism", ByteChange: -100, Timestamp: now.Add(-10 * time.Minute).Unix()},
		{User: "B", Comment: "Undid revision 12345", ByteChange: 100, Timestamp: now.Add(-8 * time.Minute).Unix()},
		{User: "C", Comment: "rvv", ByteChange: -100, Timestamp: now.Add(-6 * time.Minute).Unix()},
		{User: "D", Comment: "Reverted per BLP", ByteChange: 100, Timestamp: now.Add(-4 * time.Minute).Unix()},
		{User: "E", Comment: "Fixed typo", ByteChange: 5, Timestamp: now.Add(-2 * time.Minute).Unix()},
	}
	_, user := svc.buildPrompt("Revert_Keywords", entries, nil)

	assert.Contains(t, user, "Detected reverts (from edit summaries): 4",
		"all four revert-signal keywords should be counted")
}

// TestBuildPrompt_EditRate_Positive verifies the edit rate is included and
// is a positive number.
func TestBuildPrompt_EditRate_Positive(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	now := time.Now()
	entries := []EditTimelineEntry{
		{User: "A", Comment: "edit", ByteChange: 50, Timestamp: now.Add(-60 * time.Minute).Unix()},
		{User: "B", Comment: "revert", ByteChange: -50, Timestamp: now.Unix()},
	}
	_, user := svc.buildPrompt("Rate_Test", entries, nil)

	assert.Contains(t, user, "Edit rate:", "edit rate should appear in stats block")
	assert.Contains(t, user, "edits/hour", "edit rate should include unit label")
}

// ─── System prompt content tests ─────────────────────────────────────────────

// TestSystemPrompt_ContainsVandalismDetectionGuidance verifies the system
// prompt explicitly instructs the LLM about the vandalism vs genuine dispute
// distinction and the is_vandalism field.
func TestSystemPrompt_ContainsVandalismDetectionGuidance(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	entries := []EditTimelineEntry{
		{User: "A", Comment: "edit", ByteChange: 50, Timestamp: time.Now().Add(-5 * time.Minute).Unix()},
		{User: "B", Comment: "revert", ByteChange: -50, Timestamp: time.Now().Unix()},
	}
	system, _ := svc.buildPrompt("Test_Page", entries, nil)

	assert.Contains(t, system, "VANDALISM", "system prompt must mention vandalism detection")
	assert.Contains(t, system, "is_vandalism", "system prompt must specify the is_vandalism JSON field")
	assert.Contains(t, system, "genuine content dispute", "system prompt must distinguish dispute types")
}

// TestSystemPrompt_BannedHeadlineOpeners verifies the system prompt explicitly
// lists the known-bad headline opener phrases the LLM must not use.
func TestSystemPrompt_BannedHeadlineOpeners(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	entries := []EditTimelineEntry{
		{User: "A", Comment: "edit", ByteChange: 50, Timestamp: time.Now().Add(-5 * time.Minute).Unix()},
		{User: "B", Comment: "revert", ByteChange: -50, Timestamp: time.Now().Unix()},
	}
	system, _ := svc.buildPrompt("Test_Page", entries, nil)

	assert.Contains(t, system, "BANNED OPENERS", "system prompt must call out the banned opener list")
	assert.Contains(t, system, "Edit war over content", "must list the banned opener")
	assert.Contains(t, system, "Dispute over the details", "must list the banned opener")
}

// TestSystemPrompt_RequiresDiffQuotingWhenAvailable verifies the system prompt
// instructs the LLM to quote diff text in headline/stakes/summary.
func TestSystemPrompt_RequiresDiffQuotingWhenAvailable(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	entries := []EditTimelineEntry{
		{User: "A", Comment: "edit", ByteChange: 50, Timestamp: time.Now().Add(-5 * time.Minute).Unix(), RevisionID: 1},
		{User: "B", Comment: "revert", ByteChange: -50, Timestamp: time.Now().Unix(), RevisionID: 2},
	}
	diffs := map[int64]string{1: "+ disputed text added here"}
	system, user := svc.buildPrompt("Diff_Page", entries, diffs)

	// System prompt should require quoting
	assert.Contains(t, system, "QUOTE", "system prompt must instruct LLM to quote diff text")
	// User prompt should also reinforce quoting instruction when diffs exist
	assert.Contains(t, user, "QUOTE", "user prompt should reinforce quoting when diffs present")
}

// TestSystemPrompt_RequiresHardNumbers verifies the system prompt requires
// the summary to contain concrete numbers (revert count, time span, etc.).
func TestSystemPrompt_RequiresHardNumbers(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	entries := []EditTimelineEntry{
		{User: "A", Comment: "edit", ByteChange: 50, Timestamp: time.Now().Add(-5 * time.Minute).Unix()},
		{User: "B", Comment: "revert", ByteChange: -50, Timestamp: time.Now().Unix()},
	}
	system, _ := svc.buildPrompt("Numbers_Test", entries, nil)

	// The prompt should require "hard numbers" or "exact numbers"
	assert.True(t,
		strings.Contains(strings.ToLower(system), "hard numbers") || strings.Contains(strings.ToLower(system), "exact numbers") ||
			strings.Contains(strings.ToLower(system), "exact number"),
		"system prompt must demand specific numeric values in the summary")
}

// ─── is_vandalism field parsing tests ────────────────────────────────────────

// TestParseLLMResponse_IsVandalism_True verifies that is_vandalism:true in the
// LLM JSON response is stored on the Analysis struct.
func TestParseLLMResponse_IsVandalism_True(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	response := `{
		"is_vandalism": true,
		"headline": "Editors battle over whether 'Crystal Pite' should be replaced with 'Crystal from Beverly Hills Housewives'.",
		"what_is_at_stake": "If the vandal wins, the article will misidentify the subject. If defenders win, her correct name is maintained.",
		"summary": "A clear vandalism case: one editor inserted 'Crystal from Beverly Hills Housewives' 3 times in 4 minutes.",
		"sides": [
			{"position": "Keep the correct name", "editors": [{"user": "Tessaract2", "edit_count": 2, "role": "Reverted the name replacement twice."}]},
			{"position": "Replace with joke name", "editors": [{"user": "Vandal99", "edit_count": 2, "role": "Inserted 'Crystal from Beverly Hills Housewives' twice."}]}
		],
		"content_area": "name replacement in biography",
		"severity": "high",
		"recommendation": "Report the vandal to WP:AIV.",
		"escalation_trend": "rising"
	}`

	analysis := svc.parseLLMResponse("Crystal_Pite", response, 4)

	assert.True(t, analysis.IsVandalism, "IsVandalism should be true when LLM reports is_vandalism:true")
	assert.Contains(t, analysis.Headline, "Beverly Hills")
	assert.Equal(t, "high", analysis.Severity)
}

// TestParseLLMResponse_IsVandalism_False verifies that is_vandalism:false is
// correctly stored (not defaulting to true).
func TestParseLLMResponse_IsVandalism_False(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	response := `{
		"is_vandalism": false,
		"headline": "Editors dispute whether the Gaza casualty figure should be from UNRWA or the Israeli MOD.",
		"what_is_at_stake": "If Side A wins, the lead uses UNRWA's figure. If Side B wins, the article attributes the figure to the IDF.",
		"summary": "A genuine sourcing dispute running for 40 minutes across 8 edits.",
		"sides": [],
		"content_area": "casualty figures in the Gaza conflict",
		"severity": "high",
		"recommendation": "Both sides should open a talk page discussion citing WP:RS.",
		"escalation_trend": "steady"
	}`

	analysis := svc.parseLLMResponse("Gaza_Conflict", response, 8)

	assert.False(t, analysis.IsVandalism, "IsVandalism should be false for a genuine content dispute")
	assert.Contains(t, analysis.ContentArea, "Gaza")
}

// TestParseLLMResponse_IsVandalism_Absent_DefaultsFalse verifies that when the
// is_vandalism field is absent from the JSON, IsVandalism defaults to false.
func TestParseLLMResponse_IsVandalism_Absent_DefaultsFalse(t *testing.T) {
	rdb, _ := setupTestRedis(t)
	svc := NewAnalysisService(NewClient(Config{}, zerolog.Nop()), rdb, 0, zerolog.Nop())

	// No is_vandalism field
	response := `{
		"headline": "Some dispute.",
		"summary": "Old format response without is_vandalism field.",
		"sides": [],
		"content_area": "testing",
		"severity": "low",
		"recommendation": "Monitor.",
		"escalation_trend": "cooling"
	}`

	analysis := svc.parseLLMResponse("Old_Format", response, 2)

	assert.False(t, analysis.IsVandalism,
		"absent is_vandalism should default to false (bool zero value)")
}

// ─── Mock LLM end-to-end vandalism/dispute tests ─────────────────────────────

// TestE2E_VandalismFlag_TrueFlowsThroughAnalyze verifies that a mock LLM
// returning is_vandalism:true produces an Analysis with IsVandalism==true.
func TestE2E_VandalismFlag_TrueFlowsThroughAnalyze(t *testing.T) {
	rdb, _ := setupTestRedis(t)

	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": `{
					"is_vandalism": true,
					"headline": "An anonymous editor replaced 'Napoleon Bonaparte' with 'SpongeBob' in the biography lead.",
					"what_is_at_stake": "If the vandal wins, Napoleon's article calls him 'SpongeBob'. If defenders win, the correct name is restored.",
					"summary": "Vandalism: the editor replaced 'Napoleon Bonaparte' with 'SpongeBob' in the lead section twice in 3 minutes before being reverted.",
					"sides": [
						{"position": "Restore correct name", "editors": [{"user": "GuardianA", "edit_count": 2, "role": "Reverted 'SpongeBob' insertion twice."}]},
						{"position": "Insert 'SpongeBob'", "editors": [{"user": "AnonVandal", "edit_count": 2, "role": "Replaced 'Napoleon Bonaparte' with 'SpongeBob' twice."}]}
					],
					"content_area": "lead section name in biography",
					"severity": "high",
					"recommendation": "Report AnonVandal to WP:AIV for persistent vandalism.",
					"escalation_trend": "rising"
				}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close()

	svc := NewAnalysisService(
		NewClient(Config{Provider: ProviderOpenAI, APIKey: "k", BaseURL: mockLLM.URL}, zerolog.Nop()),
		rdb, 5*time.Minute, zerolog.Nop(),
	)

	now := time.Now()
	seedTimeline(t, rdb, "Napoleon_Bonaparte", []EditTimelineEntry{
		{User: "AnonVandal", Comment: "edit", ByteChange: 0, Timestamp: now.Add(-5 * time.Minute).Unix()},
		{User: "GuardianA", Comment: "Reverted", ByteChange: 0, Timestamp: now.Add(-4 * time.Minute).Unix()},
		{User: "AnonVandal", Comment: "edit", ByteChange: 0, Timestamp: now.Add(-3 * time.Minute).Unix()},
		{User: "GuardianA", Comment: "Reverted", ByteChange: 0, Timestamp: now.Add(-2 * time.Minute).Unix()},
	})

	analysis, err := svc.Analyze(context.Background(), "Napoleon_Bonaparte")
	require.NoError(t, err)

	assert.True(t, analysis.IsVandalism, "IsVandalism must be true when LLM returns is_vandalism:true")
	assert.Contains(t, analysis.Headline, "SpongeBob")
	assert.Equal(t, "high", analysis.Severity)
	assert.Equal(t, 4, analysis.EditCount)
	t.Logf("Vandalism E2E: is_vandalism=%v headline=%q", analysis.IsVandalism, analysis.Headline)
}

// TestE2E_GenuineDispute_IsVandalismFalse verifies a genuine content dispute
// is correctly marked is_vandalism:false end-to-end.
func TestE2E_GenuineDispute_IsVandalismFalse(t *testing.T) {
	rdb, _ := setupTestRedis(t)

	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Also verify the stats block is present in the user message.
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		messages := req["messages"].([]interface{})
		userMsg := messages[1].(map[string]interface{})["content"].(string)
		assert.Contains(t, userMsg, "Computed statistics",
			"user prompt sent to LLM should contain the stats block")
		assert.Contains(t, userMsg, "Total edits:", "stats block should include total edits")

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": `{
					"is_vandalism": false,
					"headline": "Editors have battled over whether Roe v. Wade should be described as 'controversial' in the opening sentence for 2 hours.",
					"what_is_at_stake": "If Side A wins, the article's opening sentence will call the ruling 'controversial'. If Side B wins, the word will be removed as editorially loaded.",
					"summary": "The dispute centers on the word 'controversial' in the first sentence, with 6 reverts over 2 hours. Side A cites multiple RS that use the word; Side B argues WP:NPOV forbids it.",
					"sides": [
						{"position": "Keep 'controversial'", "editors": [{"user": "CivEditor", "edit_count": 3, "role": "Re-added 'controversial' three times citing FT, NYT, and SCOTUS Blog."}]},
						{"position": "Remove 'controversial'", "editors": [{"user": "NeutralEd", "edit_count": 3, "role": "Removed 'controversial' three times citing WP:NPOV and WP:EDITORIAL."}]}
					],
					"content_area": "characterization in lead sentence of Supreme Court case article",
					"severity": "moderate",
					"recommendation": "Both editors should open a WP:RfC on the talk page.",
					"escalation_trend": "steady"
				}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close()

	svc := NewAnalysisService(
		NewClient(Config{Provider: ProviderOpenAI, APIKey: "k", BaseURL: mockLLM.URL}, zerolog.Nop()),
		rdb, 5*time.Minute, zerolog.Nop(),
	)

	now := time.Now()
	seedTimeline(t, rdb, "Roe_v_Wade", []EditTimelineEntry{
		{User: "CivEditor", Comment: "Added 'controversial' per FT", ByteChange: 14, Timestamp: now.Add(-120 * time.Minute).Unix()},
		{User: "NeutralEd", Comment: "Reverted NPOV violation", ByteChange: -14, Timestamp: now.Add(-110 * time.Minute).Unix()},
		{User: "CivEditor", Comment: "Restored - sourced in NYT", ByteChange: 14, Timestamp: now.Add(-90 * time.Minute).Unix()},
		{User: "NeutralEd", Comment: "Removed again", ByteChange: -14, Timestamp: now.Add(-70 * time.Minute).Unix()},
		{User: "CivEditor", Comment: "SCOTUS Blog uses the term", ByteChange: 14, Timestamp: now.Add(-40 * time.Minute).Unix()},
		{User: "NeutralEd", Comment: "Rv - WP:EDITORIAL", ByteChange: -14, Timestamp: now.Add(-20 * time.Minute).Unix()},
	})

	analysis, err := svc.Analyze(context.Background(), "Roe_v_Wade")
	require.NoError(t, err)

	assert.False(t, analysis.IsVandalism, "genuine content dispute should not be marked vandalism")
	assert.Contains(t, analysis.Headline, "controversial")
	assert.Equal(t, "moderate", analysis.Severity)
	assert.Equal(t, "steady", analysis.EscalationTrend)
	assert.Equal(t, 6, analysis.EditCount)
	t.Logf("Genuine dispute E2E: is_vandalism=%v headline=%q", analysis.IsVandalism, analysis.Headline)
}

// TestE2E_PromptContainsStatsForLLM verifies that when calling Analyze with a
// mock LLM, the stats block (time span, revert count, edit rate) from the user
// prompt actually reaches the LLM payload.
func TestE2E_PromptContainsStatsForLLM(t *testing.T) {
	rdb, _ := setupTestRedis(t)

	capturedUserMsg := ""
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		messages := req["messages"].([]interface{})
		capturedUserMsg = messages[1].(map[string]interface{})["content"].(string)

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": `{
					"is_vandalism": false,
					"headline": "Editors have been fighting over vaccine safety claims for 45 minutes.",
					"what_is_at_stake": "If Side A wins, the article will include a 'Safety controversies' section. If Side B wins, that section will be removed.",
					"summary": "8 edits in 45 minutes, 4 reverts. One editor adds a 'Safety controversies' section; another removes it as pseudoscience.",
					"sides": [
						{"position": "Include safety controversies section", "editors": [{"user": "VaxSkeptic", "edit_count": 4, "role": "Added the safety controversies section 4 times."}]},
						{"position": "Remove pseudoscientific content", "editors": [{"user": "SciEditor", "edit_count": 4, "role": "Removed the safety controversies section 4 times citing WP:FRINGE."}]}
					],
					"content_area": "vaccine safety controversy section",
					"severity": "high",
					"recommendation": "Seek WP:MedRS guidance on the talk page.",
					"escalation_trend": "rising"
				}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close()

	svc := NewAnalysisService(
		NewClient(Config{Provider: ProviderOpenAI, APIKey: "k", BaseURL: mockLLM.URL}, zerolog.Nop()),
		rdb, 5*time.Minute, zerolog.Nop(),
	)

	now := time.Now()
	seedTimeline(t, rdb, "COVID_Vaccine", []EditTimelineEntry{
		{User: "VaxSkeptic", Comment: "Added safety controversies section", ByteChange: 600, Timestamp: now.Add(-45 * time.Minute).Unix()},
		{User: "SciEditor", Comment: "Reverted - WP:FRINGE", ByteChange: -590, Timestamp: now.Add(-40 * time.Minute).Unix()},
		{User: "VaxSkeptic", Comment: "Restored with sources", ByteChange: 605, Timestamp: now.Add(-30 * time.Minute).Unix()},
		{User: "SciEditor", Comment: "Undid - pseudoscience not allowed", ByteChange: -600, Timestamp: now.Add(-20 * time.Minute).Unix()},
		{User: "VaxSkeptic", Comment: "Added back with CDC citation", ByteChange: 610, Timestamp: now.Add(-15 * time.Minute).Unix()},
		{User: "SciEditor", Comment: "Reverted again", ByteChange: -605, Timestamp: now.Add(-10 * time.Minute).Unix()},
		{User: "VaxSkeptic", Comment: "re-adding", ByteChange: 600, Timestamp: now.Add(-5 * time.Minute).Unix()},
		{User: "SciEditor", Comment: "rvv", ByteChange: -600, Timestamp: now.Unix()},
	})

	analysis, err := svc.Analyze(context.Background(), "COVID_Vaccine")
	require.NoError(t, err)

	// Verify stats block was sent to the LLM
	assert.Contains(t, capturedUserMsg, "Computed statistics",
		"stats block must be present in user prompt sent to LLM")
	assert.Contains(t, capturedUserMsg, "Total edits:    8",
		"stats block should report correct edit count")
	assert.Contains(t, capturedUserMsg, "Unique editors: 2",
		"stats block should report correct editor count")
	// "Reverted", "Undid", "Reverted again", "Rv" = 4 revert signals
	assert.Contains(t, capturedUserMsg, "Detected reverts (from edit summaries): 4",
		"stats block should count revert keywords correctly")

	// Output quality checks
	assert.False(t, analysis.IsVandalism)
	assert.Contains(t, analysis.Headline, "vaccine")
	assert.Equal(t, "high", analysis.Severity)
	assert.Equal(t, "rising", analysis.EscalationTrend)
	assert.Equal(t, 8, analysis.EditCount)
}

// TestE2E_NonEnglishPageTitle verifies that a non-ASCII page title is handled
// correctly through the analysis pipeline without panicking or encoding errors.
func TestE2E_NonEnglishPageTitle(t *testing.T) {
	rdb, _ := setupTestRedis(t)

	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		messages := req["messages"].([]interface{})
		userMsg := messages[1].(map[string]interface{})["content"].(string)
		// Page title should appear verbatim in the user prompt
		assert.Contains(t, userMsg, "Roe対Wade判決",
			"non-ASCII page title must appear verbatim in user prompt")

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": `{
					"is_vandalism": false,
					"headline": "Japanese editors dispute whether Roe v. Wade should be described as 'unpopular' in the lead.",
					"what_is_at_stake": "If Side A wins, the article calls the ruling unpopular. If Side B wins, it uses neutral language.",
					"summary": "4 reverts in 30 minutes over the word 'unpopular' in a Japanese Wikipedia article about Roe v. Wade.",
					"sides": [
						{"position": "Use 'unpopular'", "editors": [{"user": "EditorJA1", "edit_count": 2, "role": "Added '不人気' twice."}]},
						{"position": "Neutral language", "editors": [{"user": "EditorJA2", "edit_count": 2, "role": "Removed '不人気' twice citing ja:WP:NPOV."}]}
					],
					"content_area": "characterization of Supreme Court ruling in Japanese Wikipedia",
					"severity": "moderate",
					"recommendation": "Use ja:WP:3O process for third opinion.",
					"escalation_trend": "steady"
				}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close()

	svc := NewAnalysisService(
		NewClient(Config{Provider: ProviderOpenAI, APIKey: "k", BaseURL: mockLLM.URL}, zerolog.Nop()),
		rdb, 5*time.Minute, zerolog.Nop(),
	)

	now := time.Now()
	seedTimeline(t, rdb, "Roe対Wade判決", []EditTimelineEntry{
		{User: "EditorJA1", Comment: "不人気を追加", ByteChange: 10, Timestamp: now.Add(-30 * time.Minute).Unix()},
		{User: "EditorJA2", Comment: "Reverted", ByteChange: -10, Timestamp: now.Add(-20 * time.Minute).Unix()},
		{User: "EditorJA1", Comment: "再追加", ByteChange: 10, Timestamp: now.Add(-10 * time.Minute).Unix()},
		{User: "EditorJA2", Comment: "Reverted again", ByteChange: -10, Timestamp: now.Unix()},
	})

	analysis, err := svc.Analyze(context.Background(), "Roe対Wade判決")
	require.NoError(t, err, "non-ASCII page title should not cause an error")

	assert.Equal(t, "Roe対Wade判決", analysis.PageTitle)
	assert.Equal(t, 4, analysis.EditCount)
	assert.False(t, analysis.IsVandalism)
	t.Logf("Non-English E2E: page=%q headline=%q", analysis.PageTitle, analysis.Headline)
}
