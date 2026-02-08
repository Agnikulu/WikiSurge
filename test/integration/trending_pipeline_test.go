package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/processor"
	"github.com/Agnikulu/WikiSurge/internal/storage"
)

func TestTrendingPipeline_Integration(t *testing.T) {
	// Setup mini Redis
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	
	cfg := &config.Config{
		Redis: config.Redis{
			Trending: config.TrendingConfig{
				Enabled:         true,
				MaxPages:        1000,
				HalfLifeMinutes: 30.0,
			},
		},
	}
	
	// Create a mock time provider that can be controlled
	mockTimeProvider := storage.NewMockTimeProvider()
	
	// Create components
	scorer := storage.NewTrendingScorerWithTimeProvider(client, &cfg.Redis.Trending, mockTimeProvider)
	defer scorer.Stop()
	
	logger := zerolog.New(zerolog.NewTestWriter(t))
	aggregator := processor.NewTrendingAggregatorForTest(scorer, cfg, logger)
	
	// Test data: simulate Wikipedia edits over time
	testEdits := []struct {
		edit   *models.WikipediaEdit
		delay  time.Duration
		verify func(t *testing.T, scorer *storage.TrendingScorer)
	}{
		{
			// Initial high-scoring edit
			edit: &models.WikipediaEdit{
				Title: "Breaking News",
				Type:  "new",
				Bot:   false,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 0, New: 1500},
			},
			delay: 0,
			verify: func(t *testing.T, scorer *storage.TrendingScorer) {
				entries, err := scorer.GetTopTrending(1)
				require.NoError(t, err)
				require.Len(t, entries, 1)
				assert.Equal(t, "Breaking News", entries[0].PageTitle)
				// New page (2.0) + large edit (1.5) = 3.0
				assert.InDelta(t, 3.0, entries[0].CurrentScore, 0.1)
			},
		},
		{
			// Another page with regular edit
			edit: &models.WikipediaEdit{
				Title: "Regular Article",
				Type:  "edit",
				Bot:   false,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 300},
			},
			delay: 5 * time.Minute,
			verify: func(t *testing.T, scorer *storage.TrendingScorer) {
				entries, err := scorer.GetTopTrending(2)
				require.NoError(t, err)
				require.Len(t, entries, 2)
				
				// Breaking News should still be first (even with some decay)
				assert.Equal(t, "Breaking News", entries[0].PageTitle)
				assert.Equal(t, "Regular Article", entries[1].PageTitle)
			},
		},
		{
			// Bot edit on same page
			edit: &models.WikipediaEdit{
				Title: "Regular Article",
				Type:  "edit",
				Bot:   true,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 300, New: 350},
			},
			delay: 10 * time.Minute,
			verify: func(t *testing.T, scorer *storage.TrendingScorer) {
				entries, err := scorer.GetTopTrending(2)
				require.NoError(t, err)
				
				// Regular Article had initial score 1.0, decayed for 10 min, then +0.5 (bot)
				// After decay: 1.0 * 0.5^(10/30) ≈ 0.794 + 0.5 = ~1.294 raw score
				regularEntry := entries[1] // Should still be second
				assert.Equal(t, "Regular Article", regularEntry.PageTitle)
				assert.True(t, regularEntry.RawScore > 1.0 && regularEntry.RawScore < 1.6,
					"Expected decayed+accumulated score between 1.0 and 1.6, got %f", regularEntry.RawScore)
			},
		},
		{
			// Very old edit - should demonstrate decay
			edit: &models.WikipediaEdit{
				Title: "Old Page",
				Type:  "edit",
				Bot:   false,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 200},
			},
			delay: 60 * time.Minute, // 2 half-lives = 75% decay
			verify: func(t *testing.T, scorer *storage.TrendingScorer) {
				// After processing this edit, check that older pages have decayed
				entries, err := scorer.GetTopTrending(5)
				require.NoError(t, err)
				
				// Find the Breaking News entry
				var breakingEntry *storage.TrendingEntry
				for _, entry := range entries {
					if entry.PageTitle == "Breaking News" {
						breakingEntry = entry
						break
					}
				}
				require.NotNil(t, breakingEntry)
				
				// Should be significantly decayed from original 3.0
				assert.True(t, breakingEntry.CurrentScore < 1.0,
					"Breaking News should be heavily decayed after 60+ minutes")
			},
		},
	}
	
	for i, test := range testEdits {
		t.Logf("Running test step %d: %s", i+1, test.edit.Title)
		
		// Simulate time passing
		if test.delay > 0 {
			mockTimeProvider.AdvanceTime(test.delay)
		}
		
		// Process the edit through the aggregator
		err := aggregator.ProcessEdit(context.Background(), test.edit)
		require.NoError(t, err)
		
		// Run verification
		test.verify(t, scorer)
	}
}

func TestTrendingPipeline_LoadTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}
	
	// Setup
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	
	cfg := &config.Config{
		Redis: config.Redis{
			Trending: config.TrendingConfig{
				Enabled:         true,
				MaxPages:        1000,
				HalfLifeMinutes: 30.0,
			},
		},
	}
	
	scorer := storage.NewTrendingScorerForTest(client, &cfg.Redis.Trending)
	defer scorer.Stop()
	
	logger := zerolog.New(zerolog.NewTestWriter(t))
	aggregator := processor.NewTrendingAggregatorForTest(scorer, cfg, logger)
	
	// Generate test data: 10K edits across 500 pages
	const numEdits = 10000
	const numPages = 500
	
	start := time.Now()
	
	for i := 0; i < numEdits; i++ {
		pageNum := i % numPages
		edit := &models.WikipediaEdit{
			Title: fmt.Sprintf("Page_%d", pageNum),
			Type:  "edit",
			Bot:   i%10 == 0, // 10% bot edits
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{
				Old: 100,
				New: 100 + (i % 1000), // Variable sizes
			},
		}
		
		err := aggregator.ProcessEdit(context.Background(), edit)
		require.NoError(t, err)
		
		// Every 1000 edits, check that system is still responsive
		if i%1000 == 0 && i > 0 {
			_, err := scorer.GetTopTrending(10)
			require.NoError(t, err)
		}
	}
	
	duration := time.Since(start)
	
	// Performance assertions
	avgLatency := duration / numEdits
	t.Logf("Processed %d edits in %v (avg: %v per edit)", numEdits, duration, avgLatency)
	
	// Should be under 5ms per edit as specified
	assert.True(t, avgLatency < 5*time.Millisecond,
		"Average update latency should be < 5ms, got %v", avgLatency)
	
	// Verify system state
	entries, err := scorer.GetTopTrending(100)
	require.NoError(t, err)
	assert.True(t, len(entries) > 0, "Should have trending entries")
	assert.True(t, len(entries) <= 100, "Should respect limit")
	
	// Verify entries are properly sorted
	for i := 1; i < len(entries); i++ {
		assert.True(t, entries[i-1].CurrentScore >= entries[i].CurrentScore,
			"Entries should be sorted by score")
	}
	
	// Test ranking functionality
	if len(entries) > 0 {
		topPageTitle := entries[0].PageTitle
		rank, err := scorer.GetTrendingRank(topPageTitle)
		require.NoError(t, err)
		assert.Equal(t, 0, rank, "Top page should have rank 0")
	}
}

func TestTrendingPipeline_MessageProcessing(t *testing.T) {
	// Setup
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	
	cfg := &config.Config{
		Redis: config.Redis{
			Trending: config.TrendingConfig{
				Enabled:         true,
				MaxPages:        1000,
				HalfLifeMinutes: 30.0,
			},
		},
	}
	
	scorer := storage.NewTrendingScorerForTest(client, &cfg.Redis.Trending)
	defer scorer.Stop()
	
	logger := zerolog.New(zerolog.NewTestWriter(t))
	aggregator := processor.NewTrendingAggregatorForTest(scorer, cfg, logger)
	
	// Test various message formats
	tests := []struct {
		name    string
		message []byte
		wantErr bool
	}{
		{
			name: "valid message",
			message: func() []byte {
				edit := &models.WikipediaEdit{
					Title: "Test Page",
					Type:  "edit",
					Bot:   false,
					Length: struct {
						Old int `json:"old"`
						New int `json:"new"`
					}{Old: 100, New: 200},
				}
				data, _ := json.Marshal(edit)
				return data
			}(),
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			message: []byte(`{"invalid": json`),
			wantErr: true,
		},
		{
			name:    "empty message",
			message: []byte(``),
			wantErr: true,
		},
		{
			name:    "null message",
			message: nil,
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := aggregator.ProcessMessage(tt.message)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}