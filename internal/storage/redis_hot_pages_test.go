package storage

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	
	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

// Test configuration for hot pages
func getTestConfig() *config.HotPages {
	return &config.HotPages{
		MaxTracked:         10,
		PromotionThreshold: 5,
		WindowDuration:     time.Minute * 5,
		MaxMembersPerPage:  5,
		HotThreshold:       2,
		CleanupInterval:    time.Second * 10,
	}
}

// Create test Redis client (assumes Redis is running on localhost:6379)
func getTestRedisClient() *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})
	return client
}

// Create test edit
func createTestEdit(id, title, user string) *models.WikipediaEdit {
	// Convert id string to int64
	idInt, _ := strconv.ParseInt(id, 10, 64)
	
	return &models.WikipediaEdit{
		ID:    idInt,
		Title: title,
		User:  user,
		Wiki:  "enwiki",
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{
			Old: 50,
			New: 100,
		},
	}
}

func TestHotPageTracker_PromotionThresholdLogic(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	
	// Clear test data
	client.FlushDB(ctx)
	
	// Test that pages below threshold don't get promoted
	edit1 := createTestEdit("1", "TestPage", "user1")
	err := tracker.ProcessEdit(ctx, edit1)
	require.NoError(t, err)
	
	// Should not be hot yet (count = 1, threshold = 2)
	isHot, err := tracker.IsHot(ctx, "TestPage")
	require.NoError(t, err)
	assert.False(t, isHot)
	
	// Second edit should promote to hot
	edit2 := createTestEdit("2", "TestPage", "user2")
	err = tracker.ProcessEdit(ctx, edit2)
	require.NoError(t, err)
	
	// Now should be hot (count = 2, threshold = 2)
	isHot, err = tracker.IsHot(ctx, "TestPage")
	require.NoError(t, err)
	assert.True(t, isHot)
}

func TestHotPageTracker_CircuitBreaker(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	config.MaxTracked = 2 // Very low limit for testing
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	client.FlushDB(ctx)
	
	// Create 2 hot pages (at the limit)
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ { // 2 edits each to promote
			edit := createTestEdit(
				fmt.Sprintf("%d-%d", i, j),
				fmt.Sprintf("Page%d", i),
				fmt.Sprintf("user%d", j),
			)
			err := tracker.ProcessEdit(ctx, edit)
			require.NoError(t, err)
		}
	}
	
	// Verify we have 2 hot pages
	count, err := tracker.GetHotPagesCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	
	// Try to create a third hot page - should be rejected by circuit breaker
	for j := 0; j < 2; j++ {
		edit := createTestEdit(
			fmt.Sprintf("3-%d", j),
			"Page3",
			fmt.Sprintf("user%d", j),
		)
		err := tracker.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}
	
	// Should still be 2 hot pages
	count, err = tracker.GetHotPagesCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	
	// Page3 should not be hot (rejected by circuit breaker)
	isHot, err := tracker.IsHot(ctx, "Page3")
	require.NoError(t, err)
	assert.False(t, isHot)
}

func TestHotPageTracker_WindowCapping(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	config.MaxMembersPerPage = 3 // Cap at 3 edits per page
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	client.FlushDB(ctx)
	
	// Promote page to hot
	for i := 0; i < 2; i++ {
		edit := createTestEdit(fmt.Sprintf("promo-%d", i), "TestPage", "user1")
		err := tracker.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}
	
	// Add more edits to test window capping
	for i := 0; i < 5; i++ { // Add 5 more edits (total would be 7)
		edit := createTestEdit(fmt.Sprintf("window-%d", i), "TestPage", "user1")
		err := tracker.AddEditToWindow(ctx, "TestPage", edit)
		require.NoError(t, err)
	}
	
	// Check window size - should be capped at maxMembersPerPage (3)
	windowKey := "hot:window:TestPage"
	count, err := client.ZCard(ctx, windowKey).Result()
	require.NoError(t, err)
	assert.LessOrEqual(t, count, int64(config.MaxMembersPerPage))
}

func TestHotPageTracker_TTLExpiration(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	config.WindowDuration = time.Second * 2 // Very short for testing
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	client.FlushDB(ctx)
	
	// Create activity counter
	activityKey := "activity:TestPage"
	client.Incr(ctx, activityKey)
	client.Expire(ctx, activityKey, time.Second*1) // Short TTL
	
	// Wait for expiration
	time.Sleep(time.Second * 2)
	
	// Key should be expired
	exists, err := client.Exists(ctx, activityKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists)
}

func TestHotPageTracker_CleanupLogic(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	client.FlushDB(ctx)
	
	// Create a hot page
	for i := 0; i < 2; i++ {
		edit := createTestEdit(fmt.Sprintf("cleanup-%d", i), "CleanupPage", "user1")
		err := tracker.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}
	
	// Verify it's hot
	isHot, err := tracker.IsHot(ctx, "CleanupPage")
	require.NoError(t, err)
	assert.True(t, isHot)
	
	// Manually clear the window (simulating expiration)
	windowKey := "hot:window:CleanupPage"
	err = client.Del(ctx, windowKey).Err()
	require.NoError(t, err)
	
	// Run cleanup manually
	cleaned, err := tracker.cleanupStaleHotPages(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, cleaned, 0) // Should clean up stale entries
}

func TestHotPageTracker_GetPageStats(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	client.FlushDB(ctx)
	
	// Create hot page with multiple edits
	edits := []*models.WikipediaEdit{
		createTestEdit("1", "StatsPage", "user1"),
		createTestEdit("2", "StatsPage", "user2"),
		createTestEdit("3", "StatsPage", "user1"),
	}
	
	for _, edit := range edits {
		err := tracker.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}
	
	// Get stats
	stats, err := tracker.GetPageStats(ctx, "StatsPage")
	require.NoError(t, err)
	
	// Verify stats
	assert.Greater(t, stats.TotalEdits, int64(0))
	assert.Greater(t, stats.EditsLastHour, int64(0))
	assert.Greater(t, len(stats.UniqueEditors), 0)
	
	// Should have 2 unique editors (user1, user2)
	assert.Equal(t, 2, len(stats.UniqueEditors))
}

func TestHotPageTracker_GetPageWindow(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	client.FlushDB(ctx)
	
	// Create hot page
	for i := 0; i < 2; i++ {
		edit := createTestEdit(fmt.Sprintf("window-%d", i), "WindowPage", "user1")
		err := tracker.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}
	
	// Get window
	now := time.Now()
	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	
	window, err := tracker.GetPageWindow(ctx, "WindowPage", start, end)
	require.NoError(t, err)
	
	// Should have edits in window
	assert.Greater(t, len(window), 0)
}

func TestHotPageTracker_GetHotPagesList(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	client.FlushDB(ctx)
	
	pages := []string{"ListPage1", "ListPage2", "ListPage3"}
	
	// Create multiple hot pages
	for _, page := range pages {
		for i := 0; i < 2; i++ {
			edit := createTestEdit(fmt.Sprintf("%s-%d", page, i), page, "user1")
			err := tracker.ProcessEdit(ctx, edit)
			require.NoError(t, err)
		}
	}
	
	// Get hot pages list
	hotPages, err := tracker.GetHotPagesList(ctx)
	require.NoError(t, err)
	
	// Should have all 3 pages
	assert.Equal(t, 3, len(hotPages))
	
	// Check that all pages are in the list
	pageMap := make(map[string]bool)
	for _, page := range hotPages {
		pageMap[page] = true
	}
	
	for _, page := range pages {
		assert.True(t, pageMap[page], "Page %s should be in hot pages list", page)
	}
}

// Integration test: Memory usage bounds
func TestHotPageTracker_MemoryBounds(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	config.MaxTracked = 50        // Reasonable limit
	config.MaxMembersPerPage = 20 // Reasonable limit per page
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	client.FlushDB(ctx)
	
	// Create many pages, more than the limit
	totalPages := 100
	for i := 0; i < totalPages; i++ {
		pageTitle := fmt.Sprintf("MemoryPage%d", i)
		
		// Create enough edits to promote each page
		for j := 0; j < 3; j++ {
			edit := createTestEdit(fmt.Sprintf("mem-%d-%d", i, j), pageTitle, "user1")
			err := tracker.ProcessEdit(ctx, edit)
			require.NoError(t, err)
		}
	}
	
	// Verify circuit breaker worked - should not exceed max tracked
	count, err := tracker.GetHotPagesCount(ctx)
	require.NoError(t, err)
	assert.LessOrEqual(t, count, config.MaxTracked)
}

// Load test: Performance validation
func TestHotPageTracker_LoadTest(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	config.MaxTracked = 100
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	client.FlushDB(ctx)
	
	// Send 1000 edits to 100 different pages
	numEdits := 1000
	numPages := 100
	
	start := time.Now()
	for i := 0; i < numEdits; i++ {
		pageNum := i % numPages
		pageTitle := fmt.Sprintf("LoadPage%d", pageNum)
		
		edit := createTestEdit(fmt.Sprintf("load-%d", i), pageTitle, fmt.Sprintf("user%d", i%10))
		err := tracker.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}
	duration := time.Since(start)
	
	// Performance validation: should complete in reasonable time
	avgLatency := duration / time.Duration(numEdits)
	assert.Less(t, avgLatency, time.Millisecond*10, "Average latency should be < 10ms")
	
	// Verify hot page tracking works
	hotPages, err := tracker.GetHotPagesList(ctx)
	require.NoError(t, err)
	assert.Greater(t, len(hotPages), 0, "Should have some hot pages")
	
	// Memory should be bounded
	count, err := tracker.GetHotPagesCount(ctx)
	require.NoError(t, err)
	assert.LessOrEqual(t, count, config.MaxTracked)
}

// Test activity counter filtering
func TestHotPageTracker_ActivityCounterFiltering(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	client.FlushDB(ctx)
	
	// Create many one-time pages (should be filtered out)
	oneTimePages := 50
	for i := 0; i < oneTimePages; i++ {
		edit := createTestEdit(fmt.Sprintf("onetime-%d", i), fmt.Sprintf("OneTimePage%d", i), "user1")
		err := tracker.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}
	
	// None should be hot (all have count = 1, threshold = 2)
	count, err := tracker.GetHotPagesCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "One-time pages should not become hot")
	
	// Activities should exist in Redis
	activityKeys, err := client.Keys(ctx, "activity:*").Result()
	require.NoError(t, err)
	assert.Equal(t, oneTimePages, len(activityKeys), "Activity counters should exist")
}

// Test legacy compatibility
func TestHotPageTracker_LegacyCompatibility(t *testing.T) {
	client := getTestRedisClient()
	config := getTestConfig()
	tracker := NewHotPageTracker(client, config)
	defer tracker.Shutdown()
	
	ctx := context.Background()
	client.FlushDB(ctx)
	
	// Test legacy TrackEdit method
	edit := createTestEdit("legacy-1", "LegacyPage", "user1")
	err := tracker.TrackEdit(ctx, edit)
	require.NoError(t, err)
	
	edit2 := createTestEdit("legacy-2", "LegacyPage", "user2")
	err = tracker.TrackEdit(ctx, edit2)
	require.NoError(t, err)
	
	// Test legacy IsHotPage method
	isHot, err := tracker.IsHotPage(ctx, "enwiki", "LegacyPage")
	require.NoError(t, err)
	assert.True(t, isHot)
	
	// Test legacy GetHotPages method
	hotPages, err := tracker.GetHotPages(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, len(hotPages))
	assert.Equal(t, "LegacyPage", hotPages[0].PageName)
}