package processor

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/models"
)

// =============================================================================
// Object Pools — reduce GC pressure in the processing pipeline
// =============================================================================

// editPool pools WikipediaEdit objects to reduce allocations during
// high-throughput message processing.
var editPool = sync.Pool{
	New: func() interface{} {
		return &models.WikipediaEdit{}
	},
}

// GetEdit retrieves a WikipediaEdit from the pool.
func GetEdit() *models.WikipediaEdit {
	edit := editPool.Get().(*models.WikipediaEdit)
	*edit = models.WikipediaEdit{} // zero it
	return edit
}

// PutEdit returns a WikipediaEdit to the pool.
func PutEdit(edit *models.WikipediaEdit) {
	if edit == nil {
		return
	}
	editPool.Put(edit)
}

// UnmarshalEditPooled unmarshals JSON into a pooled WikipediaEdit object.
// The caller should call PutEdit when done with the edit.
func UnmarshalEditPooled(data []byte) (*models.WikipediaEdit, error) {
	edit := GetEdit()
	if err := json.Unmarshal(data, edit); err != nil {
		PutEdit(edit)
		return nil, err
	}
	return edit, nil
}

// =============================================================================
// Batch Processor — batches edits for more efficient processing
// =============================================================================

// BatchProcessor collects edits and processes them in batches.
type BatchProcessor struct {
	mu           sync.Mutex
	batch        []*models.WikipediaEdit
	maxSize      int
	flushTimeout time.Duration
	processFn    func([]*models.WikipediaEdit) error
	timer        *time.Timer
	stopped      bool
}

// NewBatchProcessor creates a new batch processor.
func NewBatchProcessor(maxSize int, flushTimeout time.Duration, processFn func([]*models.WikipediaEdit) error) *BatchProcessor {
	bp := &BatchProcessor{
		batch:        make([]*models.WikipediaEdit, 0, maxSize),
		maxSize:      maxSize,
		flushTimeout: flushTimeout,
		processFn:    processFn,
	}
	return bp
}

// Add adds an edit to the current batch. If the batch is full, it is flushed.
func (bp *BatchProcessor) Add(edit *models.WikipediaEdit) error {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if bp.stopped {
		return nil
	}

	bp.batch = append(bp.batch, edit)

	// Start flush timer on first item
	if len(bp.batch) == 1 && bp.flushTimeout > 0 {
		bp.timer = time.AfterFunc(bp.flushTimeout, func() {
			bp.Flush()
		})
	}

	// Flush if batch is full
	if len(bp.batch) >= bp.maxSize {
		return bp.flushLocked()
	}

	return nil
}

// Flush processes the current batch immediately.
func (bp *BatchProcessor) Flush() error {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	return bp.flushLocked()
}

func (bp *BatchProcessor) flushLocked() error {
	if len(bp.batch) == 0 {
		return nil
	}

	if bp.timer != nil {
		bp.timer.Stop()
		bp.timer = nil
	}

	// Process the batch
	batch := bp.batch
	bp.batch = make([]*models.WikipediaEdit, 0, bp.maxSize)

	if bp.processFn != nil {
		return bp.processFn(batch)
	}
	return nil
}

// Stop stops the batch processor and flushes remaining items.
func (bp *BatchProcessor) Stop() error {
	bp.mu.Lock()
	bp.stopped = true
	bp.mu.Unlock()
	return bp.Flush()
}

// =============================================================================
// Processing Metrics Cache — avoid expensive metric lookups
// =============================================================================

// ProcessingStats holds cached processing statistics.
type ProcessingStats struct {
	mu                sync.RWMutex
	editsProcessed    int64
	errorsEncountered int64
	avgProcessingMs   float64
	lastUpdated       time.Time
}

// NewProcessingStats creates a new processing stats tracker.
func NewProcessingStats() *ProcessingStats {
	return &ProcessingStats{
		lastUpdated: time.Now(),
	}
}

// RecordEdit records a processed edit with its processing duration.
func (ps *ProcessingStats) RecordEdit(processingMs float64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.editsProcessed++
	// Exponential moving average
	ps.avgProcessingMs = ps.avgProcessingMs*0.95 + processingMs*0.05
	ps.lastUpdated = time.Now()
}

// RecordError records a processing error.
func (ps *ProcessingStats) RecordError() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.errorsEncountered++
}

// Snapshot returns a point-in-time snapshot of processing stats.
func (ps *ProcessingStats) Snapshot() (edits, errors int64, avgMs float64) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.editsProcessed, ps.errorsEncountered, ps.avgProcessingMs
}
