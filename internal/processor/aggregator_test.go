package processor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
)

func setupTestTrendingAggregator(t *testing.T) (*TrendingAggregator, *miniredis.Miniredis) {
	// Start mini Redis server
	mr, err := miniredis.Run()
	require.NoError(t, err)
	
	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	
	// Create test config
	cfg := &config.Config{
		Redis: config.Redis{
			Trending: config.TrendingConfig{
				Enabled:         true,
				MaxPages:        1000,
				HalfLifeMinutes: 30.0,
			},
		},
	}
	
	// Create trending scorer
	scorer := storage.NewTrendingScorerForTest(client, &cfg.Redis.Trending)
	
	// Create logger
	logger := zerolog.New(zerolog.NewTestWriter(t))
	
	// Create aggregator
	aggregator := NewTrendingAggregatorForTest(scorer, cfg, logger)
	
	return aggregator, mr
}

func TestTrendingAggregator_ProcessMessage(t *testing.T) {
	aggregator, mr := setupTestTrendingAggregator(t)
	defer mr.Close()
	defer aggregator.scorer.Stop()
	
	edit := &models.WikipediaEdit{
		ID:        123,
		Type:      "edit",
		Title:     "Test Page",
		User:      "TestUser",
		Bot:       false,
		Wiki:      "enwiki",
		Timestamp: 1234567890,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 300},
	}
	
	// Serialize edit to JSON
	message, err := json.Marshal(edit)
	require.NoError(t, err)
	
	// Process message
	err = aggregator.ProcessMessage(message)
	assert.NoError(t, err)
	
	// Verify the edit was processed
	entries, err := aggregator.scorer.GetTopTrending(1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "Test Page", entries[0].PageTitle)
}

func TestTrendingAggregator_ProcessMessage_InvalidJSON(t *testing.T) {
	aggregator, mr := setupTestTrendingAggregator(t)
	defer mr.Close()
	defer aggregator.scorer.Stop()
	
	// Invalid JSON message
	invalidMessage := []byte(`{"invalid": json`)
	
	// Should return error
	err := aggregator.ProcessMessage(invalidMessage)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal")
}

func TestTrendingAggregator_ProcessEdit(t *testing.T) {
	aggregator, mr := setupTestTrendingAggregator(t)
	defer mr.Close()
	defer aggregator.scorer.Stop()
	
	tests := []struct {
		name string
		edit *models.WikipediaEdit
	}{
		{
			name: "regular edit",
			edit: &models.WikipediaEdit{
				Title: "Regular Page",
				Type:  "edit",
				Bot:   false,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 200},
			},
		},
		{
			name: "bot edit", 
			edit: &models.WikipediaEdit{
				Title: "Bot Page",
				Type:  "edit",
				Bot:   true,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 150},
			},
		},
		{
			name: "new page",
			edit: &models.WikipediaEdit{
				Title: "New Page",
				Type:  "new",
				Bot:   false,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 0, New: 500},
			},
		},
		{
			name: "large edit",
			edit: &models.WikipediaEdit{
				Title: "Large Edit",
				Type:  "edit",
				Bot:   false,
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 1200}, // 1100 byte change
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := aggregator.ProcessEdit(context.Background(), tt.edit)
			assert.NoError(t, err)
		})
	}
	
	// Verify all edits were processed
	entries, err := aggregator.scorer.GetTopTrending(10)
	require.NoError(t, err)
	require.Len(t, entries, 4)
	
	// New page should have highest score (2.0)
	assert.Equal(t, "New Page", entries[0].PageTitle)
	assert.Equal(t, 2.0, entries[0].CurrentScore)
	
	// Large edit should be second (1.5)
	assert.Equal(t, "Large Edit", entries[1].PageTitle)
	assert.Equal(t, 1.5, entries[1].CurrentScore)
	
	// Regular edit should be third (1.0)
	assert.Equal(t, "Regular Page", entries[2].PageTitle)
	assert.Equal(t, 1.0, entries[2].CurrentScore)
	
	// Bot edit should be last (0.5)
	assert.Equal(t, "Bot Page", entries[3].PageTitle)
	assert.Equal(t, 0.5, entries[3].CurrentScore)
}

func TestTrendingAggregator_GetMetrics(t *testing.T) {
	aggregator, mr := setupTestTrendingAggregator(t)
	defer mr.Close()
	defer aggregator.scorer.Stop()
	
	metrics := aggregator.GetMetrics()
	assert.NotNil(t, metrics)
	assert.NotNil(t, metrics.EditsProcessed)
	assert.NotNil(t, metrics.ProcessError)
	assert.NotNil(t, metrics.UpdateLatency)
}

func BenchmarkTrendingAggregator_ProcessEdit(b *testing.B) {
	// Setup
	mr, err := miniredis.Run()
	require.NoError(b, err)
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
	
	logger := zerolog.Nop() // No-op logger for performance
	aggregator := NewTrendingAggregatorForTest(scorer, cfg, logger)
	
	edit := &models.WikipediaEdit{
		Title: "Benchmark Page",
		Type:  "edit",
		Bot:   false,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200},
	}
	
	// Reset timer and run benchmark
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		err := aggregator.ProcessEdit(context.Background(), edit)
		require.NoError(b, err)
	}
}