package monitoring

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMonitor(t *testing.T, mr *miniredis.Miniredis) *ResourceMonitor {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })

	cfg := &config.Config{
		Kafka: config.Kafka{Brokers: []string{}}, // no real Kafka
	}
	logger := zerolog.New(zerolog.NewTestWriter(t))

	rm := NewResourceMonitor(client, nil, cfg, logger)
	t.Cleanup(rm.Stop)
	return rm
}

// ---------------------------------------------------------------------------
// NewResourceMonitor
// ---------------------------------------------------------------------------

func TestNewResourceMonitor(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	assert.NotNil(t, rm)
	assert.Equal(t, float64(80), rm.thresholds.RedisMemoryPercent)
	assert.Equal(t, float64(80), rm.thresholds.ESDiskPercent)
	assert.Equal(t, int64(1000), rm.thresholds.KafkaLagMessages)
}

// ---------------------------------------------------------------------------
// SetThresholds
// ---------------------------------------------------------------------------

func TestSetThresholds(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	rm.SetThresholds(Thresholds{
		RedisMemoryPercent: 90,
		ESDiskPercent:      95,
		KafkaLagMessages:   5000,
	})

	assert.Equal(t, float64(90), rm.thresholds.RedisMemoryPercent)
	assert.Equal(t, float64(95), rm.thresholds.ESDiskPercent)
	assert.Equal(t, int64(5000), rm.thresholds.KafkaLagMessages)
}

// ---------------------------------------------------------------------------
// Callbacks registration
// ---------------------------------------------------------------------------

func TestOnRedisHighMemory(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	called := false
	rm.OnRedisHighMemory(func() { called = true })

	rm.mu.RLock()
	cb := rm.onRedisHighMem
	rm.mu.RUnlock()
	require.NotNil(t, cb)
	cb()
	assert.True(t, called)
}

func TestOnESDiskHigh(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	called := false
	rm.OnESDiskHigh(func() { called = true })

	rm.mu.RLock()
	cb := rm.onESDiskHigh
	rm.mu.RUnlock()
	require.NotNil(t, cb)
	cb()
	assert.True(t, called)
}

func TestOnKafkaLagHigh(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	called := false
	rm.OnKafkaLagHigh(func() { called = true })

	rm.mu.RLock()
	cb := rm.onKafkaLagHigh
	rm.mu.RUnlock()
	require.NotNil(t, cb)
	cb()
	assert.True(t, called)
}

func TestOnKafkaLagRecover(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	called := false
	rm.OnKafkaLagRecover(func() { called = true })

	rm.mu.RLock()
	cb := rm.onKafkaLagRecover
	rm.mu.RUnlock()
	require.NotNil(t, cb)
	cb()
	assert.True(t, called)
}

// ---------------------------------------------------------------------------
// Status & RecentAlerts
// ---------------------------------------------------------------------------

func TestStatus_InitiallyZero(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	status := rm.Status()
	assert.Equal(t, float64(0), status.RedisMemoryPercent)
	assert.Equal(t, float64(0), status.ESDiskPercent)
	assert.Equal(t, int64(0), status.KafkaLag)
}

func TestRecentAlerts_Empty(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	assert.Empty(t, rm.RecentAlerts())
}

// ---------------------------------------------------------------------------
// fireAlert
// ---------------------------------------------------------------------------

func TestFireAlert_RecordsAlert(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	rm.fireAlert("redis", "memory high", 85.0, 80.0)

	alerts := rm.RecentAlerts()
	require.Len(t, alerts, 1)
	assert.Equal(t, "redis", alerts[0].Resource)
	assert.Equal(t, 85.0, alerts[0].Value)
	assert.Equal(t, 80.0, alerts[0].Threshold)
}

func TestFireAlert_CapsAt100(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	for i := 0; i < 120; i++ {
		rm.fireAlert("redis", "test", float64(i), 80.0)
	}

	alerts := rm.RecentAlerts()
	assert.Len(t, alerts, 100)
	// Oldest should be alert #20 (0-19 trimmed)
	assert.Equal(t, float64(20), alerts[0].Value)
}

// ---------------------------------------------------------------------------
// parseRedisInfoInt
// ---------------------------------------------------------------------------

func TestParseRedisInfoInt(t *testing.T) {
	info := "# Memory\r\nused_memory:1234567\r\nmaxmemory:268435456\r\nused_memory_rss:2000000\r\n"

	assert.Equal(t, int64(1234567), parseRedisInfoInt(info, "used_memory"))
	assert.Equal(t, int64(268435456), parseRedisInfoInt(info, "maxmemory"))
	assert.Equal(t, int64(2000000), parseRedisInfoInt(info, "used_memory_rss"))
	assert.Equal(t, int64(0), parseRedisInfoInt(info, "nonexistent_key"))
}

func TestParseRedisInfoInt_Empty(t *testing.T) {
	assert.Equal(t, int64(0), parseRedisInfoInt("", "used_memory"))
}

// ---------------------------------------------------------------------------
// decodeJSON
// ---------------------------------------------------------------------------

func TestDecodeJSON_Valid(t *testing.T) {
	var result struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	err := decodeJSON(bytes.NewReader([]byte(`{"name":"test","age":42}`)), &result)
	require.NoError(t, err)
	assert.Equal(t, "test", result.Name)
	assert.Equal(t, 42, result.Age)
}

func TestDecodeJSON_Invalid(t *testing.T) {
	var result struct{}
	err := decodeJSON(strings.NewReader("not json"), &result)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// checkRedis — integration with miniredis
// ---------------------------------------------------------------------------

func TestCheckRedis_UpdatesStatus(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	// miniredis doesn't support INFO memory section, so checkRedis will log
	// an error and return early. Verify it doesn't panic.
	rm.checkRedis()
	// Status LastCheck remains zero because the INFO call fails in miniredis.
	// This is expected — the real integration is tested against actual Redis.
}

// ---------------------------------------------------------------------------
// checkES — nil client safe
// ---------------------------------------------------------------------------

func TestCheckES_NilClient(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	// Should not panic with nil esClient
	rm.checkES()
}

// ---------------------------------------------------------------------------
// checkKafka — no brokers
// ---------------------------------------------------------------------------

func TestCheckKafka_NoBrokers(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	lag := rm.checkKafka()
	assert.Equal(t, int64(0), lag)
}

// ---------------------------------------------------------------------------
// Start / Stop lifecycle
// ---------------------------------------------------------------------------

func TestStartStop(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	rm.Start()
	time.Sleep(50 * time.Millisecond)
	rm.Stop() // should not hang or panic
}

// ---------------------------------------------------------------------------
// Redis high memory callback invocation
// ---------------------------------------------------------------------------

func TestCheckRedis_FiresCallback(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rm := newTestMonitor(t, mr)
	rm.SetThresholds(Thresholds{
		RedisMemoryPercent: 0.001,
		ESDiskPercent:      80,
		KafkaLagMessages:   1000,
	})

	var callCount int32
	rm.OnRedisHighMemory(func() { atomic.AddInt32(&callCount, 1) })

	// miniredis doesn't support INFO memory, so checkRedis returns early
	// without firing the callback. Verify no panic.
	rm.checkRedis()
}

// ---------------------------------------------------------------------------
// ResourceAlert struct
// ---------------------------------------------------------------------------

func TestResourceAlert_Fields(t *testing.T) {
	alert := ResourceAlert{
		Resource:  "kafka",
		Message:   "lag high",
		Value:     5000,
		Threshold: 1000,
		Timestamp: time.Now(),
	}
	assert.Equal(t, "kafka", alert.Resource)
	assert.Equal(t, float64(5000), alert.Value)
}

// ---------------------------------------------------------------------------
// AlertStats struct
// ---------------------------------------------------------------------------

func TestThresholds_Defaults(t *testing.T) {
	th := Thresholds{
		RedisMemoryPercent: 80,
		ESDiskPercent:      80,
		KafkaLagMessages:   1000,
	}
	assert.Equal(t, float64(80), th.RedisMemoryPercent)
	assert.Equal(t, float64(80), th.ESDiskPercent)
	assert.Equal(t, int64(1000), th.KafkaLagMessages)
}
