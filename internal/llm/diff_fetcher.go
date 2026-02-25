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
// stripping HTML.  800 chars ≈ 200 LLM tokens — enough to capture the key
// additions/removals in a typical revert while keeping total prompt size
// lean (8 diffs × 800 = 6 400 chars ≈ 1 600 tokens).
const MaxDiffChars = 800

// MaxDiffsToFetch caps how many diffs we retrieve per analysis request so we
// don't hammer the Wikipedia API.  8 diffs is enough to reveal the pattern
// in a repetitive edit war.
const MaxDiffsToFetch = 8

// maxContentChars caps the raw wikitext we read per revision in the tier-3
// content-based fallback.  We only need enough text to produce a meaningful
// diff fragment for the LLM.
const maxContentChars = 4000

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

// FetchDiffs retrieves diffs for the given revision IDs using a 3-tier
// fallback strategy to maximise coverage:
//
//   Tier 1: action=query&prop=revisions&rvdiffto=prev  (batched, fast)
//   Tier 2: action=compare&fromrev=PREV&torev=CUR      (per-rev, higher size limits)
//   Tier 3: Fetch raw wikitext of both revisions and produce a simple local diff
//
// serverURL should be the wiki's server URL (e.g. "https://he.wikipedia.org").
func (df *DiffFetcher) FetchDiffs(ctx context.Context, serverURL string, revisionIDs []int64) []DiffResult {
	if len(revisionIDs) == 0 {
		return nil
	}

	// Cap the number of diffs we fetch.
	ids := revisionIDs
	if len(ids) > MaxDiffsToFetch {
		ids = ids[len(ids)-MaxDiffsToFetch:] // keep most recent
	}

	// ── Tier 1: batched rvdiffto=prev ────────────────────────────────────
	var results []DiffResult
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		results = append(results, df.fetchBatch(ctx, serverURL, batch)...)
	}

	// Collect IDs that tier 1 failed to resolve.
	var failedIDs []int64
	failedIdx := map[int64]int{} // revID → index in results
	for i, r := range results {
		if r.DiffText == "" && r.Error != "" {
			failedIDs = append(failedIDs, r.RevisionID)
			failedIdx[r.RevisionID] = i
		}
	}

	if len(failedIDs) == 0 {
		return results
	}

	df.logger.Info().
		Int("tier1_failures", len(failedIDs)).
		Msg("Tier 1 (rvdiffto) incomplete — trying tier 2 (action=compare)")

	// ── Tier 2: action=compare per revision ──────────────────────────────
	var stillFailed []int64
	for _, id := range failedIDs {
		r := df.fetchCompare(ctx, serverURL, id)
		if r.DiffText != "" {
			results[failedIdx[id]] = r
		} else {
			stillFailed = append(stillFailed, id)
		}
	}

	if len(stillFailed) == 0 {
		return results
	}

	df.logger.Info().
		Int("tier2_failures", len(stillFailed)).
		Msg("Tier 2 (compare) incomplete — trying tier 3 (content diff)")

	// ── Tier 3: fetch raw content and diff locally ───────────────────────
	for _, id := range stillFailed {
		r := df.fetchContentDiff(ctx, serverURL, id)
		results[failedIdx[id]] = r
	}

	return results
}

// fetchBatch calls the Wikipedia query API for a batch of revision IDs,
// comparing each against its predecessor using rvdiffto=prev.  (Tier 1)
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
					RevID    int64 `json:"revid"`
					ParentID int64 `json:"parentid"`
					Diff     struct {
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

	// Build maps from the response.
	diffHTMLMap := make(map[int64]string)
	parentMap := make(map[int64]int64)
	for _, page := range mwResp.Query.Pages {
		for _, rev := range page.Revisions {
			parentMap[rev.RevID] = rev.ParentID
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
			// Distinguish page creations (parentid=0) from genuinely empty diffs.
			if pid, found := parentMap[id]; found && pid == 0 {
				results[i] = DiffResult{RevisionID: id, Error: "new page creation"}
			} else {
				results[i] = DiffResult{RevisionID: id, Error: "empty diff"}
			}
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

// ─── Tier 2: action=compare ─────────────────────────────────────────────────

// fetchCompare uses the MediaWiki action=compare endpoint which has higher
// size limits than the inline rvdiffto=prev approach.  It compares a revision
// against its predecessor one at a time.
func (df *DiffFetcher) fetchCompare(ctx context.Context, serverURL string, revID int64) DiffResult {
	apiURL := strings.TrimRight(serverURL, "/") + "/w/api.php"
	params := url.Values{
		"action":  {"compare"},
		"fromrev": {strconv.FormatInt(revID, 10)},
		"torelative": {"prev"},
		"format":  {"json"},
	}

	reqURL := apiURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return DiffResult{RevisionID: revID, Error: fmt.Sprintf("compare build: %v", err)}
	}
	req.Header.Set("User-Agent", "WikiSurge/1.0 (edit war analysis; contact: wikisurge@example.com)")

	resp, err := df.http.Do(req)
	if err != nil {
		return DiffResult{RevisionID: revID, Error: fmt.Sprintf("compare http: %v", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return DiffResult{RevisionID: revID, Error: fmt.Sprintf("compare read: %v", err)}
	}
	if resp.StatusCode != http.StatusOK {
		return DiffResult{RevisionID: revID, Error: fmt.Sprintf("compare http %d", resp.StatusCode)}
	}

	// Response: { "compare": { "*": "<diff HTML>" } }
	var cResp struct {
		Compare struct {
			Body string `json:"*"`
		} `json:"compare"`
		Error *struct {
			Code string `json:"code"`
			Info string `json:"info"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &cResp); err != nil {
		return DiffResult{RevisionID: revID, Error: fmt.Sprintf("compare json: %v", err)}
	}
	if cResp.Error != nil {
		return DiffResult{RevisionID: revID, Error: fmt.Sprintf("compare api: %s", cResp.Error.Info)}
	}
	if cResp.Compare.Body == "" {
		return DiffResult{RevisionID: revID, Error: "compare empty"}
	}

	plain := diffHTMLToPlainText(cResp.Compare.Body)
	if len(plain) > MaxDiffChars {
		plain = plain[:MaxDiffChars] + "…"
	}
	if plain == "" {
		return DiffResult{RevisionID: revID, Error: "compare empty after strip"}
	}
	return DiffResult{RevisionID: revID, DiffText: plain}
}

// ─── Tier 3: raw content diff ───────────────────────────────────────────────

// fetchContentDiff fetches the wikitext of a revision and its parent, then
// produces a simple line-level diff locally.  This works even when the
// server-side diff endpoints refuse (suppressions excluded).
func (df *DiffFetcher) fetchContentDiff(ctx context.Context, serverURL string, revID int64) DiffResult {
	apiURL := strings.TrimRight(serverURL, "/") + "/w/api.php"

	// First, fetch the revision with its parent ID and content.
	params := url.Values{
		"action":  {"query"},
		"prop":    {"revisions"},
		"revids":  {strconv.FormatInt(revID, 10)},
		"rvprop":  {"ids|content"},
		"rvslots": {"main"},
		"format":  {"json"},
	}

	reqURL := apiURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return DiffResult{RevisionID: revID, Error: fmt.Sprintf("content build: %v", err)}
	}
	req.Header.Set("User-Agent", "WikiSurge/1.0 (edit war analysis; contact: wikisurge@example.com)")

	resp, err := df.http.Do(req)
	if err != nil {
		return DiffResult{RevisionID: revID, Error: fmt.Sprintf("content http: %v", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return DiffResult{RevisionID: revID, Error: fmt.Sprintf("content read: %v", err)}
	}
	if resp.StatusCode != http.StatusOK {
		return DiffResult{RevisionID: revID, Error: fmt.Sprintf("content http %d", resp.StatusCode)}
	}

	// Parse to get content + parentid.
	var qResp struct {
		Query struct {
			Pages map[string]struct {
				Revisions []struct {
					RevID    int64 `json:"revid"`
					ParentID int64 `json:"parentid"`
					Slots    struct {
						Main struct {
							Content string `json:"*"`
						} `json:"main"`
					} `json:"slots"`
				} `json:"revisions"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &qResp); err != nil {
		return DiffResult{RevisionID: revID, Error: fmt.Sprintf("content json: %v", err)}
	}

	var newContent string
	var parentID int64
	for _, page := range qResp.Query.Pages {
		for _, rev := range page.Revisions {
			if rev.RevID == revID {
				newContent = rev.Slots.Main.Content
				parentID = rev.ParentID
			}
		}
	}

	if newContent == "" {
		return DiffResult{RevisionID: revID, Error: "content: revision content unavailable (possibly suppressed)"}
	}

	// Page creation — no parent to diff against.
	if parentID == 0 {
		text := truncateText(newContent, maxContentChars)
		return DiffResult{
			RevisionID: revID,
			DiffText:   fmt.Sprintf("[NEW PAGE CREATED]\n%s", text),
		}
	}

	// Fetch parent content.
	parentContent, err := df.fetchRevisionContent(ctx, serverURL, parentID)
	if err != nil {
		// Can't get parent — just show what was added.
		text := truncateText(newContent, maxContentChars)
		return DiffResult{
			RevisionID: revID,
			DiffText:   fmt.Sprintf("[PARENT CONTENT UNAVAILABLE — showing current revision]\n%s", text),
		}
	}

	// Produce a simple line-level diff.
	diffText := simpleDiff(parentContent, newContent)
	if diffText == "" {
		return DiffResult{RevisionID: revID, Error: "content: revisions are identical"}
	}
	if len(diffText) > MaxDiffChars {
		diffText = diffText[:MaxDiffChars] + "…"
	}
	return DiffResult{RevisionID: revID, DiffText: diffText}
}

// fetchRevisionContent retrieves the wikitext of a single revision.
func (df *DiffFetcher) fetchRevisionContent(ctx context.Context, serverURL string, revID int64) (string, error) {
	apiURL := strings.TrimRight(serverURL, "/") + "/w/api.php"
	params := url.Values{
		"action":  {"query"},
		"prop":    {"revisions"},
		"revids":  {strconv.FormatInt(revID, 10)},
		"rvprop":  {"content"},
		"rvslots": {"main"},
		"format":  {"json"},
	}

	reqURL := apiURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "WikiSurge/1.0 (edit war analysis; contact: wikisurge@example.com)")

	resp, err := df.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}

	var qResp struct {
		Query struct {
			Pages map[string]struct {
				Revisions []struct {
					Slots struct {
						Main struct {
							Content string `json:"*"`
						} `json:"main"`
					} `json:"slots"`
				} `json:"revisions"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &qResp); err != nil {
		return "", err
	}
	for _, page := range qResp.Query.Pages {
		for _, rev := range page.Revisions {
			return rev.Slots.Main.Content, nil
		}
	}
	return "", fmt.Errorf("no content in response")
}

// simpleDiff produces a concise line-level diff between two texts, keeping
// only changed lines with ± markers.  This is intentionally simple — we don't
// need a perfect Myers diff, just enough signal for the LLM.
func simpleDiff(oldText, newText string) string {
	oldLines := strings.Split(truncateText(oldText, maxContentChars), "\n")
	newLines := strings.Split(truncateText(newText, maxContentChars), "\n")

	oldSet := make(map[string]struct{}, len(oldLines))
	for _, l := range oldLines {
		l = strings.TrimSpace(l)
		if l != "" {
			oldSet[l] = struct{}{}
		}
	}
	newSet := make(map[string]struct{}, len(newLines))
	for _, l := range newLines {
		l = strings.TrimSpace(l)
		if l != "" {
			newSet[l] = struct{}{}
		}
	}

	var sb strings.Builder

	// Lines removed.
	for _, l := range oldLines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if _, ok := newSet[l]; !ok {
			sb.WriteString("- REMOVED: ")
			sb.WriteString(l)
			sb.WriteString("\n")
		}
	}
	// Lines added.
	for _, l := range newLines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if _, ok := oldSet[l]; !ok {
			sb.WriteString("+ ADDED: ")
			sb.WriteString(l)
			sb.WriteString("\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

// truncateText returns at most maxLen characters of s, cutting at a newline
// boundary when possible.
func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := s[:maxLen]
	if idx := strings.LastIndex(cut, "\n"); idx > maxLen/2 {
		cut = cut[:idx]
	}
	return cut + "\n…[truncated]"
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
