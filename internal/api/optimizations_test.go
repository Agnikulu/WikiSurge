package api

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Buffer pool
// ---------------------------------------------------------------------------

func TestGetPutBuffer(t *testing.T) {
	buf := getBuffer()
	require.NotNil(t, buf)
	assert.Equal(t, 0, buf.Len())

	buf.WriteString("hello")
	putBuffer(buf)

	// Next get should return a reset buffer (may or may not be same instance)
	buf2 := getBuffer()
	assert.Equal(t, 0, buf2.Len())
	putBuffer(buf2)
}

func TestPutBuffer_LargeBufferNotPooled(t *testing.T) {
	buf := getBuffer()
	// Grow > 1MB so it's discarded
	buf.Grow(1 << 21)
	putBuffer(buf) // Should not panic
}

// ---------------------------------------------------------------------------
// Trending response pool
// ---------------------------------------------------------------------------

func TestGetPutTrendingSlice(t *testing.T) {
	s := getTrendingSlice()
	require.NotNil(t, s)
	assert.Empty(t, *s)

	*s = append(*s, TrendingPageResponse{Title: "x"})
	putTrendingSlice(s)

	// Get again → should be empty (length reset)
	s2 := getTrendingSlice()
	assert.Empty(t, *s2)
	putTrendingSlice(s2)
}

// ---------------------------------------------------------------------------
// respondJSONPooled
// ---------------------------------------------------------------------------

func TestRespondJSONPooled_Success(t *testing.T) {
	rec := httptest.NewRecorder()

	respondJSONPooled(
		rec,
		200,
		rec.Header(),
		rec.WriteHeader,
		map[string]string{"status": "ok"},
	)

	assert.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Body.String(), `"status":"ok"`)
}

func TestRespondJSONPooled_EncodeError(t *testing.T) {
	rec := httptest.NewRecorder()

	// Functions are not JSON-serializable
	respondJSONPooled(
		rec,
		200,
		rec.Header(),
		rec.WriteHeader,
		func() {},
	)

	assert.Equal(t, 500, rec.Code)
}

// ---------------------------------------------------------------------------
// cachedExtractLanguage
// ---------------------------------------------------------------------------

func TestCachedExtractLanguage(t *testing.T) {
	// Clear any existing cache entry
	languageCache.Delete("en.wikipedia.org:Test")

	lang1 := cachedExtractLanguage("en.wikipedia.org:Test")
	lang2 := cachedExtractLanguage("en.wikipedia.org:Test") // from cache

	assert.Equal(t, lang1, lang2)
}

// ---------------------------------------------------------------------------
// DefaultBatchConfig
// ---------------------------------------------------------------------------

func TestDefaultBatchConfig(t *testing.T) {
	cfg := DefaultBatchConfig()
	assert.Equal(t, 100, cfg.MaxBatchSize)
	assert.Equal(t, 500, cfg.FlushInterval)
}

// ---------------------------------------------------------------------------
// Buffer pool reuse
// ---------------------------------------------------------------------------

func TestBufferPoolReuse(t *testing.T) {
	for i := 0; i < 100; i++ {
		buf := getBuffer()
		buf.WriteString("test data")
		putBuffer(buf)
	}
	// No panic, no leak
	buf := getBuffer()
	assert.Equal(t, 0, buf.Len())
	putBuffer(buf)
}

// ---------------------------------------------------------------------------
// respondJSONPooled — no HTML escaping
// ---------------------------------------------------------------------------

func TestRespondJSONPooled_NoHTMLEscape(t *testing.T) {
	rec := httptest.NewRecorder()

	respondJSONPooled(
		rec,
		200,
		rec.Header(),
		rec.WriteHeader,
		map[string]string{"html": "<b>bold</b>"},
	)

	body := rec.Body.String()
	// Without HTML escaping, < and > should be literal (not \u003c and \u003e)
	assert.Contains(t, body, "<b>bold</b>")
	assert.NotContains(t, body, `\u003c`)
}

// ---------------------------------------------------------------------------
// getBuffer Reset
// ---------------------------------------------------------------------------

func TestGetBuffer_IsReset(t *testing.T) {
	buf := getBuffer()
	buf.WriteString("leftover")
	putBuffer(buf)

	buf2 := getBuffer()
	// Must be reset — should not have leftover content
	assert.Equal(t, 0, buf2.Len())
	putBuffer(buf2)
}

// Write a helper to verify buffer used is different from bytes.Buffer{}
func TestBufferPool_ConcurrentSafe(t *testing.T) {
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				buf := getBuffer()
				buf.WriteString("test")
				putBuffer(buf)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// Make sure respondJSONPooled writes bytes correctly
func TestRespondJSONPooled_WritesCompleteJSON(t *testing.T) {
	var buf bytes.Buffer
	var headers = make(fakeHeader)
	var statusCode int

	respondJSONPooled(
		&buf,
		201,
		headers,
		func(code int) { statusCode = code },
		[]int{1, 2, 3},
	)

	assert.Equal(t, 201, statusCode)
	assert.Equal(t, "application/json; charset=utf-8", headers.Get("Content-Type"))
	assert.Contains(t, buf.String(), "[1,2,3]")
}

// fakeHeader is a minimal http.Header-like type for testing.
type fakeHeader map[string]string

func (h fakeHeader) Set(key, val string) { h[key] = val }
func (h fakeHeader) Get(key string) string { return h[key] }
