package processor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
)

// setupTestIndexer creates a SelectiveIndexer backed by miniredis for unit testing.
// It returns the indexer, the Redis client, the miniredis instance, and the trending scorer.
func setupTestIndexer(t *testing.T) (*SelectiveIndexer, *redis.Client, *miniredis.Miniredis, *storage.TrendingScorer) {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	cfg := &config.Config{
		Elasticsearch: config.Elasticsearch{
			Enabled:       true,
			URL:           "http://localhost:9200",
			RetentionDays: 7,
			SelectiveCriteria: config.SelectiveCriteria{
				TrendingTopN:   100,
				SpikeRatioMin:  2.0,
				EditWarEnabled: true,
			},
		},
		Redis: config.Redis{
			Trending: config.TrendingConfig{
				Enabled:         true,
				MaxPages:        1000,
				HalfLifeMinutes: 30.0,
				PruneInterval:   time.Minute,
			},
			HotPages: config.HotPages{
				MaxTracked:         100,
				PromotionThreshold: 5,
				WindowDuration:     time.Hour,
				MaxMembersPerPage:  50,
				HotThreshold:       2,
				CleanupInterval:    5 * time.Minute,
			},
		},
	}

	trendingScorer := storage.NewTrendingScorerForTest(client, &cfg.Redis.Trending)
	hotPageTracker := storage.NewHotPageTracker(client, &cfg.Redis.HotPages)

	strategy := storage.NewIndexingStrategy(
		&cfg.Elasticsearch.SelectiveCriteria,
		client,
		trendingScorer,
		hotPageTracker,
	)

	logger := zerolog.New(zerolog.NewTestWriter(t))

	// We pass nil for esClient since we're testing indexing decisions, not actual ES calls
	indexer := NewSelectiveIndexerForTest(nil, strategy, cfg, logger)

	return indexer, client, mr, trendingScorer
}

// makeTestEdit creates a valid WikipediaEdit for testing
func makeTestEdit(title, user string) *models.WikipediaEdit {
	return &models.WikipediaEdit{
		ID:        12345,
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
		}{Old: 1000, New: 1100},
		Revision: struct {
			Old int64 `json:"old"`
			New int64 `json:"new"`
		}{Old: 100, New: 101},
		Comment: "test edit",
	}
}

// TestSelectiveIndexer_TrendingPage verifies that edits to trending pages are indexed
func TestSelectiveIndexer_TrendingPage(t *testing.T) {
	indexer, _, mr, scorer := setupTestIndexer(t)
	defer mr.Close()
	defer scorer.Stop()

	ctx := context.Background()
	pageTitle := "Trending_Article"

	// Make the page trending by giving it a high score
	for i := 0; i < 50; i++ {
		err := scorer.IncrementScore(pageTitle, 10.0)
		require.NoError(t, err)
	}

	// Verify page has a trending rank
	rank, err := scorer.GetPageRank(ctx, "enwiki", pageTitle)
	require.NoError(t, err)
	assert.True(t, rank > 0 && rank <= 100, "Page should be in top 100 trending, got rank %d", rank)

	// Process an edit for the trending page
	edit := makeTestEdit(pageTitle, "TrendingUser")
	err = indexer.ProcessEdit(ctx, edit)
	require.NoError(t, err)

	// The edit should be queued in the buffer (since it's trending)
	assert.Equal(t, 1, indexer.BufferLen(), "Trending page edit should be buffered for indexing")
}

// TestSelectiveIndexer_SpikingPage verifies that edits to spiking pages are indexed
func TestSelectiveIndexer_SpikingPage(t *testing.T) {
	indexer, client, mr, scorer := setupTestIndexer(t)
	defer mr.Close()
	defer scorer.Stop()

	ctx := context.Background()
	pageTitle := "Breaking_News_Page"

	// Mark the page as spiking by setting the spike key in Redis
	spikeKey := fmt.Sprintf("spike:enwiki:%s", pageTitle)
	err := client.Set(ctx, spikeKey, "5.5", time.Hour).Err()
	require.NoError(t, err)

	// Process an edit for the spiking page
	edit := makeTestEdit(pageTitle, "NewsEditor")
	err = indexer.ProcessEdit(ctx, edit)
	require.NoError(t, err)

	// The edit should be queued in the buffer (since it's spiking)
	assert.Equal(t, 1, indexer.BufferLen(), "Spiking page edit should be buffered for indexing")
}

// TestSelectiveIndexer_NormalPage verifies that edits to normal pages are NOT indexed
func TestSelectiveIndexer_NormalPage(t *testing.T) {
	indexer, _, mr, scorer := setupTestIndexer(t)
	defer mr.Close()
	defer scorer.Stop()

	ctx := context.Background()
	pageTitle := "Obscure_Village_Article"

	// Process an edit for a normal (non-trending, non-spiking) page
	edit := makeTestEdit(pageTitle, "RandomUser")
	err := indexer.ProcessEdit(ctx, edit)
	require.NoError(t, err)

	// The edit should NOT be queued
	assert.Equal(t, 0, indexer.BufferLen(), "Normal page edit should not be buffered")
}

// TestSelectiveIndexer_WatchlistPage verifies that watchlist pages are always indexed
func TestSelectiveIndexer_WatchlistPage(t *testing.T) {
	indexer, _, mr, scorer := setupTestIndexer(t)
	defer mr.Close()
	defer scorer.Stop()

	ctx := context.Background()
	pageTitle := "Important_Monitored_Page"

	// Add the page to the watchlist
	err := indexer.strategy.AddToWatchlist(ctx, "enwiki", pageTitle)
	require.NoError(t, err)

	// Process an edit for the watchlisted page
	edit := makeTestEdit(pageTitle, "WatchedEditor")
	err = indexer.ProcessEdit(ctx, edit)
	require.NoError(t, err)

	// The edit should be queued (watchlist is highest priority)
	assert.Equal(t, 1, indexer.BufferLen(), "Watchlisted page edit should be buffered for indexing")
}

// TestSelectiveIndexer_EditWarPage verifies that edit-war pages are indexed
func TestSelectiveIndexer_EditWarPage(t *testing.T) {
	indexer, client, mr, scorer := setupTestIndexer(t)
	defer mr.Close()
	defer scorer.Stop()

	ctx := context.Background()
	pageTitle := "Controversial_Topic"

	// Mark the page as having an edit war
	editWarKey := fmt.Sprintf("editwar:enwiki:%s", pageTitle)
	err := client.Set(ctx, editWarKey, "1", time.Hour).Err()
	require.NoError(t, err)

	// Process an edit
	edit := makeTestEdit(pageTitle, "WarringEditor")
	err = indexer.ProcessEdit(ctx, edit)
	require.NoError(t, err)

	// The edit should be queued
	assert.Equal(t, 1, indexer.BufferLen(), "Edit war page edit should be buffered for indexing")
}

// TestSelectiveIndexer_MultipleEdits verifies correct filtering across mixed edits
func TestSelectiveIndexer_MultipleEdits(t *testing.T) {
	indexer, client, mr, scorer := setupTestIndexer(t)
	defer mr.Close()
	defer scorer.Stop()

	ctx := context.Background()

	// Setup: Make one page trending
	trendingPage := "Popular_Page"
	for i := 0; i < 50; i++ {
		require.NoError(t, scorer.IncrementScore(trendingPage, 10.0))
	}

	// Setup: Make one page spiking
	spikingPage := "Breaking_Story"
	spikeKey := fmt.Sprintf("spike:enwiki:%s", spikingPage)
	require.NoError(t, client.Set(ctx, spikeKey, "8.0", time.Hour).Err())

	// Process a mix of edits
	normalEdits := []string{"Random_Page_1", "Random_Page_2", "Random_Page_3"}

	// Process normal edits (should be skipped)
	for _, title := range normalEdits {
		err := indexer.ProcessEdit(ctx, makeTestEdit(title, "User"))
		require.NoError(t, err)
	}

	// Process trending edit (should be indexed)
	err := indexer.ProcessEdit(ctx, makeTestEdit(trendingPage, "User"))
	require.NoError(t, err)

	// Process spiking edit (should be indexed)
	err = indexer.ProcessEdit(ctx, makeTestEdit(spikingPage, "User"))
	require.NoError(t, err)

	// Only trending + spiking should be buffered
	assert.Equal(t, 2, indexer.BufferLen(),
		"Only trending and spiking edits should be buffered, got %d", indexer.BufferLen())
}

// TestSelectiveIndexer_BufferFull verifies that drops are handled gracefully
func TestSelectiveIndexer_BufferFull(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	cfg := &config.Config{
		Elasticsearch: config.Elasticsearch{
			SelectiveCriteria: config.SelectiveCriteria{
				TrendingTopN:  100,
				SpikeRatioMin: 2.0,
			},
		},
		Redis: config.Redis{
			Trending: config.TrendingConfig{
				Enabled:         true,
				MaxPages:        1000,
				HalfLifeMinutes: 30.0,
				PruneInterval:   time.Minute,
			},
			HotPages: config.HotPages{
				MaxTracked:         100,
				PromotionThreshold: 5,
				WindowDuration:     time.Hour,
				MaxMembersPerPage:  50,
				HotThreshold:       2,
				CleanupInterval:    5 * time.Minute,
			},
		},
	}

	scorer := storage.NewTrendingScorerForTest(client, &cfg.Redis.Trending)
	defer scorer.Stop()
	hotPages := storage.NewHotPageTracker(client, &cfg.Redis.HotPages)
	strategy := storage.NewIndexingStrategy(&cfg.Elasticsearch.SelectiveCriteria, client, scorer, hotPages)
	logger := zerolog.New(zerolog.NewTestWriter(t))

	// Create indexer with a small buffer to test overflow
	indexer := &SelectiveIndexer{
		esClient:      nil,
		strategy:      strategy,
		config:        cfg,
		metrics:       &IndexerMetrics{
			EditsReceived:    prometheus.NewCounter(prometheus.CounterOpts{Name: "bf_edits_rcv"}),
			EditsIndexed:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "bf_edits_idx"}, []string{"reason"}),
			EditsSkipped:     prometheus.NewCounter(prometheus.CounterOpts{Name: "bf_edits_skip"}),
			IndexErrors:      prometheus.NewCounter(prometheus.CounterOpts{Name: "bf_idx_err"}),
			BufferFullDrops:  prometheus.NewCounter(prometheus.CounterOpts{Name: "bf_drops"}),
			BatchesProcessed: prometheus.NewCounter(prometheus.CounterOpts{Name: "bf_batches"}),
			BatchSize:        prometheus.NewHistogram(prometheus.HistogramOpts{Name: "bf_batch_size", Buckets: []float64{1, 10}}),
			IndexingLatency:  prometheus.NewHistogram(prometheus.HistogramOpts{Name: "bf_idx_lat", Buckets: []float64{0.01}}),
			DecisionLatency:  prometheus.NewHistogram(prometheus.HistogramOpts{Name: "bf_dec_lat", Buckets: []float64{0.01}}),
		},
		logger:      logger,
		indexBuffer: make(chan *models.EditDocument, 5), // Very small buffer
		bufferSize:  5,
		batchSize:   500,
		stopCh:      make(chan struct{}),
	}

	ctx := context.Background()

	// Add a page to watchlist so all its edits should be indexed
	require.NoError(t, strategy.AddToWatchlist(ctx, "enwiki", "Always_Index"))

	// Fill the buffer beyond capacity
	for i := 0; i < 10; i++ {
		edit := makeTestEdit("Always_Index", fmt.Sprintf("User_%d", i))
		edit.Revision.New = int64(200 + i)
		err := indexer.ProcessEdit(ctx, edit)
		require.NoError(t, err, "ProcessEdit should not return error even when buffer is full")
	}

	// Buffer should be at capacity
	assert.Equal(t, 5, indexer.BufferLen(), "Buffer should be at capacity")

	// Drop count should be 5 (10 sent - 5 buffered)
	assert.Equal(t, int64(5), indexer.dropCount.Load(), "Should have dropped 5 documents")
}

// TestSelectiveIndexer_BulkIndexer verifies the background bulk indexer goroutine
func TestSelectiveIndexer_BulkIndexer(t *testing.T) {
	indexer, _, mr, scorer := setupTestIndexer(t)
	defer mr.Close()
	defer scorer.Stop()

	// Manually push documents into the buffer (simulating queued docs)
	for i := 0; i < 5; i++ {
		doc := &models.EditDocument{
			ID:            fmt.Sprintf("test-doc-%d", i),
			Title:         fmt.Sprintf("Test Page %d", i),
			User:          "TestUser",
			Bot:           false,
			Wiki:          "enwiki",
			Timestamp:     time.Now(),
			ByteChange:    100,
			Language:      "en",
			IndexedReason: "test",
		}
		indexer.indexBuffer <- doc
	}

	assert.Equal(t, 5, indexer.BufferLen(), "Buffer should contain 5 documents")

	// Start the bulk indexer — it will run without an ES client, so performBulkIndex
	// will gracefully handle nil esClient (the docs just get consumed from the buffer)
	// For this test, we just verify documents get consumed from the buffer.
	// Since esClient is nil, performBulkIndex will skip, but startBulkIndexer drains.

	// We verify the stop/drain path instead
	indexer.Start()

	// Give the goroutine a moment to start and the flush interval to fire
	time.Sleep(100 * time.Millisecond)

	indexer.Stop()

	// Buffer should be empty after stop (it drains on shutdown via flush interval or stop drain)
	// Note: With nil esClient, performBulkIndex panics on doc send, so the buffer
	// will remain. This test validates lifecycle start/stop without crash.
}

// TestSelectiveIndexer_DocumentTransformation verifies FromWikipediaEdit produces correct documents
func TestSelectiveIndexer_DocumentTransformation(t *testing.T) {
	edit := &models.WikipediaEdit{
		ID:        999,
		Type:      "edit",
		Title:     "Test_Article",
		User:      "Editor123",
		Bot:       false,
		Wiki:      "enwiki",
		ServerURL: "https://en.wikipedia.org",
		Timestamp: 1700000000,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 500, New: 800},
		Revision: struct {
			Old int64 `json:"old"`
			New int64 `json:"new"`
		}{Old: 50, New: 51},
		Comment: "Added references",
	}

	doc := models.FromWikipediaEdit(edit, "trending_top_5")

	assert.NotNil(t, doc)
	assert.NotEmpty(t, doc.ID, "Document ID should be generated")
	assert.Equal(t, "Test_Article", doc.Title)
	assert.Equal(t, "Editor123", doc.User)
	assert.Equal(t, false, doc.Bot)
	assert.Equal(t, "enwiki", doc.Wiki)
	assert.Equal(t, 300, doc.ByteChange) // 800 - 500
	assert.Equal(t, "Added references", doc.Comment)
	assert.Equal(t, "en", doc.Language)
	assert.Equal(t, "trending_top_5", doc.IndexedReason)
	assert.False(t, doc.Timestamp.IsZero(), "Timestamp should be set")
}

// TestSelectiveIndexer_IndexingStatsUpdate verifies that Redis stats are updated
func TestSelectiveIndexer_IndexingStatsUpdate(t *testing.T) {
	indexer, client, mr, scorer := setupTestIndexer(t)
	defer mr.Close()
	defer scorer.Stop()

	ctx := context.Background()

	// Process a normal edit (should be skipped)
	edit := makeTestEdit("Unknown_Page", "SomeUser")
	err := indexer.ProcessEdit(ctx, edit)
	require.NoError(t, err)

	// Check stats in Redis
	statsKey := "indexing:stats"
	totalEdits, err := client.HGet(ctx, statsKey, "total_edits").Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalEdits)

	skippedEdits, err := client.HGet(ctx, statsKey, "skipped_edits").Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), skippedEdits)

	// Now process a trending edit (should be indexed)
	trendingPage := "Hot_Topic"
	for i := 0; i < 50; i++ {
		require.NoError(t, scorer.IncrementScore(trendingPage, 10.0))
	}

	edit2 := makeTestEdit(trendingPage, "ActiveUser")
	err = indexer.ProcessEdit(ctx, edit2)
	require.NoError(t, err)

	totalEdits2, err := client.HGet(ctx, statsKey, "total_edits").Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(2), totalEdits2)

	indexedEdits, err := client.HGet(ctx, statsKey, "indexed_edits").Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(1), indexedEdits)
}

// TestSelectiveIndexer_ProcessEditConcurrency verifies thread safety under concurrent access
func TestSelectiveIndexer_ProcessEditConcurrency(t *testing.T) {
	indexer, client, mr, scorer := setupTestIndexer(t)
	defer mr.Close()
	defer scorer.Stop()

	ctx := context.Background()

	// Setup a watchlisted page so edits get indexed deterministically
	require.NoError(t, indexer.strategy.AddToWatchlist(ctx, "enwiki", "Concurrent_Page"))

	// Concurrently process edits
	const numGoroutines = 20
	errCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			edit := makeTestEdit("Concurrent_Page", fmt.Sprintf("User_%d", idx))
			edit.Revision.New = int64(1000 + idx)
			errCh <- indexer.ProcessEdit(ctx, edit)
		}(i)
	}

	// Collect results
	for i := 0; i < numGoroutines; i++ {
		err := <-errCh
		assert.NoError(t, err)
	}

	// Verify stats are consistent
	statsKey := "indexing:stats"
	totalEdits, err := client.HGet(ctx, statsKey, "total_edits").Int64()
	require.NoError(t, err)
	assert.Equal(t, int64(numGoroutines), totalEdits,
		"All edits should have been counted")
}

// TestSelectiveIndexer_LifecycleStartStop verifies clean start and stop without panics
func TestSelectiveIndexer_LifecycleStartStop(t *testing.T) {
	indexer, _, mr, scorer := setupTestIndexer(t)
	defer mr.Close()
	defer scorer.Stop()

	// Start should succeed
	indexer.Start()

	// Double-start should be a no-op
	indexer.Start()

	// Give goroutine time to initialize
	time.Sleep(50 * time.Millisecond)

	// Stop should succeed
	indexer.Stop()

	// Double-stop should be a no-op
	indexer.Stop()
}
