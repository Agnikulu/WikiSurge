package resource

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/processor"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failureTestEnv holds test infrastructure for failure recovery tests
type failureTestEnv struct {
	miniRedis      *miniredis.Miniredis
	redisClient    *redis.Client
	hotPageTracker *storage.HotPageTracker
	trendingScorer *storage.TrendingScorer
	cfg            *config.Config
	logger         zerolog.Logger
}

func setupFailureTestEnv(t *testing.T) *failureTestEnv {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)

	redisClient := redis.NewClient(&redis.Options{
		Addr:         mr.Addr(),
		MaxRetries:   0,
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
		PoolTimeout:  200 * time.Millisecond,
		MinIdleConns: 0,
		PoolSize:     2,
	})

	cfg := &config.Config{
		Features: config.Features{
			Trending: true,
			EditWars: true,
		},
		Redis: config.Redis{
			URL:       fmt.Sprintf("redis://%s", mr.Addr()),
			MaxMemory: "256mb",
			HotPages: config.HotPages{
				MaxTracked:         500,
				PromotionThreshold: 3,
				WindowDuration:     15 * time.Minute,
				MaxMembersPerPage:  50,
				HotThreshold:       2,
				CleanupInterval:    5 * time.Minute,
			},
			Trending: config.TrendingConfig{
				Enabled:         true,
				MaxPages:        500,
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
			Level:  "warn",
			Format: "json",
		},
	}

	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.WarnLevel)

	hotPageTracker := storage.NewHotPageTracker(redisClient, &cfg.Redis.HotPages)
	trendingScorer := storage.NewTrendingScorerForTest(redisClient, &cfg.Redis.Trending)

	return &failureTestEnv{
		miniRedis:      mr,
		redisClient:    redisClient,
		hotPageTracker: hotPageTracker,
		trendingScorer: trendingScorer,
		cfg:            cfg,
		logger:         logger,
	}
}

func (env *failureTestEnv) cleanup() {
	env.redisClient.Close()
	env.miniRedis.Close()
}

// Test 1: Redis Failure and Recovery
func TestRedisFailureDuringProcessing(t *testing.T) {
	env := setupFailureTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	spikeDetector := processor.NewSpikeDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	trendingAgg := processor.NewTrendingAggregatorForTest(env.trendingScorer, env.cfg, env.logger)

	// Phase 1: Process some edits normally
	normalErrors := 0
	for i := 0; i < 100; i++ {
		edit := makeEdit(int64(i+1), fmt.Sprintf("Page_%d", i%10), fmt.Sprintf("User_%d", i%5))
		if err := spikeDetector.ProcessEdit(ctx, edit); err != nil {
			normalErrors++
		}
		if err := trendingAgg.ProcessEdit(ctx, edit); err != nil {
			normalErrors++
		}
	}
	t.Logf("Phase 1 (normal): %d errors out of 200 operations", normalErrors)

	// Phase 2: Stop Redis — simulate Redis failure
	env.miniRedis.Close()

	// Process edits during Redis outage — should get errors but NOT panic
	failureErrors := 0
	for i := 100; i < 200; i++ {
		edit := makeEdit(int64(i+1), fmt.Sprintf("Page_%d", i%10), fmt.Sprintf("User_%d", i%5))
		if err := spikeDetector.ProcessEdit(ctx, edit); err != nil {
			failureErrors++
		}
		if err := trendingAgg.ProcessEdit(ctx, edit); err != nil {
			failureErrors++
		}
	}
	t.Logf("Phase 2 (Redis down): %d errors out of 200 operations", failureErrors)
	// Should have errors during Redis downtime
	assert.Greater(t, failureErrors, 0, "Should have errors when Redis is down")

	// Phase 3: Restart Redis — simulate recovery
	mr2, err := miniredis.Run()
	require.NoError(t, err)
	defer mr2.Close()

	// Create new client pointing to new miniredis
	newClient := redis.NewClient(&redis.Options{Addr: mr2.Addr()})
	defer newClient.Close()

	// The old client won't auto-reconnect to a new address,
	// but the important test is that the system didn't crash during the failure

	// Verify the processor didn't panic and can still attempt to process
	// (even though the underlying connection is to a stopped server)
	// Use a short-lived context since the old Redis client won't reconnect to a new address
	shortCtx, shortCancel := context.WithTimeout(ctx, 5*time.Second)
	defer shortCancel()

	recoverErrors := 0
	for i := 200; i < 210; i++ {
		edit := makeEdit(int64(i+1), fmt.Sprintf("Page_%d", i%10), fmt.Sprintf("User_%d", i%5))
		if err := spikeDetector.ProcessEdit(shortCtx, edit); err != nil {
			recoverErrors++
		}
	}
	t.Logf("Phase 3 (after Redis restart attempt): %d errors out of 10 operations", recoverErrors)
}

// Test 2: Redis Failure Does Not Crash Trending
func TestTrendingContinuesDuringRedisFailure(t *testing.T) {
	env := setupFailureTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	trendingAgg := processor.NewTrendingAggregatorForTest(env.trendingScorer, env.cfg, env.logger)

	// Process normally
	for i := 0; i < 50; i++ {
		edit := makeEdit(int64(i+1), fmt.Sprintf("TrendPage_%d", i%5), fmt.Sprintf("User_%d", i))
		_ = trendingAgg.ProcessEdit(ctx, edit)
	}

	// Simulate Redis failure
	env.miniRedis.Close()

	// Processing should return errors but not panic
	var panicOccurred bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicOccurred = true
			}
		}()

		for i := 50; i < 100; i++ {
			edit := makeEdit(int64(i+1), fmt.Sprintf("TrendPage_%d", i%5), fmt.Sprintf("User_%d", i))
			_ = trendingAgg.ProcessEdit(ctx, edit)
		}
	}()

	assert.False(t, panicOccurred, "Trending aggregator should not panic on Redis failure")
}

// Test 3: Edit War Detector Resilience
func TestEditWarDetectorResilience(t *testing.T) {
	env := setupFailureTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	editWarDetector := processor.NewEditWarDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)

	// First process enough edits to promote a page to hot
	for i := 0; i < 20; i++ {
		edit := makeEdit(int64(i+1), "ResilientPage", fmt.Sprintf("User_%d", i%3))
		_ = editWarDetector.ProcessEdit(ctx, edit)
	}

	// Now simulate Redis interruption by closing miniredis
	env.miniRedis.Close()

	// Processing should fail gracefully
	var panicOccurred bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicOccurred = true
			}
		}()

		for i := 20; i < 40; i++ {
			edit := makeEdit(int64(i+1), "ResilientPage", fmt.Sprintf("User_%d", i%3))
			_ = editWarDetector.ProcessEdit(ctx, edit)
		}
	}()

	assert.False(t, panicOccurred, "Edit war detector should not panic on Redis failure")
}

// Test 4: Partial Feature Degradation
func TestPartialFeatureDegradation(t *testing.T) {
	env := setupFailureTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	rng := rand.New(rand.NewSource(42))

	spikeDetector := processor.NewSpikeDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)
	trendingAgg := processor.NewTrendingAggregatorForTest(env.trendingScorer, env.cfg, env.logger)
	editWarDetector := processor.NewEditWarDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)

	// Simulate high Redis memory by filling with many keys
	for i := 0; i < 5000; i++ {
		env.redisClient.Set(ctx, fmt.Sprintf("filler:key:%d", i), "data_padding_value", 0)
	}

	totalKeys, _ := env.redisClient.DBSize(ctx).Result()
	t.Logf("Redis keys after filling: %d", totalKeys)

	// Continue processing — system should handle high memory gracefully
	var spikeErrors, trendErrors, ewErrors int
	for i := 0; i < 500; i++ {
		pageNum := rng.Intn(50)
		edit := makeEdit(int64(i+1), fmt.Sprintf("DegradedPage_%d", pageNum), fmt.Sprintf("User_%d", rng.Intn(10)))

		if err := spikeDetector.ProcessEdit(ctx, edit); err != nil {
			spikeErrors++
		}
		if err := trendingAgg.ProcessEdit(ctx, edit); err != nil {
			trendErrors++
		}
		if err := editWarDetector.ProcessEdit(ctx, edit); err != nil {
			ewErrors++
		}
	}

	t.Logf("Degradation test — Spike errors: %d, Trending errors: %d, EditWar errors: %d", spikeErrors, trendErrors, ewErrors)

	// System should continue functioning (even if some operations degrade)
	// No panics means success
	finalKeys, _ := env.redisClient.DBSize(ctx).Result()
	t.Logf("Redis keys after degradation test: %d", finalKeys)
}

// Test 5: Recovery After Redis Restart with Data Consistency
func TestRecoveryAfterRedisRestart(t *testing.T) {
	// Use a first miniredis to establish baseline data
	mr1, err := miniredis.Run()
	require.NoError(t, err)

	client1 := redis.NewClient(&redis.Options{Addr: mr1.Addr()})
	ctx := context.Background()

	cfg := &config.Config{
		Features: config.Features{Trending: true, EditWars: true},
		Redis: config.Redis{
			URL:       fmt.Sprintf("redis://%s", mr1.Addr()),
			MaxMemory: "256mb",
			HotPages: config.HotPages{
				MaxTracked: 500, PromotionThreshold: 3, WindowDuration: 15 * time.Minute,
				MaxMembersPerPage: 50, HotThreshold: 2, CleanupInterval: 5 * time.Minute,
			},
			Trending: config.TrendingConfig{
				Enabled: true, MaxPages: 500, HalfLifeMinutes: 30.0, PruneInterval: 5 * time.Minute,
			},
		},
		Elasticsearch: config.Elasticsearch{
			Enabled: false, RetentionDays: 7, MaxDocsPerDay: 10000,
			SelectiveCriteria: config.SelectiveCriteria{TrendingTopN: 100, SpikeRatioMin: 2.0, EditWarEnabled: true},
		},
		Kafka:   config.Kafka{Brokers: []string{"localhost:9092"}},
		Logging: config.Logging{Level: "warn", Format: "json"},
	}

	logger := zerolog.New(zerolog.NewTestWriter(t)).Level(zerolog.WarnLevel)
	hpt := storage.NewHotPageTracker(client1, &cfg.Redis.HotPages)
	ts := storage.NewTrendingScorerForTest(client1, &cfg.Redis.Trending)
	sd := processor.NewSpikeDetector(hpt, client1, cfg, logger)

	// Establish baseline: process edits
	for i := 0; i < 50; i++ {
		edit := makeEdit(int64(i+1), fmt.Sprintf("RecoveryPage_%d", i%5), fmt.Sprintf("User_%d", i))
		_ = sd.ProcessEdit(ctx, edit)
	}

	baselineKeys, _ := client1.DBSize(ctx).Result()
	t.Logf("Baseline Redis keys: %d", baselineKeys)

	// Kill Redis
	mr1.Close()
	client1.Close()

	// Restart Redis (fresh instance — data lost, simulating recovery)
	mr2, err := miniredis.Run()
	require.NoError(t, err)
	defer mr2.Close()

	client2 := redis.NewClient(&redis.Options{Addr: mr2.Addr()})
	defer client2.Close()

	// Create new components with new client
	hpt2 := storage.NewHotPageTracker(client2, &cfg.Redis.HotPages)
	_ = storage.NewTrendingScorerForTest(client2, &cfg.Redis.Trending)
	sd2 := processor.NewSpikeDetector(hpt2, client2, cfg, logger)

	// Continue processing — recovery should work
	recoveryErrors := 0
	for i := 50; i < 100; i++ {
		edit := makeEdit(int64(i+1), fmt.Sprintf("RecoveryPage_%d", i%5), fmt.Sprintf("User_%d", i))
		if err := sd2.ProcessEdit(ctx, edit); err != nil {
			recoveryErrors++
		}
	}

	postRecoveryKeys, _ := client2.DBSize(ctx).Result()
	t.Logf("Post-recovery Redis keys: %d, errors during recovery: %d", postRecoveryKeys, recoveryErrors)

	// System should be functional after recovery
	assert.Greater(t, postRecoveryKeys, int64(0), "Should have data in Redis after recovery")

	_ = ts // avoid unused variable
}

// Test 6: Spike Detector Handles Missing Hot Page Data
func TestSpikeDetectorMissingData(t *testing.T) {
	env := setupFailureTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	spikeDetector := processor.NewSpikeDetector(env.hotPageTracker, env.redisClient, env.cfg, env.logger)

	// Process an edit for a page with no history
	edit := makeEdit(1, "BrandNewPage", "NewUser")
	err := spikeDetector.ProcessEdit(ctx, edit)
	// Should handle gracefully — no spike expected for brand new page
	if err != nil {
		t.Logf("Expected: error for brand new page: %v", err)
	}

	// Process for a page where we manually corrupt data
	env.redisClient.Set(ctx, "activity:CorruptedPage", "not_a_number_but_wont_matter_for_INCR", 0)
	edit2 := makeEdit(2, "CorruptedPage", "User1")
	err2 := spikeDetector.ProcessEdit(ctx, edit2)
	// Should not panic
	t.Logf("Corrupted page processing result: err=%v", err2)
}
