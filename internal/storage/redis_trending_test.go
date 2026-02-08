package storage

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

func setupTestTrendingScorer(t *testing.T) (*TrendingScorer, *miniredis.Miniredis) {
	// Start mini Redis server
	mr, err := miniredis.Run()
	require.NoError(t, err)
	
	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	
	// Create test config
	cfg := &config.TrendingConfig{
		Enabled:         true,
		MaxPages:        1000,
		HalfLifeMinutes: 30.0,
		PruneInterval:   time.Minute,
	}
	
	scorer := NewTrendingScorerForTest(client, cfg) // Use test constructor
	return scorer, mr
}

func TestTrendingScorer_IncrementScore(t *testing.T) {
	scorer, mr := setupTestTrendingScorer(t)
	defer mr.Close()
	defer scorer.Stop()
	
	tests := []struct {
		name      string
		pageTitle string
		increment float64
		wantErr   bool
	}{
		{
			name:      "basic increment",
			pageTitle: "Test Page",
			increment: 1.0,
			wantErr:   false,
		},
		{
			name:      "multiple increments same page",
			pageTitle: "Test Page",
			increment: 0.5,
			wantErr:   false,
		},
		{
			name:      "different page",
			pageTitle: "Another Page",
			increment: 2.0,
			wantErr:   false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := scorer.IncrementScore(tt.pageTitle, tt.increment)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
	
	// Verify scores were stored
	entries, err := scorer.GetTopTrending(10)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	
	// Should be sorted by current score
	assert.Equal(t, "Another Page", entries[0].PageTitle)
	assert.Equal(t, 2.0, entries[0].CurrentScore)
	assert.Equal(t, "Test Page", entries[1].PageTitle)
	assert.Equal(t, 1.5, entries[1].CurrentScore) // 1.0 + 0.5
}

func TestTrendingScorer_LazyDecay(t *testing.T) {
	scorer, mr := setupTestTrendingScorer(t)
	defer mr.Close()
	defer scorer.Stop()
	
	// Use mock time provider for controlled testing
	mockTime := &MockTimeProvider{currentTime: time.Now()}
	scorer.timeProvider = mockTime
	
	// Add initial score
	err := scorer.IncrementScore("Test Page", 100.0)
	require.NoError(t, err)
	
	// Simulate 30 minutes passing (one half-life)
	mockTime.FastForward(30 * time.Minute)
	
	// Add another increment - this should trigger lazy decay
	err = scorer.IncrementScore("Test Page", 10.0)
	require.NoError(t, err)
	
	// Get the page 
	entries, err := scorer.GetTopTrending(1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	
	// After 30 minutes (1 half-life), 100 should decay to ~50, then add 10 = ~60
	expectedScore := 50.0 + 10.0
	tolerance := 0.1
	assert.InDelta(t, expectedScore, entries[0].CurrentScore, tolerance)
}

func TestTrendingScorer_GetTopTrending(t *testing.T) {
	scorer, mr := setupTestTrendingScorer(t)
	defer mr.Close()
	defer scorer.Stop()
	
	// Add scores for multiple pages with different timestamps
	pages := []struct {
		title string
		score float64
		delay time.Duration
	}{
		{"Page A", 10.0, 0},
		{"Page B", 8.0, 10 * time.Minute},  // Older
		{"Page C", 12.0, 0},
		{"Page D", 5.0, 20 * time.Minute},  // Much older
		{"Page E", 15.0, 0},
	}
	
	// Add pages with different timestamps to simulate decay
	baseTime := time.Now().Unix()
	
	for _, p := range pages {
		err := scorer.IncrementScore(p.title, p.score)
		require.NoError(t, err)
		
		// Manually adjust the timestamp if needed
		if p.delay > 0 {
			adjustedTime := baseTime - int64(p.delay.Seconds())
			pageKey := fmt.Sprintf("trending:%s", p.title)
			err = scorer.redis.HSet(context.Background(), pageKey, "last_updated", adjustedTime).Err()
			require.NoError(t, err)
		}
	}
	
	// Get top 3 trending
	entries, err := scorer.GetTopTrending(3)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	
	// Should be sorted by current score (recent pages rank higher)
	assert.Equal(t, "Page E", entries[0].PageTitle)
	assert.Equal(t, "Page C", entries[1].PageTitle)
	assert.Equal(t, "Page A", entries[2].PageTitle)
	
	// Verify scores are properly decayed
	for _, entry := range entries {
		assert.True(t, entry.CurrentScore <= entry.RawScore, 
			"Current score should be <= raw score due to decay")
		assert.True(t, entry.LastUpdated > 0, 
			"Last updated should be set")
	}
}

func TestTrendingScorer_GetTrendingRank(t *testing.T) {
	scorer, mr := setupTestTrendingScorer(t)
	defer mr.Close()
	defer scorer.Stop()
	
	// Add pages with different scores
	pages := []struct {
		title string
		score float64
	}{
		{"Top Page", 100.0},
		{"Second Page", 50.0}, 
		{"Third Page", 25.0},
	}
	
	for _, p := range pages {
		err := scorer.IncrementScore(p.title, p.score)
		require.NoError(t, err)
	}
	
	tests := []struct {
		name         string
		pageTitle    string
		expectedRank int
	}{
		{"top page", "Top Page", 0},
		{"second page", "Second Page", 1},
		{"third page", "Third Page", 2},
		{"non-existent page", "Missing Page", -1},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rank, err := scorer.GetTrendingRank(tt.pageTitle)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedRank, rank)
		})
	}
}

func TestTrendingScorer_CalculateIncrement(t *testing.T) {
	scorer, mr := setupTestTrendingScorer(t)
	defer mr.Close()
	defer scorer.Stop()
	
	tests := []struct {
		name     string
		edit     *models.WikipediaEdit
		expected float64
	}{
		{
			name: "basic edit",
			edit: &models.WikipediaEdit{
				Title: "Test",
				Type:  "edit",
				Bot:   false,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 200},
			},
			expected: 1.0,
		},
		{
			name: "large edit",
			edit: &models.WikipediaEdit{
				Title: "Test",
				Type:  "edit",
				Bot:   false,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 1200}, // 1100 byte change
			},
			expected: 1.5, // 1.0 * 1.5 for large edit
		},
		{
			name: "bot edit",
			edit: &models.WikipediaEdit{
				Title: "Test",
				Type:  "edit", 
				Bot:   true,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 200},
			},
			expected: 0.5, // 1.0 * 0.5 for bot
		},
		{
			name: "new page",
			edit: &models.WikipediaEdit{
				Title: "Test",
				Type:  "new",
				Bot:   false,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 0, New: 500},
			},
			expected: 2.0, // 1.0 * 2.0 for new page
		},
		{
			name: "large bot edit on new page",
			edit: &models.WikipediaEdit{
				Title: "Test",
				Type:  "new",
				Bot:   true,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`  
				}{Old: 0, New: 1500}, // Large new page by bot
			},
			expected: 1.5, // 1.0 * 1.5 * 0.5 * 2.0 = 1.5
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scorer.calculateIncrement(tt.edit)
			assert.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestTrendingScorer_ProcessEdit(t *testing.T) {
	scorer, mr := setupTestTrendingScorer(t)
	defer mr.Close()
	defer scorer.Stop()
	
	edit := &models.WikipediaEdit{
		Title: "Test Page",
		Type:  "edit",
		Bot:   false,
		User:  "TestUser",
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200},
	}
	
	// Process edit
	err := scorer.ProcessEdit(edit)
	assert.NoError(t, err)
	
	// Verify it was processed
	entries, err := scorer.GetTopTrending(1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "Test Page", entries[0].PageTitle)
	assert.Equal(t, 1.0, entries[0].CurrentScore)
}

func TestTrendingScorer_PruneTrendingSet(t *testing.T) {
	scorer, mr := setupTestTrendingScorer(t)
	defer mr.Close()
	defer scorer.Stop()
	
	// Override max pages for testing
	scorer.maxPages = 3
	
	// Add many pages
	for i := 0; i < 10; i++ {
		title := fmt.Sprintf("Page %d", i)
		score := float64(i + 1)
		err := scorer.IncrementScore(title, score)
		require.NoError(t, err)
	}
	
	// Verify all pages were added
	entries, err := scorer.GetTopTrending(20)
	require.NoError(t, err)
	assert.Len(t, entries, 10)
	
	// Run pruning
	count, err := scorer.pruneTrendingSet()
	require.NoError(t, err)
	assert.True(t, count > 0, "Should have pruned some entries")
	
	// Verify only top pages remain
	entries, err = scorer.GetTopTrending(20)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(entries), scorer.maxPages)
	
	// Top pages should still be there
	topTitles := []string{"Page 9", "Page 8", "Page 7"}
	for i, entry := range entries[:3] {
		assert.Equal(t, topTitles[i], entry.PageTitle)
	}
}

// Test that demonstrates lazy decay only happens when needed
func TestTrendingScorer_LazyDecayOptimization(t *testing.T) {
	scorer, mr := setupTestTrendingScorer(t)
	defer mr.Close()
	defer scorer.Stop()
	
	// Use mock time provider for controlled testing
	mockTime := &MockTimeProvider{currentTime: time.Now()}
	scorer.timeProvider = mockTime
	
	// Add score at T=0
	err := scorer.IncrementScore("Test Page", 100.0)
	require.NoError(t, err)
	
	// Verify raw score is stored correctly
	ctx := context.Background()
	rawScore, err := scorer.redis.HGet(ctx, "trending:Test Page", "raw_score").Result()
	require.NoError(t, err)
	assert.Equal(t, "100", rawScore)
	
	// Simulate time passing but don't access the page
	mockTime.FastForward(30 * time.Minute)
	
	// Raw score should still be 100 (no decay applied yet)
	rawScore, err = scorer.redis.HGet(ctx, "trending:Test Page", "raw_score").Result()
	require.NoError(t, err)
	assert.Equal(t, "100", rawScore) // Still 100 - lazy decay
	
	// Now access the page - this should trigger decay calculation
	entries, err := scorer.GetTopTrending(1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	
	// Current score should be decayed but raw score unchanged until next update
	assert.InDelta(t, 50.0, entries[0].CurrentScore, 0.1) // Decayed
	assert.Equal(t, 100.0, entries[0].RawScore)            // Still original
	
	// Now update the score - this should apply decay to raw score
	err = scorer.IncrementScore("Test Page", 1.0)
	require.NoError(t, err)
	
	// Raw score should now be decayed + increment
	rawScore, err = scorer.redis.HGet(ctx, "trending:Test Page", "raw_score").Result()
	require.NoError(t, err)
	
	// Should be approximately 50 + 1 = 51
	rawScoreFloat, err := strconv.ParseFloat(rawScore, 64)
	require.NoError(t, err)
	assert.InDelta(t, 51.0, rawScoreFloat, 0.1)
}