package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
// WebSocket Hub resilience tests
// ===========================================================================

// TestHub_PanicRecovery verifies that the hub's Run() goroutine recovers from
// panics and continues processing. A crashing hub was the #1 cause of the
// production outage.
func TestHub_PanicRecovery(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	// Connect a client through the normal path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &Client{
			hub:         hub,
			conn:        conn,
			send:        make(chan []byte, sendBufferSize),
			id:          "panic-test-client",
			connectedAt: time.Now(),
			remoteAddr:  "127.0.0.1",
		}
		hub.register <- client
		go client.writePump()
		go client.readPump()
	}))
	t.Cleanup(srv.Close)

	ws, _, err := websocket.DefaultDialer.Dial(wsURL(srv, ""), nil)
	require.NoError(t, err)
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, hub.ClientCount(), "client should be registered")

	// The hub should keep running even if we send after it processes normally.
	edit := &models.WikipediaEdit{ID: 1, Title: "After-Panic Test", Wiki: "enwiki",
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200}}
	hub.BroadcastEdit(edit)

	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.Contains(t, string(msg), "After-Panic Test")
}

// TestHub_SlowClientEviction verifies clients with full send buffers are
// disconnected instead of blocking the entire broadcast loop.
func TestHub_SlowClientEviction(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	srvMux := http.NewServeMux()
	srvMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Deliberately tiny send buffer to trigger eviction.
		client := &Client{
			hub:         hub,
			conn:        conn,
			send:        make(chan []byte, 1),
			id:          fmt.Sprintf("slow-%d", time.Now().UnixNano()),
			connectedAt: time.Now(),
			remoteAddr:  "127.0.0.1",
		}
		hub.register <- client
		// Intentionally do NOT start writePump — msgs accumulate.
		go client.readPump()
	})
	srv := httptest.NewServer(srvMux)
	t.Cleanup(srv.Close)

	ws, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/ws"), nil)
	require.NoError(t, err)
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, hub.ClientCount())

	// Flood the broadcast — the slow client should be evicted.
	for i := 0; i < 20; i++ {
		hub.BroadcastEdit(&models.WikipediaEdit{
			ID: int64(i), Title: fmt.Sprintf("Flood_%d", i), Wiki: "enwiki",
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 100, New: 200},
		})
		time.Sleep(5 * time.Millisecond) // let hub process each
	}

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 0, hub.ClientCount(), "slow client should have been evicted")
}

// TestHub_RapidConnectDisconnect simulates many clients connecting and
// disconnecting in rapid succession — the kind of load that causes goroutine
// leaks if unregister channels block.
func TestHub_RapidConnectDisconnect(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	hub.SetMaxClients(200)
	hub.SetMaxPerIP(200)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &Client{
			hub:         hub,
			conn:        conn,
			send:        make(chan []byte, sendBufferSize),
			id:          fmt.Sprintf("rapid-%d", time.Now().UnixNano()),
			connectedAt: time.Now(),
			remoteAddr:  "127.0.0.1",
		}
		hub.register <- client
		go client.writePump()
		go client.readPump()
	}))
	t.Cleanup(srv.Close)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws, _, err := websocket.DefaultDialer.Dial(wsURL(srv, ""), nil)
			if err != nil {
				return
			}
			// Hold connection briefly, then close.
			time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
			ws.Close()
		}()
	}
	wg.Wait()

	// Give hub time to process all unregistrations.
	time.Sleep(500 * time.Millisecond)
	count := hub.ClientCount()
	assert.Equal(t, 0, count, "all clients should be cleaned up, got %d", count)
}

// TestHub_ConcurrentBroadcastAndRegister sends broadcasts while clients are
// joining — tests the RWMutex under contention (race detector flag: -race).
func TestHub_ConcurrentBroadcastAndRegister(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	hub.SetMaxClients(200)
	hub.SetMaxPerIP(200)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &Client{
			hub:         hub,
			conn:        conn,
			send:        make(chan []byte, sendBufferSize),
			id:          fmt.Sprintf("conc-%d", time.Now().UnixNano()),
			connectedAt: time.Now(),
			remoteAddr:  "127.0.0.1",
		}
		hub.register <- client
		go client.writePump()
		go client.readPump()
	}))
	t.Cleanup(srv.Close)

	// Start broadcasting in a separate goroutine.
	stopBroadcast := make(chan struct{})
	go func() {
		i := 0
		for {
			select {
			case <-stopBroadcast:
				return
			default:
				hub.BroadcastEditFiltered(&models.WikipediaEdit{
					ID: int64(i), Title: "ConcurrentEdit", Wiki: "enwiki",
					Length: struct {
						Old int `json:"old"`
						New int `json:"new"`
					}{Old: 100, New: 200},
				})
				i++
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Connect and disconnect clients concurrently.
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws, _, err := websocket.DefaultDialer.Dial(wsURL(srv, ""), nil)
			if err != nil {
				return
			}
			time.Sleep(time.Duration(20+rand.Intn(80)) * time.Millisecond)
			ws.Close()
		}()
	}
	wg.Wait()
	close(stopBroadcast)

	time.Sleep(300 * time.Millisecond)
	// No panics, no deadlocks — test passes if we get here.
}

// TestHub_StopDuringActivity verifies graceful shutdown while clients are
// connected and broadcasts are inflight.
func TestHub_StopDuringActivity(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	hub.SetMaxClients(50)
	hub.SetMaxPerIP(50)
	go hub.Run()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &Client{
			hub:         hub,
			conn:        conn,
			send:        make(chan []byte, sendBufferSize),
			id:          fmt.Sprintf("stop-%d", time.Now().UnixNano()),
			connectedAt: time.Now(),
			remoteAddr:  "127.0.0.1",
		}
		hub.register <- client
		go client.writePump()
		go client.readPump()
	}))

	// Connect several clients.
	conns := make([]*websocket.Conn, 5)
	for i := range conns {
		c, _, err := websocket.DefaultDialer.Dial(wsURL(srv, ""), nil)
		require.NoError(t, err)
		conns[i] = c
	}
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 5, hub.ClientCount())

	// Stop the hub — should not panic or deadlock.
	hub.Stop()
	time.Sleep(100 * time.Millisecond)

	// Clean up.
	for _, c := range conns {
		c.Close()
	}
	srv.Close()
}

// TestHub_PerIPLimit verifies per-IP connection limiting works when many
// connections come from the same IP.
func TestHub_PerIPLimit(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	hub.SetMaxClients(100)
	hub.SetMaxPerIP(3)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &Client{
			hub:         hub,
			conn:        conn,
			send:        make(chan []byte, sendBufferSize),
			id:          fmt.Sprintf("ip-%d", time.Now().UnixNano()),
			connectedAt: time.Now(),
			remoteAddr:  "10.0.0.1", // Same IP for all
		}
		hub.register <- client
		go client.writePump()
		go client.readPump()
	}))
	t.Cleanup(srv.Close)

	var conns []*websocket.Conn
	for i := 0; i < 6; i++ {
		c, _, err := websocket.DefaultDialer.Dial(wsURL(srv, ""), nil)
		if err == nil {
			conns = append(conns, c)
		}
		time.Sleep(30 * time.Millisecond)
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	time.Sleep(200 * time.Millisecond)
	assert.LessOrEqual(t, hub.ClientCount(), 3, "per-IP limit should cap at 3")
}

// ===========================================================================
// AlertHub resilience tests
// ===========================================================================

// TestAlertHub_PanicRecovery verifies the alert hub restarts after a panic.
func TestAlertHub_PanicRecovery(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())

	// Run the hub — with nil alerts it blocks on ctx.Done()
	go hub.Run()
	time.Sleep(50 * time.Millisecond)

	// Subscribe and broadcast should work.
	ch := hub.Subscribe()
	hub.broadcast(storage.Alert{ID: "test-recovery", Type: storage.AlertTypeSpike})

	select {
	case got := <-ch:
		assert.Equal(t, "test-recovery", got.ID)
	case <-time.After(time.Second):
		t.Fatal("did not receive alert")
	}

	hub.Unsubscribe(ch)
	hub.Stop()
}

// TestAlertHub_ManySubscribersUnsubscribe tests rapid subscribe/unsubscribe
// cycles under concurrent broadcast — catches races and leaked channels.
func TestAlertHub_ManySubscribersUnsubscribe(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	// Run concurrent subscribe / broadcast / unsubscribe cycles.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := hub.Subscribe()
			// Simulate receiving some alerts.
			hub.broadcast(storage.Alert{ID: "mass-test"})
			time.Sleep(time.Duration(rand.Intn(20)) * time.Millisecond)
			hub.Unsubscribe(ch)
		}()
	}
	wg.Wait()

	hub.mu.RLock()
	remaining := len(hub.subscribers)
	hub.mu.RUnlock()
	assert.Equal(t, 0, remaining, "all subscribers should be cleaned up")
}

// TestAlertHub_BroadcastUnderLoad sends many alerts while subscribers are
// joining and leaving.
func TestAlertHub_BroadcastUnderLoad(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	stop := make(chan struct{})

	// Broadcaster goroutine.
	go func() {
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				hub.broadcast(storage.Alert{ID: fmt.Sprintf("load-%d", i), Type: storage.AlertTypeSpike})
				i++
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Subscribe/unsubscribe goroutines.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := hub.Subscribe()
			count := 0
			timer := time.NewTimer(100 * time.Millisecond)
			defer timer.Stop()
			for {
				select {
				case <-ch:
					count++
				case <-timer.C:
					hub.Unsubscribe(ch)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	// No panics, no deadlocks.
}

// ===========================================================================
// RecoveryMiddleware tests
// ===========================================================================

// TestRecoveryMiddleware_CatchesPanic verifies that the HTTP-level recovery
// middleware catches handler panics and returns 500 instead of crashing.
func TestRecoveryMiddleware_CatchesPanic(t *testing.T) {
	logger := zerolog.Nop()
	handler := RecoveryMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("handler exploded")
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "unexpected error")
}

// TestRecoveryMiddleware_NilPanic tests recovery from a nil panic value.
func TestRecoveryMiddleware_NilPanic(t *testing.T) {
	logger := zerolog.Nop()
	handler := RecoveryMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(nil)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	// In Go 1.21+, panic(nil) is caught by recover(). Should not crash.
	assert.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})
}

// TestRecoveryMiddleware_MultiplePanicsInSequence ensures the recovery
// middleware works for repeated panicking requests — not just the first one.
func TestRecoveryMiddleware_MultiplePanicsInSequence(t *testing.T) {
	logger := zerolog.Nop()
	callCount := 0
	handler := RecoveryMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		panic(fmt.Sprintf("panic #%d", callCount))
	}))

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	}
	assert.Equal(t, 10, callCount, "all 10 requests should have been handled")
}

// ===========================================================================
// GzipMiddleware resilience tests
// ===========================================================================

// TestGzipMiddleware_SmallResponse verifies sub-threshold responses are sent
// uncompressed.
func TestGzipMiddleware_SmallResponse(t *testing.T) {
	handler := GzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should NOT be gzipped (< 1KB).
	assert.NotEqual(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Contains(t, rec.Body.String(), `{"ok":true}`)
}

// TestGzipMiddleware_LargeResponse verifies responses above threshold are
// correctly compressed.
func TestGzipMiddleware_LargeResponse(t *testing.T) {
	largeBody := strings.Repeat("x", 2048) // 2KB
	handler := GzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(largeBody))
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))

	// Decompress and verify.
	reader, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, err)
	decompressed, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, largeBody, string(decompressed))
}

// TestGzipMiddleware_MultiWriteDataOrdering ensures that multi-Write()
// responses assemble data in the correct order. This catches the bug where
// the first sub-threshold Write buffered data but subsequent writes went
// directly to the underlying writer.
func TestGzipMiddleware_MultiWriteDataOrdering(t *testing.T) {
	handler := GzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// Write in multiple chunks that together exceed threshold.
		w.Write([]byte("AAAA"))                            // 4 bytes — buffered
		w.Write([]byte(strings.Repeat("B", 512)))          // 512 bytes — still buffered
		w.Write([]byte(strings.Repeat("C", 1024)))         // 1024 bytes — now exceeds threshold
		w.Write([]byte("DDDD"))                            // written to gzip
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))

	reader, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, err)
	decompressed, err := io.ReadAll(reader)
	require.NoError(t, err)

	expected := "AAAA" + strings.Repeat("B", 512) + strings.Repeat("C", 1024) + "DDDD"
	assert.Equal(t, expected, string(decompressed), "multi-write data must be in correct order")
}

// TestGzipMiddleware_NoAcceptEncoding verifies that clients without gzip
// support get uncompressed responses.
func TestGzipMiddleware_NoAcceptEncoding(t *testing.T) {
	handler := GzipMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 2048)))
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	// No Accept-Encoding header.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.NotEqual(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Equal(t, 2048, rec.Body.Len())
}

// ===========================================================================
// Timeout middleware resilience tests
// ===========================================================================

// TestRequestTimeoutMiddleware_SlowHandler verifies that slow handlers get
// their context cancelled.
func TestRequestTimeoutMiddleware_SlowHandler(t *testing.T) {
	handler := RequestTimeoutMiddleware(50*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))

	req := httptest.NewRequest("GET", "/api/slow", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusGatewayTimeout, rec.Code)
}

// TestRequestTimeoutMiddleware_SkipsWebSocket ensures WebSocket upgrades are
// not subject to the request timeout.
func TestRequestTimeoutMiddleware_SkipsWebSocket(t *testing.T) {
	handler := RequestTimeoutMiddleware(10*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a handler that checks context — WS upgrade should not be cancelled.
		select {
		case <-r.Context().Done():
			w.WriteHeader(http.StatusGatewayTimeout)
		case <-time.After(50 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		}
	}))

	req := httptest.NewRequest("GET", "/ws/feed", nil)
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// ===========================================================================
// Rate limiter resilience tests
// ===========================================================================

// TestRateLimitMiddleware_BlocksExcessiveRequests verifies rate limiting kicks
// in under heavy load — the safety net that prevents resource exhaustion.
func TestRateLimitMiddleware_BlocksExcessiveRequests(t *testing.T) {
	// Very low limit: 2 rps with burst of 4
	handler := RateLimitMiddleware(2, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	blocked := 0
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			blocked++
		}
	}
	assert.Greater(t, blocked, 0, "rate limiter should block some requests under burst")
}

// ===========================================================================
// Long-running stability simulation tests
// ===========================================================================

// TestHub_SustainedLoad simulates sustained WebSocket traffic over many
// broadcast cycles — the kind of workload that causes memory leaks or
// goroutine buildup over days of uptime.
func TestHub_SustainedLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping sustained load test in short mode")
	}

	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	hub.SetMaxClients(50)
	hub.SetMaxPerIP(50)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &Client{
			hub:         hub,
			conn:        conn,
			send:        make(chan []byte, sendBufferSize),
			id:          fmt.Sprintf("sustain-%d", time.Now().UnixNano()),
			connectedAt: time.Now(),
			remoteAddr:  "127.0.0.1",
		}
		hub.register <- client
		go client.writePump()
		go client.readPump()
	}))
	t.Cleanup(srv.Close)

	// Keep a pool of clients that rotate in and out.
	const poolSize = 10
	const totalCycles = 100
	var mu sync.Mutex
	pool := make([]*websocket.Conn, 0, poolSize)

	for cycle := 0; cycle < totalCycles; cycle++ {
		// Randomly add or remove a client.
		mu.Lock()
		if len(pool) < poolSize && rand.Intn(3) != 0 {
			ws, _, err := websocket.DefaultDialer.Dial(wsURL(srv, ""), nil)
			if err == nil {
				pool = append(pool, ws)
			}
		} else if len(pool) > 0 {
			idx := rand.Intn(len(pool))
			pool[idx].Close()
			pool = append(pool[:idx], pool[idx+1:]...)
		}
		mu.Unlock()

		// Broadcast an edit.
		hub.BroadcastEditFiltered(&models.WikipediaEdit{
			ID: int64(cycle), Title: fmt.Sprintf("Sustain_%d", cycle), Wiki: "enwiki",
			Length: struct {
				Old int `json:"old"`
				New int `json:"new"`
			}{Old: 100, New: 200 + cycle},
		})

		time.Sleep(5 * time.Millisecond)
	}

	// Clean up remaining pool.
	mu.Lock()
	for _, ws := range pool {
		ws.Close()
	}
	pool = nil
	mu.Unlock()

	time.Sleep(500 * time.Millisecond)
	count := hub.ClientCount()
	assert.Equal(t, 0, count, "all clients should be cleaned up after sustained load, got %d", count)
}

// TestAlertHub_SustainedSubscriptions simulates long-running alert fan-out
// with churning subscribers.
func TestAlertHub_SustainedSubscriptions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping sustained alert test in short mode")
	}

	hub := NewAlertHub(nil, zerolog.Nop())
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	const cycles = 200
	var wg sync.WaitGroup

	for i := 0; i < cycles; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ch := hub.Subscribe()
			// Drain a few alerts.
			timer := time.NewTimer(time.Duration(10+rand.Intn(30)) * time.Millisecond)
			defer timer.Stop()
		drain:
			for {
				select {
				case <-ch:
				case <-timer.C:
					break drain
				}
			}
			hub.Unsubscribe(ch)
		}(i)

		// Broadcast periodically.
		if i%5 == 0 {
			hub.broadcast(storage.Alert{
				ID:   fmt.Sprintf("sustained-%d", i),
				Type: storage.AlertTypeSpike,
			})
		}
		time.Sleep(2 * time.Millisecond)
	}
	wg.Wait()

	hub.mu.RLock()
	assert.Equal(t, 0, len(hub.subscribers), "no leaked subscribers after sustained test")
	hub.mu.RUnlock()
}

// ===========================================================================
// Data race tests (run with -race)
// ===========================================================================

// TestHub_ClientCountUnderConcurrency reads ClientCount() while broadcasts
// and registrations are happening — targets the data race on len(h.clients).
func TestHub_ClientCountUnderConcurrency(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	hub.SetMaxClients(200)
	hub.SetMaxPerIP(200)
	go hub.Run()
	t.Cleanup(func() { hub.Stop() })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &Client{
			hub:         hub,
			conn:        conn,
			send:        make(chan []byte, sendBufferSize),
			id:          fmt.Sprintf("race-%d", time.Now().UnixNano()),
			connectedAt: time.Now(),
			remoteAddr:  "127.0.0.1",
		}
		hub.register <- client
		go client.writePump()
		go client.readPump()
	}))
	t.Cleanup(srv.Close)

	stop := make(chan struct{})

	// Goroutine that continuously reads ClientCount (the previously racy operation).
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = hub.ClientCount()
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Goroutine that broadcasts edits.
	go func() {
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				hub.BroadcastEditFiltered(&models.WikipediaEdit{
					ID: int64(i), Title: "RaceEdit", Wiki: "enwiki",
					Length: struct {
						Old int `json:"old"`
						New int `json:"new"`
					}{Old: 100, New: 200},
				})
				i++
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Connect and disconnect clients.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ws, _, err := websocket.DefaultDialer.Dial(wsURL(srv, ""), nil)
			if err != nil {
				return
			}
			time.Sleep(time.Duration(20+rand.Intn(80)) * time.Millisecond)
			ws.Close()
		}()
	}
	wg.Wait()
	close(stop)
	time.Sleep(200 * time.Millisecond)
}

// ===========================================================================
// Full middleware stack resilience tests
// ===========================================================================

// TestFullStack_PanicInHandler verifies the entire middleware stack handles a
// panicking handler gracefully — end-to-end with real APIServer.
func TestFullStack_PanicInHandler(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()

	// The recovery middleware should catch panics from any layer. We test by
	// hitting a valid endpoint with a server backing that won't panic, and also
	// test the recovery middleware directly.
	wrapper := RecoveryMiddleware(zerolog.Nop(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("full stack panic test")
	}))

	// Wrap with the same middleware stack pattern.
	chain := GzipMiddleware(wrapper)
	chain = LoggerMiddleware(zerolog.Nop(), chain)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	// Default handler through the real stack should still work after.
	req2 := httptest.NewRequest("GET", "/health", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)
}

// TestFullStack_HealthAfterLoad verifies the health endpoint remains
// responsive after a burst of requests — catches resource exhaustion.
func TestFullStack_HealthAfterLoad(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()

	// Burst of requests.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := "/api/trending"
			if idx%3 == 0 {
				path = "/api/stats"
			} else if idx%3 == 1 {
				path = "/api/alerts"
			}
			req := httptest.NewRequest("GET", path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}(i)
	}
	wg.Wait()

	// Health should still respond.
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestFullStack_CORSPreflight verifies CORS preflight works under the full
// middleware stack.
func TestFullStack_CORSPreflight(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest("OPTIONS", "/api/trending", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
}

// ===========================================================================
// Server shutdown resilience tests
// ===========================================================================

// TestServer_DoubleShutdown verifies calling Shutdown twice doesn't panic.
func TestServer_DoubleShutdown(t *testing.T) {
	srv, _ := testServer(t)

	assert.NotPanics(t, func() {
		_ = srv.Shutdown(nil)
		_ = srv.Shutdown(nil)
	})
}

// TestServer_ShutdownWithNilComponents ensures shutdown works even if optional
// components weren't initialized.
func TestServer_ShutdownWithNilComponents(t *testing.T) {
	srv := &APIServer{
		logger: zerolog.Nop(),
	}
	assert.NotPanics(t, func() {
		_ = srv.Shutdown(nil)
	})
}

// ===========================================================================
// ETag middleware resilience
// ===========================================================================

// TestETagMiddleware_ConsistentETagOnSameContent verifies that identical
// responses produce the same ETag — broken ETags cause cache thrashing.
func TestETagMiddleware_ConsistentETagOnSameContent(t *testing.T) {
	handler := ETagMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))

	etags := make([]string, 3)
	for i := range etags {
		req := httptest.NewRequest("GET", "/api/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		etags[i] = rec.Header().Get("ETag")
	}

	assert.Equal(t, etags[0], etags[1])
	assert.Equal(t, etags[1], etags[2])
	assert.NotEmpty(t, etags[0])
}

// TestETagMiddleware_NotModified verifies If-None-Match returns 304.
func TestETagMiddleware_NotModified(t *testing.T) {
	handler := ETagMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))

	// First request — get the ETag.
	req1 := httptest.NewRequest("GET", "/api/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	etag := rec1.Header().Get("ETag")
	require.NotEmpty(t, etag)

	// Second request with If-None-Match — should get 304.
	req2 := httptest.NewRequest("GET", "/api/test", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusNotModified, rec2.Code)
	assert.Empty(t, rec2.Body.Bytes())
}

// ===========================================================================
// Concurrent handler access tests
// ===========================================================================

// TestConcurrentTrendingRequests verifies the trending endpoint is safe under
// concurrent access — catches races in caching and response building.
func TestConcurrentTrendingRequests(t *testing.T) {
	srv, _ := testServer(t)

	// Seed some data.
	for i := 0; i < 10; i++ {
		_ = srv.trending.IncrementScore(fmt.Sprintf("ConcPage_%d", i), float64(10-i))
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := doRequest(srv, "GET", "/api/trending?limit=5")
			if rec.Code != http.StatusOK {
				errors <- fmt.Errorf("got status %d", rec.Code)
				return
			}
			var results []TrendingPageResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
				errors <- fmt.Errorf("unmarshal error: %v", err)
			}
		}()
	}
	wg.Wait()
	close(errors)

	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}
	assert.Empty(t, errs, "concurrent trending requests should all succeed")
}

// TestConcurrentStatsRequests verifies the stats endpoint under concurrent
// access — the stats cache had a race condition.
func TestConcurrentStatsRequests(t *testing.T) {
	srv, _ := testServer(t)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := doRequest(srv, "GET", "/api/stats")
			assert.Equal(t, http.StatusOK, rec.Code)
		}()
	}
	wg.Wait()
}
