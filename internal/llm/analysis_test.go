package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	assert.Contains(t, system, "Wikipedia edit war analyst")
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
	assert.Contains(t, user, "Diff content was not available") // no diffs passed
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
	assert.Contains(t, user, "EXACT text that was added or removed")
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
