package api

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = 30 * time.Second

	// Maximum message size allowed from peer.
	maxMessageSize = 4096

	// Default max concurrent WebSocket clients.
	defaultMaxClients = 100

	// Default max connections per IP.
	defaultMaxPerIP = 5

	// Client send channel buffer size.
	sendBufferSize = 256

	// Stale connection timeout.
	staleTimeout = 60 * time.Second
)

// upgrader is the gorilla/websocket upgrader shared across handlers.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// CheckOrigin allows connections from any origin (configure for prod).
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ---------------------------------------------------------------------------
// EditFilter — per-client message filter
// ---------------------------------------------------------------------------

// EditFilter controls which edits are forwarded to a WebSocket client.
type EditFilter struct {
	Languages    []string `json:"languages,omitempty"`
	ExcludeBots  bool     `json:"exclude_bots,omitempty"`
	PagePattern  string   `json:"page_pattern,omitempty"`
	MinByteChange int     `json:"min_byte_change,omitempty"`

	compiledPattern *regexp.Regexp
}

// Matches returns true if the edit passes all filter criteria.
func (f *EditFilter) Matches(edit *models.WikipediaEdit) bool {
	// Bot filter
	if f.ExcludeBots && edit.Bot {
		return false
	}

	// Language filter
	if len(f.Languages) > 0 {
		lang := edit.Language()
		found := false
		for _, l := range f.Languages {
			if strings.EqualFold(l, lang) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Page title pattern filter
	if f.PagePattern != "" {
		if f.compiledPattern == nil {
			// Compile lazily; swallow bad patterns (match everything).
			p, err := regexp.Compile(f.PagePattern)
			if err == nil {
				f.compiledPattern = p
			}
		}
		if f.compiledPattern != nil && !f.compiledPattern.MatchString(edit.Title) {
			return false
		}
	}

	// Minimum byte change filter
	if f.MinByteChange > 0 {
		abs := edit.ByteChange()
		if abs < 0 {
			abs = -abs
		}
		if abs < f.MinByteChange {
			return false
		}
	}

	return true
}

// ---------------------------------------------------------------------------
// Client — a single WebSocket connection
// ---------------------------------------------------------------------------

// Client represents a connected WebSocket client.
type Client struct {
	hub         *WebSocketHub
	conn        *websocket.Conn
	send        chan []byte
	filter      *EditFilter
	id          string
	connectedAt time.Time
	remoteAddr  string
}

// readPump reads messages from the WebSocket connection.
// It runs as a goroutine per client and is mainly used for ping/pong.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
			) {
				c.hub.logger.Debug().Err(err).Str("client", c.id).Msg("WebSocket read error")
			}
			break
		}
		// Reset read deadline on any message.
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	}
}

// writePump sends messages from the send channel to the WebSocket connection.
// Runs as a goroutine per client.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Drain queued messages into the current write to reduce syscalls.
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte("\n"))
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// WebSocketHub — manages all connected clients
// ---------------------------------------------------------------------------

// WebSocketHub maintains active WebSocket clients and broadcasts messages.
type WebSocketHub struct {
	// Registered clients.
	clients map[*Client]bool

	// Channel for broadcast messages.
	broadcast chan []byte

	// Register channel for new clients.
	register chan *Client

	// Unregister channel for disconnecting clients.
	unregister chan *Client

	// Maximum concurrent connections.
	maxClients int

	// Maximum connections per IP address.
	maxPerIP int

	// Mutex for thread-safe client map access.
	mu sync.RWMutex

	// Logger
	logger zerolog.Logger

	// stop channel to shut down the hub.
	stop chan struct{}
}

// NewWebSocketHub creates and returns a new WebSocketHub.
func NewWebSocketHub(logger zerolog.Logger) *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		maxClients: defaultMaxClients,
		maxPerIP:   defaultMaxPerIP,
		logger:     logger.With().Str("component", "websocket-hub").Logger(),
		stop:       make(chan struct{}),
	}
}

// SetMaxClients configures the global maximum number of WebSocket clients.
func (h *WebSocketHub) SetMaxClients(max int) {
	if max > 0 {
		h.maxClients = max
	}
}

// SetMaxPerIP configures the per-IP connection limit.
func (h *WebSocketHub) SetMaxPerIP(max int) {
	if max > 0 {
		h.maxPerIP = max
	}
}

// ClientCount returns the number of currently connected clients.
func (h *WebSocketHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Run is the main event loop for the hub. Start it as a goroutine.
func (h *WebSocketHub) Run() {
	ticker := time.NewTicker(pingPeriod)
	staleTicker := time.NewTicker(staleTimeout)
	defer ticker.Stop()
	defer staleTicker.Stop()

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			// Check global limit.
			if len(h.clients) >= h.maxClients {
				h.mu.Unlock()
				h.logger.Warn().
					Str("client", client.id).
					Int("current", len(h.clients)).
					Int("max", h.maxClients).
					Msg("Max clients reached, rejecting connection")
				_ = client.conn.WriteMessage(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "max connections reached"),
				)
				client.conn.Close()
				continue
			}
			// Check per-IP limit.
			ipCount := 0
			for c := range h.clients {
				if c.remoteAddr == client.remoteAddr {
					ipCount++
				}
			}
			if ipCount >= h.maxPerIP {
				h.mu.Unlock()
				h.logger.Warn().
					Str("client", client.id).
					Str("ip", client.remoteAddr).
					Int("ip_count", ipCount).
					Msg("Per-IP limit reached, rejecting connection")
				_ = client.conn.WriteMessage(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "per-IP limit reached"),
				)
				client.conn.Close()
				continue
			}

			h.clients[client] = true
			h.mu.Unlock()

			metrics.WebSocketConnectionsTotal.With(nil).Inc()
			metrics.WebSocketConnectionsActive.With(nil).Set(float64(len(h.clients)))

			h.logger.Info().
				Str("client", client.id).
				Str("ip", client.remoteAddr).
				Int("total", len(h.clients)).
				Msg("Client connected")

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				metrics.WebSocketDisconnectionsTotal.With(nil).Inc()
				metrics.WebSocketConnectionsActive.With(nil).Set(float64(len(h.clients)))
				h.logger.Info().
					Str("client", client.id).
					Int("total", len(h.clients)).
					Msg("Client disconnected")
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Send buffer full — disconnect slow client.
					h.mu.RUnlock()
					h.mu.Lock()
					if _, ok := h.clients[client]; ok {
						delete(h.clients, client)
						close(client.send)
						metrics.WebSocketDisconnectionsTotal.With(nil).Inc()
						metrics.WebSocketConnectionsActive.With(nil).Set(float64(len(h.clients)))
						h.logger.Warn().
							Str("client", client.id).
							Msg("Slow client disconnected (send buffer full)")
					}
					h.mu.Unlock()
					h.mu.RLock()
				}
			}
			h.mu.RUnlock()

		case <-staleTicker.C:
			h.cleanupStaleConnections()

		case <-h.stop:
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				client.conn.Close()
				delete(h.clients, client)
			}
			h.mu.Unlock()
			h.logger.Info().Msg("WebSocket hub stopped")
			return
		}
	}
}

// Stop shuts down the hub gracefully.
func (h *WebSocketHub) Stop() {
	close(h.stop)
}

// cleanupStaleConnections removes clients that have been idle too long.
func (h *WebSocketHub) cleanupStaleConnections() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	for client := range h.clients {
		// Connections older than staleTimeout with no recent pong are cleaned.
		// The pong handler resets the read deadline; if the connection is truly
		// stale the readPump will exit on its own. This is a safety net.
		if now.Sub(client.connectedAt) > staleTimeout {
			// Send a ping; if it fails the client is truly dead.
			_ = client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				delete(h.clients, client)
				close(client.send)
				client.conn.Close()
				metrics.WebSocketDisconnectionsTotal.With(nil).Inc()
				h.logger.Info().
					Str("client", client.id).
					Msg("Stale connection cleaned up")
			}
		}
	}
	metrics.WebSocketConnectionsActive.With(nil).Set(float64(len(h.clients)))
}

// ---------------------------------------------------------------------------
// WebSocket message envelope
// ---------------------------------------------------------------------------

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// ---------------------------------------------------------------------------
// BroadcastEdit — called by the processor to push edits to clients
// ---------------------------------------------------------------------------

// BroadcastEdit serializes an edit and sends it to all matching clients.
func (h *WebSocketHub) BroadcastEdit(edit *models.WikipediaEdit) {
	msg := WSMessage{
		Type: "edit",
		Data: edit,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to marshal edit for WebSocket broadcast")
		return
	}

	// Non-blocking send to broadcast channel.
	select {
	case h.broadcast <- data:
	default:
		h.logger.Warn().Msg("Broadcast channel full, dropping message")
	}
}

// BroadcastAlert sends an alert message to all connected clients.
func (h *WebSocketHub) BroadcastAlert(alertType string, data interface{}) {
	msg := WSMessage{
		Type: alertType,
		Data: data,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to marshal alert for WebSocket broadcast")
		return
	}

	select {
	case h.broadcast <- payload:
	default:
		h.logger.Warn().Msg("Broadcast channel full, dropping alert")
	}
}

// ---------------------------------------------------------------------------
// HTTP handler — /ws/feed
// ---------------------------------------------------------------------------

// WebSocketFeed upgrades an HTTP connection to WebSocket and streams edits.
//
// Query parameters:
//
//	languages      — comma-separated language codes (e.g. "en,es,fr")
//	exclude_bots   — "true" to exclude bot edits
//	page_pattern   — regex pattern for page titles
//	min_byte_change — minimum absolute byte change size
func (s *APIServer) WebSocketFeed(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error().Err(err).Msg("WebSocket upgrade failed")
		return
	}

	// Parse filter from query string.
	filter := parseEditFilter(r)

	client := &Client{
		hub:         s.wsHub,
		conn:        conn,
		send:        make(chan []byte, sendBufferSize),
		filter:      filter,
		id:          uuid.New().String(),
		connectedAt: time.Now(),
		remoteAddr:  extractIP(r),
	}

	client.hub.register <- client

	// Start read/write pumps.
	go client.writePump()
	go client.readPump()
}

// ---------------------------------------------------------------------------
// Filter helpers
// ---------------------------------------------------------------------------

// parseEditFilter builds an EditFilter from HTTP query parameters.
func parseEditFilter(r *http.Request) *EditFilter {
	q := r.URL.Query()
	f := &EditFilter{}

	if langs := q.Get("languages"); langs != "" {
		for _, l := range strings.Split(langs, ",") {
			l = strings.TrimSpace(l)
			if l != "" {
				f.Languages = append(f.Languages, l)
			}
		}
	}

	f.ExcludeBots = parseBoolQuery(r, "exclude_bots", false)

	if pattern := q.Get("page_pattern"); pattern != "" {
		f.PagePattern = pattern
	}

	if raw := q.Get("min_byte_change"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			f.MinByteChange = v
		}
	}

	return f
}

// extractIP returns the client IP, respecting X-Forwarded-For.
func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// Strip port from RemoteAddr.
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// ---------------------------------------------------------------------------
// Broadcast filtering — overrides basic broadcast for filtered delivery
// ---------------------------------------------------------------------------

// BroadcastEditFiltered sends an edit only to clients whose filter matches.
func (h *WebSocketHub) BroadcastEditFiltered(edit *models.WikipediaEdit) {
	msg := WSMessage{
		Type: "edit",
		Data: edit,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to marshal edit for filtered broadcast")
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		if client.filter != nil && !client.filter.Matches(edit) {
			continue
		}
		select {
		case client.send <- data:
		default:
			// Non-blocking: will be cleaned up by the hub's main loop.
			h.logger.Warn().Str("client", client.id).Msg("Slow client during filtered broadcast")
		}
	}
}

// absInt returns the absolute value of n.
func absInt(n int) int {
	return int(math.Abs(float64(n)))
}
