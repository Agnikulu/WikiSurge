package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// MaxDiffChars is the maximum number of characters to keep per diff after
// stripping HTML.  2 000 chars ≈ 500 LLM tokens — enough to capture the full
// substance of a typical edit-war revert while keeping total prompt size
// manageable (20 edits × 2 000 = 40 000 chars ≈ 10 000 tokens).
const MaxDiffChars = 2000

// MaxDiffsToFetch caps how many diffs we retrieve per analysis request so we
// don't hammer the Wikipedia API.
const MaxDiffsToFetch = 20

// DiffResult holds the plain-text diff for a single revision.
type DiffResult struct {
	RevisionID int64  `json:"revision_id"`
	DiffText   string `json:"diff_text"` // human-readable, HTML-stripped
	Error      string `json:"error,omitempty"`
}

// DiffFetcher retrieves revision diffs from the Wikipedia compare API on
// demand (at analysis time — nothing is stored in Redis).
type DiffFetcher struct {
	http   *http.Client
	logger zerolog.Logger
}

// NewDiffFetcher creates a DiffFetcher with sensible defaults.
func NewDiffFetcher(logger zerolog.Logger) *DiffFetcher {
	return &DiffFetcher{
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger.With().Str("component", "diff_fetcher").Logger(),
	}
}

// batchSize is the maximum number of revision IDs to include in a single
// Wikipedia API call.  The revisions query endpoint supports pipe-separated
// revids and returns all diffs in one response.
const batchSize = 50

// FetchDiffs retrieves diffs for the given revision IDs using the Wikipedia
// revisions query API (action=query&prop=revisions&rvdiffto=prev).
// serverURL should be the wiki's server URL (e.g. "https://he.wikipedia.org").
// Revisions without a usable diff are returned with an empty DiffText and a
// non-empty Error field.
func (df *DiffFetcher) FetchDiffs(ctx context.Context, serverURL string, revisionIDs []int64) []DiffResult {
	if len(revisionIDs) == 0 {
		return nil
	}

	// Cap the number of diffs we fetch.
	ids := revisionIDs
	if len(ids) > MaxDiffsToFetch {
		ids = ids[len(ids)-MaxDiffsToFetch:] // keep most recent
	}

	// Batch the IDs and fetch in groups.
	var results []DiffResult
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		results = append(results, df.fetchBatch(ctx, serverURL, batch)...)
	}
	return results
}

// fetchBatch calls the Wikipedia query API for a batch of revision IDs,
// comparing each against its predecessor using rvdiffto=prev.
func (df *DiffFetcher) fetchBatch(ctx context.Context, serverURL string, revIDs []int64) []DiffResult {
	// Build pipe-separated revids string.
	idStrs := make([]string, len(revIDs))
	for i, id := range revIDs {
		idStrs[i] = strconv.FormatInt(id, 10)
	}

	apiURL := strings.TrimRight(serverURL, "/") + "/w/api.php"
	params := url.Values{
		"action":   {"query"},
		"prop":     {"revisions"},
		"revids":   {strings.Join(idStrs, "|")},
		"rvprop":   {"ids"},
		"rvdiffto": {"prev"},
		"format":   {"json"},
	}

	reqURL := apiURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		// Return errors for all IDs in this batch.
		results := make([]DiffResult, len(revIDs))
		for i, id := range revIDs {
			results[i] = DiffResult{RevisionID: id, Error: fmt.Sprintf("build request: %v", err)}
		}
		return results
	}
	req.Header.Set("User-Agent", "WikiSurge/1.0 (edit war analysis; contact: wikisurge@example.com)")

	resp, err := df.http.Do(req)
	if err != nil {
		df.logger.Warn().Err(err).Int("batch_size", len(revIDs)).Msg("Wikipedia revisions API request failed")
		results := make([]DiffResult, len(revIDs))
		for i, id := range revIDs {
			results[i] = DiffResult{RevisionID: id, Error: fmt.Sprintf("http: %v", err)}
		}
		return results
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2 MB safety cap for batched responses
	if err != nil {
		results := make([]DiffResult, len(revIDs))
		for i, id := range revIDs {
			results[i] = DiffResult{RevisionID: id, Error: fmt.Sprintf("read body: %v", err)}
		}
		return results
	}

	if resp.StatusCode != http.StatusOK {
		results := make([]DiffResult, len(revIDs))
		for i, id := range revIDs {
			results[i] = DiffResult{RevisionID: id, Error: fmt.Sprintf("http %d", resp.StatusCode)}
		}
		return results
	}

	// Parse the MediaWiki query response.
	// Structure: { "query": { "pages": { "<pageid>": { "revisions": [ { "revid": N, "diff": { "*": "..." } } ] } } } }
	var mwResp struct {
		Query struct {
			Pages map[string]struct {
				Revisions []struct {
					RevID int64 `json:"revid"`
					Diff  struct {
						Body string `json:"*"`
					} `json:"diff"`
				} `json:"revisions"`
			} `json:"pages"`
		} `json:"query"`
		Error *struct {
			Code string `json:"code"`
			Info string `json:"info"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &mwResp); err != nil {
		df.logger.Warn().Err(err).Msg("Failed to parse Wikipedia revisions API response")
		results := make([]DiffResult, len(revIDs))
		for i, id := range revIDs {
			results[i] = DiffResult{RevisionID: id, Error: fmt.Sprintf("json: %v", err)}
		}
		return results
	}
	if mwResp.Error != nil {
		results := make([]DiffResult, len(revIDs))
		for i, id := range revIDs {
			results[i] = DiffResult{RevisionID: id, Error: mwResp.Error.Info}
		}
		return results
	}

	// Build a map of revid → diff HTML from the response.
	diffHTMLMap := make(map[int64]string)
	for _, page := range mwResp.Query.Pages {
		for _, rev := range page.Revisions {
			if rev.Diff.Body != "" {
				diffHTMLMap[rev.RevID] = rev.Diff.Body
			}
		}
	}

	// Convert to results.
	results := make([]DiffResult, len(revIDs))
	for i, id := range revIDs {
		html, ok := diffHTMLMap[id]
		if !ok || html == "" {
			results[i] = DiffResult{RevisionID: id, Error: "empty diff"}
			continue
		}

		plain := diffHTMLToPlainText(html)
		if len(plain) > MaxDiffChars {
			plain = plain[:MaxDiffChars] + "…"
		}
		results[i] = DiffResult{RevisionID: id, DiffText: plain}
	}

	return results
}

// ─── HTML → plain text helpers ──────────────────────────────────────────────

var (
	// Regex patterns for extracting meaningful content from MediaWiki diff HTML.
	reAddedLine   = regexp.MustCompile(`<td class="diff-addedline"[^>]*>(.*?)</td>`)
	reDeletedLine = regexp.MustCompile(`<td class="diff-deletedline"[^>]*>(.*?)</td>`)
	reInsMarker   = regexp.MustCompile(`<ins class="diffchange diffchange-inline">(.*?)</ins>`)
	reDelMarker   = regexp.MustCompile(`<del class="diffchange diffchange-inline">(.*?)</del>`)
	reHTMLTag     = regexp.MustCompile(`<[^>]+>`)
	reWhitespace  = regexp.MustCompile(`\s+`)
)

// diffHTMLToPlainText converts a MediaWiki HTML diff table into a concise,
// human-readable plain-text summary that the LLM can reason about.
//
// Output format:
//
//	- REMOVED: <text that was deleted>
//	+ ADDED:   <text that was added>
func diffHTMLToPlainText(html string) string {
	var sb strings.Builder

	// Extract deleted lines
	for _, m := range reDeletedLine.FindAllStringSubmatch(html, -1) {
		line := m[1]
		// Highlight inline deletions
		line = reDelMarker.ReplaceAllString(line, "\u00ab$1\u00bb")
		line = stripHTML(line)
		line = collapseWhitespace(line)
		if line != "" {
			sb.WriteString("- REMOVED: ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	// Extract added lines
	for _, m := range reAddedLine.FindAllStringSubmatch(html, -1) {
		line := m[1]
		// Highlight inline additions
		line = reInsMarker.ReplaceAllString(line, "\u00ab$1\u00bb")
		line = stripHTML(line)
		line = collapseWhitespace(line)
		if line != "" {
			sb.WriteString("+ ADDED: ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	result := strings.TrimSpace(sb.String())
	if result == "" {
		// Fallback: strip all HTML and return raw text (handles edge cases like
		// moves or formatting-only changes).
		fallback := stripHTML(html)
		fallback = collapseWhitespace(fallback)
		return fallback
	}
	return result
}

func stripHTML(s string) string {
	return reHTMLTag.ReplaceAllString(s, "")
}

func collapseWhitespace(s string) string {
	return strings.TrimSpace(reWhitespace.ReplaceAllString(s, " "))
}
