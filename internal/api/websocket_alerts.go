package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// /ws/alerts — stream spike and edit-war alerts in real time
// ---------------------------------------------------------------------------

// WebSocketAlerts upgrades the connection and streams alerts from the shared
// AlertHub.  Unlike the old implementation, each client does NOT start its own
// Redis subscription — the AlertHub runs a single XRead loop and fans out.
//
// Route: WS /ws/alerts
func (s *APIServer) WebSocketAlerts(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error().Err(err).Msg("WebSocket alert upgrade failed")
		return
	}

	s.logger.Info().Str("remote", r.RemoteAddr).Msg("Alerts WebSocket client connected")

	// Subscribe to the shared alert hub (no extra Redis connection).
	alertCh := s.alertHub.Subscribe()

	done := make(chan struct{})

	// readPump — detect client disconnect.
	go func() {
		defer close(done)
		conn.SetReadLimit(maxMessageSize)
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}()

	// Ping ticker to keep the connection alive.
	pingTicker := time.NewTicker(pingPeriod)

	// Main write loop.
	go func() {
		defer func() {
			pingTicker.Stop()
			s.alertHub.Unsubscribe(alertCh)
			conn.Close()
		}()

		for {
			select {
			case alert := <-alertCh:
				msg := WSMessage{
					Type: alert.Type,
					Data: alert,
				}
				payload, err := json.Marshal(msg)
				if err != nil {
					s.logger.Error().Err(err).Msg("Failed to marshal alert")
					continue
				}
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
					s.logger.Debug().Err(err).Msg("Alert WS write error")
					return
				}

			case <-pingTicker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}

			case <-done:
				return
			}
		}
	}()
}
