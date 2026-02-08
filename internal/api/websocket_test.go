package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// EditFilter tests
// ---------------------------------------------------------------------------

func TestEditFilter_MatchesAll(t *testing.T) {
	f := &EditFilter{}
	edit := &models.WikipediaEdit{
		ID: 1, Title: "Go (programming language)", Wiki: "enwiki", Bot: false,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200},
	}
	assert.True(t, f.Matches(edit), "empty filter should match everything")
}

func TestEditFilter_ExcludeBots(t *testing.T) {
	f := &EditFilter{ExcludeBots: true}

	botEdit := &models.WikipediaEdit{ID: 1, Title: "Test", Wiki: "enwiki", Bot: true}
	assert.False(t, f.Matches(botEdit))

	humanEdit := &models.WikipediaEdit{ID: 2, Title: "Test", Wiki: "enwiki", Bot: false}
	assert.True(t, f.Matches(humanEdit))
}

func TestEditFilter_Languages(t *testing.T) {
	f := &EditFilter{Languages: []string{"en", "es"}}

	enEdit := &models.WikipediaEdit{ID: 1, Title: "Test", Wiki: "enwiki"}
	assert.True(t, f.Matches(enEdit))

	esEdit := &models.WikipediaEdit{ID: 2, Title: "Test", Wiki: "eswiki"}
	assert.True(t, f.Matches(esEdit))

	frEdit := &models.WikipediaEdit{ID: 3, Title: "Test", Wiki: "frwiki"}
	assert.False(t, f.Matches(frEdit))
}

func TestEditFilter_PagePattern(t *testing.T) {
	f := &EditFilter{PagePattern: "^Go.*language"}

	match := &models.WikipediaEdit{ID: 1, Title: "Go (programming language)", Wiki: "enwiki"}
	assert.True(t, f.Matches(match))

	noMatch := &models.WikipediaEdit{ID: 2, Title: "Rust (programming language)", Wiki: "enwiki"}
	assert.False(t, f.Matches(noMatch))
}

func TestEditFilter_PagePattern_Invalid(t *testing.T) {
	f := &EditFilter{PagePattern: "[invalid"}

	edit := &models.WikipediaEdit{ID: 1, Title: "Test", Wiki: "enwiki"}
	// Invalid regex should not crash; should match everything.
	assert.True(t, f.Matches(edit))
}

func TestEditFilter_MinByteChange(t *testing.T) {
	f := &EditFilter{MinByteChange: 50}

	bigEdit := &models.WikipediaEdit{
		ID: 1, Title: "Test", Wiki: "enwiki",
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200},
	}
	assert.True(t, f.Matches(bigEdit)) // |+100| >= 50

	smallEdit := &models.WikipediaEdit{
		ID: 2, Title: "Test", Wiki: "enwiki",
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 110},
	}
	assert.False(t, f.Matches(smallEdit)) // |+10| < 50

	negEdit := &models.WikipediaEdit{
		ID: 3, Title: "Test", Wiki: "enwiki",
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 200, New: 100},
	}
	assert.True(t, f.Matches(negEdit)) // |-100| >= 50
}

func TestEditFilter_CombinedFilters(t *testing.T) {
	f := &EditFilter{
		Languages:     []string{"en"},
		ExcludeBots:   true,
		MinByteChange: 50,
	}

	// Matches all criteria.
	good := &models.WikipediaEdit{
		ID: 1, Title: "Test", Wiki: "enwiki", Bot: false,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200},
	}
	assert.True(t, f.Matches(good))

	// Wrong language.
	wrongLang := &models.WikipediaEdit{
		ID: 2, Title: "Test", Wiki: "frwiki", Bot: false,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200},
	}
	assert.False(t, f.Matches(wrongLang))

	// Is a bot.
	bot := &models.WikipediaEdit{
		ID: 3, Title: "Test", Wiki: "enwiki", Bot: true,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200},
	}
	assert.False(t, f.Matches(bot))

	// Too small.
	small := &models.WikipediaEdit{
		ID: 4, Title: "Test", Wiki: "enwiki", Bot: false,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 105},
	}
	assert.False(t, f.Matches(small))
}

// ---------------------------------------------------------------------------
// Hub lifecycle tests
// ---------------------------------------------------------------------------

func TestNewWebSocketHub(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)

	assert.NotNil(t, hub)
	assert.NotNil(t, hub.clients)
	assert.NotNil(t, hub.broadcast)
	assert.NotNil(t, hub.register)
	assert.NotNil(t, hub.unregister)
	assert.Equal(t, defaultMaxClients, hub.maxClients)
	assert.Equal(t, defaultMaxPerIP, hub.maxPerIP)
}

func TestHub_SetMaxClients(t *testing.T) {
	hub := NewWebSocketHub(zerolog.Nop())
	hub.SetMaxClients(50)
	assert.Equal(t, 50, hub.maxClients)

	// Ignore invalid values.
	hub.SetMaxClients(0)
	assert.Equal(t, 50, hub.maxClients)
	hub.SetMaxClients(-1)
	assert.Equal(t, 50, hub.maxClients)
}

func TestHub_SetMaxPerIP(t *testing.T) {
	hub := NewWebSocketHub(zerolog.Nop())
	hub.SetMaxPerIP(10)
	assert.Equal(t, 10, hub.maxPerIP)
}

func TestHub_ClientCount(t *testing.T) {
	hub := NewWebSocketHub(zerolog.Nop())
	assert.Equal(t, 0, hub.ClientCount())
}

// ---------------------------------------------------------------------------
// WebSocket integration tests
// ---------------------------------------------------------------------------

func newTestHubAndServer(t *testing.T) (*WebSocketHub, *httptest.Server) {
	t.Helper()
	logger := zerolog.Nop()
	hub := NewWebSocketHub(logger)
	hub.SetMaxClients(10)
	hub.SetMaxPerIP(5)
	go hub.Run()

	mux := http.NewServeMux()

	// Minimal handler that mocks the API server's WebSocketFeed.
	mux.HandleFunc("/ws/feed", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		filter := parseEditFilter(r)
		client := &Client{
			hub:         hub,
			conn:        conn,
			send:        make(chan []byte, sendBufferSize),
			filter:      filter,
			id:          fmt.Sprintf("test-%d", time.Now().UnixNano()),
			connectedAt: time.Now(),
			remoteAddr:  extractIP(r),
		}
		hub.register <- client
		go client.writePump()
		go client.readPump()
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		hub.Stop()
		srv.Close()
	})
	return hub, srv
}

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

func TestWebSocket_ConnectAndReceive(t *testing.T) {
	hub, srv := newTestHubAndServer(t)

	// Connect a client.
	dialer := websocket.DefaultDialer
	conn, resp, err := dialer.Dial(wsURL(srv, "/ws/feed"), nil)
	require.NoError(t, err)
	defer conn.Close()
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	// Wait for registration.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, hub.ClientCount())

	// Broadcast an edit.
	edit := &models.WikipediaEdit{
		ID: 42, Title: "Test Page", Wiki: "enwiki", User: "Alice",
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200},
	}
	hub.BroadcastEdit(edit)

	// Read the message.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)

	var msg WSMessage
	require.NoError(t, json.Unmarshal(message, &msg))
	assert.Equal(t, "edit", msg.Type)
}

func TestWebSocket_FilterExcludesBots(t *testing.T) {
	hub, srv := newTestHubAndServer(t)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/ws/feed?exclude_bots=true"), nil)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	// Broadcast a bot edit via filtered method.
	botEdit := &models.WikipediaEdit{ID: 1, Title: "Bot Page", Wiki: "enwiki", Bot: true}
	hub.BroadcastEditFiltered(botEdit)

	// Broadcast a human edit.
	humanEdit := &models.WikipediaEdit{
		ID: 2, Title: "Human Page", Wiki: "enwiki", Bot: false,
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200},
	}
	hub.BroadcastEditFiltered(humanEdit)

	// Should only receive the human edit.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)

	var msg WSMessage
	require.NoError(t, json.Unmarshal(message, &msg))
	assert.Equal(t, "edit", msg.Type)

	// Extract data to verify it's the human edit.
	dataBytes, _ := json.Marshal(msg.Data)
	var received models.WikipediaEdit
	_ = json.Unmarshal(dataBytes, &received)
	assert.Equal(t, "Human Page", received.Title)
}

func TestWebSocket_FilterLanguages(t *testing.T) {
	hub, srv := newTestHubAndServer(t)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/ws/feed?languages=en,es"), nil)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	// French edit (filtered out).
	frEdit := &models.WikipediaEdit{ID: 1, Title: "Page FR", Wiki: "frwiki"}
	hub.BroadcastEditFiltered(frEdit)

	// English edit (passes).
	enEdit := &models.WikipediaEdit{
		ID: 2, Title: "Page EN", Wiki: "enwiki",
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200},
	}
	hub.BroadcastEditFiltered(enEdit)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)

	var msg WSMessage
	require.NoError(t, json.Unmarshal(message, &msg))
	dataBytes, _ := json.Marshal(msg.Data)
	var received models.WikipediaEdit
	_ = json.Unmarshal(dataBytes, &received)
	assert.Equal(t, "Page EN", received.Title)
}

func TestWebSocket_MaxClientsLimit(t *testing.T) {
	hub, srv := newTestHubAndServer(t)
	hub.SetMaxClients(3)
	hub.SetMaxPerIP(10) // Raise per-IP limit so global limit triggers first.

	conns := make([]*websocket.Conn, 0, 4)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	// Open 3 connections (should succeed).
	for i := 0; i < 3; i++ {
		c, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/ws/feed"), nil)
		require.NoError(t, err, "connection %d should succeed", i)
		conns = append(conns, c)
		time.Sleep(30 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 3, hub.ClientCount())

	// 4th connection — should be rejected (server closes immediately).
	c4, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/ws/feed"), nil)
	if err == nil {
		// Connection might succeed but get closed immediately.
		_ = c4.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, _, readErr := c4.ReadMessage()
		assert.Error(t, readErr, "4th client should be disconnected")
		c4.Close()
	}
}

func TestWebSocket_MultipleClients(t *testing.T) {
	hub, srv := newTestHubAndServer(t)
	hub.SetMaxPerIP(10)

	const numClients = 5
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		c, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/ws/feed"), nil)
		require.NoError(t, err)
		conns[i] = c
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, numClients, hub.ClientCount())

	// Broadcast an edit.
	edit := &models.WikipediaEdit{
		ID: 99, Title: "Multi-client Test", Wiki: "enwiki",
		Length: struct {
			Old int `json:"old"`
			New int `json:"new"`
		}{Old: 100, New: 200},
	}
	hub.BroadcastEdit(edit)

	// All clients should receive.
	var wg sync.WaitGroup
	for i, c := range conns {
		wg.Add(1)
		go func(idx int, conn *websocket.Conn) {
			defer wg.Done()
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, msg, err := conn.ReadMessage()
			assert.NoError(t, err, "client %d should receive message", idx)
			assert.Contains(t, string(msg), "Multi-client Test")
		}(i, c)
	}
	wg.Wait()
}

func TestWebSocket_DisconnectCleansUp(t *testing.T) {
	hub, srv := newTestHubAndServer(t)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/ws/feed"), nil)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, hub.ClientCount())

	// Disconnect.
	conn.Close()
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 0, hub.ClientCount())
}

func TestWebSocket_BroadcastAlert(t *testing.T) {
	hub, srv := newTestHubAndServer(t)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/ws/feed"), nil)
	require.NoError(t, err)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	alertData := map[string]interface{}{
		"title":       "Breaking News",
		"spike_ratio": 5.0,
	}
	hub.BroadcastAlert("spike", alertData)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)

	var msg WSMessage
	require.NoError(t, json.Unmarshal(message, &msg))
	assert.Equal(t, "spike", msg.Type)
}

// ---------------------------------------------------------------------------
// parseEditFilter tests
// ---------------------------------------------------------------------------

func TestParseEditFilter(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		expect EditFilter
	}{
		{
			name:   "empty",
			query:  "/ws/feed",
			expect: EditFilter{},
		},
		{
			name:  "languages",
			query: "/ws/feed?languages=en,es,fr",
			expect: EditFilter{
				Languages: []string{"en", "es", "fr"},
			},
		},
		{
			name:  "exclude_bots",
			query: "/ws/feed?exclude_bots=true",
			expect: EditFilter{
				ExcludeBots: true,
			},
		},
		{
			name:  "page_pattern",
			query: "/ws/feed?page_pattern=^Go",
			expect: EditFilter{
				PagePattern: "^Go",
			},
		},
		{
			name:  "min_byte_change",
			query: "/ws/feed?min_byte_change=100",
			expect: EditFilter{
				MinByteChange: 100,
			},
		},
		{
			name:  "combined",
			query: "/ws/feed?languages=en&exclude_bots=true&min_byte_change=50",
			expect: EditFilter{
				Languages:     []string{"en"},
				ExcludeBots:   true,
				MinByteChange: 50,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.query, nil)
			f := parseEditFilter(req)

			assert.Equal(t, tc.expect.Languages, f.Languages)
			assert.Equal(t, tc.expect.ExcludeBots, f.ExcludeBots)
			assert.Equal(t, tc.expect.PagePattern, f.PagePattern)
			assert.Equal(t, tc.expect.MinByteChange, f.MinByteChange)
		})
	}
}

// ---------------------------------------------------------------------------
// extractIP tests
// ---------------------------------------------------------------------------

func TestExtractIP(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		remote   string
		expected string
	}{
		{
			name:     "X-Forwarded-For",
			headers:  map[string]string{"X-Forwarded-For": "1.2.3.4, 5.6.7.8"},
			remote:   "127.0.0.1:1234",
			expected: "1.2.3.4",
		},
		{
			name:     "X-Real-IP",
			headers:  map[string]string{"X-Real-IP": "10.0.0.1"},
			remote:   "127.0.0.1:1234",
			expected: "10.0.0.1",
		},
		{
			name:     "RemoteAddr",
			headers:  map[string]string{},
			remote:   "192.168.1.1:5678",
			expected: "192.168.1.1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remote
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			assert.Equal(t, tc.expected, extractIP(req))
		})
	}
}
