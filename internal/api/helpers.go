package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrorResponse is the legacy error envelope (kept for backward compatibility).
// New code should use APIErrorResponse from errors.go.
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Details string `json:"details,omitempty"`
}

// respondStandardError writes the new standardised error format with request_id.
func respondStandardError(w http.ResponseWriter, r *http.Request, status int, message, code, details string) {
	writeAPIError(w, r, status, message, code, details)
}

// PaginationInfo carries pagination metadata in responses.
type PaginationInfo struct {
	Total   int64 `json:"total"`
	Limit   int   `json:"limit"`
	Offset  int   `json:"offset"`
	HasMore bool  `json:"has_more"`
}

// SearchResponse is returned by GET /api/search.
type SearchResponse struct {
	Hits       []SearchHit    `json:"hits"`
	Total      int64          `json:"total"`
	Query      string         `json:"query"`
	Pagination PaginationInfo `json:"pagination"`
}

// SearchHit represents a single search result.
type SearchHit struct {
	Title      string  `json:"title"`
	User       string  `json:"user"`
	Timestamp  string  `json:"timestamp"`
	Comment    string  `json:"comment"`
	ByteChange int     `json:"byte_change,omitempty"`
	Wiki       string  `json:"wiki"`
	Score      float64 `json:"score"`
	Language   string  `json:"language,omitempty"`
	ServerURL  string  `json:"server_url,omitempty"`
}

// AlertsResponse is returned by GET /api/alerts.
type AlertsResponse struct {
	Alerts     []AlertEntry   `json:"alerts"`
	Total      int            `json:"total"`
	Pagination PaginationInfo `json:"pagination"`
}

// AlertEntry is a single alert in the response.
type AlertEntry struct {
	Type         string   `json:"type"`
	PageTitle    string   `json:"page_title"`
	SpikeRatio   float64  `json:"spike_ratio,omitempty"`
	Severity     string   `json:"severity"`
	Timestamp    string   `json:"timestamp"`
	Edits5Min    int      `json:"edits_5min,omitempty"`
	EditorCount  int      `json:"editor_count,omitempty"`
	EditCount    int      `json:"edit_count,omitempty"`
	RevertCount  int      `json:"revert_count,omitempty"`
	Editors      []string `json:"editors,omitempty"`
	Wiki         string   `json:"wiki,omitempty"`
	ServerURL    string   `json:"server_url,omitempty"`
}

// EditWarEntry is returned by GET /api/edit-wars.
type EditWarEntry struct {
	PageTitle   string      `json:"page_title"`
	EditorCount int         `json:"editor_count"`
	EditCount   int         `json:"edit_count"`
	RevertCount int         `json:"revert_count"`
	Severity    string      `json:"severity"`
	StartTime   string      `json:"start_time,omitempty"`
	LastEdit    string      `json:"last_edit,omitempty"`
	Editors     []string    `json:"editors"`
	Active      bool        `json:"active"`
	ServerURL   string      `json:"server_url,omitempty"`
	Analysis    interface{} `json:"analysis,omitempty"`
}

// ---------------------------------------------------------------------------
// Geo Activity types
// ---------------------------------------------------------------------------

// GeoRegion represents a language-wiki's geographic activity.
type GeoRegion struct {
	Wiki           string  `json:"wiki"`
	Lat            float64 `json:"lat"`
	Lng            float64 `json:"lng"`
	EditsPerMinute float64 `json:"edits_per_minute"`
	EditCount1h    int     `json:"edit_count_1h"`
	SpikeCount     int     `json:"spike_count"`
}

// GeoHotspot represents a single trending page geolocated on the map.
type GeoHotspot struct {
	PageTitle      string  `json:"page_title"`
	Score          float64 `json:"score"`
	Edits1h        int     `json:"edits_1h"`
	Lat            float64 `json:"lat"`
	Lng            float64 `json:"lng"`
	LocationSource string  `json:"location_source"` // "article", "semantic", or "wiki_centroid"
	Language       string  `json:"language,omitempty"`
	ServerURL      string  `json:"server_url,omitempty"`
	Rank           int     `json:"rank"`
}

// GeoWar represents an active edit war with geographic coordinates.
type GeoWar struct {
	PageTitle      string      `json:"page_title"`
	Severity       string      `json:"severity"`
	EditorCount    int         `json:"editor_count"`
	EditCount      int         `json:"edit_count"`
	RevertCount    int         `json:"revert_count"`
	Lat            float64     `json:"lat"`
	Lng            float64     `json:"lng"`
	LocationSource string      `json:"location_source"` // "article" or "wiki_centroid"
	SummarySnippet string      `json:"summary_snippet,omitempty"`
	StartTime      string      `json:"start_time,omitempty"`
	Active         bool        `json:"active"`
	ServerURL      string      `json:"server_url,omitempty"`
	Analysis       interface{} `json:"analysis,omitempty"`
}

// GeoActivityResponse is returned by GET /api/geo-activity.
type GeoActivityResponse struct {
	Regions  []GeoRegion  `json:"regions"`
	Wars     []GeoWar     `json:"wars"`
	Hotspots []GeoHotspot `json:"hotspots"`
}

// WikiCentroid maps a language code to an approximate geographic centroid.
type WikiCentroid struct {
	Lat float64
	Lng float64
}

// wikiCentroids maps wiki language codes to approximate geographic centroids
// of the region where that language is predominantly spoken.
var wikiCentroids = map[string]WikiCentroid{
	"en": {37.0902, -95.7129},   // USA (largest English Wikipedia contributor base)
	"de": {51.1657, 10.4515},    // Germany
	"fr": {46.6034, 1.8883},     // France
	"es": {40.4637, -3.7492},    // Spain
	"it": {41.8719, 12.5674},    // Italy
	"pt": {-14.2350, -51.9253},  // Brazil
	"ru": {61.5240, 105.3188},   // Russia
	"ja": {36.2048, 138.2529},   // Japan
	"zh": {35.8617, 104.1954},   // China
	"ko": {35.9078, 127.7669},   // South Korea
	"ar": {26.8206, 30.8025},    // Egypt (Arabic hub)
	"hi": {20.5937, 78.9629},    // India
	"pl": {51.9194, 19.1451},    // Poland
	"nl": {52.1326, 5.2913},     // Netherlands
	"sv": {60.1282, 18.6435},    // Sweden
	"uk": {48.3794, 31.1656},    // Ukraine
	"vi": {14.0583, 108.2772},   // Vietnam
	"fa": {32.4279, 53.6880},    // Iran
	"id": {-0.7893, 113.9213},   // Indonesia
	"tr": {38.9637, 35.2433},    // Turkey
	"th": {15.8700, 100.9925},   // Thailand
	"he": {31.0461, 34.8516},    // Israel
	"fi": {61.9241, 25.7482},    // Finland
	"cs": {49.8175, 15.4730},    // Czech Republic
	"el": {39.0742, 21.8243},    // Greece
	"da": {56.2639, 9.5018},     // Denmark
	"no": {60.4720, 8.4689},     // Norway
	"hu": {47.1625, 19.5033},    // Hungary
	"ro": {45.9432, 24.9668},    // Romania
	"ca": {41.5912, 1.5209},     // Catalonia
	"sr": {44.0165, 21.0059},    // Serbia
	"bg": {42.7339, 25.4858},    // Bulgaria
	"ms": {4.2105, 101.9758},    // Malaysia
}

// GetWikiCentroid returns the centroid for a wiki language code.
// Returns (0,0, false) if unknown.
func GetWikiCentroid(lang string) (float64, float64, bool) {
	c, ok := wikiCentroids[lang]
	if !ok {
		return 0, 0, false
	}
	return c.Lat, c.Lng, true
}

// placeCoord is a keyword-to-coordinate mapping for semantic geocoding.
type placeCoord struct {
	Keyword string
	Lat     float64
	Lng     float64
}

// placeKeywords maps place names commonly found in article titles to coordinates.
// Longer/more specific keywords are listed first so they match before shorter ones.
var placeKeywords = []placeCoord{
	// Regions & conflicts
	{"Kashmir", 34.0837, 74.7973},
	{"Jammu", 32.7266, 74.8570},
	{"Crimea", 45.3453, 34.0549},
	{"Donbas", 48.0159, 37.8028},
	{"Gaza", 31.3547, 34.3088},
	{"West Bank", 31.9466, 35.3027},
	{"Xinjiang", 41.1129, 85.2401},
	{"Tibet", 29.6524, 91.1719},
	{"Taiwan", 23.6978, 120.9605},
	{"Hong Kong", 22.3193, 114.1694},
	{"Catalonia", 41.5912, 1.5209},
	{"Kurdistan", 36.4103, 44.3872},
	{"Sahel", 14.4974, 2.1160},
	{"Balkans", 42.7339, 21.0059},
	{"Caucasus", 42.2, 43.5},

	// Countries
	{"United States", 38.9072, -77.0369},
	{"United Kingdom", 51.5074, -0.1278},
	{"Ukraine", 48.3794, 31.1656},
	{"Russia", 55.7558, 37.6173},
	{"China", 39.9042, 116.4074},
	{"Japan", 35.6762, 139.6503},
	{"India", 28.6139, 77.2090},
	{"Pakistan", 33.6844, 73.0479},
	{"Iran", 35.6892, 51.3890},
	{"Iraq", 33.3152, 44.3661},
	{"Syria", 33.5138, 36.2765},
	{"Israel", 31.7683, 35.2137},
	{"Palestine", 31.9522, 35.2332},
	{"Egypt", 30.0444, 31.2357},
	{"Turkey", 39.9334, 32.8597},
	{"Germany", 52.5200, 13.4050},
	{"France", 48.8566, 2.3522},
	{"Brazil", -15.7975, -47.8919},
	{"Mexico", 19.4326, -99.1332},
	{"Canada", 45.4215, -75.6972},
	{"Australia", -35.2809, 149.1300},
	{"South Korea", 37.5665, 126.9780},
	{"North Korea", 39.0392, 125.7625},
	{"South Africa", -25.7479, 28.2293},
	{"Nigeria", 9.0579, 7.4951},
	{"Kenya", -1.2921, 36.8219},
	{"Ethiopia", 9.0250, 38.7469},
	{"Saudi Arabia", 24.7136, 46.6753},
	{"Afghanistan", 34.5553, 69.2075},
	{"Myanmar", 19.7633, 96.0785},
	{"Thailand", 13.7563, 100.5018},
	{"Vietnam", 21.0285, 105.8542},
	{"Indonesia", -6.2088, 106.8456},
	{"Philippines", 14.5995, 120.9842},
	{"Argentina", -34.6037, -58.3816},
	{"Colombia", 4.7110, -74.0721},
	{"Venezuela", 10.4806, -66.9036},
	{"Cuba", 23.1136, -82.3666},
	{"Poland", 52.2297, 21.0122},
	{"Italy", 41.9028, 12.4964},
	{"Spain", 40.4168, -3.7038},
	{"Netherlands", 52.3676, 4.9041},
	{"Sweden", 59.3293, 18.0686},
	{"Norway", 59.9139, 10.7522},

	// Adjective forms ("Russian", "American", "Chinese" etc.)
	{"American", 38.9072, -77.0369},
	{"Russian", 55.7558, 37.6173},
	{"Chinese", 39.9042, 116.4074},
	{"Japanese", 35.6762, 139.6503},
	{"Indian", 28.6139, 77.2090},
	{"Israeli", 31.7683, 35.2137},
	{"Palestinian", 31.9522, 35.2332},
	{"Ukrainian", 48.3794, 31.1656},
	{"Iranian", 35.6892, 51.3890},
	{"Iraqi", 33.3152, 44.3661},
	{"Syrian", 33.5138, 36.2765},
	{"Turkish", 39.9334, 32.8597},
	{"Brazilian", -15.7975, -47.8919},
	{"Mexican", 19.4326, -99.1332},
	{"British", 51.5074, -0.1278},
	{"Korean", 37.5665, 126.9780},
	{"Pakistani", 33.6844, 73.0479},
	{"Afghan", 34.5553, 69.2075},
	{"Nigerian", 9.0579, 7.4951},
	{"Egyptian", 30.0444, 31.2357},
	{"Saudi", 24.7136, 46.6753},
	{"German", 52.5200, 13.4050},
	{"French", 48.8566, 2.3522},
}

// semanticGeocode attempts to extract a geographic location from an article
// title by matching known place/country keywords. Returns the coordinates of
// the first (most specific) match.
func semanticGeocode(title string) (float64, float64, bool) {
	lower := strings.ToLower(title)
	for _, pc := range placeKeywords {
		if strings.Contains(lower, strings.ToLower(pc.Keyword)) {
			return pc.Lat, pc.Lng, true
		}
	}
	return 0, 0, false
}

// respondJSON writes a JSON payload with the given HTTP status.
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Only set no-store when the handler hasn't already set a Cache-Control header
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// respondError writes a standard error response.
func respondError(w http.ResponseWriter, status int, message, code string) {
	respondJSON(w, status, ErrorResponse{
		Error: message,
		Code:  code,
	})
}

// respondErrorWithDetails writes an error response with additional details.
func respondErrorWithDetails(w http.ResponseWriter, status int, message, code, details string) {
	respondJSON(w, status, ErrorResponse{
		Error:   message,
		Code:    code,
		Details: details,
	})
}

// parseIntQuery reads an integer query param with a default and max.
func parseIntQuery(r *http.Request, name string, defaultVal, maxVal int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultVal, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, strconv.ErrRange
	}
	if maxVal > 0 && v > maxVal {
		return 0, strconv.ErrRange
	}
	return v, nil
}

// parseTimeQuery reads a time query param (RFC3339 or Unix seconds).
func parseTimeQuery(r *http.Request, name string, defaultVal time.Time) (time.Time, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultVal, nil
	}
	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	// Try Unix timestamp
	unix, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(unix, 0), nil
}

// parseBoolQuery reads a boolean query param with a default value.
func parseBoolQuery(r *http.Request, name string, defaultVal bool) bool {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return defaultVal
	}
	return v
}
