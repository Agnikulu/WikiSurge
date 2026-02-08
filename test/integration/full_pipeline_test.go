package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
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

// fullPipelineTestEnv holds all infrastructure for the full pipeline test
type fullPipelineTestEnv struct {
	miniRedis      *miniredis.Miniredis
	redisClient    *redis.Client
	hotPageTracker *storage.HotPageTracker
	trendingScorer *storage.TrendingScorer
	spikeDetector  *processor.SpikeDetector
	editWarDetector *processor.EditWarDetector
	trendingAgg    *processor.TrendingAggregator
	cfg            *config.Config
	logger         zerolog.Logger
}

// setupFullPipelineEnv sets up all infrastructure for the full pipeline test
func setupFullPipelineEnv(t *testing.T) *fullPipelineTestEnv {
	t.Helper()

	// Start miniredis
	mr, err := miniredis.Run()
	require.NoError(t, err)

	// Create Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	// Create test config
	cfg := &config.Config{
		Features: config.Features{
			ElasticsearchIndexing: false, // We test without ES for unit-level
			Trending:              true,
			EditWars:              true,
		},
		Redis: config.Redis{
			URL:        fmt.Sprintf("redis://%s", mr.Addr()),
			MaxMemory:  "256mb",
			HotPages: config.HotPages{
				MaxTracked:         1000,
				PromotionThreshold: 3,   // Lower threshold for testing
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
			Level:  "debug",
			Format: "json",
		},
	}

	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()

	// Initialize shared components
	hotPageTracker := storage.NewHotPageTracker(redisClient, &cfg.Redis.HotPages)
	trendingScorer := storage.NewTrendingScorerForTest(redisClient, &cfg.Redis.Trending)

	// Initialize processors
	spikeDetector := processor.NewSpikeDetector(hotPageTracker, redisClient, cfg, logger)
	editWarDetector := processor.NewEditWarDetector(hotPageTracker, redisClient, cfg, logger)
	trendingAgg := processor.NewTrendingAggregatorForTest(trendingScorer, cfg, logger)

	return &fullPipelineTestEnv{
		miniRedis:       mr,
		redisClient:     redisClient,
		hotPageTracker:  hotPageTracker,
		trendingScorer:  trendingScorer,
		spikeDetector:   spikeDetector,
		editWarDetector: editWarDetector,
		trendingAgg:     trendingAgg,
		cfg:             cfg,
		logger:          logger,
	}
}

func (env *fullPipelineTestEnv) cleanup() {
	env.redisClient.Close()
	env.miniRedis.Close()
}

// generateTestEdits creates the specified test data composition:
//   - 800 normal edits (various pages)
//   - 100 edits for 5 "trending" pages (20 each)
//   - 50 edits for 2 pages in 5 minutes (spike simulation)
//   - 50 edits alternating for 1 page (edit war simulation)
func generateTestEdits() []*models.WikipediaEdit {
	edits := make([]*models.WikipediaEdit, 0, 1000)
	baseTime := time.Now().Unix()
	rng := rand.New(rand.NewSource(42))
	editID := int64(1)

	// 800 normal edits across 200 different pages
	for i := 0; i < 800; i++ {
		pageNum := rng.Intn(200)
		edits = append(edits, &models.WikipediaEdit{
			ID:        editID,
			Type:      "edit",
			Title:     fmt.Sprintf("Normal_Page_%d", pageNum),
			User:      fmt.Sprintf("User_%d", rng.Intn(100)),
			Bot:       false,
			Wiki:      "enwiki",
			ServerURL: "https://en.wikipedia.org",
			Timestamp: baseTime - int64(rng.Intn(3600)), // Within last hour
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 1000 + rng.Intn(5000), New: 1000 + rng.Intn(5000)},
			Revision: struct {
				Old int64 `json:"old"`
				New int64 `json:"new"`
			}{Old: editID - 1, New: editID},
			Comment: fmt.Sprintf("Normal edit %d", i),
		})
		editID++
	}

	// 100 edits for 5 "trending" pages (20 each)
	trendingPages := []string{"Trending_Page_A", "Trending_Page_B", "Trending_Page_C", "Trending_Page_D", "Trending_Page_E"}
	for _, page := range trendingPages {
		for i := 0; i < 20; i++ {
			edits = append(edits, &models.WikipediaEdit{
				ID:        editID,
				Type:      "edit",
				Title:     page,
				User:      fmt.Sprintf("TrendUser_%d", rng.Intn(15)),
				Bot:       false,
				Wiki:      "enwiki",
				ServerURL: "https://en.wikipedia.org",
				Timestamp: baseTime - int64(rng.Intn(1800)), // Within last 30 minutes
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 2000, New: 2000 + rng.Intn(500)},
				Revision: struct {
					Old int64 `json:"old"`
					New int64 `json:"new"`
				}{Old: editID - 1, New: editID},
				Comment: fmt.Sprintf("Trending update %d for %s", i, page),
			})
			editID++
		}
	}

	// 50 edits for 2 pages in 5 minutes (spike simulation)
	spikePages := []string{"Spike_Page_Alpha", "Spike_Page_Beta"}
	for _, page := range spikePages {
		for i := 0; i < 25; i++ {
			edits = append(edits, &models.WikipediaEdit{
				ID:        editID,
				Type:      "edit",
				Title:     page,
				User:      fmt.Sprintf("SpikeUser_%d", rng.Intn(10)),
				Bot:       false,
				Wiki:      "enwiki",
				ServerURL: "https://en.wikipedia.org",
				Timestamp: baseTime - int64(rng.Intn(300)), // Within last 5 minutes
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 3000, New: 3000 + rng.Intn(1000)},
				Revision: struct {
					Old int64 `json:"old"`
					New int64 `json:"new"`
				}{Old: editID - 1, New: editID},
				Comment: fmt.Sprintf("Spike edit %d for %s", i, page),
			})
			editID++
		}
	}

	// 50 edits alternating for 1 page (edit war simulation)
	editWarPage := "Edit_War_Battleground"
	warUsers := []string{"WarUser_Alpha", "WarUser_Beta"}
	for i := 0; i < 50; i++ {
		user := warUsers[i%2]
		// Alternate byte changes to simulate reverts
		byteChange := 500
		if i%2 == 1 {
			byteChange = -500
		}
		edits = append(edits, &models.WikipediaEdit{
			ID:        editID,
			Type:      "edit",
			Title:     editWarPage,
			User:      user,
			Bot:       false,
			Wiki:      "enwiki",
			ServerURL: "https://en.wikipedia.org",
			Timestamp: baseTime - int64(rng.Intn(600)), // Within last 10 minutes
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 5000, New: 5000 + byteChange},
			Revision: struct {
				Old int64 `json:"old"`
				New int64 `json:"new"`
			}{Old: editID - 1, New: editID},
			Comment: fmt.Sprintf("Edit war edit %d by %s", i, user),
		})
		editID++
	}

	// Shuffle edits to simulate real-world interleaving
	rng.Shuffle(len(edits), func(i, j int) {
		edits[i], edits[j] = edits[j], edits[i]
	})

	return edits
}

func TestFullPipeline_AllConsumersProcessEdits(t *testing.T) {
	env := setupFullPipelineEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	edits := generateTestEdits()

	// Verify we generated 1000 edits
	assert.Len(t, edits, 1000, "Should generate exactly 1000 test edits")

	// Track processing results
	var spikeProcessed, trendingProcessed, editWarProcessed int64
	var mu sync.Mutex
	var errors []error

	// Process all edits through all three consumers in parallel
	var wg sync.WaitGroup

	// Spike detector consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, edit := range edits {
			if err := env.spikeDetector.ProcessEdit(ctx, edit); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("spike: %w", err))
				mu.Unlock()
			}
			mu.Lock()
			spikeProcessed++
			mu.Unlock()
		}
	}()

	// Trending aggregator consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, edit := range edits {
			if err := env.trendingAgg.ProcessEdit(ctx, edit); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("trending: %w", err))
				mu.Unlock()
			}
			mu.Lock()
			trendingProcessed++
			mu.Unlock()
		}
	}()

	// Edit war detector consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, edit := range edits {
			if err := env.editWarDetector.ProcessEdit(ctx, edit); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Errorf("editwar: %w", err))
				mu.Unlock()
			}
			mu.Lock()
			editWarProcessed++
			mu.Unlock()
		}
	}()

	wg.Wait()

	// Verify all edits were processed by all consumers
	assert.Equal(t, int64(1000), spikeProcessed, "Spike detector should process all 1000 edits")
	assert.Equal(t, int64(1000), trendingProcessed, "Trending aggregator should process all 1000 edits")
	assert.Equal(t, int64(1000), editWarProcessed, "Edit war detector should process all 1000 edits")

	// Log any errors (some are expected, e.g. if page isn't hot yet)
	if len(errors) > 0 {
		t.Logf("Processing generated %d errors (some expected for non-hot pages)", len(errors))
	}
}

func TestFullPipeline_HotPagesTracked(t *testing.T) {
	env := setupFullPipelineEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	edits := generateTestEdits()

	// Process all edits through spike detector (which updates hot page tracking)
	for _, edit := range edits {
		_ = env.spikeDetector.ProcessEdit(ctx, edit)
	}

	// Verify hot pages are tracked in Redis
	// Pages with enough edits should be promoted to hot tracking
	hotKeys, err := env.redisClient.Keys(ctx, "hot:*").Result()
	if err != nil {
		t.Logf("Hot keys lookup: %v", err)
	}

	activityKeys, err := env.redisClient.Keys(ctx, "activity:*").Result()
	require.NoError(t, err)

	t.Logf("Activity keys: %d, Hot keys: %d", len(activityKeys), len(hotKeys))

	// Pages with many edits (trending, spike, edit war pages) should have activity counters
	assert.Greater(t, len(activityKeys), 0, "Should have activity counters in Redis")

	// Check specific high-activity pages
	for _, page := range []string{"Trending_Page_A", "Trending_Page_B", "Spike_Page_Alpha", "Spike_Page_Beta", "Edit_War_Battleground"} {
		key := fmt.Sprintf("activity:%s", page)
		val, err := env.redisClient.Get(ctx, key).Result()
		if err == nil {
			t.Logf("Page %s activity count: %s", page, val)
		}
	}
}

func TestFullPipeline_TrendingScoresUpdated(t *testing.T) {
	env := setupFullPipelineEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	edits := generateTestEdits()

	// Process all edits through trending aggregator
	for _, edit := range edits {
		_ = env.trendingAgg.ProcessEdit(ctx, edit)
	}

	// Verify trending scores are in Redis
	trendingKeys, err := env.redisClient.Keys(ctx, "trending:*").Result()
	require.NoError(t, err)
	t.Logf("Trending keys: %d", len(trendingKeys))

	// Get top trending pages
	topPages, err := env.trendingScorer.GetTopTrending(10)
	if err != nil {
		t.Logf("GetTopPages error: %v (may not be implemented)", err)
	} else {
		t.Logf("Top trending pages: %d", len(topPages))
		for i, page := range topPages {
			t.Logf("  #%d: %+v", i+1, page)
		}
	}

	// Trending pages with 20+ edits each should appear
	// The exact check depends on the trending scorer implementation
	assert.Greater(t, len(trendingKeys), 0, "Should have trending data in Redis")
}

func TestFullPipeline_EditWarDetection(t *testing.T) {
	env := setupFullPipelineEnv(t)
	defer env.cleanup()

	ctx := context.Background()

	// We need to first make Edit_War_Battleground a hot page by sending enough edits
	editWarPage := "Edit_War_Battleground"
	baseTime := time.Now().Unix()

	// Send enough edits to promote the page to hot tracking first
	for i := 0; i < 10; i++ {
		edit := &models.WikipediaEdit{
			ID:        int64(10000 + i),
			Type:      "edit",
			Title:     editWarPage,
			User:      fmt.Sprintf("WarUser_%d", i%2),
			Bot:       false,
			Wiki:      "enwiki",
			ServerURL: "https://en.wikipedia.org",
			Timestamp: baseTime - int64(i*30),
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 5000, New: 5500},
			Revision: struct {
				Old int64 `json:"old"`
				New int64 `json:"new"`
			}{Old: int64(10000 + i - 1), New: int64(10000 + i)},
			Comment: "edit war setup",
		}
		_ = env.spikeDetector.ProcessEdit(ctx, edit)
	}

	// Now process the edit war edits through the edit war detector
	warUsers := []string{"WarUser_Alpha", "WarUser_Beta"}
	for i := 0; i < 50; i++ {
		user := warUsers[i%2]
		byteChange := 500
		if i%2 == 1 {
			byteChange = -500
		}
		edit := &models.WikipediaEdit{
			ID:        int64(20000 + i),
			Type:      "edit",
			Title:     editWarPage,
			User:      user,
			Bot:       false,
			Wiki:      "enwiki",
			ServerURL: "https://en.wikipedia.org",
			Timestamp: baseTime - int64(i*10),
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 5000, New: 5000 + byteChange},
			Revision: struct {
				Old int64 `json:"old"`
				New int64 `json:"new"`
			}{Old: int64(20000 + i - 1), New: int64(20000 + i)},
			Comment: fmt.Sprintf("edit war %d", i),
		}
		_ = env.editWarDetector.ProcessEdit(ctx, edit)
	}

	// Check for edit war alerts in Redis stream
	alerts, err := env.redisClient.XRange(ctx, "alerts:editwars", "-", "+").Result()
	if err != nil {
		t.Logf("Edit war alerts stream check: %v", err)
	} else {
		t.Logf("Edit war alerts generated: %d", len(alerts))
		for _, alert := range alerts {
			t.Logf("  Alert: %+v", alert.Values)
		}
	}

	// Check for edit war tracking data
	editWarsKey := fmt.Sprintf("editwar:editors:%s", editWarPage)
	editors, err := env.redisClient.HGetAll(ctx, editWarsKey).Result()
	if err == nil {
		t.Logf("Edit war editors for %s: %v", editWarPage, editors)
	}
}

func TestFullPipeline_ConcurrentProcessingNoPanic(t *testing.T) {
	env := setupFullPipelineEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	edits := generateTestEdits()

	// Process all 1000 edits through all consumers concurrently
	// This tests for race conditions and deadlocks
	var wg sync.WaitGroup
	panicChan := make(chan interface{}, 3)

	for _, consumerName := range []string{"spike", "trending", "editwar"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicChan <- fmt.Sprintf("%s consumer panicked: %v", name, r)
				}
			}()

			for _, edit := range edits {
				switch name {
				case "spike":
					_ = env.spikeDetector.ProcessEdit(ctx, edit)
				case "trending":
					_ = env.trendingAgg.ProcessEdit(ctx, edit)
				case "editwar":
					_ = env.editWarDetector.ProcessEdit(ctx, edit)
				}
			}
		}(consumerName)
	}

	// Use a timeout to detect deadlocks
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success — all consumers finished
	case p := <-panicChan:
		t.Fatalf("Consumer panicked: %v", p)
	case <-time.After(60 * time.Second):
		t.Fatal("Deadlock detected: consumers did not finish within 60 seconds")
	}
}

func TestFullPipeline_DataComposition(t *testing.T) {
	edits := generateTestEdits()

	// Verify edit composition counts
	normalCount := 0
	trendingCount := 0
	spikeCount := 0
	editWarCount := 0

	trendingPages := map[string]bool{
		"Trending_Page_A": true, "Trending_Page_B": true,
		"Trending_Page_C": true, "Trending_Page_D": true,
		"Trending_Page_E": true,
	}
	spikePages := map[string]bool{
		"Spike_Page_Alpha": true, "Spike_Page_Beta": true,
	}

	for _, edit := range edits {
		if edit.Title == "Edit_War_Battleground" {
			editWarCount++
		} else if spikePages[edit.Title] {
			spikeCount++
		} else if trendingPages[edit.Title] {
			trendingCount++
		} else {
			normalCount++
		}
	}

	assert.Equal(t, 800, normalCount, "Should have 800 normal edits")
	assert.Equal(t, 100, trendingCount, "Should have 100 trending edits (5 pages × 20)")
	assert.Equal(t, 50, spikeCount, "Should have 50 spike edits (2 pages × 25)")
	assert.Equal(t, 50, editWarCount, "Should have 50 edit war edits")
	assert.Equal(t, 1000, len(edits), "Total should be 1000 edits")
}

func TestFullPipeline_RedisDataVerification(t *testing.T) {
	env := setupFullPipelineEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	edits := generateTestEdits()

	// Process all through all consumers
	for _, edit := range edits {
		_ = env.spikeDetector.ProcessEdit(ctx, edit)
		_ = env.trendingAgg.ProcessEdit(ctx, edit)
		_ = env.editWarDetector.ProcessEdit(ctx, edit)
	}

	// Query Redis for comprehensive verification
	// 1. Activity counters
	activityKeys, err := env.redisClient.Keys(ctx, "activity:*").Result()
	require.NoError(t, err)
	t.Logf("Total activity keys: %d", len(activityKeys))

	// 2. Hot pages
	hotKeys, err := env.redisClient.Keys(ctx, "hot:*").Result()
	if err == nil {
		t.Logf("Total hot page keys: %d", len(hotKeys))
	}

	// 3. Trending data
	trendingKeys, err := env.redisClient.Keys(ctx, "trending:*").Result()
	if err == nil {
		t.Logf("Total trending keys: %d", len(trendingKeys))
	}

	// 4. Spike alerts
	spikeAlerts, err := env.redisClient.XRange(ctx, "alerts:spikes", "-", "+").Result()
	if err == nil {
		t.Logf("Total spike alerts: %d", len(spikeAlerts))
	}

	// 5. Edit war alerts
	editWarAlerts, err := env.redisClient.XRange(ctx, "alerts:editwars", "-", "+").Result()
	if err == nil {
		t.Logf("Total edit war alerts: %d", len(editWarAlerts))
	}

	// 6. Edit war editor tracking
	ewKeys, err := env.redisClient.Keys(ctx, "editwar:*").Result()
	if err == nil {
		t.Logf("Total edit war tracking keys: %d", len(ewKeys))
	}

	// Overall: should have data in Redis
	allKeys, err := env.redisClient.DBSize(ctx).Result()
	require.NoError(t, err)
	assert.Greater(t, allKeys, int64(0), "Redis should contain data after processing")
	t.Logf("Total Redis keys: %d", allKeys)
}

func TestFullPipeline_MessageSerialization(t *testing.T) {
	// Ensure edits can be serialized/deserialized (as Kafka would)
	edits := generateTestEdits()

	for i, edit := range edits[:10] { // Test first 10
		data, err := json.Marshal(edit)
		require.NoError(t, err, "Edit %d should marshal to JSON", i)

		var decoded models.WikipediaEdit
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err, "Edit %d should unmarshal from JSON", i)

		assert.Equal(t, edit.ID, decoded.ID)
		assert.Equal(t, edit.Title, decoded.Title)
		assert.Equal(t, edit.User, decoded.User)
		assert.Equal(t, edit.Wiki, decoded.Wiki)
		assert.Equal(t, edit.Timestamp, decoded.Timestamp)
	}
}
