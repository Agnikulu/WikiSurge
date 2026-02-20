package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── HTML → plain-text conversion ───────────────────────────────────────────

func TestDiffHTMLToPlainText_AddedAndRemoved(t *testing.T) {
	html := `<tr>
		<td class="diff-deletedline"><div>Old content about <del class="diffchange diffchange-inline">cats</del></div></td>
		<td class="diff-addedline"><div>New content about <ins class="diffchange diffchange-inline">dogs</ins></div></td>
	</tr>`

	plain := diffHTMLToPlainText(html)

	assert.Contains(t, plain, "- REMOVED:")
	assert.Contains(t, plain, "\u00abcats\u00bb")
	assert.Contains(t, plain, "+ ADDED:")
	assert.Contains(t, plain, "\u00abdogs\u00bb")
}

func TestDiffHTMLToPlainText_OnlyAdded(t *testing.T) {
	html := `<tr>
		<td class="diff-addedline"><div>Brand new paragraph about Wikipedia policies.</div></td>
	</tr>`

	plain := diffHTMLToPlainText(html)

	assert.Contains(t, plain, "+ ADDED:")
	assert.Contains(t, plain, "Brand new paragraph about Wikipedia policies.")
	assert.NotContains(t, plain, "- REMOVED:")
}

func TestDiffHTMLToPlainText_OnlyRemoved(t *testing.T) {
	html := `<tr>
		<td class="diff-deletedline"><div>This paragraph was removed entirely.</div></td>
	</tr>`

	plain := diffHTMLToPlainText(html)

	assert.Contains(t, plain, "- REMOVED:")
	assert.Contains(t, plain, "This paragraph was removed entirely.")
	assert.NotContains(t, plain, "+ ADDED:")
}

func TestDiffHTMLToPlainText_Fallback(t *testing.T) {
	// If no diff-addedline/diff-deletedline classes, fallback to stripping all HTML.
	html := `<div>Some plain text with <b>bold</b> formatting.</div>`

	plain := diffHTMLToPlainText(html)

	assert.Contains(t, plain, "Some plain text with bold formatting.")
}

func TestDiffHTMLToPlainText_Empty(t *testing.T) {
	assert.Equal(t, "", diffHTMLToPlainText(""))
}

func TestDiffHTMLToPlainText_NonEnglish(t *testing.T) {
	html := `<tr>
		<td class="diff-deletedline"><div>קתדרלת שארטר היא <del class="diffchange diffchange-inline">כנסייה</del></div></td>
		<td class="diff-addedline"><div>קתדרלת שארטר היא <ins class="diffchange diffchange-inline">קתדרלה גותית</ins></div></td>
	</tr>`

	plain := diffHTMLToPlainText(html)

	assert.Contains(t, plain, "- REMOVED:")
	assert.Contains(t, plain, "\u00abכנסייה\u00bb")
	assert.Contains(t, plain, "+ ADDED:")
	assert.Contains(t, plain, "\u00abקתדרלה גותית\u00bb")
}

// ─── Truncation ─────────────────────────────────────────────────────────────

func TestTruncation(t *testing.T) {
	// Build a diff that exceeds MaxDiffChars.
	longLine := ""
	for i := 0; i < MaxDiffChars+500; i++ {
		longLine += "x"
	}
	html := fmt.Sprintf(`<tr><td class="diff-addedline"><div>%s</div></td></tr>`, longLine)

	plain := diffHTMLToPlainText(html)

	// The diffHTMLToPlainText doesn't truncate, but fetchOne does.
	// Verify the plain text is longer than MaxDiffChars.
	assert.Greater(t, len(plain), MaxDiffChars)
}

// ─── DiffFetcher with mock Wikipedia API ────────────────────────────────────

// queryRevisionsResponse builds a mock MediaWiki action=query response with
// diffs for the given revision IDs (pipe-separated in the "revids" param).
func queryRevisionsResponse(w http.ResponseWriter, r *http.Request) {
	revidsParam := r.URL.Query().Get("revids")
	revids := strings.Split(revidsParam, "|")

	revisions := make([]map[string]interface{}, 0, len(revids))
	for _, rev := range revids {
		revID, _ := strconv.ParseInt(rev, 10, 64)
		diffHTML := fmt.Sprintf(`<tr><td class="diff-addedline"><div>Content for rev %s</div></td></tr>`, rev)
		revisions = append(revisions, map[string]interface{}{
			"revid": revID,
			"diff":  map[string]interface{}{"*": diffHTML},
		})
	}

	resp := map[string]interface{}{
		"query": map[string]interface{}{
			"pages": map[string]interface{}{
				"12345": map[string]interface{}{
					"revisions": revisions,
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func TestDiffFetcher_FetchDiffs_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(queryRevisionsResponse))
	defer server.Close()

	df := NewDiffFetcher(zerolog.Nop())
	results := df.FetchDiffs(context.Background(), server.URL, []int64{100, 200, 300})

	require.Len(t, results, 3)
	for _, r := range results {
		assert.NotEmpty(t, r.DiffText, "rev %d should have diff text", r.RevisionID)
		assert.Empty(t, r.Error)
		assert.Contains(t, r.DiffText, "+ ADDED:")
	}
}

func TestDiffFetcher_FetchDiffs_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"error": map[string]interface{}{
				"code": "nosuchrevid",
				"info": "There is no revision with ID 99999.",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	df := NewDiffFetcher(zerolog.Nop())
	results := df.FetchDiffs(context.Background(), server.URL, []int64{99999})

	require.Len(t, results, 1)
	assert.Empty(t, results[0].DiffText)
	assert.Contains(t, results[0].Error, "no revision with ID 99999")
}

func TestDiffFetcher_FetchDiffs_HTTP500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	df := NewDiffFetcher(zerolog.Nop())
	results := df.FetchDiffs(context.Background(), server.URL, []int64{100})

	require.Len(t, results, 1)
	assert.Empty(t, results[0].DiffText)
	assert.Contains(t, results[0].Error, "http 500")
}

func TestDiffFetcher_FetchDiffs_Empty(t *testing.T) {
	df := NewDiffFetcher(zerolog.Nop())
	results := df.FetchDiffs(context.Background(), "http://example.com", nil)
	assert.Nil(t, results)
}

func TestDiffFetcher_FetchDiffs_CapsAtMax(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Return a valid batched response for whatever revids were sent.
		queryRevisionsResponse(w, r)
	}))
	defer server.Close()

	// Generate more IDs than MaxDiffsToFetch.
	ids := make([]int64, MaxDiffsToFetch+10)
	for i := range ids {
		ids[i] = int64(i + 1)
	}

	df := NewDiffFetcher(zerolog.Nop())
	results := df.FetchDiffs(context.Background(), server.URL, ids)

	assert.Len(t, results, MaxDiffsToFetch)
	// With batching (batch size 50), 20 IDs should only require 1 API call.
	assert.Equal(t, 1, callCount)
}

func TestDiffFetcher_FetchDiffs_TruncatesLongDiff(t *testing.T) {
	longContent := ""
	for i := 0; i < MaxDiffChars+1000; i++ {
		longContent += "a"
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := fmt.Sprintf(`<tr><td class="diff-addedline"><div>%s</div></td></tr>`, longContent)
		resp := map[string]interface{}{
			"query": map[string]interface{}{
				"pages": map[string]interface{}{
					"12345": map[string]interface{}{
						"revisions": []map[string]interface{}{
							{"revid": 42, "diff": map[string]interface{}{"*": html}},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	df := NewDiffFetcher(zerolog.Nop())
	results := df.FetchDiffs(context.Background(), server.URL, []int64{42})

	require.Len(t, results, 1)
	// Should be truncated to MaxDiffChars + "…" (3 bytes in UTF-8).
	assert.LessOrEqual(t, len(results[0].DiffText), MaxDiffChars+10)
}
