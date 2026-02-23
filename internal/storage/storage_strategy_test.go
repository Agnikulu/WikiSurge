package storage

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

func setupTestStrategy(t *testing.T) (*IndexingStrategy, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })

	cfg := &config.SelectiveCriteria{
		TrendingTopN:   100,
		SpikeRatioMin:  2.0,
		EditWarEnabled: true,
		SampleRate:     0, // disable sampling for deterministic tests
	}

	strategy := NewIndexingStrategy(cfg, client, nil, nil)
	return strategy, mr, client
}

func testEdit(wiki, title string) *models.WikipediaEdit {
	return &models.WikipediaEdit{
		Wiki:  wiki,
		Title: title,
	}
}

// ---------------------------------------------------------------------------
// ShouldIndex — basic decisions
// ---------------------------------------------------------------------------

func TestShouldIndex_NotSignificant(t *testing.T) {
	strategy, _, _ := setupTestStrategy(t)
	ctx := context.Background()

	decision, err := strategy.ShouldIndex(ctx, testEdit("enwiki", "Normal_Page"))
	require.NoError(t, err)
	assert.False(t, decision.ShouldIndex)
	assert.Equal(t, "not_significant", decision.Reason)
}

func TestShouldIndex_Watchlist(t *testing.T) {
	strategy, _, _ := setupTestStrategy(t)
	ctx := context.Background()

	require.NoError(t, strategy.AddToWatchlist(ctx, "enwiki", "Important_Page"))

	decision, err := strategy.ShouldIndex(ctx, testEdit("enwiki", "Important_Page"))
	require.NoError(t, err)
	assert.True(t, decision.ShouldIndex)
	assert.Equal(t, "watchlist", decision.Reason)
}

func TestShouldIndex_EditWar(t *testing.T) {
	strategy, _, rc := setupTestStrategy(t)
	ctx := context.Background()

	// Set edit war marker
	rc.Set(ctx, "editwar:enwiki:Disputed_Page", "1", 12*time.Hour)

	decision, err := strategy.ShouldIndex(ctx, testEdit("enwiki", "Disputed_Page"))
	require.NoError(t, err)
	assert.True(t, decision.ShouldIndex)
	assert.Equal(t, "edit_war", decision.Reason)
}

func TestShouldIndex_Spiking(t *testing.T) {
	strategy, _, rc := setupTestStrategy(t)
	ctx := context.Background()

	// Set spike marker
	rc.Set(ctx, "spike:enwiki:Hot_Topic", "5.5", time.Hour)

	decision, err := strategy.ShouldIndex(ctx, testEdit("enwiki", "Hot_Topic"))
	require.NoError(t, err)
	assert.True(t, decision.ShouldIndex)
	assert.Contains(t, decision.Reason, "spiking")
}

func TestShouldIndex_SpikingBelowMin(t *testing.T) {
	strategy, _, rc := setupTestStrategy(t)
	ctx := context.Background()

	// Spike ratio below minimum threshold (2.0)
	rc.Set(ctx, "spike:enwiki:Low_Spike", "1.5", time.Hour)

	decision, err := strategy.ShouldIndex(ctx, testEdit("enwiki", "Low_Spike"))
	require.NoError(t, err)
	assert.False(t, decision.ShouldIndex)
}

func TestShouldIndex_EditWarDisabled(t *testing.T) {
	strategy, _, rc := setupTestStrategy(t)
	ctx := context.Background()

	strategy.config.EditWarEnabled = false
	rc.Set(ctx, "editwar:enwiki:War_Page", "1", 12*time.Hour)

	decision, err := strategy.ShouldIndex(ctx, testEdit("enwiki", "War_Page"))
	require.NoError(t, err)
	assert.False(t, decision.ShouldIndex)
}

// ---------------------------------------------------------------------------
// Watchlist operations
// ---------------------------------------------------------------------------

func TestAddToWatchlist(t *testing.T) {
	strategy, _, rc := setupTestStrategy(t)
	ctx := context.Background()

	err := strategy.AddToWatchlist(ctx, "enwiki", "MyPage")
	require.NoError(t, err)

	// Verify in Redis
	isMember, err := rc.SIsMember(ctx, "indexing:watchlist", "enwiki:MyPage").Result()
	require.NoError(t, err)
	assert.True(t, isMember)

	// Verify in memory
	assert.True(t, strategy.isInWatchlist("enwiki:MyPage"))
}

func TestRemoveFromWatchlist(t *testing.T) {
	strategy, _, rc := setupTestStrategy(t)
	ctx := context.Background()

	strategy.AddToWatchlist(ctx, "enwiki", "MyPage")
	err := strategy.RemoveFromWatchlist(ctx, "enwiki", "MyPage")
	require.NoError(t, err)

	// Verify removed from Redis
	isMember, err := rc.SIsMember(ctx, "indexing:watchlist", "enwiki:MyPage").Result()
	require.NoError(t, err)
	assert.False(t, isMember)

	// Verify removed from memory
	assert.False(t, strategy.isInWatchlist("enwiki:MyPage"))
}

func TestGetWatchlist(t *testing.T) {
	strategy, _, _ := setupTestStrategy(t)
	ctx := context.Background()

	strategy.AddToWatchlist(ctx, "enwiki", "Page1")
	strategy.AddToWatchlist(ctx, "enwiki", "Page2")

	pages, err := strategy.GetWatchlist(ctx)
	require.NoError(t, err)
	assert.Len(t, pages, 2)
	assert.Contains(t, pages, "enwiki:Page1")
	assert.Contains(t, pages, "enwiki:Page2")
}

func TestGetWatchlist_Empty(t *testing.T) {
	strategy, _, _ := setupTestStrategy(t)
	ctx := context.Background()

	pages, err := strategy.GetWatchlist(ctx)
	require.NoError(t, err)
	assert.Empty(t, pages)
}

// ---------------------------------------------------------------------------
// LoadWatchlist from Redis
// ---------------------------------------------------------------------------

func TestLoadWatchlist(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	ctx := context.Background()

	// Pre-populate Redis
	client.SAdd(ctx, "indexing:watchlist", "enwiki:PreExisting")

	cfg := &config.SelectiveCriteria{TrendingTopN: 100, SpikeRatioMin: 2.0}
	strategy := NewIndexingStrategy(cfg, client, nil, nil)

	assert.True(t, strategy.isInWatchlist("enwiki:PreExisting"))
}

// ---------------------------------------------------------------------------
// GetIndexingStats
// ---------------------------------------------------------------------------

func TestGetIndexingStats_Empty(t *testing.T) {
	strategy, _, _ := setupTestStrategy(t)
	ctx := context.Background()

	stats, err := strategy.GetIndexingStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), stats.TotalEdits)
	assert.Equal(t, float64(0), stats.IndexingRate)
}

func TestGetIndexingStats_WithData(t *testing.T) {
	strategy, _, rc := setupTestStrategy(t)
	ctx := context.Background()

	rc.HSet(ctx, "indexing:stats", "total_edits", "1000")
	rc.HSet(ctx, "indexing:stats", "indexed_edits", "200")
	rc.HSet(ctx, "indexing:stats", "skipped_edits", "800")
	rc.HSet(ctx, "indexing:stats", "watchlist_index", "50")
	rc.HSet(ctx, "indexing:stats", "trending_index", "100")

	stats, err := strategy.GetIndexingStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), stats.TotalEdits)
	assert.Equal(t, int64(200), stats.IndexedEdits)
	assert.Equal(t, int64(800), stats.SkippedEdits)
	assert.Equal(t, int64(50), stats.WatchlistIndex)
	assert.Equal(t, int64(100), stats.TrendingIndex)
	assert.InDelta(t, 0.2, stats.IndexingRate, 0.001)
}

// ---------------------------------------------------------------------------
// UpdateIndexingStats
// ---------------------------------------------------------------------------

func TestUpdateIndexingStats_Indexed(t *testing.T) {
	strategy, _, rc := setupTestStrategy(t)
	ctx := context.Background()

	decision := &IndexingDecision{ShouldIndex: true, Reason: "watchlist"}
	err := strategy.UpdateIndexingStats(ctx, decision)
	require.NoError(t, err)

	total, _ := rc.HGet(ctx, "indexing:stats", "total_edits").Result()
	indexed, _ := rc.HGet(ctx, "indexing:stats", "indexed_edits").Result()
	wl, _ := rc.HGet(ctx, "indexing:stats", "watchlist_index").Result()
	assert.Equal(t, "1", total)
	assert.Equal(t, "1", indexed)
	assert.Equal(t, "1", wl)
}

func TestUpdateIndexingStats_Skipped(t *testing.T) {
	strategy, _, rc := setupTestStrategy(t)
	ctx := context.Background()

	decision := &IndexingDecision{ShouldIndex: false, Reason: "not_significant"}
	err := strategy.UpdateIndexingStats(ctx, decision)
	require.NoError(t, err)

	skipped, _ := rc.HGet(ctx, "indexing:stats", "skipped_edits").Result()
	assert.Equal(t, "1", skipped)
}

func TestUpdateIndexingStats_VariousReasons(t *testing.T) {
	strategy, _, rc := setupTestStrategy(t)
	ctx := context.Background()

	reasons := []struct {
		reason string
		field  string
	}{
		{"trending_top_5", "trending_index"},
		{"spiking_3.50", "spiking_index"},
		{"edit_war", "editwar_index"},
		{"hot_page", "hotpage_index"},
	}

	for _, r := range reasons {
		strategy.UpdateIndexingStats(ctx, &IndexingDecision{ShouldIndex: true, Reason: r.reason})
	}

	for _, r := range reasons {
		val, _ := rc.HGet(ctx, "indexing:stats", r.field).Result()
		assert.Equal(t, "1", val, "field %s should be 1", r.field)
	}
}

// ---------------------------------------------------------------------------
// getPageContext
// ---------------------------------------------------------------------------

func TestGetPageContext_NoMarkers(t *testing.T) {
	strategy, _, _ := setupTestStrategy(t)
	ctx := context.Background()

	pc, err := strategy.getPageContext(ctx, "enwiki", "Normal_Page")
	require.NoError(t, err)
	assert.False(t, pc.IsHotPage)
	assert.False(t, pc.IsSpiking)
	assert.False(t, pc.IsEditWar)
	assert.Equal(t, 0, pc.TrendingRank)
}

func TestGetPageContext_Cached(t *testing.T) {
	strategy, _, rc := setupTestStrategy(t)
	ctx := context.Background()

	// First call populates cache
	pc1, err := strategy.getPageContext(ctx, "enwiki", "TestPage")
	require.NoError(t, err)
	assert.False(t, pc1.IsSpiking)

	// Set spike AFTER first call
	rc.Set(ctx, "spike:enwiki:TestPage", "5.0", time.Hour)

	// Second call within TTL returns cached version (not spiking)
	pc2, err := strategy.getPageContext(ctx, "enwiki", "TestPage")
	require.NoError(t, err)
	assert.False(t, pc2.IsSpiking) // still cached as not spiking
}

// ---------------------------------------------------------------------------
// parseInt64
// ---------------------------------------------------------------------------

func TestParseInt64(t *testing.T) {
	assert.Equal(t, int64(42), parseInt64("42"))
	assert.Equal(t, int64(0), parseInt64(""))
	assert.Equal(t, int64(0), parseInt64("notanumber"))
	assert.Equal(t, int64(-10), parseInt64("-10"))
}
