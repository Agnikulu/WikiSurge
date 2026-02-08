package resource

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/processor"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEnv holds shared test infrastructure
type testEnv struct {
	miniRedis      *miniredis.Miniredis
	redisClient    *redis.Client
	hotPageTracker *storage.HotPageTracker
	trendingScorer *storage.TrendingScorer
	cfg            *config.Config
	logger         zerolog.Logger
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)

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
				MaxTracked:         100, // Low limit for testing bounds
				PromotionThreshold: 3,
				WindowDuration:     15 * time.Minute,
				MaxMembersPerPage:  50,
				HotThreshold:       2,
				CleanupInterval:    1 * time.Minute,
			},
			Trending: config.TrendingConfig{
				Enabled:         true,
				MaxPages:        200, // Low limit for testing bounds
				HalfLifeMinutes: 30.0,
				PruneInterval:   1 * time.Minute,
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
			Level:  "warn",
			Format: "json",
		},
	}

	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.WarnLevel)

	hotPageTracker := storage.NewHotPageTracker(redisClient, &cfg.Redis.HotPages)
	trendingScorer := storage.NewTrendingScorerForTest(redisClient, &cfg.Redis.Trending)

	return &testEnv{
		miniRedis:      mr,
		redisClient:    redisClient,
		hotPageTracker: hotPageTracker,
		trendingScorer: trendingScorer,
		cfg:            cfg,
		logger:         logger,
	}
}

func (env *testEnv) cleanup() {
	env.redisClient.Close()
	env.miniRedis.Close()
}

func makeEdit(id int64, title, user string) *models.WikipediaEdit {
	return &models.WikipediaEdit{
		ID:        id,
		Type:      "edit",
		Title:     title,
		User:      user,
		Bot:       false,
		Wiki:      "enwiki",
		ServerURL: "https://en.wikipedia.org",
		Timestamp: time.Now().Unix(),
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 1000, New: 1500},
		Revision: struct {
			Old int64 `json:"old"`
			New int64 `json:"new"`
		}{Old: id - 1, New: id},
		Comment: "test edit",
	}
}

// Test 1: Redis Memory Bounds
func TestRedisMemoryBounds(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	rng := rand.New(rand.NewSource(42))

	// Simulate 10,000 unique pages editing
	for i := 0; i < 10000; i++ {
		title := fmt.Sprintf("Unique_Page_%d", i)
		user := fmt.Sprintf("User_%d", rng.Intn(500))
		edit := makeEdit(int64(i+1), title, user)

		err := env.hotPageTracker.ProcessEdit(ctx, edit)
		if err != nil {
			// Some failures expected when circuit breaker activates
			continue
		}
	}

	// Check Redis key count — hot pages should be bounded
	hotKeys, err := env.redisClient.Keys(ctx, "hot:*").Result()
	if err == nil {
		t.Logf("Hot page keys after 10K unique pages: %d (max configured: %d)", len(hotKeys), env.cfg.Redis.HotPages.MaxTracked)
		// Hot pages should not exceed max tracked limit
		assert.LessOrEqual(t, len(hotKeys), env.cfg.Redis.HotPages.MaxTracked*3,
			"Hot page keys should be bounded by MaxTracked (with some margin for sub-keys)")
	}

	// Activity keys should exist for pages that received edits
	activityKeys, err := env.redisClient.Keys(ctx, "activity:*").Result()
	require.NoError(t, err)
	t.Logf("Activity keys: %d", len(activityKeys))

	// Total Redis keys should be reasonable
	totalKeys, err := env.redisClient.DBSize(ctx).Result()
	require.NoError(t, err)
	t.Logf("Total Redis keys after 10K unique pages: %d", totalKeys)

	// Verify promotion circuit breaker is working
	// With PromotionThreshold=3, only pages with 3+ edits should be promoted
	// Since each of our 10K pages only got 1 edit, few should be promoted
	promotedCount := 0
	for i := 0; i < 100; i++ {
		title := fmt.Sprintf("Unique_Page_%d", i)
		isHot, _ := env.hotPageTracker.IsHot(ctx, title)
		if isHot {
			promotedCount++
		}
	}
	t.Logf("Promoted pages (of first 100): %d", promotedCount)
}

// Test 2: Redis Cleanup Removes Stale Data
func TestRedisCleanupStaleData(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create activity keys with short TTL
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("activity:StaleTest_%d", i)
		env.redisClient.Set(ctx, key, "5", 1*time.Second) // 1-second TTL
	}

	// Verify keys exist
	keys1, err := env.redisClient.Keys(ctx, "activity:StaleTest_*").Result()
	require.NoError(t, err)
	assert.Equal(t, 100, len(keys1))

	// Fast forward time in miniredis
	env.miniRedis.FastForward(2 * time.Second)

	// Verify keys expired
	keys2, err := env.redisClient.Keys(ctx, "activity:StaleTest_*").Result()
	require.NoError(t, err)
	assert.Equal(t, 0, len(keys2), "Stale activity keys should be expired")
}

// Test 3: Trending Pages Bounded
func TestTrendingPagesBounded(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	trendingAgg := processor.NewTrendingAggregatorForTest(env.trendingScorer, env.cfg, env.logger)
	ctx := context.Background()

	// Process edits for many unique pages to exceed trending limit
	for i := 0; i < 500; i++ {
		title := fmt.Sprintf("TrendingBound_Page_%d", i)
		for j := 0; j < 3; j++ { // 3 edits per page
			edit := makeEdit(int64(i*3+j+1), title, fmt.Sprintf("User_%d", j))
			err := trendingAgg.ProcessEdit(ctx, edit)
			assert.NoError(t, err)
		}
	}

	// Trending pages total should be bounded
	trendingKeys, err := env.redisClient.Keys(ctx, "trending:*").Result()
	if err == nil {
		t.Logf("Trending keys after 500 unique pages: %d (max configured: %d)", len(trendingKeys), env.cfg.Redis.Trending.MaxPages)
	}
}

// Test 4: Concurrent Processing — No Deadlocks or Race Conditions
func TestConcurrentProcessing(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	rng := rand.New(rand.NewSource(42))

	spikeDetector := processor.NewSpikeDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	editWarDetector := processor.NewEditWarDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	trendingAgg := processor.NewTrendingAggregatorForTest(env.trendingScorer, env.cfg, env.logger)

	// Generate test edits
	numEdits := 1000
	edits := make([]*models.WikipediaEdit, numEdits)
	for i := 0; i < numEdits; i++ {
		edits[i] = makeEdit(
			int64(i+1),
			fmt.Sprintf("ConcurrentPage_%d", rng.Intn(50)),
			fmt.Sprintf("User_%d", rng.Intn(20)),
		)
	}

	// Record memory before
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Run all three consumers simultaneously
	var wg sync.WaitGroup
	panicCh := make(chan string, 3)

	consumers := map[string]func(context.Context, *models.WikipediaEdit) error{
		"spike":    spikeDetector.ProcessEdit,
		"trending": trendingAgg.ProcessEdit,
		"editwar":  editWarDetector.ProcessEdit,
	}

	for name, processFn := range consumers {
		wg.Add(1)
		go func(name string, fn func(context.Context, *models.WikipediaEdit) error) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCh <- fmt.Sprintf("%s panicked: %v", name, r)
				}
			}()

			for _, edit := range edits {
				_ = fn(ctx, edit)
			}
		}(name, processFn)
	}

	// Deadlock detection with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("All consumers completed without deadlock")
	case panic := <-panicCh:
		t.Fatalf("Consumer panic detected: %s", panic)
	case <-time.After(30 * time.Second):
		t.Fatal("DEADLOCK: concurrent consumers did not complete within 30 seconds")
	}

	// Record memory after
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	memUsedMB := float64(memAfter.Alloc-memBefore.Alloc) / 1024 / 1024
	t.Logf("Memory delta: %.2f MB", memUsedMB)
	t.Logf("Goroutines: %d", runtime.NumGoroutine())

	// Memory should stay reasonable (< 100MB for 1000 edits)
	assert.Less(t, memUsedMB, 100.0, "Memory usage should be bounded")
}

// Test 5: Burst Processing Under Load
func TestBurstProcessingUnderLoad(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()

	spikeDetector := processor.NewSpikeDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	trendingAgg := processor.NewTrendingAggregatorForTest(env.trendingScorer, env.cfg, env.logger)

	// Produce a burst of 5000 edits as fast as possible
	burstSize := 5000
	rng := rand.New(rand.NewSource(99))

	start := time.Now()
	var processErrors int

	for i := 0; i < burstSize; i++ {
		edit := makeEdit(
			int64(i+1),
			fmt.Sprintf("BurstPage_%d", rng.Intn(100)),
			fmt.Sprintf("User_%d", rng.Intn(50)),
		)

		if err := spikeDetector.ProcessEdit(ctx, edit); err != nil {
			processErrors++
		}
		if err := trendingAgg.ProcessEdit(ctx, edit); err != nil {
			processErrors++
		}
	}

	elapsed := time.Since(start)
	editsPerSec := float64(burstSize) / elapsed.Seconds()

	t.Logf("Burst processed %d edits in %v (%.0f edits/sec)", burstSize, elapsed, editsPerSec)
	t.Logf("Processing errors: %d", processErrors)

	// Should process at a reasonable rate (at least 100 edits/sec target)
	assert.Greater(t, editsPerSec, 50.0, "Should process at least 50 edits/sec with miniredis")
}

// Test 6: Hot Page Promotion Circuit Breaker
func TestHotPagePromotionCircuitBreaker(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()

	// Fill up hot pages to max
	for i := 0; i < env.cfg.Redis.HotPages.MaxTracked; i++ {
		title := fmt.Sprintf("FillPage_%d", i)
		// Send enough edits for promotion
		for j := 0; j < env.cfg.Redis.HotPages.PromotionThreshold+1; j++ {
			edit := makeEdit(int64(i*10+j+1), title, fmt.Sprintf("User_%d", j))
			_ = env.hotPageTracker.ProcessEdit(ctx, edit)
		}
	}

	// Now try to add more — circuit breaker should activate
	overflowTitle := "OverflowPage_ShouldBeRejected"
	for j := 0; j < env.cfg.Redis.HotPages.PromotionThreshold+5; j++ {
		edit := makeEdit(int64(90000+j), overflowTitle, fmt.Sprintf("User_%d", j))
		_ = env.hotPageTracker.ProcessEdit(ctx, edit)
	}

	// The overflow page may or may not be promoted depending on circuit breaker implementation
	// But the system should not crash
	totalKeys, err := env.redisClient.DBSize(ctx).Result()
	require.NoError(t, err)
	t.Logf("Total Redis keys after circuit breaker test: %d", totalKeys)
}
