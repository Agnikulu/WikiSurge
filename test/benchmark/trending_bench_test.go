package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/processor"
	"github.com/Agnikulu/WikiSurge/internal/storage"
)

// BenchmarkTrendingScorer_IncrementScore tests raw trending score update performance
func BenchmarkTrendingScorer_IncrementScore(b *testing.B) {
	// Setup
	mr, err := miniredis.Run()
	require.NoError(b, err)
	defer mr.Close()
	
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	
	cfg := &config.TrendingConfig{
		Enabled:         true,
		MaxPages:        10000,
		HalfLifeMinutes: 30.0,
	}
	
	scorer := storage.NewTrendingScorer(client, cfg)
	defer scorer.Stop()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		pageTitle := fmt.Sprintf("Page_%d", i%1000) // 1000 unique pages
		err := scorer.IncrementScore(pageTitle, 1.0)
		require.NoError(b, err)
	}
}

// BenchmarkTrendingScorer_GetTopTrending tests trending retrieval performance
func BenchmarkTrendingScorer_GetTopTrending(b *testing.B) {
	// Setup with pre-populated data
	mr, err := miniredis.Run()
	require.NoError(b, err)
	defer mr.Close()
	
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	
	cfg := &config.TrendingConfig{
		Enabled:         true,
		MaxPages:        10000,
		HalfLifeMinutes: 30.0,
	}
	
	scorer := storage.NewTrendingScorer(client, cfg)
	defer scorer.Stop()
	
	// Pre-populate with 1000 pages
	for i := 0; i < 1000; i++ {
		pageTitle := fmt.Sprintf("Page_%d", i)
		score := float64(1000 - i) // Decreasing scores
		err := scorer.IncrementScore(pageTitle, score)
		require.NoError(b, err)
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_, err := scorer.GetTopTrending(20)
		require.NoError(b, err)
	}
}

// BenchmarkTrendingAggregator_ProcessMessage tests message processing performance
func BenchmarkTrendingAggregator_ProcessMessage(b *testing.B) {
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
				MaxPages:        10000,
				HalfLifeMinutes: 30.0,
			},
		},
	}
	
	scorer := storage.NewTrendingScorer(client, &cfg.Redis.Trending)
	defer scorer.Stop()
	
	logger := zerolog.New(zerolog.NewTestWriter(b))
	aggregator := processor.NewTrendingAggregator(scorer, cfg, logger)
	
	// Pre-create messages
	messages := make([][]byte, 100)
	for i := 0; i < 100; i++ {
		edit := &models.WikipediaEdit{
			Title: fmt.Sprintf("Page_%d", i),
			Type:  "edit",
			Bot:   i%10 == 0, // 10% bots
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{
				Old: 100,
				New: 100 + (i % 1000),
			},
		}
		data, _ := json.Marshal(edit)
		messages[i] = data
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		message := messages[i%len(messages)]
		err := aggregator.ProcessMessage(message)
		require.NoError(b, err)
	}
}

// BenchmarkTrendingPipeline_FullPipeline tests end-to-end performance
func BenchmarkTrendingPipeline_FullPipeline(b *testing.B) {
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
	
	scorer := storage.NewTrendingScorer(client, &cfg.Redis.Trending)
	defer scorer.Stop()
	
	logger := zerolog.Nop() // No-op logger for performance
	aggregator := processor.NewTrendingAggregator(scorer, cfg, logger)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		// Create edit
		edit := &models.WikipediaEdit{
			Title: fmt.Sprintf("Page_%d", i%500), // 500 unique pages
			Type:  "edit",
			Bot:   i%20 == 0, // 5% bots
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{
				Old: 100,
				New: 100 + (i % 500),
			},
		}
		
		// Process through full pipeline
		err := aggregator.ProcessEdit(context.Background(), edit)
		require.NoError(b, err)
		
		// Every 100 iterations, check trending (simulates API calls)
		if i%100 == 0 {
			_, err := scorer.GetTopTrending(10)
			require.NoError(b, err)
		}
	}
}

// BenchmarkTrendingScorer_LazyDecay tests lazy decay performance impact
func BenchmarkTrendingScorer_LazyDecay(b *testing.B) {
	mr, err := miniredis.Run()
	require.NoError(b, err)
	defer mr.Close()
	
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	
	cfg := &config.TrendingConfig{
		Enabled:         true,
		MaxPages:        1000,
		HalfLifeMinutes: 30.0,
	}
	
	scorer := storage.NewTrendingScorer(client, cfg)
	defer scorer.Stop()
	
	// Pre-populate with old data
	for i := 0; i < 100; i++ {
		pageTitle := fmt.Sprintf("OldPage_%d", i)
		err := scorer.IncrementScore(pageTitle, 100.0)
		require.NoError(b, err)
	}
	
	// Simulate time passing
	mr.FastForward(60 * time.Minute) // 2 half-lives
	
	b.ResetTimer()
	
	// Test performance of updates to old pages (triggers decay)
	for i := 0; i < b.N; i++ {
		pageTitle := fmt.Sprintf("OldPage_%d", i%100)
		err := scorer.IncrementScore(pageTitle, 1.0) // This triggers lazy decay
		require.NoError(b, err)
	}
}

// BenchmarkTrendingScorer_PruneTrendingSet tests pruning performance
func BenchmarkTrendingScorer_PruneTrendingSet(b *testing.B) {
	mr, err := miniredis.Run()
	require.NoError(b, err)
	defer mr.Close()
	
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	
	cfg := &config.TrendingConfig{
		Enabled:         true,
		MaxPages:        1000,
		HalfLifeMinutes: 30.0,
	}
	
	scorer := storage.NewTrendingScorer(client, cfg)
	defer scorer.Stop()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		// Pre-populate with data each iteration
		for j := 0; j < 2000; j++ { // Exceed max pages
			pageTitle := fmt.Sprintf("Page_%d_%d", i, j)
			score := float64(j)
			err := scorer.IncrementScore(pageTitle, score)
			require.NoError(b, err)
		}
		
		// Time the pruning operation - use IncrementScore with 0 to trigger cleanup via public API
		// Since pruneTrendingSet is unexported, we test via the GetTopTrending path
		_, err := scorer.GetTopTrending(10)
		require.NoError(b, err)
		
		// Clean up for next iteration
		client.FlushAll(context.Background())
	}
}

// Benchmark results helper
func BenchmarkResults(b *testing.B) {
	b.Skip("This is not a real benchmark - just documentation")
	
	// Expected results on a modern machine:
	// BenchmarkTrendingScorer_IncrementScore:    ~1000-5000 ns/op  (target: <5ms = 5,000,000 ns)
	// BenchmarkTrendingScorer_GetTopTrending:    ~10000-50000 ns/op
	// BenchmarkTrendingAggregator_ProcessMessage: ~2000-8000 ns/op
	// BenchmarkTrendingPipeline_FullPipeline:    ~3000-10000 ns/op
	// BenchmarkTrendingScorer_LazyDecay:         ~2000-8000 ns/op  (should be similar to regular updates)
	// BenchmarkTrendingScorer_PruneTrendingSet:  ~1ms-10ms per prune cycle
}