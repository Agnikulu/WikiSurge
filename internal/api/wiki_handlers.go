package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"
)

var wikiLangPattern = regexp.MustCompile(`^[a-z-]{2,10}$`)

// handleWikiAutocomplete returns article title suggestions from Wikipedia.
// Query params:
// - q: search query (required, min length 2)
// - lang: wiki language code (optional, default: en)
func (s *APIServer) handleWikiAutocomplete(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) < 2 {
		writeAPIError(w, r, http.StatusBadRequest, "query parameter 'q' must be at least 2 characters", ErrCodeInvalidParameter, "")
		return
	}

	lang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))
	if lang == "" {
		lang = "en"
	}
	if !wikiLangPattern.MatchString(lang) {
		writeAPIError(w, r, http.StatusBadRequest, "invalid language code", ErrCodeInvalidParameter, "")
		return
	}

	reqURL := fmt.Sprintf(
		"https://%s.wikipedia.org/w/api.php?action=opensearch&search=%s&limit=6&format=json&origin=*",
		lang,
		neturl.QueryEscape(query),
	)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(reqURL)
	if err != nil {
		s.logger.Warn().Err(err).Str("query", query).Str("lang", lang).Msg("wiki autocomplete request failed")
		writeAPIError(w, r, http.StatusBadGateway, "wikipedia autocomplete unavailable", ErrCodeServiceUnavailable, "")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		s.logger.Warn().Int("status", resp.StatusCode).Str("body", string(body)).Msg("wiki autocomplete non-200")
		writeAPIError(w, r, http.StatusBadGateway, "wikipedia autocomplete unavailable", ErrCodeServiceUnavailable, "")
		return
	}

	var payload []any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		s.logger.Warn().Err(err).Msg("failed to decode wikipedia autocomplete response")
		writeAPIError(w, r, http.StatusBadGateway, "wikipedia autocomplete unavailable", ErrCodeServiceUnavailable, "")
		return
	}

	titles := make([]string, 0)
	if len(payload) > 1 {
		if rawTitles, ok := payload[1].([]any); ok {
			for _, v := range rawTitles {
				title, ok := v.(string)
				if !ok {
					continue
				}
				title = strings.TrimSpace(title)
				if title != "" {
					titles = append(titles, title)
				}
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"query":       query,
		"lang":        lang,
		"suggestions": titles,
	})
}
