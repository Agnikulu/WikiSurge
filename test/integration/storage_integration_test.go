package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

// Integration tests require Redis and Elasticsearch to be running
// These tests are skipped if services are not available

func TestStorageIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	ctx := context.Background()

	// Setup Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Test Redis connection
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available for integration tests")
	}

	// Clean up test data
	defer func() {
		redisClient.FlushDB(ctx)
		redisClient.Close()
	}()

	// Test configurations
	hotPagesConfig := &config.HotPages{
		MaxTracked:         100,
		PromotionThreshold: 5,
		WindowDuration:     5 * time.Minute,
		MaxMembersPerPage:  10,
	}

	trendingConfig := &config.TrendingConfig{
		Enabled:         true,
		MaxPages:        1000,
		HalfLifeMinutes: 30.0,
		PruneInterval:   1 * time.Minute,
	}

	selectiveConfig := &config.SelectiveCriteria{
		TrendingTopN:   10,
		SpikeRatioMin:  2.0,
		EditWarEnabled: true,
	}

	// Create storage components
	hotPages := NewRedisHotPages(redisClient, hotPagesConfig)
	trending := NewRedisTrending(redisClient, trendingConfig)
	alerts := NewRedisAlerts(redisClient)
	strategy := NewIndexingStrategy(selectiveConfig, redisClient, trending, hotPages)

	t.Run("HotPagesTracking", func(t *testing.T) {
		testHotPagesTracking(t, ctx, hotPages)
	})

	t.Run("TrendingTracking", func(t *testing.T) {
		testTrendingTracking(t, ctx, trending)
	})

	t.Run("AlertStreaming", func(t *testing.T) {
		testAlertStreaming(t, ctx, alerts)
	})

	t.Run("IndexingStrategy", func(t *testing.T) {
		testIndexingStrategy(t, ctx, strategy, hotPages, trending)
	})

	t.Run("EndToEndFlow", func(t *testing.T) {
		testEndToEndFlow(t, ctx, hotPages, trending, alerts, strategy)
	})
}

func testHotPagesTracking(t *testing.T, ctx context.Context, hotPages *RedisHotPages) {
	// Create test edit
	edit := &models.WikipediaEdit{
		Title: "Integration Test Page",
		User:  "TestUser1",
		Wiki:  "testwiki",
		Timestamp: time.Now().UnixMilli(),
	}

	// Track multiple edits to promote page to hot
	for i := 0; i < 6; i++ {
		edit.User = fmt.Sprintf("TestUser%d", i+1)
		err := hotPages.TrackEdit(ctx, edit)
		if err != nil {
			t.Fatalf("Failed to track edit %d: %v", i, err)
		}
	}

	// Verify page is now hot
	isHot, err := hotPages.IsHotPage(ctx, edit.Wiki, edit.Title)
	if err != nil {
		t.Fatalf("Failed to check hot page status: %v", err)
	}
	if !isHot {
		t.Errorf("Page should be hot after %d edits", 6)
	}

	// Get edit count
	count, err := hotPages.GetPageEditCount(ctx, edit.Wiki, edit.Title)
	if err != nil {
		t.Fatalf("Failed to get edit count: %v", err)
	}
	if count < 5 {
		t.Errorf("Expected edit count >= 5, got %d", count)
	}

	// Get hot pages list
	hotPagesList, err := hotPages.GetHotPages(ctx, 10)
	if err != nil {
		t.Fatalf("Failed to get hot pages list: %v", err)
	}
	if len(hotPagesList) == 0 {
		t.Errorf("Expected at least 1 hot page")
	}

	// Verify our page is in the list
	found := false
	expectedPageName := fmt.Sprintf("%s:%s", edit.Wiki, edit.Title)
	for _, page := range hotPagesList {
		if page.PageName == expectedPageName {
			found = true
			if page.EditCount < 5 {
				t.Errorf("Expected edit count >= 5, got %d", page.EditCount)
			}
			break
		}
	}
	if !found {
		t.Errorf("Hot page not found in list")
	}
}

func testTrendingTracking(t *testing.T, ctx context.Context, trending *RedisTrending) {
	// Create test edits for trending
	baseEdit := &models.WikipediaEdit{
		Title: "Trending Test Page",
		User:  "TrendingUser",
		Wiki:  "testwiki",
		Timestamp: time.Now().UnixMilli(),
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 1000, New: 1500}, // Significant change
		Type: "edit",
	}

	// Add multiple edits to build trending score
	for i := 0; i < 5; i++ {
		baseEdit.User = fmt.Sprintf("TrendingUser%d", i+1)
		err := trending.UpdateScore(ctx, baseEdit)
		if err != nil {
			t.Fatalf("Failed to update trending score %d: %v", i, err)
		}
	}

	// Get trending pages
	trendingPages, err := trending.GetTrendingPages(ctx, 10)
	if err != nil {
		t.Fatalf("Failed to get trending pages: %v", err)
	}

	// Verify our page is trending
	found := false
	for _, page := range trendingPages {
		if page.Wiki == baseEdit.Wiki && page.Title == baseEdit.Title {
			found = true
			if page.Score <= 0 {
				t.Errorf("Expected positive trending score, got %f", page.Score)
			}
			if page.EditCount != 5 {
				t.Errorf("Expected edit count 5, got %d", page.EditCount)
			}
			break
		}
	}
	if !found {
		t.Errorf("Trending page not found in results")
	}

	// Test page rank
	rank, err := trending.GetPageRank(ctx, baseEdit.Wiki, baseEdit.Title)
	if err != nil {
		t.Fatalf("Failed to get page rank: %v", err)
	}
	if rank <= 0 {
		t.Errorf("Expected positive rank, got %d", rank)
	}

	// Test top trending check
	isTopTrending, err := trending.IsTopTrending(ctx, baseEdit.Wiki, baseEdit.Title, 5)
	if err != nil {
		t.Fatalf("Failed to check top trending: %v", err)
	}
	if !isTopTrending {
		t.Errorf("Page should be in top 5 trending")
	}
}

func testAlertStreaming(t *testing.T, ctx context.Context, alerts *RedisAlerts) {
	// Test spike alert
	err := alerts.PublishSpikeAlert(ctx, "testwiki", "Alert Test Page", 3.5, 15)
	if err != nil {
		t.Fatalf("Failed to publish spike alert: %v", err)
	}

	// Test edit war alert
	participants := []string{"User1", "User2", "User3"}
	err = alerts.PublishEditWarAlert(ctx, "testwiki", "Edit War Page", participants, 500)
	if err != nil {
		t.Fatalf("Failed to publish edit war alert: %v", err)
	}

	// Test trending alert
	err = alerts.PublishTrendingAlert(ctx, "testwiki", "Trending Alert Page", 1, 95.5)
	if err != nil {
		t.Fatalf("Failed to publish trending alert: %v", err)
	}

	// Get recent alerts
	spikes, err := alerts.GetRecentAlerts(ctx, "spikes", 10)
	if err != nil {
		t.Fatalf("Failed to get recent spike alerts: %v", err)
	}
	if len(spikes) == 0 {
		t.Errorf("Expected at least 1 spike alert")
	}

	editWars, err := alerts.GetRecentAlerts(ctx, "editwars", 10)
	if err != nil {
		t.Fatalf("Failed to get recent edit war alerts: %v", err)
	}
	if len(editWars) == 0 {
		t.Errorf("Expected at least 1 edit war alert")
	}

	// Get alert stats
	alertTypes := []string{"spikes", "editwars", "trending"}
	stats, err := alerts.GetAlertStats(ctx, alertTypes)
	if err != nil {
		t.Fatalf("Failed to get alert stats: %v", err)
	}

	for _, alertType := range alertTypes {
		if stat, exists := stats[alertType]; exists {
			if stat.Length <= 0 {
				t.Errorf("Expected positive length for %s alerts, got %d", alertType, stat.Length)
			}
		}
	}
}

func testIndexingStrategy(t *testing.T, ctx context.Context, strategy *IndexingStrategy, hotPages *RedisHotPages, trending *RedisTrending) {
	// Add a page to watchlist
	err := strategy.AddToWatchlist(ctx, "testwiki", "Watchlist Page")
	if err != nil {
		t.Fatalf("Failed to add to watchlist: %v", err)
	}

	// Test watchlist page indexing (should always index)
	watchlistEdit := &models.WikipediaEdit{
		Title: "Watchlist Page",
		Wiki:  "testwiki",
		User:  "WatchlistUser",
		Timestamp: time.Now().UnixMilli(),
	}

	decision, err := strategy.ShouldIndex(ctx, watchlistEdit)
	if err != nil {
		t.Fatalf("Failed to make indexing decision: %v", err)
	}
	if !decision.ShouldIndex {
		t.Errorf("Watchlist page should always be indexed")
	}
	if decision.Reason != "watchlist" {
		t.Errorf("Expected reason 'watchlist', got '%s'", decision.Reason)
	}

	// Make a page trending and test indexing
	trendingEdit := &models.WikipediaEdit{
		Title: "Strategy Trending Page",
		Wiki:  "testwiki",
		User:  "StrategyUser",
		Timestamp: time.Now().UnixMilli(),
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 1000, New: 2000},
	}

	// Build trending score
	for i := 0; i < 10; i++ {
		trendingEdit.User = fmt.Sprintf("StrategyUser%d", i+1)
		trending.UpdateScore(ctx, trendingEdit)
	}

	// Test trending indexing decision
	time.Sleep(100 * time.Millisecond) // Brief delay for Redis operations
	decision, err = strategy.ShouldIndex(ctx, trendingEdit)
	if err != nil {
		t.Fatalf("Failed to make trending indexing decision: %v", err)
	}
	
	// Should index because it's trending
	if !decision.ShouldIndex {
		t.Errorf("Trending page should be indexed")
	}

	// Test normal page (should not index)
	normalEdit := &models.WikipediaEdit{
		Title: "Normal Page",
		Wiki:  "testwiki",
		User:  "NormalUser",
		Timestamp: time.Now().UnixMilli(),
	}

	decision, err = strategy.ShouldIndex(ctx, normalEdit)
	if err != nil {
		t.Fatalf("Failed to make normal indexing decision: %v", err)
	}
	if decision.ShouldIndex {
		t.Errorf("Normal page should not be indexed (reason: %s)", decision.Reason)
	}
	if decision.Reason != "not_significant" {
		t.Errorf("Expected reason 'not_significant', got '%s'", decision.Reason)
	}

	// Update stats and verify
	strategy.UpdateIndexingStats(ctx, decision)
	stats, err := strategy.GetIndexingStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get indexing stats: %v", err)
	}
	if stats.TotalEdits < 1 {
		t.Errorf("Expected at least 1 total edit in stats")
	}
}

func testEndToEndFlow(t *testing.T, ctx context.Context, hotPages *RedisHotPages, trending *RedisTrending, alerts *RedisAlerts, strategy *IndexingStrategy) {
	// Simulate a page getting significant activity
	pageTitle := "End to End Test Page"
	pageWiki := "testwiki"

	// Create multiple edits to trigger hot page and trending
	for i := 0; i < 8; i++ {
		edit := &models.WikipediaEdit{
			Title: pageTitle,
			Wiki:  pageWiki,
			User:  fmt.Sprintf("E2EUser%d", i+1),
			Timestamp: time.Now().UnixMilli(),
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 1000 + i*100, New: 1000 + (i+1)*100 + 50},
		}

		// Track in hot pages
		err := hotPages.TrackEdit(ctx, edit)
		if err != nil {
			t.Fatalf("Failed to track edit in hot pages: %v", err)
		}

		// Update trending score
		err = trending.UpdateScore(ctx, edit)
		if err != nil {
			t.Fatalf("Failed to update trending score: %v", err)
		}

		// Make indexing decision
		decision, err := strategy.ShouldIndex(ctx, edit)
		if err != nil {
			t.Fatalf("Failed to make indexing decision: %v", err)
		}

		// Update stats
		err = strategy.UpdateIndexingStats(ctx, decision)
		if err != nil {
			t.Fatalf("Failed to update indexing stats: %v", err)
		}

		if i >= 5 { // Should be hot and trending by now
			if !decision.ShouldIndex {
				t.Errorf("Edit %d should be indexed (page should be hot/trending)", i)
			}
		}
	}

	// Verify the page is hot
	isHot, err := hotPages.IsHotPage(ctx, pageWiki, pageTitle)
	if err != nil {
		t.Fatalf("Failed to check hot status: %v", err)
	}
	if !isHot {
		t.Errorf("Page should be hot after multiple edits")
	}

	// Verify the page is trending
	rank, err := trending.GetPageRank(ctx, pageWiki, pageTitle)
	if err != nil {
		t.Fatalf("Failed to get trending rank: %v", err)
	}
	if rank > 10 {
		t.Errorf("Page should be in top 10 trending, got rank %d", rank)
	}

	// Publish spike alert for the page
	err = alerts.PublishSpikeAlert(ctx, pageWiki, pageTitle, 4.2, 8)
	if err != nil {
		t.Fatalf("Failed to publish spike alert: %v", err)
	}

	// Verify alert was created
	recentSpikes, err := alerts.GetRecentAlerts(ctx, "spikes", 1)
	if err != nil {
		t.Fatalf("Failed to get recent spike alerts: %v", err)
	}
	if len(recentSpikes) == 0 {
		t.Errorf("Expected at least 1 recent spike alert")
	}

	// Get final stats
	stats, err := strategy.GetIndexingStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get final stats: %v", err)
	}

	t.Logf("End-to-end test completed successfully")
	t.Logf("Total edits processed: %d", stats.TotalEdits)
	t.Logf("Edits indexed: %d", stats.IndexedEdits)
	t.Logf("Indexing rate: %.2f%%", stats.IndexingRate*100)
}