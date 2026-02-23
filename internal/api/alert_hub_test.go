package api

import (
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAlertHub(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	require.NotNil(t, hub)
	assert.NotNil(t, hub.subscribers)
}

func TestAlertHub_Subscribe(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	ch := hub.Subscribe()
	require.NotNil(t, ch)

	hub.mu.RLock()
	assert.Len(t, hub.subscribers, 1)
	hub.mu.RUnlock()

	hub.Unsubscribe(ch)
	hub.mu.RLock()
	assert.Len(t, hub.subscribers, 0)
	hub.mu.RUnlock()
}

func TestAlertHub_MultipleSubscribers(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	ch1 := hub.Subscribe()
	ch2 := hub.Subscribe()
	ch3 := hub.Subscribe()

	hub.mu.RLock()
	assert.Len(t, hub.subscribers, 3)
	hub.mu.RUnlock()

	hub.Unsubscribe(ch2)
	hub.mu.RLock()
	assert.Len(t, hub.subscribers, 2)
	hub.mu.RUnlock()

	hub.Unsubscribe(ch1)
	hub.Unsubscribe(ch3)
	hub.mu.RLock()
	assert.Len(t, hub.subscribers, 0)
	hub.mu.RUnlock()
}

func TestAlertHub_Broadcast(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	ch1 := hub.Subscribe()
	ch2 := hub.Subscribe()

	alert := storage.Alert{
		ID:        "a1",
		Type:      storage.AlertTypeSpike,
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"page": "Go_(programming_language)"},
	}

	hub.broadcast(alert)

	select {
	case got := <-ch1:
		assert.Equal(t, "a1", got.ID)
		assert.Equal(t, storage.AlertTypeSpike, got.Type)
	case <-time.After(time.Second):
		t.Fatal("ch1 did not receive alert")
	}

	select {
	case got := <-ch2:
		assert.Equal(t, "a1", got.ID)
	case <-time.After(time.Second):
		t.Fatal("ch2 did not receive alert")
	}

	hub.Unsubscribe(ch1)
	hub.Unsubscribe(ch2)
}

func TestAlertHub_BroadcastDropsWhenFull(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	ch := hub.Subscribe() // buffer = 128

	// Fill the channel
	for i := 0; i < 128; i++ {
		hub.broadcast(storage.Alert{ID: "fill"})
	}

	// This broadcast should not block (drops silently)
	done := make(chan struct{})
	go func() {
		hub.broadcast(storage.Alert{ID: "overflow"})
		close(done)
	}()

	select {
	case <-done:
		// good — didn't block
	case <-time.After(time.Second):
		t.Fatal("broadcast blocked on full channel")
	}

	hub.Unsubscribe(ch)
}

func TestAlertHub_Stop(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())

	// Run in background — since alerts is nil it will just block on ctx.Done()
	go hub.Run()

	// Give goroutine time to start
	time.Sleep(50 * time.Millisecond)

	// Stop should not panic
	hub.Stop()
}

func TestAlertHub_StopBeforeRun(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	// cancel is nil — should not panic
	hub.Stop()
}

func TestAlertHub_RunWithNilAlerts(t *testing.T) {
	hub := NewAlertHub(nil, zerolog.Nop())
	done := make(chan struct{})
	go func() {
		hub.Run()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	hub.Stop()

	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after Stop")
	}
}
