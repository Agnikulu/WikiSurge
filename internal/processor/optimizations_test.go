package processor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock broadcaster
// ---------------------------------------------------------------------------

type mockBroadcaster struct {
	edits []*models.WikipediaEdit
}

func (m *mockBroadcaster) BroadcastEditFiltered(edit *models.WikipediaEdit) {
	m.edits = append(m.edits, edit)
}

// ---------------------------------------------------------------------------
// WebSocketForwarder
// ---------------------------------------------------------------------------

func TestNewWebSocketForwarder(t *testing.T) {
	f := NewWebSocketForwarder(&mockBroadcaster{}, nil, zerolog.Nop())
	require.NotNil(t, f)
}

func TestWebSocketForwarder_ProcessEdit_BroadcastOnly(t *testing.T) {
	bc := &mockBroadcaster{}
	f := NewWebSocketForwarder(bc, nil, zerolog.Nop())

	edit := &models.WikipediaEdit{Title: "TestPage", Wiki: "enwiki"}
	err := f.ProcessEdit(context.Background(), edit)
	require.NoError(t, err)
	require.Len(t, bc.edits, 1)
	assert.Equal(t, "TestPage", bc.edits[0].Title)
}

func TestWebSocketForwarder_ProcessEdit_WithRedis(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rc.Close()

	bc := &mockBroadcaster{}
	f := NewWebSocketForwarder(bc, rc, zerolog.Nop())

	edit := &models.WikipediaEdit{Title: "RedisPage", Wiki: "enwiki", ID: 42}
	err = f.ProcessEdit(context.Background(), edit)
	require.NoError(t, err)
	assert.Len(t, bc.edits, 1)
	// Redis publish happened (we can't easily verify in miniredis, but no error)
}

// ---------------------------------------------------------------------------
// Object pool: GetEdit / PutEdit
// ---------------------------------------------------------------------------

func TestGetEdit_ReturnsZeroedEdit(t *testing.T) {
	edit := GetEdit()
	require.NotNil(t, edit)
	assert.Equal(t, int64(0), edit.ID)
	assert.Equal(t, "", edit.Title)

	// Put it back
	PutEdit(edit)
}

func TestPutEdit_NilSafe(t *testing.T) {
	// Should not panic
	PutEdit(nil)
}

func TestGetEdit_RecycledEdit(t *testing.T) {
	// Get, set, put, get again — should be zeroed
	e1 := GetEdit()
	e1.ID = 999
	e1.Title = "ShouldBeCleared"
	PutEdit(e1)

	e2 := GetEdit()
	assert.Equal(t, int64(0), e2.ID)
	assert.Equal(t, "", e2.Title)
	PutEdit(e2)
}

// ---------------------------------------------------------------------------
// UnmarshalEditPooled
// ---------------------------------------------------------------------------

func TestUnmarshalEditPooled_Valid(t *testing.T) {
	edit := &models.WikipediaEdit{ID: 100, Title: "Test", Wiki: "enwiki"}
	data, _ := json.Marshal(edit)

	result, err := UnmarshalEditPooled(data)
	require.NoError(t, err)
	assert.Equal(t, int64(100), result.ID)
	assert.Equal(t, "Test", result.Title)
	PutEdit(result)
}

func TestUnmarshalEditPooled_Invalid(t *testing.T) {
	result, err := UnmarshalEditPooled([]byte("invalid"))
	require.Error(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// BatchProcessor
// ---------------------------------------------------------------------------

func TestBatchProcessor_FlushOnSize(t *testing.T) {
	var processed [][]*models.WikipediaEdit
	bp := NewBatchProcessor(3, 0, func(batch []*models.WikipediaEdit) error {
		processed = append(processed, batch)
		return nil
	})

	bp.Add(&models.WikipediaEdit{ID: 1})
	bp.Add(&models.WikipediaEdit{ID: 2})
	assert.Empty(t, processed)

	bp.Add(&models.WikipediaEdit{ID: 3})
	require.Len(t, processed, 1)
	assert.Len(t, processed[0], 3)
}

func TestBatchProcessor_FlushExplicit(t *testing.T) {
	var processed [][]*models.WikipediaEdit
	bp := NewBatchProcessor(10, 0, func(batch []*models.WikipediaEdit) error {
		processed = append(processed, batch)
		return nil
	})

	bp.Add(&models.WikipediaEdit{ID: 1})
	bp.Add(&models.WikipediaEdit{ID: 2})

	bp.Flush()
	require.Len(t, processed, 1)
	assert.Len(t, processed[0], 2)
}

func TestBatchProcessor_FlushEmpty(t *testing.T) {
	var count int
	bp := NewBatchProcessor(10, 0, func(batch []*models.WikipediaEdit) error {
		count++
		return nil
	})

	bp.Flush()
	assert.Equal(t, 0, count) // no-op on empty batch
}

func TestBatchProcessor_Stop(t *testing.T) {
	var processed [][]*models.WikipediaEdit
	bp := NewBatchProcessor(100, 0, func(batch []*models.WikipediaEdit) error {
		processed = append(processed, batch)
		return nil
	})

	bp.Add(&models.WikipediaEdit{ID: 1})
	bp.Stop()

	require.Len(t, processed, 1)

	// After stop, Add is no-op
	bp.Add(&models.WikipediaEdit{ID: 2})
	assert.Len(t, processed, 1)
}

func TestBatchProcessor_FlushOnTimeout(t *testing.T) {
	var processed [][]*models.WikipediaEdit
	var mu = make(chan struct{}, 1)
	bp := NewBatchProcessor(100, 100*time.Millisecond, func(batch []*models.WikipediaEdit) error {
		processed = append(processed, batch)
		mu <- struct{}{}
		return nil
	})

	bp.Add(&models.WikipediaEdit{ID: 1})

	// Wait for timer-triggered flush
	select {
	case <-mu:
		require.Len(t, processed, 1)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout-triggered flush did not fire")
	}
}

// ---------------------------------------------------------------------------
// ProcessingStats
// ---------------------------------------------------------------------------

func TestProcessingStats_Initial(t *testing.T) {
	ps := NewProcessingStats()
	edits, errs, avgMs := ps.Snapshot()
	assert.Equal(t, int64(0), edits)
	assert.Equal(t, int64(0), errs)
	assert.Equal(t, float64(0), avgMs)
}

func TestProcessingStats_RecordEdit(t *testing.T) {
	ps := NewProcessingStats()
	ps.RecordEdit(10.0)
	ps.RecordEdit(20.0)

	edits, _, avgMs := ps.Snapshot()
	assert.Equal(t, int64(2), edits)
	assert.Greater(t, avgMs, float64(0))
}

func TestProcessingStats_RecordError(t *testing.T) {
	ps := NewProcessingStats()
	ps.RecordError()
	ps.RecordError()

	_, errs, _ := ps.Snapshot()
	assert.Equal(t, int64(2), errs)
}

func TestProcessingStats_EMA(t *testing.T) {
	ps := NewProcessingStats()
	// With EMA factor 0.05, the first observation contributes only 5%
	ps.RecordEdit(100.0)
	_, _, avg1 := ps.Snapshot()
	assert.InDelta(t, 5.0, avg1, 0.1) // 0*0.95 + 100*0.05 = 5.0

	ps.RecordEdit(100.0)
	_, _, avg2 := ps.Snapshot()
	assert.Greater(t, avg2, avg1)
}
