package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/models"
	"github.com/Agnikulu/WikiSurge/internal/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

var (
	indexerMetricsOnce sync.Once
	sharedIndexerMetrics *IndexerMetrics
)

// IndexerMetrics contains Prometheus metrics for the selective indexer
type IndexerMetrics struct {
	EditsReceived     prometheus.Counter
	EditsIndexed      *prometheus.CounterVec
	EditsSkipped      prometheus.Counter
	IndexErrors       prometheus.Counter
	BufferFullDrops   prometheus.Counter
	BatchesProcessed  prometheus.Counter
	BatchSize         prometheus.Histogram
	IndexingLatency   prometheus.Histogram
	DecisionLatency   prometheus.Histogram
}

// SelectiveIndexer implements selective Elasticsearch indexing based on page significance
type SelectiveIndexer struct {
	esClient     *storage.ElasticsearchClient
	strategy     *storage.IndexingStrategy
	config       *config.Config
	metrics      *IndexerMetrics
	logger       zerolog.Logger

	// Buffering
	indexBuffer  chan *models.EditDocument
	bufferSize   int

	// Batching
	batchSize      int
	flushInterval  time.Duration

	// Lifecycle
	stopCh   chan struct{}
	wg       sync.WaitGroup
	started  atomic.Bool

	// Drop tracking
	dropCount atomic.Int64
}

// NewSelectiveIndexer creates a new selective Elasticsearch indexer consumer
func NewSelectiveIndexer(
	esClient *storage.ElasticsearchClient,
	strategy *storage.IndexingStrategy,
	cfg *config.Config,
	logger zerolog.Logger,
) *SelectiveIndexer {
	indexerMetricsOnce.Do(func() {
		sharedIndexerMetrics = &IndexerMetrics{
			EditsReceived: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "indexer_edits_received_total",
				Help: "Total edits received by selective indexer",
			}),
			EditsIndexed: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "indexer_edits_indexed_total",
				Help: "Total edits indexed by selective indexer",
			}, []string{"reason"}),
			EditsSkipped: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "indexer_edits_skipped_total",
				Help: "Total edits skipped by selective indexer",
			}),
			IndexErrors: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "indexer_index_errors_total",
				Help: "Total indexing errors in selective indexer",
			}),
			BufferFullDrops: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "indexer_buffer_full_drops_total",
				Help: "Total documents dropped due to full index buffer",
			}),
			BatchesProcessed: prometheus.NewCounter(prometheus.CounterOpts{
				Name: "indexer_batches_processed_total",
				Help: "Total bulk index batches processed",
			}),
			BatchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
				Name:    "indexer_batch_size",
				Help:    "Number of documents per bulk index batch",
				Buckets: []float64{1, 10, 50, 100, 250, 500, 1000},
			}),
			IndexingLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
				Name:    "indexer_bulk_index_duration_seconds",
				Help:    "Time spent performing bulk index operations",
				Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
			}),
			DecisionLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
				Name:    "indexer_decision_duration_seconds",
				Help:    "Time spent making indexing decisions",
				Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10),
			}),
		}

		prometheus.MustRegister(
			sharedIndexerMetrics.EditsReceived,
			sharedIndexerMetrics.EditsIndexed,
			sharedIndexerMetrics.EditsSkipped,
			sharedIndexerMetrics.IndexErrors,
			sharedIndexerMetrics.BufferFullDrops,
			sharedIndexerMetrics.BatchesProcessed,
			sharedIndexerMetrics.BatchSize,
			sharedIndexerMetrics.IndexingLatency,
			sharedIndexerMetrics.DecisionLatency,
		)
	})

	bufferSize := 1000
	batchSize := 500

	indexer := &SelectiveIndexer{
		esClient:      esClient,
		strategy:      strategy,
		config:        cfg,
		metrics:       sharedIndexerMetrics,
		logger:        logger.With().Str("component", "selective-indexer").Logger(),
		indexBuffer:   make(chan *models.EditDocument, bufferSize),
		bufferSize:    bufferSize,
		batchSize:     batchSize,
		flushInterval: 5 * time.Second,
		stopCh:        make(chan struct{}),
	}

	return indexer
}

// NewSelectiveIndexerForTest creates an indexer for testing without metrics registration
func NewSelectiveIndexerForTest(
	esClient *storage.ElasticsearchClient,
	strategy *storage.IndexingStrategy,
	cfg *config.Config,
	logger zerolog.Logger,
) *SelectiveIndexer {
	testMetrics := &IndexerMetrics{
		EditsReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "test_indexer_edits_received_total",
			Help: "Total edits received by selective indexer",
		}),
		EditsIndexed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "test_indexer_edits_indexed_total",
			Help: "Total edits indexed by selective indexer",
		}, []string{"reason"}),
		EditsSkipped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "test_indexer_edits_skipped_total",
			Help: "Total edits skipped by selective indexer",
		}),
		IndexErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "test_indexer_index_errors_total",
			Help: "Total indexing errors in selective indexer",
		}),
		BufferFullDrops: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "test_indexer_buffer_full_drops_total",
			Help: "Total documents dropped due to full index buffer",
		}),
		BatchesProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "test_indexer_batches_processed_total",
			Help: "Total bulk index batches processed",
		}),
		BatchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "test_indexer_batch_size",
			Help:    "Number of documents per bulk index batch",
			Buckets: []float64{1, 10, 50, 100, 250, 500, 1000},
		}),
		IndexingLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "test_indexer_bulk_index_duration_seconds",
			Help:    "Time spent performing bulk index operations",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 10),
		}),
		DecisionLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "test_indexer_decision_duration_seconds",
			Help:    "Time spent making indexing decisions",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10),
		}),
	}

	return &SelectiveIndexer{
		esClient:      esClient,
		strategy:      strategy,
		config:        cfg,
		metrics:       testMetrics,
		logger:        logger.With().Str("component", "selective-indexer").Logger(),
		indexBuffer:   make(chan *models.EditDocument, 1000),
		bufferSize:    1000,
		batchSize:     500,
		flushInterval: 5 * time.Second,
		stopCh:        make(chan struct{}),
	}
}

// ProcessEdit implements the kafka.MessageHandler interface.
// It decides whether an edit should be indexed and buffers it if so.
func (si *SelectiveIndexer) ProcessEdit(ctx context.Context, edit *models.WikipediaEdit) error {
	si.metrics.EditsReceived.Inc()

	// Make indexing decision
	decisionStart := time.Now()
	decision, err := si.strategy.ShouldIndex(ctx, edit)
	si.metrics.DecisionLatency.Observe(time.Since(decisionStart).Seconds())

	if err != nil {
		si.logger.Error().Err(err).Str("title", edit.Title).Msg("Error making indexing decision")
		// Don't fail the consumer on decision errors; skip the edit
		si.metrics.EditsSkipped.Inc()
		return nil
	}

	// Update indexing stats in Redis
	if err := si.strategy.UpdateIndexingStats(ctx, decision); err != nil {
		si.logger.Warn().Err(err).Msg("Failed to update indexing stats")
		// Non-fatal, continue processing
	}

	if !decision.ShouldIndex {
		si.metrics.EditsSkipped.Inc()
		si.logger.Debug().
			Str("title", edit.Title).
			Str("reason", decision.Reason).
			Msg("Edit skipped for indexing")
		return nil
	}

	// Transform edit to EditDocument
	doc := models.FromWikipediaEdit(edit, decision.Reason)
	if doc == nil {
		si.metrics.IndexErrors.Inc()
		si.logger.Error().Str("title", edit.Title).Msg("Failed to transform edit to document")
		return nil
	}

	// Non-blocking send to index buffer
	select {
	case si.indexBuffer <- doc:
		si.metrics.EditsIndexed.WithLabelValues(decision.Reason).Inc()
		si.logger.Debug().
			Str("title", edit.Title).
			Str("reason", decision.Reason).
			Str("doc_id", doc.ID).
			Msg("Edit queued for indexing")
	default:
		// Buffer full — drop the document to avoid blocking the Kafka consumer
		si.metrics.BufferFullDrops.Inc()
		drops := si.dropCount.Add(1)
		if drops%100 == 0 {
			si.logger.Warn().
				Int64("total_drops", drops).
				Msg("Index buffer full, documents being dropped")
		}
	}

	return nil
}

// ProcessMessage implements the message handler interface for raw Kafka messages
func (si *SelectiveIndexer) ProcessMessage(message []byte) error {
	var edit models.WikipediaEdit
	if err := json.Unmarshal(message, &edit); err != nil {
		si.metrics.IndexErrors.Inc()
		return fmt.Errorf("failed to unmarshal edit: %w", err)
	}

	return si.ProcessEdit(context.Background(), &edit)
}

// Start begins the background bulk indexer goroutine
func (si *SelectiveIndexer) Start() {
	if si.started.CompareAndSwap(false, true) {
		si.wg.Add(1)
		go si.startBulkIndexer()
		si.logger.Info().
			Int("buffer_size", si.bufferSize).
			Int("batch_size", si.batchSize).
			Dur("flush_interval", si.flushInterval).
			Msg("Selective indexer bulk processor started")
	}
}

// Stop gracefully stops the bulk indexer, flushing remaining documents
func (si *SelectiveIndexer) Stop() {
	if si.started.CompareAndSwap(true, false) {
		close(si.stopCh)
		si.wg.Wait()
		si.logger.Info().Msg("Selective indexer stopped")
	}
}

// startBulkIndexer runs the background goroutine that accumulates and bulk-indexes documents
func (si *SelectiveIndexer) startBulkIndexer() {
	defer si.wg.Done()

	ticker := time.NewTicker(si.flushInterval)
	defer ticker.Stop()

	batch := make([]*models.EditDocument, 0, si.batchSize)

	for {
		select {
		case doc := <-si.indexBuffer:
			batch = append(batch, doc)
			// Flush immediately when batch reaches target size
			if len(batch) >= si.batchSize {
				si.performBulkIndex(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			// Periodic flush for partially-filled batches
			if len(batch) > 0 {
				si.performBulkIndex(batch)
				batch = batch[:0]
			}

		case <-si.stopCh:
			// Drain remaining items from buffer
			draining := true
			for draining {
				select {
				case doc := <-si.indexBuffer:
					batch = append(batch, doc)
				default:
					draining = false
				}
			}
			// Final flush
			if len(batch) > 0 {
				si.performBulkIndex(batch)
			}
			return
		}
	}
}

// performBulkIndex indexes a batch of documents to Elasticsearch
func (si *SelectiveIndexer) performBulkIndex(docs []*models.EditDocument) {
	if len(docs) == 0 {
		return
	}

	start := time.Now()
	batchLen := len(docs)

	si.metrics.BatchSize.Observe(float64(batchLen))

	// If ES client is nil, log and discard (useful in tests or when ES is disabled)
	if si.esClient == nil {
		duration := time.Since(start)
		si.metrics.IndexingLatency.Observe(duration.Seconds())
		si.metrics.BatchesProcessed.Inc()
		si.logger.Debug().
			Int("batch_size", batchLen).
			Dur("duration", duration).
			Msg("Bulk index batch discarded (no ES client)")
		return
	}

	// Send each document to the ES client's bulk buffer
	var indexErrors int
	for _, doc := range docs {
		if err := si.esClient.IndexDocument(doc); err != nil {
			indexErrors++
			si.logger.Debug().
				Err(err).
				Str("doc_id", doc.ID).
				Msg("Failed to send document to ES bulk buffer")
		}
	}

	duration := time.Since(start)
	si.metrics.IndexingLatency.Observe(duration.Seconds())
	si.metrics.BatchesProcessed.Inc()

	if indexErrors > 0 {
		si.metrics.IndexErrors.Add(float64(indexErrors))
		si.logger.Warn().
			Int("batch_size", batchLen).
			Int("errors", indexErrors).
			Dur("duration", duration).
			Msg("Bulk index batch completed with errors")
	} else {
		si.logger.Debug().
			Int("batch_size", batchLen).
			Dur("duration", duration).
			Msg("Bulk index batch completed")
	}
}

// GetMetrics returns the indexer metrics for external inspection
func (si *SelectiveIndexer) GetMetrics() *IndexerMetrics {
	return si.metrics
}

// BufferLen returns the current number of documents in the index buffer
func (si *SelectiveIndexer) BufferLen() int {
	return len(si.indexBuffer)
}
