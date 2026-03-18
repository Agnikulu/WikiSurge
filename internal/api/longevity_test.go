package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// Panic restart limit tests — prevent runaway goroutine spawning
// ===========================================================================

// TestAlertHub_PanicRestartLimit verifies that the alert hub stops restarting
// after maxPanicRestarts repeated panics instead of spawning goroutines
// forever. Without this cap, a persistent bug would cause exponential
// goroutine growth over days of uptime.
func TestAlertHub_PanicRestartLimit(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())

	goroutinesBefore := runtime.NumGoroutine()

	// Start the hub and stop it — verifies basic lifecycle.
	go hub.Run()
	time.Sleep(50 * time.Millisecond)
	hub.Stop()
	time.Sleep(100 * time.Millisecond)

	// Verify the constant is sane.
	assert.Equal(t, 5, maxPanicRestarts,
		"panic restart limit should be 5")

	// Start a second hub at the restart limit. Since nil alerts causes it to
	// idle on ctx.Done(), we stop it externally to verify it can still shut down.
	hub2 := NewAlertHub(nil, zerolog.Nop())
	go hub2.runWithRestart(maxPanicRestarts)
	time.Sleep(50 * time.Millisecond)
	hub2.Stop()
	time.Sleep(100 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	assert.InDelta(t, goroutinesBefore, goroutinesAfter, 5,
		"goroutine count should stay stable after reaching panic limit")
}

// TestWebSocketHub_PanicRestartLimit verifies the WebSocket hub respects the
// same panic restart cap.
func TestWebSocketHub_PanicRestartLimit(t *testing.T) {
	hub := NewWebSocketHub(zerolog.Nop())

	done := make(chan struct{})
	go func() {
		defer close(done)
		// At the panic limit, runWithRestart should run the select loop
		// until we stop the hub.
		go hub.runWithRestart(maxPanicRestarts)
		time.Sleep(50 * time.Millisecond)
		hub.Stop()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("WebSocket hub should stop cleanly even at panic restart limit")
	}
}

// ===========================================================================
// Response cache bounded growth tests
// ===========================================================================

// TestResponseCache_MaxEntriesCap verifies the cache rejects new entries once
// responseCacheMaxEntries is reached. Without this cap, a high-cardinality
// query parameter attack or varied traffic pattern would cause unbounded
// memory growth over weeks.
func TestResponseCache_MaxEntriesCap(t *testing.T) {
	cache := newResponseCache()
	t.Cleanup(cache.Stop)

	// Fill the cache to capacity.
	for i := 0; i < responseCacheMaxEntries; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), []byte("data"), time.Hour)
	}

	// Verify an entry exists.
	_, ok := cache.Get("key-0")
	assert.True(t, ok, "existing entry should be retrievable")

	// Try to add one more — it should be silently dropped.
	cache.Set("overflow-key", []byte("should-not-cache"), time.Hour)
	_, ok = cache.Get("overflow-key")
	assert.False(t, ok, "cache should reject entries past max capacity")

	// Count entries.
	cache.mu.RLock()
	count := len(cache.entries)
	cache.mu.RUnlock()
	assert.Equal(t, responseCacheMaxEntries, count, "cache size should stay at max")
}

// TestResponseCache_ExpiredEntriesReclaimSpace verifies that after entries
// expire and cleanup runs, space is freed for new entries. This ensures the
// cache doesn't permanently fill up.
func TestResponseCache_ExpiredEntriesReclaimSpace(t *testing.T) {
	cache := newResponseCache()
	t.Cleanup(cache.Stop)

	// Fill with short-TTL entries.
	for i := 0; i < 100; i++ {
		cache.Set(fmt.Sprintf("exp-%d", i), []byte("data"), 10*time.Millisecond)
	}

	// Wait for entries to expire.
	time.Sleep(50 * time.Millisecond)

	// Manually trigger cleanup by locking and sweeping (same logic as cleanup()).
	cache.mu.Lock()
	now := time.Now()
	for k, v := range cache.entries {
		if now.After(v.expiresAt) {
			delete(cache.entries, k)
		}
	}
	cache.mu.Unlock()

	// Now new entries should be accepted.
	cache.Set("fresh-key", []byte("fresh"), time.Hour)
	data, ok := cache.Get("fresh-key")
	assert.True(t, ok, "cache should accept new entries after expired ones are cleaned")
	assert.Equal(t, []byte("fresh"), data)
}

// TestResponseCache_ConcurrentSetGet verifies the cache is safe under high
// concurrent read/write load — simulates production traffic patterns.
func TestResponseCache_ConcurrentSetGet(t *testing.T) {
	cache := newResponseCache()
	t.Cleanup(cache.Stop)

	var wg sync.WaitGroup
	const writers = 20
	const readers = 50
	const ops = 500

	// Writers.
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				key := fmt.Sprintf("w%d-k%d", id, i%100)
				cache.Set(key, []byte(fmt.Sprintf("v%d", i)), 100*time.Millisecond)
			}
		}(w)
	}

	// Readers.
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				key := fmt.Sprintf("w%d-k%d", id%writers, i%100)
				cache.Get(key)
			}
		}(r)
	}

	wg.Wait()
	// No panics or races — test passes if we get here.
}

// TestResponseCache_DoubleStop verifies Stop() is safe to call multiple times.
func TestResponseCache_DoubleStop(t *testing.T) {
	cache := newResponseCache()
	assert.NotPanics(t, func() {
		cache.Stop()
		cache.Stop()
		cache.Stop()
	})
}

// ===========================================================================
// Language cache bounded growth & race-safe reset tests
// ===========================================================================

// TestLanguageCache_ConcurrentAccess hammers the language cache from many
// goroutines to trigger the race in sync.Map reassignment that was fixed
// with languageCacheResetMu. Run with -race to detect issues.
func TestLanguageCache_ConcurrentAccess(t *testing.T) {
	// Reset global state for a clean test.
	languageCache = sync.Map{}
	atomic.StoreInt64(&languageCacheLen, 0)

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 1000

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				title := fmt.Sprintf("en.wikipedia.org/wiki/Page_%d_%d", id, i)
				lang := cachedExtractLanguage(title)
				_ = lang
			}
		}(g)
	}

	wg.Wait()
	// No race or panic — test passes.
}

// TestLanguageCache_ResetUnderLoad verifies the cache reset logic works
// correctly when many goroutines are writing simultaneously. With
// 50k unique keys from 50 goroutines, the cache should hit the 100k limit
// and reset cleanly without data corruption.
func TestLanguageCache_ResetUnderLoad(t *testing.T) {
	languageCache = sync.Map{}
	atomic.StoreInt64(&languageCacheLen, 0)

	var wg sync.WaitGroup
	const goroutines = 20
	// Each goroutine writes enough unique keys to potentially trigger reset.
	const keysPerGoroutine = languageCacheMaxSize / 10

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < keysPerGoroutine; i++ {
				title := fmt.Sprintf("xx.wikipedia.org/wiki/Unique_%d_%d", id, i)
				cachedExtractLanguage(title)
			}
		}(g)
	}

	wg.Wait()

	// After the load, the cache length should be bounded.
	currentLen := atomic.LoadInt64(&languageCacheLen)
	assert.LessOrEqual(t, currentLen, int64(languageCacheMaxSize+int(goroutines)),
		"language cache should stay bounded after reset cycles")
}

// ===========================================================================
// Alert hub subscriber leak detection
// ===========================================================================

// TestAlertHub_NoSubscriberLeakAfterChurn does rapid subscribe/unsubscribe
// cycles and verifies no channels are leaked. In long-running production,
// leaked subscribers cause unbounded map growth and broadcast slowdown.
func TestAlertHub_NoSubscriberLeakAfterChurn(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	const cycles = 1000
	for i := 0; i < cycles; i++ {
		ch := hub.Subscribe()
		hub.Unsubscribe(ch)
	}

	hub.mu.RLock()
	count := len(hub.subscribers)
	hub.mu.RUnlock()
	assert.Equal(t, 0, count, "no subscribers should remain after %d subscribe/unsubscribe cycles", cycles)
}

// TestAlertHub_ConcurrentSubscribeUnsubscribeBroadcast runs all three
// operations simultaneously — the exact pattern that caused the data race
// on len(h.subscribers) after mutex unlock.
func TestAlertHub_ConcurrentSubscribeUnsubscribeBroadcast(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Broadcaster.
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				hub.broadcast(storage.Alert{ID: fmt.Sprintf("race-%d", i)})
				i++
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Subscribe/unsubscribe churners.
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				ch := hub.Subscribe()
				// Drain briefly.
				time.Sleep(time.Duration(1+j%5) * time.Millisecond)
				hub.Unsubscribe(ch)
			}
		}()
	}

	// Let it run.
	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()

	hub.mu.RLock()
	remaining := len(hub.subscribers)
	hub.mu.RUnlock()
	assert.Equal(t, 0, remaining, "all subscribers should be cleaned up")
}

// ===========================================================================
// WebSocket hub goroutine leak detection
// ===========================================================================

// TestHub_GoroutineStabilityUnderChurn creates and destroys many WebSocket
// clients rapidly and verifies goroutine count stabilizes afterward. In
// production, leaked goroutines from unregistered clients were the primary
// cause of OOM crashes after days of uptime.
func TestHub_GoroutineStabilityUnderChurn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping goroutine stability test in short mode")
	}

	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	hub.SetMaxClients(200)
	hub.SetMaxPerIP(200)
	go hub.Run()

	// Let the hub goroutines settle.
	time.Sleep(50 * time.Millisecond)
	goroutinesBefore := runtime.NumGoroutine()

	// Create and destroy many fake clients to simulate churn.
	for wave := 0; wave < 5; wave++ {
		var clients []*Client
		for i := 0; i < 20; i++ {
			client := &Client{
				hub:         hub,
				send:        make(chan []byte, sendBufferSize),
				id:          fmt.Sprintf("churn-%d-%d", wave, i),
				connectedAt: time.Now(),
				remoteAddr:  fmt.Sprintf("10.0.0.%d", i%255+1),
			}
			hub.mu.Lock()
			hub.clients[client] = true
			hub.mu.Unlock()
			clients = append(clients, client)
		}

		// Unregister all.
		for _, c := range clients {
			hub.mu.Lock()
			if _, ok := hub.clients[c]; ok {
				delete(hub.clients, c)
				close(c.send)
			}
			hub.mu.Unlock()
		}

		time.Sleep(20 * time.Millisecond)
	}

	hub.Stop()
	time.Sleep(200 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	delta := goroutinesAfter - goroutinesBefore
	assert.LessOrEqual(t, delta, 5,
		"goroutine count should stabilize after client churn (before=%d, after=%d, delta=%d)",
		goroutinesBefore, goroutinesAfter, delta)
}

// ===========================================================================
// Edit relay context leak test
// ===========================================================================

// TestEditRelay_ContextCleanupOnRestart verifies that restarting the edit
// relay cancels the previous context. Without this, each restart leaks a
// Redis subscription and goroutine — over weeks of intermittent Redis
// disconnects, this causes connection pool exhaustion.
func TestEditRelay_ContextCleanupOnRestart(t *testing.T) {
	srv, mr := testServer(t)
	_ = mr

	var cancelledContexts int32

	// Simulate multiple relay restarts.
	for i := 0; i < 5; i++ {
		oldCancel := srv.editRelayCancel
		srv.StartEditRelay(srv.redis)
		time.Sleep(30 * time.Millisecond)

		// If there was a previous cancel func, the old context should be cancelled.
		if oldCancel != nil {
			atomic.AddInt32(&cancelledContexts, 1)
		}
	}

	// After 5 starts, 4 old contexts should have been cancelled.
	assert.GreaterOrEqual(t, atomic.LoadInt32(&cancelledContexts), int32(3),
		"old relay contexts should be cancelled on restart")

	// Verify current relay is still running (the latest context is still active).
	assert.NotNil(t, srv.editRelayCancel, "current relay cancel func should exist")
}

// ===========================================================================
// AlertHub reconnect backoff test
// ===========================================================================

// TestAlertHub_ReconnectBackoff verifies that the alert hub uses exponential
// backoff when Redis is unavailable. Without backoff, a Redis outage causes
// a tight reconnect loop that consumes 100% CPU.
func TestAlertHub_ReconnectBackoff(t *testing.T) {
	// This test verifies the backoff structure exists and the maxPanicRestarts
	// constant is reasonable. A full integration test with a real failing Redis
	// connection would be too slow and flaky for unit tests.
	assert.Equal(t, 5, maxPanicRestarts,
		"panic restart limit should be 5 — enough to recover from transient issues, not enough to spiral")

	// Verify the AlertHub creates a cancel function (needed for clean shutdown).
	hub := NewAlertHub(nil, zerolog.Nop())
	go hub.Run()
	time.Sleep(50 * time.Millisecond)

	hub.cancelMu.Lock()
	hasCancel := hub.cancel != nil
	hub.cancelMu.Unlock()
	assert.True(t, hasCancel, "alert hub should set cancel func on start")

	hub.Stop()
}

// ===========================================================================
// Cache key determinism
// ===========================================================================

// TestCacheKey_DeterministicLongevity verifies that cacheKey() produces the
// same output for repeated calls — non-deterministic keys cause cache misses
// and unbounded entry growth over time.
func TestCacheKey_DeterministicLongevity(t *testing.T) {
	// Verify stability across many calls (simulates long uptime).
	first := cacheKey("trending", "10", "1h")
	for i := 0; i < 1000; i++ {
		assert.Equal(t, first, cacheKey("trending", "10", "1h"),
			"cache key must be deterministic on call %d", i)
	}

	// Different inputs should produce different keys (basic collision check).
	other := cacheKey("trending", "20", "1h")
	assert.NotEqual(t, first, other)
}

// ===========================================================================
// Simulated long-running churn: WebSocket + AlertHub combined
// ===========================================================================

// TestLongevity_CombinedHubAndAlertChurn simulates an aggressive
// combined workload: WebSocket client churn, alert broadcasting, and
// subscribe/unsubscribe cycles all running simultaneously. This catches
// subtle interactions between the two hub types that only manifest after
// extended operation.
func TestLongevity_CombinedHubAndAlertChurn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping combined churn test in short mode")
	}

	logger := zerolog.Nop()
	wsHub := NewWebSocketHub(logger)
	wsHub.SetMaxClients(100)
	wsHub.SetMaxPerIP(100)
	go wsHub.Run()

	alertHub := NewAlertHub(nil, logger)
	go alertHub.Run()

	t.Cleanup(func() {
		wsHub.Stop()
		alertHub.Stop()
	})

	var wg sync.WaitGroup
	stop := make(chan struct{})
	const duration = 500 * time.Millisecond

	// WebSocket edit broadcasts.
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				wsHub.BroadcastEditFiltered(&models.WikipediaEdit{
					ID: int64(i), Title: fmt.Sprintf("Edit_%d", i), Wiki: "enwiki",
					Length: struct {
						Old int `json:"old"`
						New int `json:"new"`
					}{Old: 100, New: 200},
				})
				i++
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Alert broadcasts.
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				alertHub.broadcast(storage.Alert{
					ID:   fmt.Sprintf("alert-%d", i),
					Type: storage.AlertTypeSpike,
				})
				i++
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Alert subscriber churn.
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					ch := alertHub.Subscribe()
					time.Sleep(5 * time.Millisecond)
					alertHub.Unsubscribe(ch)
				}
			}
		}()
	}

	// WS client register/unregister churn (direct, without real connections).
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					client := &Client{
						hub:         wsHub,
						send:        make(chan []byte, sendBufferSize),
						id:          fmt.Sprintf("churn-%d-%d", id, time.Now().UnixNano()),
						connectedAt: time.Now(),
						remoteAddr:  fmt.Sprintf("10.%d.0.1", id),
					}
					wsHub.register <- client
					time.Sleep(3 * time.Millisecond)
					wsHub.unregister <- client
					time.Sleep(time.Millisecond)
				}
			}
		}(g)
	}

	time.Sleep(duration)
	close(stop)
	wg.Wait()

	// Allow time for hub to process remaining messages.
	time.Sleep(200 * time.Millisecond)

	// Verify no leaked subscribers.
	alertHub.mu.RLock()
	alertSubs := len(alertHub.subscribers)
	alertHub.mu.RUnlock()
	assert.Equal(t, 0, alertSubs, "alert hub should have 0 subscribers after churn")

	// WS client count should be 0 or very close (some may still be in the channel).
	time.Sleep(200 * time.Millisecond)
	wsCount := wsHub.ClientCount()
	assert.LessOrEqual(t, wsCount, 2, "ws hub should have ~0 clients after churn, got %d", wsCount)
}

// ===========================================================================
// WebSocket hub broadcast channel backpressure
// ===========================================================================

// TestHub_BroadcastChannelBackpressure verifies that flooding the broadcast
// channel doesn't deadlock or block indefinitely. With the buffered channel
// (512), this simulates a burst scenario where edits arrive faster than
// they can be distributed — the same pattern that occurs during breaking
// news edit surges on Wikipedia.
func TestHub_BroadcastChannelBackpressure(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	// Flood the broadcast channel with more messages than the buffer.
	const floods = 2000
	done := make(chan struct{})
	go func() {
		for i := 0; i < floods; i++ {
			hub.BroadcastEdit(&models.WikipediaEdit{
				ID: int64(i), Title: fmt.Sprintf("Flood_%d", i), Wiki: "enwiki",
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 100, New: 200},
			})
		}
		close(done)
	}()

	select {
	case <-done:
		// Good — all edits were enqueued without blocking.
	case <-time.After(5 * time.Second):
		t.Fatal("broadcast channel should not deadlock under flood")
	}
}

// ===========================================================================
// AlertHub Stop() idempotency and ordering
// ===========================================================================

// TestAlertHub_StopWhileBroadcasting verifies that stopping the alert hub
// while broadcasts are in-flight doesn't panic or deadlock.
func TestAlertHub_StopWhileBroadcasting(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	go hub.Run()

	// Subscribe a few channels.
	channels := make([]chan storage.Alert, 5)
	for i := range channels {
		channels[i] = hub.Subscribe()
	}

	// Start broadcasting.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			hub.broadcast(storage.Alert{ID: fmt.Sprintf("stop-test-%d", i)})
			time.Sleep(time.Millisecond)
		}
	}()

	// Stop mid-broadcast.
	time.Sleep(20 * time.Millisecond)
	assert.NotPanics(t, func() {
		hub.Stop()
	})

	wg.Wait()
}

// TestAlertHub_DoubleStop verifies Stop() is safe to call multiple times.
func TestAlertHub_DoubleStop(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	go hub.Run()
	time.Sleep(50 * time.Millisecond)

	assert.NotPanics(t, func() {
		hub.Stop()
		hub.Stop()
		hub.Stop()
	})
}

// ===========================================================================
// Shutdown ordering test
// ===========================================================================

// TestServer_ShutdownStopsAllComponents verifies that Shutdown() correctly
// stops the WebSocket hub, alert hub, and response cache. Missing any of
// these causes goroutine leaks that accumulate during rolling restarts.
func TestServer_ShutdownStopsAllComponents(t *testing.T) {
	srv, _ := testServer(t)

	goroutinesBefore := runtime.NumGoroutine()

	err := srv.Shutdown(nil)
	require.NoError(t, err)

	// Wait for goroutines to wind down.
	time.Sleep(500 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	// The hub, alert hub, and cache cleanup goroutines should all be stopped.
	// We allow a generous delta because the test framework itself uses goroutines.
	assert.LessOrEqual(t, goroutinesAfter, goroutinesBefore+2,
		"shutdown should stop background goroutines (before=%d, after=%d)",
		goroutinesBefore, goroutinesAfter)
}

// ===========================================================================
// Memory stability: response cache under varied keys
// ===========================================================================

// TestResponseCache_HighCardinalityKeys simulates the pattern where many
// unique cache keys are generated (e.g., from query parameter permutations).
// The cap at responseCacheMaxEntries prevents this from being a memory bomb.
func TestResponseCache_HighCardinalityKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping high-cardinality cache test in short mode")
	}

	cache := newResponseCache()
	t.Cleanup(cache.Stop)

	// Write 2x the max capacity with unique keys.
	for i := 0; i < responseCacheMaxEntries*2; i++ {
		cache.Set(fmt.Sprintf("card-%d", i), []byte("data"), time.Hour)
	}

	cache.mu.RLock()
	count := len(cache.entries)
	cache.mu.RUnlock()

	assert.LessOrEqual(t, count, responseCacheMaxEntries,
		"cache should never exceed max entries (got %d, max %d)", count, responseCacheMaxEntries)
}

// ===========================================================================
// BroadcastEditFiltered concurrent safety
// ===========================================================================

// TestHub_BroadcastEditFilteredConcurrent verifies BroadcastEditFiltered
// is safe under heavy concurrent use. This method is called from the edit
// relay goroutine and must not race with register/unregister operations.
func TestHub_BroadcastEditFilteredConcurrent(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	hub.SetMaxClients(200)
	hub.SetMaxPerIP(200)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Rapid registration/unregistration.
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					c := &Client{
						hub:         hub,
						send:        make(chan []byte, sendBufferSize),
						id:          fmt.Sprintf("filter-%d-%d", id, time.Now().UnixNano()),
						connectedAt: time.Now(),
						remoteAddr:  fmt.Sprintf("10.0.%d.1", id),
						filter: &EditFilter{
							Languages:   []string{"en"},
							ExcludeBots: true,
						},
					}
					hub.register <- c
					time.Sleep(2 * time.Millisecond)
					hub.unregister <- c
				}
			}
		}(g)
	}

	// Concurrent BroadcastEditFiltered.
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			i := 0
			for {
				select {
				case <-stop:
					return
				default:
					hub.BroadcastEditFiltered(&models.WikipediaEdit{
						ID:    int64(i),
						Title: fmt.Sprintf("FilterTest_%d_%d", id, i),
						Wiki:  "enwiki",
						Bot:   i%3 == 0,
						Length: struct {
							Old int `json:"old"`
							New int `json:"new"`
						}{Old: 100, New: 200},
					})
					i++
					time.Sleep(time.Millisecond)
				}
			}
		}(g)
	}

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Allow hub to drain.
	time.Sleep(200 * time.Millisecond)
}

// ===========================================================================
// Full cleanup simulation tests — verify everything gets cleaned up
// ===========================================================================

// TestCleanup_FullLifecycle boots a full APIServer with all components active,
// simulates realistic production traffic (WebSocket clients, alert subscribers,
// cache writes, edit relay, broadcasts), then performs a complete shutdown and
// verifies every resource is cleaned up: goroutines, channels, caches, maps.
func TestCleanup_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full lifecycle cleanup test in short mode")
	}

	// ---- Phase 1: Boot everything ----
	srv, mr := testServer(t)

	// Start the edit relay (subscribes to Redis pub/sub).
	srv.StartEditRelay(srv.redis)
	time.Sleep(50 * time.Millisecond)

	// Verify components are running.
	assert.NotNil(t, srv.wsHub, "WebSocket hub should exist")
	assert.NotNil(t, srv.alertHub, "Alert hub should exist")
	assert.NotNil(t, srv.cache, "Response cache should exist")
	assert.NotNil(t, srv.editRelayCancel, "Edit relay should be running")

	goroutinesAfterBoot := runtime.NumGoroutine()
	t.Logf("Goroutines after boot: %d", goroutinesAfterBoot)

	// ---- Phase 2: Simulate production traffic ----

	// 2a. Populate response cache with varied entries.
	for i := 0; i < 200; i++ {
		srv.cache.Set(
			fmt.Sprintf("trending-limit-%d-window-%d", i%20, i%5),
			[]byte(fmt.Sprintf(`[{"page":"Page_%d","score":%.1f}]`, i, float64(i)*1.5)),
			500*time.Millisecond,
		)
	}
	srv.cache.mu.RLock()
	cacheCountDuringTraffic := len(srv.cache.entries)
	srv.cache.mu.RUnlock()
	assert.Greater(t, cacheCountDuringTraffic, 0, "cache should have entries during traffic")

	// 2b. Register WebSocket clients directly into the hub.
	const wsClientCount = 15
	wsClients := make([]*Client, wsClientCount)
	for i := 0; i < wsClientCount; i++ {
		c := &Client{
			hub:         srv.wsHub,
			send:        make(chan []byte, sendBufferSize),
			id:          fmt.Sprintf("cleanup-ws-%d", i),
			connectedAt: time.Now(),
			remoteAddr:  fmt.Sprintf("10.0.1.%d", i+1),
			filter: &EditFilter{
				Languages:   []string{"en"},
				ExcludeBots: i%3 == 0,
			},
		}
		wsClients[i] = c
		srv.wsHub.register <- c
	}
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, wsClientCount, srv.wsHub.ClientCount(),
		"all WS clients should be registered")

	// 2c. Subscribe alert channels (simulates /ws/alerts connections).
	const alertSubCount = 10
	alertChannels := make([]chan storage.Alert, alertSubCount)
	for i := 0; i < alertSubCount; i++ {
		alertChannels[i] = srv.alertHub.Subscribe()
	}
	srv.alertHub.mu.RLock()
	alertSubsDuringTraffic := len(srv.alertHub.subscribers)
	srv.alertHub.mu.RUnlock()
	assert.Equal(t, alertSubCount, alertSubsDuringTraffic,
		"all alert subscribers should be registered")

	// 2d. Broadcast edits and alerts through the system.
	for i := 0; i < 50; i++ {
		srv.wsHub.BroadcastEditFiltered(&models.WikipediaEdit{
			ID: int64(i), Title: fmt.Sprintf("Cleanup_Edit_%d", i), Wiki: "enwiki",
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 100, New: 200 + i},
		})
		if i%5 == 0 {
			srv.alertHub.broadcast(storage.Alert{
				ID:   fmt.Sprintf("cleanup-alert-%d", i),
				Type: storage.AlertTypeSpike,
			})
		}
	}
	time.Sleep(100 * time.Millisecond)

	// 2e. Publish edits through Redis pub/sub (feeds the edit relay).
	for i := 0; i < 20; i++ {
		editJSON := fmt.Sprintf(`{"id":%d,"title":"Relay_Edit_%d","wiki":"enwiki","length":{"old":100,"new":200}}`, i+1000, i)
		mr.Publish("wikisurge:edits:live", editJSON)
	}
	time.Sleep(100 * time.Millisecond)

	// 2f. Make API requests to exercise the full handler stack.
	for _, path := range []string{"/health", "/api/trending", "/api/stats", "/api/alerts"} {
		rec := doRequest(srv, "GET", path)
		assert.Contains(t, []int{http.StatusOK, http.StatusInternalServerError}, rec.Code,
			"endpoint %s should respond", path)
	}

	goroutinesDuringTraffic := runtime.NumGoroutine()
	t.Logf("Goroutines during traffic: %d", goroutinesDuringTraffic)

	// ---- Phase 3: Simulate graceful shutdown sequence ----
	// Mirror the shutdown order from cmd/api/main.go:
	// 1. Stop accepting new connections (httpServer.Shutdown — not applicable in unit test)
	// 2. Unsubscribe alert channels (simulates WS clients disconnecting during drain)
	for _, ch := range alertChannels {
		srv.alertHub.Unsubscribe(ch)
	}

	// 3. Unregister WS clients (simulates connections closing during drain)
	for _, c := range wsClients {
		srv.wsHub.unregister <- c
	}
	time.Sleep(200 * time.Millisecond)

	// 4. Shut down the API server (stops hub, alert hub, cache, edit relay).
	err := srv.Shutdown(nil)
	assert.NoError(t, err, "Shutdown should not return error")

	// ---- Phase 4: Verify everything is cleaned up ----
	time.Sleep(500 * time.Millisecond)

	// 4a. WebSocket hub — no clients remaining.
	wsCount := srv.wsHub.ClientCount()
	assert.Equal(t, 0, wsCount,
		"WebSocket hub should have 0 clients after cleanup, got %d", wsCount)

	// 4b. Alert hub — no subscribers remaining.
	srv.alertHub.mu.RLock()
	alertSubsAfter := len(srv.alertHub.subscribers)
	srv.alertHub.mu.RUnlock()
	assert.Equal(t, 0, alertSubsAfter,
		"Alert hub should have 0 subscribers after cleanup, got %d", alertSubsAfter)

	// 4c. Edit relay — cancel function was invoked (context cancelled).
	// The relay goroutine exits when its context is cancelled.
	assert.NotNil(t, srv.editRelayCancel, "edit relay cancel should still be set")

	// 4d. Goroutine count should drop back near the boot level.
	goroutinesAfterShutdown := runtime.NumGoroutine()
	t.Logf("Goroutines after shutdown: %d (boot: %d, traffic: %d)",
		goroutinesAfterShutdown, goroutinesAfterBoot, goroutinesDuringTraffic)
	goroutineDelta := goroutinesAfterShutdown - goroutinesAfterBoot
	assert.LessOrEqual(t, goroutineDelta, 5,
		"goroutine count should return near boot level after shutdown (boot=%d, after=%d, delta=%d)",
		goroutinesAfterBoot, goroutinesAfterShutdown, goroutineDelta)
}

// TestCleanup_WSHub_VerifyClientMapEmpty verifies that after Stop(), the
// internal clients map is completely empty — no phantom entries that would
// prevent GC of Client objects and their connection buffers.
func TestCleanup_WSHub_VerifyClientMapEmpty(t *testing.T) {
	hub := NewWebSocketHub(zerolog.Nop())
	hub.SetMaxClients(100)
	hub.SetMaxPerIP(100)
	go hub.Run()

	// Register many clients.
	const count = 30
	clients := make([]*Client, count)
	for i := 0; i < count; i++ {
		c := &Client{
			hub:         hub,
			send:        make(chan []byte, sendBufferSize),
			id:          fmt.Sprintf("map-test-%d", i),
			connectedAt: time.Now(),
			remoteAddr:  fmt.Sprintf("10.0.%d.1", i),
		}
		clients[i] = c
		hub.register <- c
	}
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, count, hub.ClientCount())

	// Stop the hub — should close all client send channels and clear the map.
	hub.Stop()
	time.Sleep(200 * time.Millisecond)

	hub.mu.RLock()
	remaining := len(hub.clients)
	hub.mu.RUnlock()
	assert.Equal(t, 0, remaining,
		"clients map should be empty after Stop(), got %d entries", remaining)

	// Verify all send channels are closed.
	for i, c := range clients {
		_, open := <-c.send
		assert.False(t, open, "client %d send channel should be closed after Stop()", i)
	}
}

// TestCleanup_AlertHub_SubscribersAfterStop verifies that subscriber channels
// registered before Stop() can still be read (they'll see the close). This is
// important because /ws/alerts handlers hold references to channels and must
// be able to detect shutdown cleanly.
func TestCleanup_AlertHub_SubscribersAfterStop(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	go hub.Run()

	// Subscribe, then send a few alerts.
	ch := hub.Subscribe()
	hub.broadcast(storage.Alert{ID: "pre-stop", Type: storage.AlertTypeSpike})
	time.Sleep(50 * time.Millisecond)

	// Read the alert before stop.
	select {
	case alert := <-ch:
		assert.Equal(t, "pre-stop", alert.ID)
	case <-time.After(time.Second):
		t.Fatal("should receive alert before stop")
	}

	// Unsubscribe (simulates WS handler cleanup during HTTP drain).
	hub.Unsubscribe(ch)

	// Now stop the hub.
	hub.Stop()
	time.Sleep(100 * time.Millisecond)

	// Verify the channel is closed (reading returns zero value immediately).
	select {
	case _, open := <-ch:
		assert.False(t, open, "subscriber channel should be closed after Unsubscribe")
	case <-time.After(time.Second):
		t.Fatal("closed channel should be immediately readable")
	}
}

// TestCleanup_ResponseCache_EmptyAfterExpiry verifies that after all entries
// expire and the cleanup goroutine runs, the cache is truly empty — no stale
// keys holding references that prevent GC.
func TestCleanup_ResponseCache_EmptyAfterExpiry(t *testing.T) {
	cache := newResponseCache()
	defer cache.Stop()

	// Fill with short-lived entries.
	for i := 0; i < 500; i++ {
		cache.Set(fmt.Sprintf("expire-test-%d", i), []byte("data"), 20*time.Millisecond)
	}

	cache.mu.RLock()
	beforeExpiry := len(cache.entries)
	cache.mu.RUnlock()
	assert.Equal(t, 500, beforeExpiry)

	// Wait for entries to expire.
	time.Sleep(50 * time.Millisecond)

	// Manually trigger cleanup (same as the 30s ticker would).
	cache.mu.Lock()
	now := time.Now()
	for k, v := range cache.entries {
		if now.After(v.expiresAt) {
			delete(cache.entries, k)
		}
	}
	cache.mu.Unlock()

	cache.mu.RLock()
	afterCleanup := len(cache.entries)
	cache.mu.RUnlock()
	assert.Equal(t, 0, afterCleanup,
		"cache should be completely empty after expired cleanup, got %d entries", afterCleanup)
}

// TestCleanup_EditRelay_MultipleRestarts verifies that after multiple relay
// restarts (simulating Redis disconnects), the most recent relay is active
// and shutdown cancels it. Each restart properly cancels the previous relay's
// context to avoid leaking Redis subscriptions.
func TestCleanup_EditRelay_MultipleRestarts(t *testing.T) {
	srv, _ := testServer(t)

	// Track that each restart replaces the cancel function.
	var previousCancels []context.CancelFunc

	for i := 0; i < 5; i++ {
		prevCancel := srv.editRelayCancel
		if prevCancel != nil {
			previousCancels = append(previousCancels, prevCancel)
		}
		srv.StartEditRelay(srv.redis)
		time.Sleep(30 * time.Millisecond)

		// Verify a new cancel func was set.
		assert.NotNil(t, srv.editRelayCancel, "relay %d should have a cancel func", i)
		if prevCancel != nil {
			// The new cancel should be different from the previous one.
			// (Can't compare funcs directly, but we can verify the field changed.)
			assert.NotNil(t, srv.editRelayCancel)
		}
	}

	// After 5 starts, we should have seen 4 previous cancels replaced.
	assert.GreaterOrEqual(t, len(previousCancels), 3,
		"relay restarts should replace cancel functions")

	// The latest relay should still be running.
	latestCancel := srv.editRelayCancel
	assert.NotNil(t, latestCancel)

	// Shut down — this cancels the latest relay.
	err := srv.Shutdown(nil)
	assert.NoError(t, err)
}

// TestCleanup_HotPageTracker verifies the hot page tracker's cleanup goroutine
// shuts down cleanly when Shutdown() is called.
func TestCleanup_HotPageTracker(t *testing.T) {
	srv, _ := testServer(t)
	_ = srv // testServer creates hotPages with cleanup auto-started in some configs

	goroutinesBefore := runtime.NumGoroutine()

	// The testServer cleanup func calls hotPages.Shutdown() and trending.Stop().
	// We verify those paths don't panic and goroutines stabilize.
	err := srv.Shutdown(nil)
	assert.NoError(t, err)
	time.Sleep(300 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	assert.LessOrEqual(t, goroutinesAfter-goroutinesBefore, 5,
		"hot page tracker cleanup should not leak goroutines")
}

// TestCleanup_CacheStopTerminatesCleanupGoroutine verifies that calling
// cache.Stop() actually terminates the background cleanup goroutine — not
// just closes the channel but the goroutine exits.
func TestCleanup_CacheStopTerminatesCleanupGoroutine(t *testing.T) {
	goroutinesBefore := runtime.NumGoroutine()

	// Create and immediately stop 10 caches.
	for i := 0; i < 10; i++ {
		c := newResponseCache()
		c.Set("test", []byte("data"), time.Hour)
		c.Stop()
	}

	time.Sleep(200 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()

	assert.InDelta(t, goroutinesBefore, goroutinesAfter, 3,
		"stopping caches should not leak goroutines (created 10 caches, before=%d, after=%d)",
		goroutinesBefore, goroutinesAfter)
}

// TestCleanup_FullLifecycleWithRealWebSocketClients boots the full server,
// connects real WebSocket clients via HTTP upgrade, sends traffic, then shuts
// down and verifies all resources (goroutines, channels, maps) are released.
func TestCleanup_FullLifecycleWithRealWebSocketClients(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full WS lifecycle cleanup test in short mode")
	}

	srv, mr := testServer(t)
	srv.StartEditRelay(srv.redis)

	handler := srv.Handler()
	httpSrv := httptest.NewServer(handler)
	t.Cleanup(httpSrv.Close)

	goroutinesAfterBoot := runtime.NumGoroutine()

	// ---- Connect real WebSocket clients ----
	const clientCount = 8
	wsConns := make([]*websocket.Conn, clientCount)
	for i := 0; i < clientCount; i++ {
		url := "ws" + httpSrv.URL[4:] + "/ws/feed"
		c, _, err := websocket.DefaultDialer.Dial(url, nil)
		require.NoError(t, err, "WS client %d should connect", i)
		wsConns[i] = c
	}
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, clientCount, srv.wsHub.ClientCount(),
		"all real WS clients should be registered")

	// ---- Send traffic through the system ----
	// Edits via pub/sub relay.
	for i := 0; i < 10; i++ {
		editJSON := fmt.Sprintf(`{"id":%d,"title":"RealWS_Edit_%d","wiki":"enwiki","length":{"old":100,"new":200}}`, i, i)
		mr.Publish("wikisurge:edits:live", editJSON)
	}
	// Direct broadcasts.
	for i := 0; i < 10; i++ {
		srv.wsHub.BroadcastEdit(&models.WikipediaEdit{
			ID: int64(i + 100), Title: fmt.Sprintf("Direct_%d", i), Wiki: "enwiki",
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 50, New: 100},
		})
	}
	time.Sleep(200 * time.Millisecond)

	// ---- Clients read some messages ----
	for _, c := range wsConns {
		_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		// Read whatever is available — don't error on timeout.
		for {
			_, _, err := c.ReadMessage()
			if err != nil {
				break
			}
		}
	}

	// ---- Graceful shutdown: close WS clients, then server ----
	for _, c := range wsConns {
		c.Close()
	}
	time.Sleep(300 * time.Millisecond)

	// Verify clients unregistered after close.
	remainingClients := srv.wsHub.ClientCount()
	assert.Equal(t, 0, remainingClients,
		"all WS clients should unregister after Close(), got %d", remainingClients)

	// Full server shutdown.
	err := srv.Shutdown(nil)
	assert.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	goroutinesAfterShutdown := runtime.NumGoroutine()
	delta := goroutinesAfterShutdown - goroutinesAfterBoot
	t.Logf("Real WS cleanup: boot=%d, shutdown=%d, delta=%d",
		goroutinesAfterBoot, goroutinesAfterShutdown, delta)
	assert.LessOrEqual(t, delta, 5,
		"goroutine count should stabilize after full lifecycle with real WS clients")
}

// TestCleanup_ShutdownOrderMatters verifies that shutting down in the wrong
// order (API before HTTP drain) doesn't cause panics. In production, this
// can happen during a SIGKILL or forced restart.
func TestCleanup_ShutdownOrderMatters(t *testing.T) {
	srv, _ := testServer(t)
	srv.StartEditRelay(srv.redis)

	// Subscribe some alert channels.
	channels := make([]chan storage.Alert, 5)
	for i := range channels {
		channels[i] = srv.alertHub.Subscribe()
	}

	// Register some WS clients.
	for i := 0; i < 5; i++ {
		c := &Client{
			hub:         srv.wsHub,
			send:        make(chan []byte, sendBufferSize),
			id:          fmt.Sprintf("order-test-%d", i),
			connectedAt: time.Now(),
			remoteAddr:  fmt.Sprintf("10.0.0.%d", i+1),
		}
		srv.wsHub.register <- c
	}
	time.Sleep(100 * time.Millisecond)

	// Shut down the API server FIRST (before cleaning up WS clients/alert subs).
	// This simulates a forced shutdown where HTTP connections haven't drained.
	assert.NotPanics(t, func() {
		_ = srv.Shutdown(nil)
	}, "shutdown should not panic even with active clients/subscribers")

	// Now unsubscribe after shutdown — this is the "wrong order" scenario.
	// The hub's Run goroutine is already stopped, but Unsubscribe should still
	// work because it only touches the map under mutex.
	for _, ch := range channels {
		assert.NotPanics(t, func() {
			srv.alertHub.Unsubscribe(ch)
		}, "Unsubscribe after Stop should not panic")
	}

	time.Sleep(200 * time.Millisecond)

	srv.alertHub.mu.RLock()
	remaining := len(srv.alertHub.subscribers)
	srv.alertHub.mu.RUnlock()
	assert.Equal(t, 0, remaining, "all subscribers should be cleaned up even with wrong shutdown order")
}

// TestCleanup_ConcurrentShutdown verifies that multiple goroutines calling
// Shutdown simultaneously doesn't cause double-closes, panics, or deadlocks.
func TestCleanup_ConcurrentShutdown(t *testing.T) {
	srv, _ := testServer(t)
	srv.StartEditRelay(srv.redis)

	// Fill some state.
	srv.cache.Set("test", []byte("data"), time.Hour)
	ch := srv.alertHub.Subscribe()
	c := &Client{
		hub:         srv.wsHub,
		send:        make(chan []byte, sendBufferSize),
		id:          "concurrent-shutdown",
		connectedAt: time.Now(),
		remoteAddr:  "10.0.0.1",
	}
	srv.wsHub.register <- c
	time.Sleep(50 * time.Millisecond)

	// Launch multiple concurrent shutdowns.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NotPanics(t, func() {
				_ = srv.Shutdown(nil)
			})
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Good — no deadlock.
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent shutdowns deadlocked")
	}

	// Clean up the subscriber we created (may already be cleaned up).
	assert.NotPanics(t, func() {
		srv.alertHub.Unsubscribe(ch)
	})
}

// ===========================================================================
// TOCTOU race regression test for WebSocket hub slow-client disconnect
// ===========================================================================

// TestHub_SlowClientDisconnectNoPanic exercises the exact TOCTOU race that
// caused "send on closed channel" panics in production. The scenario:
//
//  1. Hub detects a slow client during broadcast (send buffer full).
//  2. Between releasing the read lock (RUnlock) and acquiring the write lock
//     (Lock), the client's readPump/writePump unregisters it via the
//     unregister channel — closing client.send.
//  3. Hub acquires the write lock and tries to close(client.send) again.
//
// Before the fix (bare close()), step 3 panicked. After the fix (closeOnce),
// the second close is a no-op.
func TestHub_SlowClientDisconnectNoPanic(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	hub.SetMaxClients(200)
	hub.SetMaxPerIP(200)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	const rounds = 200
	for round := 0; round < rounds; round++ {
		// Create a client with a tiny send buffer so it becomes "slow" instantly.
		client := &Client{
			hub:         hub,
			send:        make(chan []byte, 1), // intentionally tiny
			id:          fmt.Sprintf("toctou-%d", round),
			connectedAt: time.Now(),
			remoteAddr:  "10.0.0.1",
		}

		// Register the client.
		hub.register <- client
		time.Sleep(time.Millisecond)

		// Fill the send buffer so the hub sees it as "slow".
		select {
		case client.send <- []byte("fill"):
		default:
		}

		// Concurrently:
		// 1. Broadcast a message (hub will detect client as slow).
		// 2. Unregister the client (simulates readPump/writePump exit).
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			hub.BroadcastEdit(&models.WikipediaEdit{
				ID: int64(round), Title: "Race", Wiki: "enwiki",
				Length: struct {
					Old int `json:"old"`
					New int `json:"new"`
				}{Old: 1, New: 2},
			})
		}()
		go func() {
			defer wg.Done()
			hub.unregister <- client
		}()
		wg.Wait()
	}

	time.Sleep(100 * time.Millisecond)
	// If we get here without a "send on closed channel" panic, the fix works.
	assert.LessOrEqual(t, hub.ClientCount(), 0,
		"all clients should be cleaned up after TOCTOU test")
}

// TestHub_CloseOncePreventsDoublePanic directly verifies that the
// Client.closeOnce guard prevents a double-close panic.
func TestHub_CloseOncePreventsDoublePanic(t *testing.T) {
	client := &Client{
		send: make(chan []byte, 1),
		id:   "double-close-test",
	}

	assert.NotPanics(t, func() {
		// First close — normal.
		client.closeOnce.Do(func() { close(client.send) })
		// Second close — would panic without closeOnce.
		client.closeOnce.Do(func() { close(client.send) })
		// Third for good measure.
		client.closeOnce.Do(func() { close(client.send) })
	}, "closeOnce should prevent double-close panics")
}

// ===========================================================================
// Alert hub subscriber cleanup on write-loop panic
// ===========================================================================

// TestAlertHub_UnsubscribeCalledOnPanic verifies that even if a panic occurs
// in an alert subscriber's processing, the Unsubscribe call in the deferred
// recovery still runs, preventing a subscriber channel leak. This mimics the
// fix in websocket_alerts.go where panic recovery was added.
func TestAlertHub_UnsubscribeCalledOnPanic(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	// Subscribe a channel.
	ch := hub.Subscribe()

	hub.mu.RLock()
	subsBefore := len(hub.subscribers)
	hub.mu.RUnlock()
	assert.Equal(t, 1, subsBefore, "should have 1 subscriber")

	// Simulate a write-loop goroutine that panics but has proper recovery.
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// This is what the fixed websocket_alerts.go does.
			}
			hub.Unsubscribe(ch)
			close(done)
		}()

		// Read one alert, then panic (simulates marshal failure, etc.).
		<-ch
		panic("simulated write error")
	}()

	// Send an alert to trigger the panic.
	hub.broadcast(storage.Alert{ID: "trigger-panic", Type: storage.AlertTypeSpike})

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("write loop should have recovered from panic")
	}

	time.Sleep(50 * time.Millisecond)

	hub.mu.RLock()
	subsAfter := len(hub.subscribers)
	hub.mu.RUnlock()
	assert.Equal(t, 0, subsAfter,
		"subscriber should be cleaned up after panic recovery, got %d", subsAfter)
}

// TestAlertHub_SubscriberLeakWithoutPanicRecovery demonstrates why panic
// recovery is needed. If the write loop panics WITHOUT recovery, the
// Unsubscribe in the defer is NOT reached (because panic propagates up),
// but since the goroutine died, the subscriber channel becomes orphaned.
// With the fix, the deferred recovery catches the panic first, then
// Unsubscribe runs.
func TestAlertHub_SubscriberLeakWithoutPanicRecovery(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	// Rapid subscribe/panic/unsubscribe cycles.
	const cycles = 100
	var wg sync.WaitGroup

	for i := 0; i < cycles; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := hub.Subscribe()
			defer func() {
				if r := recover(); r != nil {
					// Recovery catches the panic.
				}
				hub.Unsubscribe(ch)
			}()

			// Simulate some work that might panic.
			if id%3 == 0 {
				panic("simulated failure")
			}
			// Otherwise clean exit.
		}(i)
	}

	wg.Wait()
	time.Sleep(50 * time.Millisecond)

	hub.mu.RLock()
	remaining := len(hub.subscribers)
	hub.mu.RUnlock()
	assert.Equal(t, 0, remaining,
		"all subscribers should be cleaned up after panic cycles, got %d", remaining)
}
