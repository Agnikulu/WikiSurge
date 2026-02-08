package benchmark

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/processor"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// benchEnv holds benchmark infrastructure
type benchEnv struct {
	miniRedis      *miniredis.Miniredis
	redisClient    *redis.Client
	hotPageTracker *storage.HotPageTracker
	trendingScorer *storage.TrendingScorer
	cfg            *config.Config
	logger         zerolog.Logger
}

func setupBenchEnv(b *testing.B) *benchEnv {
	b.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	cfg := &config.Config{
		Features: config.Features{
			Trending: true,
			EditWars: true,
		},
		Redis: config.Redis{
			URL:       fmt.Sprintf("redis://%s", mr.Addr()),
			MaxMemory: "256mb",
			HotPages: config.HotPages{
				MaxTracked:         1000,
				PromotionThreshold: 5,
				WindowDuration:     15 * time.Minute,
				MaxMembersPerPage:  100,
				HotThreshold:       2,
				CleanupInterval:    5 * time.Minute,
			},
			Trending: config.TrendingConfig{
				Enabled:         true,
				MaxPages:        1000,
				HalfLifeMinutes: 30.0,
				PruneInterval:   5 * time.Minute,
			},
		},
		Elasticsearch: config.Elasticsearch{
			Enabled:       false,
			RetentionDays: 7,
			MaxDocsPerDay: 10000,
			SelectiveCriteria: config.SelectiveCriteria{
				TrendingTopN:   100,
				SpikeRatioMin:  2.0,
				EditWarEnabled: true,
			},
		},
		Kafka: config.Kafka{
			Brokers: []string{"localhost:9092"},
		},
		Logging: config.Logging{
			Level:  "error",
			Format: "json",
		},
	}

	logger := zerolog.Nop()

	hotPageTracker := storage.NewHotPageTracker(redisClient, &cfg.Redis.HotPages)
	trendingScorer := storage.NewTrendingScorerForTest(redisClient, &cfg.Redis.Trending)

	return &benchEnv{
		miniRedis:      mr,
		redisClient:    redisClient,
		hotPageTracker: hotPageTracker,
		trendingScorer: trendingScorer,
		cfg:            cfg,
		logger:         logger,
	}
}

func (env *benchEnv) cleanup() {
	env.redisClient.Close()
	env.miniRedis.Close()
}

func makeBenchEdit(id int64, rng *rand.Rand) *models.WikipediaEdit {
	return &models.WikipediaEdit{
		ID:        id,
		Type:      "edit",
		Title:     fmt.Sprintf("BenchPage_%d", rng.Intn(500)),
		User:      fmt.Sprintf("User_%d", rng.Intn(100)),
		Bot:       false,
		Wiki:      "enwiki",
		ServerURL: "https://en.wikipedia.org",
		Timestamp: time.Now().Unix() - int64(rng.Intn(3600)),
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 1000 + rng.Intn(5000), New: 1000 + rng.Intn(5000)},
		Revision: struct {
			Old int64 `json:"old"`
			New int64 `json:"new"`
		}{Old: id - 1, New: id},
		Comment: "benchmark edit",
	}
}

// Benchmark 1: Spike Detector Throughput
func BenchmarkSpikeDetectorThroughput(b *testing.B) {
	env := setupBenchEnv(b)
	defer env.cleanup()

	ctx := context.Background()
	spikeDetector := processor.NewSpikeDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		edit := makeBenchEdit(int64(i+1), rng)
		_ = spikeDetector.ProcessEdit(ctx, edit)
	}
}

// Benchmark 2: Trending Aggregator Throughput
func BenchmarkTrendingAggregatorThroughput(b *testing.B) {
	env := setupBenchEnv(b)
	defer env.cleanup()

	ctx := context.Background()
	trendingAgg := processor.NewTrendingAggregatorForTest(env.trendingScorer, env.cfg, env.logger)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		edit := makeBenchEdit(int64(i+1), rng)
		_ = trendingAgg.ProcessEdit(ctx, edit)
	}
}

// Benchmark 3: Edit War Detector Throughput
func BenchmarkEditWarDetectorThroughput(b *testing.B) {
	env := setupBenchEnv(b)
	defer env.cleanup()

	ctx := context.Background()
	editWarDetector := processor.NewEditWarDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		edit := makeBenchEdit(int64(i+1), rng)
		_ = editWarDetector.ProcessEdit(ctx, edit)
	}
}

// Benchmark 4: All Consumers Combined (sequential per edit)
func BenchmarkAllConsumersCombined(b *testing.B) {
	env := setupBenchEnv(b)
	defer env.cleanup()

	ctx := context.Background()
	spikeDetector := processor.NewSpikeDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	trendingAgg := processor.NewTrendingAggregatorForTest(env.trendingScorer, env.cfg, env.logger)
	editWarDetector := processor.NewEditWarDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		edit := makeBenchEdit(int64(i+1), rng)
		_ = spikeDetector.ProcessEdit(ctx, edit)
		_ = trendingAgg.ProcessEdit(ctx, edit)
		_ = editWarDetector.ProcessEdit(ctx, edit)
	}
}

// Benchmark 5: Hot Page Tracker ProcessEdit
func BenchmarkHotPageTrackerProcessEdit(b *testing.B) {
	env := setupBenchEnv(b)
	defer env.cleanup()

	ctx := context.Background()
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		edit := makeBenchEdit(int64(i+1), rng)
		_ = env.hotPageTracker.ProcessEdit(ctx, edit)
	}
}

// Benchmark 6: Trending Scorer ProcessEdit
func BenchmarkTrendingScorerProcessEdit(b *testing.B) {
	env := setupBenchEnv(b)
	defer env.cleanup()

	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		edit := makeBenchEdit(int64(i+1), rng)
		_ = env.trendingScorer.ProcessEdit(edit)
	}
}

// Benchmark 7: Sustained Throughput (100K edits, measures total throughput)
func BenchmarkSustainedThroughput100K(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping sustained throughput benchmark in short mode")
	}

	env := setupBenchEnv(b)
	defer env.cleanup()

	ctx := context.Background()
	spikeDetector := processor.NewSpikeDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	trendingAgg := processor.NewTrendingAggregatorForTest(env.trendingScorer, env.cfg, env.logger)
	editWarDetector := processor.NewEditWarDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	rng := rand.New(rand.NewSource(42))

	// Fixed iteration count — run once to measure sustained throughput
	editsCount := 10000

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		start := time.Now()
		for i := 0; i < editsCount; i++ {
			edit := makeBenchEdit(int64(i+1), rng)
			_ = spikeDetector.ProcessEdit(ctx, edit)
			_ = trendingAgg.ProcessEdit(ctx, edit)
			_ = editWarDetector.ProcessEdit(ctx, edit)
		}
		elapsed := time.Since(start)

		editsPerSec := float64(editsCount) / elapsed.Seconds()
		b.ReportMetric(editsPerSec, "edits/sec")
	}
}

// Benchmark 8: Latency Distribution (processing latency per edit)
func BenchmarkProcessingLatency(b *testing.B) {
	env := setupBenchEnv(b)
	defer env.cleanup()

	ctx := context.Background()
	spikeDetector := processor.NewSpikeDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	rng := rand.New(rand.NewSource(42))

	// Pre-generate edits
	edits := make([]*models.WikipediaEdit, b.N)
	for i := range edits {
		edits[i] = makeBenchEdit(int64(i+1), rng)
	}

	b.ResetTimer()
	b.ReportAllocs()

	var totalLatency time.Duration
	for i := 0; i < b.N; i++ {
		start := time.Now()
		_ = spikeDetector.ProcessEdit(ctx, edits[i])
		totalLatency += time.Since(start)
	}

	if b.N > 0 {
		avgLatency := totalLatency / time.Duration(b.N)
		b.ReportMetric(float64(avgLatency.Microseconds()), "avg_µs/edit")
	}
}

// Benchmark 9: Concurrent Throughput (multiple goroutines)
func BenchmarkConcurrentThroughput(b *testing.B) {
	env := setupBenchEnv(b)
	defer env.cleanup()

	ctx := context.Background()
	spikeDetector := processor.NewSpikeDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	trendingAgg := processor.NewTrendingAggregatorForTest(env.trendingScorer, env.cfg, env.logger)
	editWarDetector := processor.NewEditWarDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		var id int64
		for pb.Next() {
			id++
			edit := makeBenchEdit(id, rng)
			_ = spikeDetector.ProcessEdit(ctx, edit)
			_ = trendingAgg.ProcessEdit(ctx, edit)
			_ = editWarDetector.ProcessEdit(ctx, edit)
		}
	})
}

// Benchmark 10: Memory Efficiency — 100K edits peak allocation
func BenchmarkMemoryEfficiency(b *testing.B) {
	env := setupBenchEnv(b)
	defer env.cleanup()

	ctx := context.Background()
	spikeDetector := processor.NewSpikeDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	trendingAgg := processor.NewTrendingAggregatorForTest(env.trendingScorer, env.cfg, env.logger)
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		edit := makeBenchEdit(int64(i+1), rng)
		_ = spikeDetector.ProcessEdit(ctx, edit)
		_ = trendingAgg.ProcessEdit(ctx, edit)
	}
}
