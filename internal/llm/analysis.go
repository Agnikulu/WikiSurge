package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// EditTimelineEntry is the shape of what the edit war detector stores in Redis.
type EditTimelineEntry struct {
	User       string `json:"user"`
	Comment    string `json:"comment"`
	ByteChange int    `json:"byte_change"`
	Timestamp  int64  `json:"timestamp"`
	RevisionID int64  `json:"revision_id,omitempty"`
	ServerURL  string `json:"server_url,omitempty"`
}

// KeyEditor describes a participant in the edit war.
type KeyEditor struct {
	User      string `json:"user"`
	EditCount int    `json:"edit_count"`
	Role      string `json:"role"` // e.g. "primary aggressor", "content defender", "reverter"
}

// Side groups editors who share a common position in the edit war.
type Side struct {
	Position string      `json:"position"` // what this side wants the article to say
	Editors  []KeyEditor `json:"editors"`  // editors on this side
}

// Analysis is the LLM-generated narrative returned to the frontend.
type Analysis struct {
	PageTitle      string      `json:"page_title"`
	Summary        string      `json:"summary"`         // 2-3 sentence conflict explanation
	Sides          []Side      `json:"sides"`            // opposing sides with grouped editors
	ContentArea    string      `json:"content_area"`     // topic area of disagreement
	Severity       string      `json:"severity"`         // "low", "moderate", "high", "critical"
	Recommendation string      `json:"recommendation"`   // suggested resolution
	EditCount      int         `json:"edit_count"`       // how many edits were analyzed
	GeneratedAt    string      `json:"generated_at"`     // RFC3339
	CacheHit       bool        `json:"cache_hit"`        // whether this came from cache
}

// AnalysisService builds prompts from edit war timeline data, calls the LLM,
// and caches results in Redis.
type AnalysisService struct {
	llm      *Client
	redis    *redis.Client
	diffs    *DiffFetcher
	cacheTTL time.Duration
	logger   zerolog.Logger
}

// NewAnalysisService creates a new analysis service.
func NewAnalysisService(llm *Client, redisClient *redis.Client, cacheTTL time.Duration, logger zerolog.Logger) *AnalysisService {
	if cacheTTL == 0 {
		cacheTTL = 4 * time.Hour
	}
	return &AnalysisService{
		llm:      llm,
		redis:    redisClient,
		diffs:    NewDiffFetcher(logger),
		cacheTTL: cacheTTL,
		logger:   logger.With().Str("component", "llm_analysis").Logger(),
	}
}

// Analyze fetches the timeline from Redis, checks for a cached analysis,
// and if not cached, calls the LLM.
// Reanalyze invalidates any cached analysis and runs a fresh LLM (or heuristic)
// analysis. Use this for periodic re-analysis when new edits arrive.
func (s *AnalysisService) Reanalyze(ctx context.Context, pageTitle string) (*Analysis, error) {
	cacheKey := fmt.Sprintf("editwar:analysis:%s", pageTitle)
	_ = s.redis.Del(ctx, cacheKey).Err()
	return s.Analyze(ctx, pageTitle)
}

// FinalizeAnalysis runs a final analysis when an edit war becomes inactive.
// The result is cached with a 7-day TTL (matching history retention) so that
// the analysis survives well beyond the timeline data's 12h TTL.
func (s *AnalysisService) FinalizeAnalysis(ctx context.Context, pageTitle string) (*Analysis, error) {
	cacheKey := fmt.Sprintf("editwar:analysis:%s", pageTitle)
	_ = s.redis.Del(ctx, cacheKey).Err()

	analysis, err := s.Analyze(ctx, pageTitle)
	if err != nil {
		return nil, err
	}

	// Re-cache with a 7-day TTL so it remains accessible from historical views.
	if data, mErr := json.Marshal(analysis); mErr == nil {
		_ = s.redis.Set(ctx, cacheKey, string(data), 7*24*time.Hour).Err()
	}

	return analysis, nil
}

func (s *AnalysisService) Analyze(ctx context.Context, pageTitle string) (*Analysis, error) {
	// 1. Check cache
	cacheKey := fmt.Sprintf("editwar:analysis:%s", pageTitle)
	if cached, err := s.redis.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
		var analysis Analysis
		if err := json.Unmarshal([]byte(cached), &analysis); err == nil {
			analysis.CacheHit = true
			return &analysis, nil
		}
	}

	// 2. Get timeline from Redis
	timelineKey := fmt.Sprintf("editwar:timeline:%s", pageTitle)
	raw, err := s.redis.LRange(ctx, timelineKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to read timeline for %s: %w", pageTitle, err)
	}

	if len(raw) == 0 {
		return &Analysis{
			PageTitle:      pageTitle,
			Summary:        "No edit timeline data available for this page. The edit war may have recently started or timeline tracking was not yet active when the conflict began.",
			Sides:          []Side{},
			ContentArea:    "unknown",
			Severity:       "unknown",
			Recommendation: "Monitor this page for further edits.",
			EditCount:      0,
			GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
			CacheHit:       false,
		}, nil
	}

	entries := make([]EditTimelineEntry, 0, len(raw))
	for _, r := range raw {
		var e EditTimelineEntry
		if err := json.Unmarshal([]byte(r), &e); err == nil {
			entries = append(entries, e)
		}
	}

	if len(entries) == 0 {
		return &Analysis{
			PageTitle:      pageTitle,
			Summary:        "Timeline entries could not be parsed.",
			Sides:          []Side{},
			ContentArea:    "unknown",
			Severity:       "unknown",
			Recommendation: "Monitor this page for further edits.",
			EditCount:      0,
			GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
			CacheHit:       false,
		}, nil
	}

	// 3. If LLM is not enabled, return a heuristic-only summary
	if !s.llm.Enabled() {
		analysis := s.heuristicAnalysis(pageTitle, entries)
		return analysis, nil
	}

	// 4. Fetch diffs from Wikipedia API (lazy — nothing stored in Redis).
	//    We only need a server URL from any entry to know which wiki to call.
	diffMap := s.fetchDiffs(ctx, pageTitle, entries)

	// 5. Build prompt
	systemPrompt, userPrompt := s.buildPrompt(pageTitle, entries, diffMap)

	// 6. Call LLM
	response, err := s.llm.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		s.logger.Warn().Err(err).Str("page", pageTitle).Msg("LLM call failed, falling back to heuristic")
		analysis := s.heuristicAnalysis(pageTitle, entries)
		return analysis, nil
	}

	// 7. Parse LLM response
	analysis := s.parseLLMResponse(pageTitle, response, len(entries))

	// 8. Cache it
	if data, err := json.Marshal(analysis); err == nil {
		_ = s.redis.Set(ctx, cacheKey, string(data), s.cacheTTL).Err()
	}

	return analysis, nil
}

// fetchDiffs retrieves diffs from the Wikipedia API for timeline entries that
// have a revision ID and server URL.  Returns a map of revisionID → plain-text
// diff.  Best-effort: entries without diffs are simply omitted from the map.
func (s *AnalysisService) fetchDiffs(ctx context.Context, pageTitle string, entries []EditTimelineEntry) map[int64]string {
	diffMap := make(map[int64]string)

	// Determine the wiki's server URL from the first entry that has one.
	var serverURL string
	for _, e := range entries {
		if e.ServerURL != "" {
			serverURL = e.ServerURL
			break
		}
	}

	// Fallback: look up the server URL from Redis (set by the edit war detector).
	if serverURL == "" {
		urlKey := fmt.Sprintf("editwar:serverurl:%s", pageTitle)
		if val, err := s.redis.Get(ctx, urlKey).Result(); err == nil && val != "" {
			serverURL = val
			s.logger.Info().Str("server_url", serverURL).Msg("Using server_url from Redis fallback")
		}
	}

	// Last resort: default to English Wikipedia.
	if serverURL == "" {
		serverURL = "https://en.wikipedia.org"
		s.logger.Warn().Int("entries", len(entries)).Msg("No server_url found anywhere; defaulting to en.wikipedia.org")
	}

	// Collect revision IDs (only entries that have one).
	var revIDs []int64
	var missingRevCount int
	for _, e := range entries {
		if e.RevisionID > 0 {
			revIDs = append(revIDs, e.RevisionID)
		} else {
			missingRevCount++
		}
	}

	s.logger.Info().
		Str("server_url", serverURL).
		Int("entries", len(entries)).
		Int("with_revision_id", len(revIDs)).
		Int("missing_revision_id", missingRevCount).
		Msg("Diff fetch: starting")

	if len(revIDs) == 0 {
		s.logger.Warn().Msg("No revision IDs available in timeline entries; no diffs to fetch")
		return diffMap
	}

	results := s.diffs.FetchDiffs(ctx, serverURL, revIDs)
	var errCount int
	for _, r := range results {
		if r.DiffText != "" {
			diffMap[r.RevisionID] = r.DiffText
		} else if r.Error != "" {
			errCount++
			s.logger.Info().Int64("rev", r.RevisionID).Str("err", r.Error).Msg("Diff fetch failed for revision")
		}
	}

	s.logger.Info().
		Int("requested", len(revIDs)).
		Int("fetched", len(diffMap)).
		Int("errors", errCount).
		Msg("Diff fetch: complete")

	return diffMap
}

// buildPrompt constructs the system + user prompts for the LLM.
func (s *AnalysisService) buildPrompt(pageTitle string, entries []EditTimelineEntry, diffs map[int64]string) (string, string) {
	systemPrompt := `You are a sharp, perceptive Wikipedia edit war analyst who uncovers the real story behind editing conflicts. Your job is to read between the lines and surface the most interesting dynamics at play — the human motivations, the tactical patterns, and the bigger picture of why people are fighting over this article right now.

You are provided with two types of evidence:
1. **Edit metadata**: who edited, when, byte changes, and edit summaries.
2. **Actual diffs**: the text that was added or removed in each edit (when available). Diffs are the most important signal — they show you exactly what content is being fought over. The diff content may be in any language; analyze the substance regardless of language.

When given a sequence of edits, provide:

1. **Summary** (4-5 sentences): Tell the story of this conflict like a journalist would. Don't just list facts — explain what's actually happening and why it matters. Reference the SPECIFIC content being changed (quote key phrases from the diffs when possible). Highlight the most striking or unusual aspect of the dispute (e.g. is one editor on a crusade? Are the reverts escalating in a pattern? Is there a real-world event driving the conflict? Are editors talking past each other about different things?).

2. **Opposing sides**: Group every editor into a side. For each side, describe not just what they want, but WHY they seem to want it based on their edit patterns, comments, and the actual diff content. What motivates each side? Give each editor a vivid, specific role — not generic labels like "contributor" but something that captures their actual behavior (e.g. "persistent content restorer", "cleanup vigilante", "sourcing enforcer").

3. **Content area**: What specific topic or section is being fought over? Be precise — use the actual text from the diffs to identify the exact subject matter, not vague labels like "content dispute".

4. **Severity** (low / moderate / high / critical): Based on edit frequency, editor count, revert ratio, and escalation patterns.

5. **Recommendation**: A clear, plain-language suggestion for what should happen next, written so anyone can understand it (e.g. "Both editors should stop editing the article and hash this out on the discussion page first", "An administrator should temporarily lock the page until tempers cool down").

Dig into the data. Look for:
- The ACTUAL CONTENT being added and removed (this is the most important signal)
- Timing patterns (are edits happening within minutes of each other? that signals a live back-and-forth)
- Byte-change patterns (are the same bytes being added and removed repeatedly?)
- Edit comment tone (are editors getting more aggressive over time?)
- Asymmetries (is one editor doing most of the reverting while the other keeps trying to add content?)
- Whether this looks like a genuine content disagreement or something else (vandalism, POV pushing, territorial behavior)

Be specific, factual, and unbiased. Don't take sides — but don't be boring either. The best analysis makes the reader say "oh, that's what's really going on."

Respond in valid JSON with this exact schema:
{
  "summary": "4-5 sentence conflict narrative — make it compelling and insightful, referencing specific content from the diffs",
  "sides": [
    {
      "position": "What this side wants and why they seem to want it — reference the actual content they are adding or defending",
      "editors": [
        {"user": "username", "edit_count": N, "role": "vivid specific role description"}
      ]
    }
  ],
  "content_area": "precise topic label derived from actual diff content",
  "severity": "low | moderate | high | critical",
  "recommendation": "Plain-language actionable next step"
}`

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Wikipedia page: \"%s\"\n\n", pageTitle))
	sb.WriteString("Recent edit timeline (chronological order):\n\n")

	hasDiffs := false
	for i, e := range entries {
		ts := time.Unix(e.Timestamp, 0).UTC().Format("2006-01-02 15:04:05 UTC")
		sign := "+"
		if e.ByteChange < 0 {
			sign = ""
		}
		comment := e.Comment
		if comment == "" {
			comment = "(no edit summary)"
		}
		sb.WriteString(fmt.Sprintf("%d. [%s] User \"%s\" (%s%d bytes): %s\n",
			i+1, ts, e.User, sign, e.ByteChange, comment))

		// Append the diff content for this revision if available.
		if diff, ok := diffs[e.RevisionID]; ok && diff != "" {
			hasDiffs = true
			sb.WriteString("   Diff:\n")
			for _, line := range strings.Split(diff, "\n") {
				sb.WriteString("   ")
				sb.WriteString(line)
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}

	if hasDiffs {
		sb.WriteString("\nThe diffs above show the EXACT text that was added or removed in each edit. Use this content to determine what the fight is actually about.\n")
	} else {
		sb.WriteString("\nNote: Diff content was not available for these edits. Base your analysis on the metadata above.\n")
	}

	sb.WriteString("\nAnalyze this edit war. What's the real story here? What are the opposing sides fighting over, and what patterns do you see in how this conflict is playing out?")

	return systemPrompt, sb.String()
}

// parseLLMResponse tries to extract structured JSON from the LLM output.
// Falls back gracefully if the LLM doesn't return perfect JSON.
func (s *AnalysisService) parseLLMResponse(pageTitle, response string, editCount int) *Analysis {
	analysis := &Analysis{
		PageTitle:   pageTitle,
		EditCount:   editCount,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		CacheHit:    false,
	}

	// Try to parse as JSON first
	var parsed struct {
		Summary        string `json:"summary"`
		Sides          []Side `json:"sides"`
		ContentArea    string `json:"content_area"`
		Severity       string `json:"severity"`
		Recommendation string `json:"recommendation"`
	}

	// Try to extract JSON from the response (LLM might wrap it in markdown code blocks)
	jsonStr := response
	if idx := strings.Index(response, "{"); idx >= 0 {
		if endIdx := strings.LastIndex(response, "}"); endIdx > idx {
			jsonStr = response[idx : endIdx+1]
		}
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
		analysis.Summary = parsed.Summary
		analysis.Sides = parsed.Sides
		analysis.ContentArea = parsed.ContentArea
		analysis.Severity = parsed.Severity
		analysis.Recommendation = parsed.Recommendation
	} else {
		// Fallback: use the raw response as summary
		analysis.Summary = response
		analysis.Sides = []Side{}
		analysis.ContentArea = "unknown"
		analysis.Severity = "unknown"
		analysis.Recommendation = ""
	}

	if analysis.Sides == nil {
		analysis.Sides = []Side{}
	}

	return analysis
}

// heuristicAnalysis generates a pattern-based analysis without LLM.
func (s *AnalysisService) heuristicAnalysis(pageTitle string, entries []EditTimelineEntry) *Analysis {
	// Count edits per user and track byte patterns
	userEdits := make(map[string]int)
	userBytes := make(map[string]int)
	userReverts := make(map[string]int)
	comments := make([]string, 0)

	for _, e := range entries {
		userEdits[e.User]++
		userBytes[e.User] += e.ByteChange
		if e.Comment != "" {
			comments = append(comments, e.Comment)
			// Count explicit revert language
			lc := strings.ToLower(e.Comment)
			if strings.Contains(lc, "revert") || strings.Contains(lc, "undid") || strings.Contains(lc, "undo") || strings.Contains(lc, "rv ") {
				userReverts[e.User]++
			}
		}
	}

	// Detect revert patterns from byte-change oscillation
	revertCount := 0
	for i := 1; i < len(entries); i++ {
		prev := entries[i-1].ByteChange
		curr := entries[i].ByteChange
		if (prev > 0 && curr < 0) || (prev < 0 && curr > 0) {
			revertCount++
		}
	}

	// Infer content area from comments
	contentArea := inferContentArea(comments)

	// Compute severity from metrics
	severity := computeSeverity(len(entries), len(userEdits), revertCount)

	// Build key editors
	keyEditors := make([]KeyEditor, 0, len(userEdits))
	for user, count := range userEdits {
		role := "contributor"
		if userReverts[user] > 0 {
			role = "reverter"
		} else if userBytes[user] > 200 {
			role = "content adder"
		} else if userBytes[user] < -200 {
			role = "content remover"
		}
		keyEditors = append(keyEditors, KeyEditor{User: user, EditCount: count, Role: role})
	}

	// Build summary
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"Active edit war on \"%s\" with %d editors making %d edits (%d apparent reversions). ",
		pageTitle, len(userEdits), len(entries), revertCount,
	))

	if revertCount > len(entries)/2 {
		sb.WriteString("The high revert ratio indicates an escalating content dispute. ")
	}

	if len(comments) > 0 {
		unique := deduplicateComments(comments, 4)
		sb.WriteString("Key edit summaries: " + strings.Join(unique, "; ") + ".")
	} else {
		sb.WriteString("No edit summaries were provided — the topic of contention cannot be determined from metadata alone.")
	}

	// Build sides by grouping editors into "content adders" vs "content removers/reverters"
	adderSide := Side{Position: "Adding or restoring content", Editors: []KeyEditor{}}
	removerSide := Side{Position: "Removing or reverting content", Editors: []KeyEditor{}}

	for _, ed := range keyEditors {
		if userBytes[ed.User] < -100 || userReverts[ed.User] > 0 {
			removerSide.Editors = append(removerSide.Editors, ed)
		} else {
			adderSide.Editors = append(adderSide.Editors, ed)
		}
	}

	sides := make([]Side, 0, 2)
	if len(adderSide.Editors) > 0 {
		sides = append(sides, adderSide)
	}
	if len(removerSide.Editors) > 0 {
		sides = append(sides, removerSide)
	}
	// If all editors ended up on one side, just show them all
	if len(sides) == 0 {
		sides = append(sides, Side{Position: "Disputed content", Editors: keyEditors})
	}

	// Recommendation
	recommendation := "Monitor the page for further activity."
	if severity == "critical" {
		recommendation = "Immediate admin intervention recommended. Consider requesting full protection on the page."
	} else if severity == "high" {
		recommendation = "Consider requesting semi-protection and encouraging editors to use the talk page."
	} else if severity == "moderate" {
		recommendation = "Encourage discussion on the talk page. Post a {{talkpage}} notice if not already present."
	}

	return &Analysis{
		PageTitle:      pageTitle,
		Summary:        sb.String(),
		Sides:          sides,
		ContentArea:    contentArea,
		Severity:       severity,
		Recommendation: recommendation,
		EditCount:      len(entries),
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		CacheHit:       false,
	}
}

// computeSeverity determines conflict severity from metrics.
func computeSeverity(editCount, editorCount, revertCount int) string {
	score := 0
	if editCount >= 20 {
		score += 3
	} else if editCount >= 10 {
		score += 2
	} else if editCount >= 5 {
		score += 1
	}
	if editorCount >= 5 {
		score += 2
	} else if editorCount >= 3 {
		score += 1
	}
	revertRatio := 0.0
	if editCount > 0 {
		revertRatio = float64(revertCount) / float64(editCount)
	}
	if revertRatio >= 0.7 {
		score += 3
	} else if revertRatio >= 0.4 {
		score += 2
	} else if revertRatio >= 0.2 {
		score += 1
	}

	switch {
	case score >= 7:
		return "critical"
	case score >= 5:
		return "high"
	case score >= 3:
		return "moderate"
	default:
		return "low"
	}
}

// inferContentArea tries to guess the content area from edit comments.
func inferContentArea(comments []string) string {
	if len(comments) == 0 {
		return "undetermined"
	}

	// Check for section headers in comments (/* Section */)
	for _, c := range comments {
		if idx := strings.Index(c, "/*"); idx >= 0 {
			if endIdx := strings.Index(c[idx:], "*/"); endIdx > 0 {
				section := strings.TrimSpace(c[idx+2 : idx+endIdx])
				if section != "" {
					return section
				}
			}
		}
	}

	// Keyword-based classification
	allText := strings.ToLower(strings.Join(comments, " "))
	keywords := map[string]string{
		"death":       "biographical details",
		"died":        "biographical details",
		"born":        "biographical details",
		"vandal":      "vandalism dispute",
		"spam":        "spam/promotion",
		"source":      "sourcing/references",
		"citation":    "sourcing/references",
		"ref":         "sourcing/references",
		"pov":         "neutrality/POV",
		"bias":        "neutrality/POV",
		"neutral":     "neutrality/POV",
		"image":       "media/images",
		"infobox":     "infobox content",
		"category":    "categorization",
		"revert":      "content dispute",
	}
	for keyword, area := range keywords {
		if strings.Contains(allText, keyword) {
			return area
		}
	}

	return "general content dispute"
}

// deduplicateComments returns up to maxCount unique, non-empty comments.
func deduplicateComments(comments []string, maxCount int) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, maxCount)
	for _, c := range comments {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		result = append(result, fmt.Sprintf("\"%s\"", c))
		if len(result) >= maxCount {
			break
		}
	}
	return result
}
