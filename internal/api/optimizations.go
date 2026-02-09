package api

import (
	"bytes"
	"encoding/json"
	"sync"
)

// =============================================================================
// Object Pools — reduce GC pressure on hot paths
// =============================================================================

// bufferPool provides reusable byte buffers for JSON marshaling.
var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// getBuffer retrieves a buffer from the pool.
func getBuffer() *bytes.Buffer {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// putBuffer returns a buffer to the pool.
func putBuffer(buf *bytes.Buffer) {
	if buf.Cap() > 1<<20 { // Don't pool buffers > 1MB
		return
	}
	bufferPool.Put(buf)
}

// trendingResponsePool pools TrendingPageResponse slices.
var trendingResponsePool = sync.Pool{
	New: func() interface{} {
		s := make([]TrendingPageResponse, 0, 100)
		return &s
	},
}

// getTrendingSlice retrieves a pre-allocated trending response slice.
func getTrendingSlice() *[]TrendingPageResponse {
	s := trendingResponsePool.Get().(*[]TrendingPageResponse)
	*s = (*s)[:0]
	return s
}

// putTrendingSlice returns a trending response slice to the pool.
func putTrendingSlice(s *[]TrendingPageResponse) {
	trendingResponsePool.Put(s)
}

// =============================================================================
// Optimized JSON encoding — use encoder directly to stream
// =============================================================================

// respondJSONPooled writes a JSON response using a pooled buffer to reduce
// allocations on hot paths. Falls back to standard encoding on error.
func respondJSONPooled(w interface{ Write([]byte) (int, error) }, status int, header interface{ Set(string, string) }, writeHeader func(int), data interface{}) {
	buf := getBuffer()
	defer putBuffer(buf)

	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false) // Avoid unnecessary escaping for performance
	if err := enc.Encode(data); err != nil {
		// Fall back to standard path
		header.Set("Content-Type", "application/json; charset=utf-8")
		writeHeader(500)
		return
	}

	header.Set("Content-Type", "application/json; charset=utf-8")
	writeHeader(status)
	w.Write(buf.Bytes())
}

// =============================================================================
// Pre-computed values — avoid repeated computation
// =============================================================================

// languageCache caches extracted language codes from page titles.
// Since page titles don't change for a given entry, this is safe.
var languageCache sync.Map

// cachedExtractLanguage extracts and caches the language from a page title.
func cachedExtractLanguage(pageTitle string) string {
	if lang, ok := languageCache.Load(pageTitle); ok {
		return lang.(string)
	}
	lang := extractLanguage(pageTitle)
	languageCache.Store(pageTitle, lang)
	return lang
}

// =============================================================================
// Batch operations helper
// =============================================================================

// BatchConfig controls batch processing behavior.
type BatchConfig struct {
	// MaxBatchSize is the maximum number of items per batch.
	MaxBatchSize int
	// FlushInterval is how often to flush incomplete batches.
	FlushInterval int // in milliseconds
}

// DefaultBatchConfig returns sensible defaults for batch processing.
func DefaultBatchConfig() BatchConfig {
	return BatchConfig{
		MaxBatchSize:  100,
		FlushInterval: 500,
	}
}
