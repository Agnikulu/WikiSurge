package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agnikulu/WikiSurge/internal/models"
)

func setupTestAlerts(t *testing.T) (*RedisAlerts, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })

	return NewRedisAlerts(client), mr, client
}

// ---------------------------------------------------------------------------
// Publish tests
// ---------------------------------------------------------------------------

func TestPublishSpikeAlert(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	err := ra.PublishSpikeAlert(ctx, "enwiki", "Go_(programming_language)", "https://en.wikipedia.org", 5.5, 42)
	require.NoError(t, err)

	msgs, err := rc.XRevRangeN(ctx, "alerts:spikes", "+", "-", 10).Result()
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	assert.Equal(t, "spike", msgs[0].Values["type"])

	var alert Alert
	require.NoError(t, json.Unmarshal([]byte(msgs[0].Values["alert_data"].(string)), &alert))
	assert.Equal(t, "spike", alert.Type)
	assert.Equal(t, "Go_(programming_language)", alert.Data["title"])
	assert.Equal(t, float64(5.5), alert.Data["spike_ratio"])
	assert.Equal(t, float64(42), alert.Data["edit_count"])
}

func TestPublishEditWarAlert(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	err := ra.PublishEditWarAlert(ctx, "enwiki", "Climate_change", "https://en.wikipedia.org", []string{"Alice", "Bob"}, 300)
	require.NoError(t, err)

	msgs, err := rc.XRevRangeN(ctx, "alerts:editwars", "+", "-", 10).Result()
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	var alert Alert
	require.NoError(t, json.Unmarshal([]byte(msgs[0].Values["alert_data"].(string)), &alert))
	assert.Equal(t, "edit_war", alert.Type)
	assert.Equal(t, "Climate_change", alert.Data["title"])
	assert.Equal(t, float64(300), alert.Data["change_volume"])
	assert.Equal(t, float64(2), alert.Data["num_editors"])
}

func TestPublishTrendingAlert(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	err := ra.PublishTrendingAlert(ctx, "enwiki", "Bitcoin", "https://en.wikipedia.org", 1, 98.5)
	require.NoError(t, err)

	msgs, err := rc.XRevRangeN(ctx, "alerts:trending", "+", "-", 10).Result()
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	var alert Alert
	require.NoError(t, json.Unmarshal([]byte(msgs[0].Values["alert_data"].(string)), &alert))
	assert.Equal(t, "trending", alert.Type)
	assert.Equal(t, float64(1), alert.Data["rank"])
	assert.Equal(t, float64(98.5), alert.Data["score"])
}

func TestPublishVandalismAlert(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	edit := &models.WikipediaEdit{
		ID:    12345,
		Wiki:  "enwiki",
		Title: "Test_Page",
		User:  "Vandal123",
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 1000, New: 100},
		Comment: "blanked the page",
	}

	err := ra.PublishVandalismAlert(ctx, edit, 0.95, []string{"page blanking", "suspicious user"})
	require.NoError(t, err)

	msgs, err := rc.XRevRangeN(ctx, "alerts:vandalism", "+", "-", 10).Result()
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	var alert Alert
	require.NoError(t, json.Unmarshal([]byte(msgs[0].Values["alert_data"].(string)), &alert))
	assert.Equal(t, "vandalism", alert.Type)
	assert.Equal(t, float64(0.95), alert.Data["confidence"])
	assert.Equal(t, float64(-900), alert.Data["byte_change"])
}

// ---------------------------------------------------------------------------
// GetRecentAlerts
// ---------------------------------------------------------------------------

func TestGetRecentAlerts_Empty(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)
	ctx := context.Background()

	alerts, err := ra.GetRecentAlerts(ctx, "spikes", 10)
	require.NoError(t, err)
	assert.Empty(t, alerts)
}

func TestGetRecentAlerts_WithData(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)
	ctx := context.Background()

	// Publish several alerts
	for i := 0; i < 5; i++ {
		err := ra.PublishSpikeAlert(ctx, "enwiki", fmt.Sprintf("Page_%d", i), "https://en.wikipedia.org", float64(i+2), (i+1)*10)
		require.NoError(t, err)
	}

	alerts, err := ra.GetRecentAlerts(ctx, "spikes", 3)
	require.NoError(t, err)
	assert.Len(t, alerts, 3)

	// Should be in reverse order (most recent first)
	assert.Equal(t, "Page_4", alerts[0].Data["title"])
}

func TestGetRecentAlerts_ParseBothFormats(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	// Seed with "alert_data" format (used by publishAlert)
	ra.PublishSpikeAlert(ctx, "enwiki", "FormatA", "https://en.wikipedia.org", 3.0, 10)

	// Seed with "data" format (used by processor/edit_war_detector)
	warData, _ := json.Marshal(map[string]interface{}{
		"page_title":   "FormatB",
		"revert_count": 5,
		"type":         "edit_war",
	})
	rc.XAdd(ctx, &redis.XAddArgs{
		Stream: "alerts:editwars",
		Values: map[string]interface{}{"data": string(warData)},
	})

	// Both formats should parse
	spikes, err := ra.GetRecentAlerts(ctx, "spikes", 10)
	require.NoError(t, err)
	assert.Len(t, spikes, 1)

	wars, err := ra.GetRecentAlerts(ctx, "editwars", 10)
	require.NoError(t, err)
	assert.Len(t, wars, 1)
	assert.Equal(t, "edit_war", wars[0].Type)
}

// ---------------------------------------------------------------------------
// GetAlertsSince
// ---------------------------------------------------------------------------

func TestGetAlertsSince_FiltersByTime(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)
	ctx := context.Background()

	// Publish alerts
	for i := 0; i < 5; i++ {
		ra.PublishSpikeAlert(ctx, "enwiki", fmt.Sprintf("Page_%d", i), "", float64(i+2), 10)
	}

	// Since the beginning of time — should get all
	alerts, err := ra.GetAlertsSince(ctx, "spikes", time.Unix(0, 0), "", 100)
	require.NoError(t, err)
	assert.Len(t, alerts, 5)
}

func TestGetAlertsSince_SeverityFilter(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)
	ctx := context.Background()

	// Low spike ratio => low severity
	ra.PublishSpikeAlert(ctx, "enwiki", "LowSpike", "", 1.5, 5)
	// High spike ratio => high severity
	ra.PublishSpikeAlert(ctx, "enwiki", "HighSpike", "", 7.0, 50)
	// Critical spike ratio
	ra.PublishSpikeAlert(ctx, "enwiki", "CriticalSpike", "", 15.0, 200)

	// Filter for critical only
	alerts, err := ra.GetAlertsSince(ctx, "spikes", time.Unix(0, 0), "critical", 100)
	require.NoError(t, err)
	assert.Len(t, alerts, 1)
	assert.Equal(t, "CriticalSpike", alerts[0].Data["title"])
}

// ---------------------------------------------------------------------------
// GetEditWarAlertsSince
// ---------------------------------------------------------------------------

func TestGetEditWarAlertsSince_DataFormat(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	warData, _ := json.Marshal(map[string]interface{}{
		"page_title":   "Disputed_Article",
		"editor_count": 4,
		"revert_count": 7,
	})
	rc.XAdd(ctx, &redis.XAddArgs{
		Stream: "alerts:editwars",
		Values: map[string]interface{}{"data": string(warData)},
	})

	wars, err := ra.GetEditWarAlertsSince(ctx, time.Unix(0, 0), 10)
	require.NoError(t, err)
	require.Len(t, wars, 1)
	assert.Equal(t, "Disputed_Article", wars[0]["page_title"])
	assert.Equal(t, false, wars[0]["active"])
	// Should have derived last_edit from stream ID
	assert.NotEmpty(t, wars[0]["last_edit"])
}

func TestGetEditWarAlertsSince_AlertDataFormat(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)
	ctx := context.Background()

	// Use publishAlert path (stores under "alert_data")
	ra.PublishEditWarAlert(ctx, "enwiki", "War_Page", "https://en.wikipedia.org", []string{"A", "B"}, 100)

	wars, err := ra.GetEditWarAlertsSince(ctx, time.Unix(0, 0), 10)
	require.NoError(t, err)
	require.Len(t, wars, 1)
	assert.Equal(t, false, wars[0]["active"])
}

func TestGetEditWarAlertsSince_ExistingLastEdit(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	warData, _ := json.Marshal(map[string]interface{}{
		"page_title": "Article",
		"last_edit":  "2026-01-01T00:00:00Z",
	})
	rc.XAdd(ctx, &redis.XAddArgs{
		Stream: "alerts:editwars",
		Values: map[string]interface{}{"data": string(warData)},
	})

	wars, err := ra.GetEditWarAlertsSince(ctx, time.Unix(0, 0), 10)
	require.NoError(t, err)
	require.Len(t, wars, 1)
	// Should preserve the existing last_edit, not overwrite it
	assert.Equal(t, "2026-01-01T00:00:00Z", wars[0]["last_edit"])
}

// ---------------------------------------------------------------------------
// GetActiveEditWars
// ---------------------------------------------------------------------------

func TestGetActiveEditWars_Empty(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)
	ctx := context.Background()

	wars, err := ra.GetActiveEditWars(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, wars)
}

func TestGetActiveEditWars_WithMarkerAndEditors(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	// Set up edit war marker
	rc.Set(ctx, "editwar:TestPage", "1", 12*time.Hour)

	// Set up editors
	rc.HSet(ctx, "editwar:editors:TestPage", "Alice", "5")
	rc.HSet(ctx, "editwar:editors:TestPage", "Bob", "3")

	// Set up changes for revert counting
	rc.RPush(ctx, "editwar:changes:TestPage", "100", "-95", "200", "-190")

	wars, err := ra.GetActiveEditWars(ctx, 10)
	require.NoError(t, err)
	require.Len(t, wars, 1)

	war := wars[0]
	assert.Equal(t, "TestPage", war["page_title"])
	assert.Equal(t, true, war["active"])
	assert.Equal(t, 8, war["edit_count"])  // 5 + 3
	assert.Contains(t, war["editors"], "Alice")
	assert.Contains(t, war["editors"], "Bob")
}

func TestGetActiveEditWars_SkipsSubKeys(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	// Create marker + sub-keys
	rc.Set(ctx, "editwar:MyPage", "1", 12*time.Hour)
	rc.HSet(ctx, "editwar:editors:MyPage", "User1", "3")
	rc.HSet(ctx, "editwar:editors:MyPage", "User2", "2")
	rc.Set(ctx, "editwar:serverurl:MyPage", "https://en.wikipedia.org", 12*time.Hour)

	// These should NOT appear as active wars
	wars, err := ra.GetActiveEditWars(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, wars, 1)
	assert.Equal(t, "MyPage", wars[0]["page_title"])
}

func TestGetActiveEditWars_SkipsNoEditors(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	// Marker but no editor data (expired)
	rc.Set(ctx, "editwar:StalePage", "1", 12*time.Hour)

	wars, err := ra.GetActiveEditWars(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, wars) // Should be skipped
}

func TestGetActiveEditWars_RespectsLimit(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		page := fmt.Sprintf("Page%d", i)
		rc.Set(ctx, fmt.Sprintf("editwar:%s", page), "1", 12*time.Hour)
		rc.HSet(ctx, fmt.Sprintf("editwar:editors:%s", page), "UserA", "3", "UserB", "2")
	}

	wars, err := ra.GetActiveEditWars(ctx, 2)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(wars), 2)
}

func TestGetActiveEditWars_ServerURL(t *testing.T) {
	ra, _, rc := setupTestAlerts(t)
	ctx := context.Background()

	rc.Set(ctx, "editwar:URLPage", "1", 12*time.Hour)
	rc.HSet(ctx, "editwar:editors:URLPage", "Ed1", "2", "Ed2", "3")
	rc.Set(ctx, "editwar:serverurl:URLPage", "https://de.wikipedia.org", 12*time.Hour)

	wars, err := ra.GetActiveEditWars(ctx, 10)
	require.NoError(t, err)
	require.Len(t, wars, 1)
	assert.Equal(t, "https://de.wikipedia.org", wars[0]["server_url"])
}

// ---------------------------------------------------------------------------
// GetAlertStats
// ---------------------------------------------------------------------------

func TestGetAlertStats_NonExistentStream(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)
	ctx := context.Background()

	// miniredis returns "ERR no such key" rather than redis.Nil for XInfoStream
	// on a non-existent stream, which means GetAlertStats returns an error.
	// This test verifies the function handles the call without panicking.
	stats, err := ra.GetAlertStats(ctx, []string{"nonexistent"})
	if err != nil {
		// Expected for miniredis: stream doesn't exist
		assert.Contains(t, err.Error(), "no such key")
	} else {
		assert.Equal(t, int64(0), stats["nonexistent"].Length)
	}
}

func TestGetAlertStats_WithData(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)
	ctx := context.Background()

	ra.PublishSpikeAlert(ctx, "enwiki", "P1", "", 3.0, 10)
	ra.PublishSpikeAlert(ctx, "enwiki", "P2", "", 4.0, 20)

	stats, err := ra.GetAlertStats(ctx, []string{"spikes"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats["spikes"].Length)
}

// ---------------------------------------------------------------------------
// CleanupOldAlerts
// ---------------------------------------------------------------------------

func TestCleanupOldAlerts(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		ra.PublishSpikeAlert(ctx, "enwiki", fmt.Sprintf("P%d", i), "", 3.0, 10)
	}

	// Cleanup should not error
	err := ra.CleanupOldAlerts(ctx, []string{"spikes"}, 24)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// DeriveSeverity
// ---------------------------------------------------------------------------

func TestDeriveSeverity_ExplicitSeverity(t *testing.T) {
	alert := Alert{Type: AlertTypeSpike, Data: map[string]interface{}{"severity": "critical"}}
	assert.Equal(t, "critical", DeriveSeverity(alert))
}

func TestDeriveSeverity_Spike(t *testing.T) {
	tests := []struct {
		ratio    float64
		expected string
	}{
		{1.0, "low"},
		{2.5, "medium"},
		{5.0, "high"},
		{10.0, "critical"},
		{50.0, "critical"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("ratio_%.1f", tt.ratio), func(t *testing.T) {
			alert := Alert{Type: AlertTypeSpike, Data: map[string]interface{}{"spike_ratio": tt.ratio}}
			assert.Equal(t, tt.expected, DeriveSeverity(alert))
		})
	}
}

func TestDeriveSeverity_EditWar(t *testing.T) {
	tests := []struct {
		editors  float64
		expected string
	}{
		{2, "medium"},
		{4, "high"},
		{6, "critical"},
		{10, "critical"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("editors_%.0f", tt.editors), func(t *testing.T) {
			alert := Alert{Type: AlertTypeEditWar, Data: map[string]interface{}{"editor_count": tt.editors}}
			assert.Equal(t, tt.expected, DeriveSeverity(alert))
		})
	}
}

func TestDeriveSeverity_Vandalism(t *testing.T) {
	tests := []struct {
		confidence float64
		expected   string
	}{
		{0.3, "low"},
		{0.5, "medium"},
		{0.7, "high"},
		{0.9, "critical"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("conf_%.1f", tt.confidence), func(t *testing.T) {
			alert := Alert{Type: AlertTypeVandalism, Data: map[string]interface{}{"confidence": tt.confidence}}
			assert.Equal(t, tt.expected, DeriveSeverity(alert))
		})
	}
}

func TestDeriveSeverity_UnknownType(t *testing.T) {
	alert := Alert{Type: "unknown", Data: map[string]interface{}{}}
	assert.Equal(t, "low", DeriveSeverity(alert))
}

// ---------------------------------------------------------------------------
// countRevertPatterns
// ---------------------------------------------------------------------------

func TestCountRevertPatterns(t *testing.T) {
	tests := []struct {
		name    string
		changes []string
		want    int
	}{
		{"empty", nil, 0},
		{"single", []string{"100"}, 0},
		{"no reverts", []string{"100", "200", "50"}, 0},
		{"opposite signs similar magnitude", []string{"100", "-95"}, 1},
		{"both zero", []string{"0", "0"}, 1},
		{"small edits opposite", []string{"5", "-3"}, 1},
		{"dissimilar magnitudes", []string{"1000", "-10"}, 0},
		{"multiple reverts", []string{"100", "-100", "200", "-200"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, countRevertPatterns(tt.changes))
		})
	}
}

// ---------------------------------------------------------------------------
// classifyEditWarSeverity
// ---------------------------------------------------------------------------

func TestClassifyEditWarSeverity(t *testing.T) {
	tests := []struct {
		editors, edits, reverts int
		want                    string
	}{
		{2, 5, 2, "low"},
		{3, 10, 4, "medium"},
		{4, 15, 3, "high"},
		{6, 20, 12, "critical"},
		{2, 5, 11, "critical"}, // reverts alone trigger critical
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("e%d_ed%d_r%d", tt.editors, tt.edits, tt.reverts), func(t *testing.T) {
			assert.Equal(t, tt.want, classifyEditWarSeverity(tt.editors, tt.edits, tt.reverts))
		})
	}
}

// ---------------------------------------------------------------------------
// parseAlertMessage
// ---------------------------------------------------------------------------

func TestParseAlertMessage_MissingData(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)

	msg := redis.XMessage{
		ID:     "123-0",
		Values: map[string]interface{}{"foo": "bar"},
	}
	_, err := ra.parseAlertMessage(msg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "alert data field missing")
}

func TestParseAlertMessage_InvalidJSON(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)

	msg := redis.XMessage{
		ID:     "123-0",
		Values: map[string]interface{}{"alert_data": "not json{"},
	}
	_, err := ra.parseAlertMessage(msg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal")
}

func TestParseAlertMessage_NestedData(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)

	// When publishAlert marshals a full Alert struct, the data ends up nested
	nested, _ := json.Marshal(Alert{
		Type:      "spike",
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"title": "Nested_Page", "spike_ratio": 5.0},
	})
	msg := redis.XMessage{
		ID:     "123-0",
		Values: map[string]interface{}{"alert_data": string(nested)},
	}
	alert, err := ra.parseAlertMessage(msg)
	require.NoError(t, err)
	assert.Equal(t, "spike", alert.Type)
	assert.Equal(t, "Nested_Page", alert.Data["title"])
}

func TestParseAlertMessage_TypeInference(t *testing.T) {
	ra, _, _ := setupTestAlerts(t)

	// spike_ratio present => spike
	spikeData, _ := json.Marshal(map[string]interface{}{"spike_ratio": 3.0, "title": "S"})
	msg := redis.XMessage{ID: "1-0", Values: map[string]interface{}{"data": string(spikeData)}}
	alert, err := ra.parseAlertMessage(msg)
	require.NoError(t, err)
	assert.Equal(t, "spike", alert.Type)

	// revert_count present => edit_war
	warData, _ := json.Marshal(map[string]interface{}{"revert_count": 5, "title": "W"})
	msg2 := redis.XMessage{ID: "2-0", Values: map[string]interface{}{"data": string(warData)}}
	alert2, err := ra.parseAlertMessage(msg2)
	require.NoError(t, err)
	assert.Equal(t, "edit_war", alert2.Type)
}
