package processor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupEditWarTestComponents(t *testing.T) (*EditWarDetector, *storage.HotPageTracker, *redis.Client) {
	t.Helper()

	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   2, // Use separate test database
	})

	// Clear test database
	redisClient.FlushDB(context.Background())

	cfg := &config.Config{
		Redis: config.Redis{
			HotPages: config.HotPages{
				MaxTracked:        100,
				HotThreshold:      2,
				WindowDuration:    time.Hour,
				MaxMembersPerPage: 50,
				CleanupInterval:   5 * time.Minute,
			},
		},
	}

	logger := zerolog.New(nil).Level(zerolog.Disabled)
	hotPageTracker := storage.NewHotPageTracker(redisClient, &cfg.Redis.HotPages)
	detector := NewEditWarDetector(hotPageTracker, redisClient, cfg, logger)

	return detector, hotPageTracker, redisClient
}

// promotePageToHot simulates enough edits to promote a page to hot tracking
func promotePageToHot(t *testing.T, ctx context.Context, hotPages *storage.HotPageTracker, pageTitle string) {
	t.Helper()
	for i := 0; i < 3; i++ {
		edit := &models.WikipediaEdit{
			ID:        int64(10000 + i),
			Title:     pageTitle,
			Type:      "edit",
			User:      fmt.Sprintf("promoter_%d", i),
			Wiki:      "enwiki",
			ServerURL: "https://en.wikipedia.org",
			Timestamp: time.Now().Unix(),
			Bot:       false,
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 1000, New: 1050},
			Revision: struct {
				Old int64 `json:"old"`
				New int64 `json:"new"`
			}{Old: int64(100 + i), New: int64(101 + i)},
		}
		err := hotPages.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}
	// Verify promoted
	isHot, err := hotPages.IsHot(ctx, pageTitle)
	require.NoError(t, err)
	require.True(t, isHot, "Page %s should be hot after promotion edits", pageTitle)
}

func makeEdit(id int64, title, user string, oldLen, newLen int) *models.WikipediaEdit {
	return &models.WikipediaEdit{
		ID:        id,
		Title:     title,
		Type:      "edit",
		User:      user,
		Wiki:      "enwiki",
		ServerURL: "https://en.wikipedia.org",
		Timestamp: time.Now().Unix(),
		Bot:       false,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: oldLen, New: newLen},
		Revision: struct {
			Old int64 `json:"old"`
			New int64 `json:"new"`
		}{Old: id, New: id + 1},
	}
}

// TestScenario1_ClearEditWar tests detection of a clear edit war pattern
func TestScenario1_ClearEditWar(t *testing.T) {
	detector, hotPages, redisClient := setupEditWarTestComponents(t)
	defer redisClient.Close()

	ctx := context.Background()
	pageTitle := "Edit_War_Test_Page_1"

	// Promote page to hot
	promotePageToHot(t, ctx, hotPages, pageTitle)

	// Simulate clear edit war pattern:
	// User A: +500, User B: -480, User A: +490, User B: -495, User A: +500
	edits := []*models.WikipediaEdit{
		makeEdit(1001, pageTitle, "UserA", 1000, 1500), // +500
		makeEdit(1002, pageTitle, "UserB", 1500, 1020), // -480
		makeEdit(1003, pageTitle, "UserA", 1020, 1510), // +490
		makeEdit(1004, pageTitle, "UserB", 1510, 1015), // -495
		makeEdit(1005, pageTitle, "UserA", 1015, 1515), // +500
		makeEdit(1006, pageTitle, "UserB", 1515, 1025), // -490
	}

	for _, edit := range edits {
		err := detector.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}

	// Verify edit war was detected
	editWarKey := fmt.Sprintf("editwar:%s", pageTitle)
	exists, err := redisClient.Exists(ctx, editWarKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), exists, "Expected editwar marker key to exist")

	// Check alert stream
	alerts, err := detector.GetRecentAlerts(ctx, time.Now().Add(-time.Minute), 10)
	require.NoError(t, err)
	assert.True(t, len(alerts) > 0, "Expected edit war alert to be published")

	if len(alerts) > 0 {
		alert := alerts[0]
		assert.Equal(t, pageTitle, alert.PageTitle)
		assert.Equal(t, 2, alert.EditorCount, "Expected 2 editors")
		assert.True(t, alert.RevertCount >= 2, "Expected at least 2 reverts, got %d", alert.RevertCount)
		assert.Contains(t, []string{"low", "medium"}, alert.Severity,
			"Expected low or medium severity for 2-editor war, got %s", alert.Severity)
	}
}

// TestScenario2_CollaborativeEditing tests that collaborative editing is NOT flagged
func TestScenario2_CollaborativeEditing(t *testing.T) {
	detector, hotPages, redisClient := setupEditWarTestComponents(t)
	defer redisClient.Close()

	ctx := context.Background()
	pageTitle := "Collaborative_Test_Page"

	promotePageToHot(t, ctx, hotPages, pageTitle)

	// Simulate collaborative editing: no significant reverts
	edits := []*models.WikipediaEdit{
		makeEdit(2001, pageTitle, "UserA", 1000, 1100), // +100
		makeEdit(2002, pageTitle, "UserB", 1100, 1300), // +200
		makeEdit(2003, pageTitle, "UserC", 1300, 1350), // +50
		makeEdit(2004, pageTitle, "UserA", 1350, 1330), // -20 (minor fix)
		makeEdit(2005, pageTitle, "UserB", 1330, 1430), // +100
		makeEdit(2006, pageTitle, "UserC", 1430, 1500), // +70
	}

	for _, edit := range edits {
		err := detector.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}

	// Verify no edit war detected
	editWarKey := fmt.Sprintf("editwar:%s", pageTitle)
	exists, err := redisClient.Exists(ctx, editWarKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists, "Expected no editwar marker for collaborative editing")
}

// TestScenario3_SingleVandal tests vandalism scenario (low severity)
func TestScenario3_SingleVandal(t *testing.T) {
	detector, hotPages, redisClient := setupEditWarTestComponents(t)
	defer redisClient.Close()

	ctx := context.Background()
	pageTitle := "Vandalism_Test_Page"

	promotePageToHot(t, ctx, hotPages, pageTitle)

	// Simulate vandalism/revert pattern with only 2 users
	// but only 1 revert pair, so under the threshold
	edits := []*models.WikipediaEdit{
		makeEdit(3001, pageTitle, "Vandal", 5000, 100),  // -4900 (vandalism)
		makeEdit(3002, pageTitle, "Admin", 100, 5000),    // +4900 (revert)
		makeEdit(3003, pageTitle, "Admin", 5000, 5050),   // +50 (minor cleanup)
		makeEdit(3004, pageTitle, "Admin", 5050, 5100),   // +50
		makeEdit(3005, pageTitle, "Admin", 5100, 5120),   // +20
	}

	for _, edit := range edits {
		err := detector.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}

	// With only 1 revert pair: should be under minReverts=2
	editWarKey := fmt.Sprintf("editwar:%s", pageTitle)
	exists, err := redisClient.Exists(ctx, editWarKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists,
		"Expected no editwar marker for single vandal/revert (only 1 revert pair)")
}

// TestScenario4_ThresholdTesting tests boundary conditions
func TestScenario4_ThresholdTesting(t *testing.T) {
	detector, hotPages, redisClient := setupEditWarTestComponents(t)
	defer redisClient.Close()

	ctx := context.Background()

	t.Run("Below threshold - 4 edits 2 users", func(t *testing.T) {
		pageTitle := "Threshold_Below_Page"

		promotePageToHot(t, ctx, hotPages, pageTitle)

		// 4 edits from 2 users with reverts but below minEdits=5
		edits := []*models.WikipediaEdit{
			makeEdit(4001, pageTitle, "UserX", 1000, 1500), // +500
			makeEdit(4002, pageTitle, "UserY", 1500, 1020), // -480
		}

		for _, edit := range edits {
			err := detector.ProcessEdit(ctx, edit)
			require.NoError(t, err)
		}

		editWarKey := fmt.Sprintf("editwar:%s", pageTitle)
		exists, err := redisClient.Exists(ctx, editWarKey).Result()
		require.NoError(t, err)
		assert.Equal(t, int64(0), exists, "Expected no editwar for below-threshold scenario")
	})

	t.Run("Above threshold - 6 edits 2 users 3 reverts", func(t *testing.T) {
		pageTitle := "Threshold_Above_Page"

		promotePageToHot(t, ctx, hotPages, pageTitle)

		// 6+ edits, 2 users, alternating reverts
		edits := []*models.WikipediaEdit{
			makeEdit(5001, pageTitle, "UserX", 1000, 1500), // +500
			makeEdit(5002, pageTitle, "UserY", 1500, 1020), // -480
			makeEdit(5003, pageTitle, "UserX", 1020, 1510), // +490
			makeEdit(5004, pageTitle, "UserY", 1510, 1030), // -480
			makeEdit(5005, pageTitle, "UserX", 1030, 1520), // +490
			makeEdit(5006, pageTitle, "UserY", 1520, 1040), // -480
		}

		for _, edit := range edits {
			err := detector.ProcessEdit(ctx, edit)
			require.NoError(t, err)
		}

		editWarKey := fmt.Sprintf("editwar:%s", pageTitle)
		exists, err := redisClient.Exists(ctx, editWarKey).Result()
		require.NoError(t, err)
		assert.Equal(t, int64(1), exists, "Expected editwar marker for above-threshold scenario")
	})
}

// TestCountReverts tests the revert detection heuristic
func TestCountReverts(t *testing.T) {
	detector, _, redisClient := setupEditWarTestComponents(t)
	defer redisClient.Close()

	ctx := context.Background()

	t.Run("Alternating reverts", func(t *testing.T) {
		page := "revert_test_1"
		changesKey := fmt.Sprintf("editwar:changes:%s", page)

		// Push alternating changes: +500, -480, +490, -495, +500
		changes := []int{500, -480, 490, -495, 500}
		for _, c := range changes {
			redisClient.RPush(ctx, changesKey, c)
		}

		count, err := detector.countReverts(ctx, page)
		require.NoError(t, err)
		assert.Equal(t, 4, count, "Expected 4 reverts in alternating pattern")
	})

	t.Run("No reverts - all positive", func(t *testing.T) {
		page := "revert_test_2"
		changesKey := fmt.Sprintf("editwar:changes:%s", page)

		changes := []int{100, 200, 50, 150, 300}
		for _, c := range changes {
			redisClient.RPush(ctx, changesKey, c)
		}

		count, err := detector.countReverts(ctx, page)
		require.NoError(t, err)
		assert.Equal(t, 0, count, "Expected 0 reverts for all-positive changes")
	})

	t.Run("Opposite signs but different magnitudes", func(t *testing.T) {
		page := "revert_test_3"
		changesKey := fmt.Sprintf("editwar:changes:%s", page)

		// +500, -100 (not similar magnitude)
		changes := []int{500, -100, 300, -50}
		for _, c := range changes {
			redisClient.RPush(ctx, changesKey, c)
		}

		count, err := detector.countReverts(ctx, page)
		require.NoError(t, err)
		assert.Equal(t, 0, count, "Expected 0 reverts for dissimilar magnitudes")
	})
}

// TestCalculateEditWarSeverity tests severity calculation
func TestCalculateEditWarSeverity(t *testing.T) {
	detector, _, redisClient := setupEditWarTestComponents(t)
	defer redisClient.Close()

	tests := []struct {
		name        string
		editCount   int
		editorCount int
		revertCount int
		expected    string
	}{
		{"Low severity - 2 editors, 2 reverts", 6, 2, 2, "low"},
		{"Medium severity - 3 editors, 4 reverts", 10, 3, 4, "medium"},
		{"High severity - 4 editors, 6 reverts", 15, 4, 6, "high"},
		{"Critical severity - 6 editors, 8 reverts", 30, 6, 8, "critical"},
		{"Critical severity - many reverts", 20, 3, 12, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			severity := detector.calculateEditWarSeverity(tt.editCount, tt.editorCount, tt.revertCount)
			assert.Equal(t, tt.expected, severity)
		})
	}
}

// TestGetActiveEditWars tests retrieval of active edit wars
func TestGetActiveEditWars(t *testing.T) {
	detector, hotPages, redisClient := setupEditWarTestComponents(t)
	defer redisClient.Close()

	ctx := context.Background()

	// Initially should be empty
	wars, err := detector.GetActiveEditWars(ctx)
	require.NoError(t, err)
	assert.Empty(t, wars, "Expected no active edit wars initially")

	// Create an edit war
	pageTitle := "Active_War_Page"
	promotePageToHot(t, ctx, hotPages, pageTitle)

	edits := []*models.WikipediaEdit{
		makeEdit(6001, pageTitle, "UserA", 1000, 1500),
		makeEdit(6002, pageTitle, "UserB", 1500, 1020),
		makeEdit(6003, pageTitle, "UserA", 1020, 1510),
		makeEdit(6004, pageTitle, "UserB", 1510, 1030),
		makeEdit(6005, pageTitle, "UserA", 1030, 1520),
		makeEdit(6006, pageTitle, "UserB", 1520, 1040),
	}

	for _, edit := range edits {
		err := detector.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}

	// Check active edit wars
	wars, err = detector.GetActiveEditWars(ctx)
	require.NoError(t, err)
	assert.True(t, len(wars) > 0, "Expected at least one active edit war")

	if len(wars) > 0 {
		found := false
		for _, war := range wars {
			if war.PageTitle == pageTitle {
				found = true
				assert.Equal(t, 2, war.EditorCount)
				assert.True(t, war.RevertCount >= 2)
				break
			}
		}
		assert.True(t, found, "Expected to find the test page in active wars")
	}
}

// TestEditWarIntegration tests the full edit war detection pipeline
func TestEditWarIntegration(t *testing.T) {
	detector, hotPages, redisClient := setupEditWarTestComponents(t)
	defer redisClient.Close()

	ctx := context.Background()
	pageTitle := "Integration_Test_Page"

	// Promote page
	promotePageToHot(t, ctx, hotPages, pageTitle)

	// Process edits simulating an edit war
	edits := []*models.WikipediaEdit{
		makeEdit(7001, pageTitle, "Editor1", 2000, 2500),
		makeEdit(7002, pageTitle, "Editor2", 2500, 2020),
		makeEdit(7003, pageTitle, "Editor1", 2020, 2490),
		makeEdit(7004, pageTitle, "Editor2", 2490, 2010),
		makeEdit(7005, pageTitle, "Editor1", 2010, 2510),
		makeEdit(7006, pageTitle, "Editor2", 2510, 2020),
		makeEdit(7007, pageTitle, "Editor3", 2020, 2505),
	}

	for _, edit := range edits {
		err := detector.ProcessEdit(ctx, edit)
		require.NoError(t, err)
	}

	// Verify: alert published to stream
	alerts, err := detector.GetRecentAlerts(ctx, time.Now().Add(-time.Minute), 10)
	require.NoError(t, err)
	assert.True(t, len(alerts) > 0, "Expected alert in stream")

	// Verify: page marked with editwar key
	editWarKey := fmt.Sprintf("editwar:%s", pageTitle)
	exists, err := redisClient.Exists(ctx, editWarKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), exists, "Expected editwar marker")

	// Verify: editwar wiki key set for indexing strategy
	editWarWikiKey := fmt.Sprintf("editwar:enwiki:%s", pageTitle)
	wikiExists, err := redisClient.Exists(ctx, editWarWikiKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), wikiExists, "Expected editwar wiki marker for indexing strategy")

	// Verify: editor tracking hash exists
	editorsKey := fmt.Sprintf("editwar:editors:%s", pageTitle)
	editorCount, err := redisClient.HLen(ctx, editorsKey).Result()
	require.NoError(t, err)
	assert.True(t, editorCount >= 2, "Expected at least 2 editors tracked")

	// Verify: byte changes list exists
	changesKey := fmt.Sprintf("editwar:changes:%s", pageTitle)
	changeCount, err := redisClient.LLen(ctx, changesKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(len(edits)), changeCount, "Expected %d byte changes stored", len(edits))
}

// TestProcessEditNonHotPage tests that non-hot pages are skipped
func TestProcessEditNonHotPage(t *testing.T) {
	detector, _, redisClient := setupEditWarTestComponents(t)
	defer redisClient.Close()

	ctx := context.Background()

	edit := makeEdit(9001, "NonHot_Page", "SomeUser", 1000, 1500)
	err := detector.ProcessEdit(ctx, edit)
	require.NoError(t, err)

	// No editor tracking should exist
	editorsKey := fmt.Sprintf("editwar:editors:%s", "NonHot_Page")
	exists, err := redisClient.Exists(ctx, editorsKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists, "Expected no tracking for non-hot page")
}
