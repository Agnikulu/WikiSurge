package api

import (
	"encoding/json"
	"net/http"
	"regexp"
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
	LocationSource string  `json:"location_source"` // "article", "wikidata", "semantic", or "wiki_centroid"
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
	LocationSource string      `json:"location_source"` // "article", "wikidata", "semantic", or "wiki_centroid"
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
	"bn": {23.6850, 90.3563},    // Bangladesh (Bengali)
	"ta": {11.1271, 78.6569},    // Tamil Nadu, India
	"te": {15.9129, 79.7400},    // Andhra Pradesh, India (Telugu)
	"ml": {10.8505, 76.2711},    // Kerala, India (Malayalam)
	"kn": {15.3173, 75.7139},    // Karnataka, India (Kannada)
	"mr": {19.7515, 75.7139},    // Maharashtra, India (Marathi)
	"gu": {22.2587, 71.1924},    // Gujarat, India (Gujarati)
	"pa": {31.1471, 75.3412},    // Punjab, India (Punjabi)
	"ur": {30.3753, 69.3451},    // Pakistan (Urdu)
	"ne": {28.3949, 84.1240},    // Nepal (Nepali)
	"si": {7.8731, 80.7718},     // Sri Lanka (Sinhala)
	"my": {19.7633, 96.0785},    // Myanmar (Burmese)
	"km": {12.5657, 104.9910},   // Cambodia (Khmer)
	"lo": {19.8563, 102.4955},   // Laos (Lao)
	"ka": {42.3154, 43.3569},    // Georgia (Georgian)
	"hy": {40.0691, 45.0382},    // Armenia (Armenian)
	"az": {40.1431, 47.5769},    // Azerbaijan (Azerbaijani)
	"uz": {41.3775, 64.5853},    // Uzbekistan (Uzbek)
	"kk": {48.0196, 66.9237},    // Kazakhstan (Kazakh)
	"tl": {14.5995, 120.9842},   // Philippines (Tagalog)
	"sw": {-6.3690, 34.8888},    // Tanzania (Swahili)
	"am": {9.1450, 40.4897},     // Ethiopia (Amharic)
	"yo": {7.9465, 3.7842},      // Nigeria (Yoruba)
	"ha": {12.0, 8.5167},        // Nigeria (Hausa)
	"af": {-30.5595, 22.9375},   // South Africa (Afrikaans)
	"eu": {43.2627, -2.9253},    // Basque Country
	"gl": {42.5751, -8.1339},    // Galicia, Spain (Galician)
	"cy": {52.1307, -3.7837},    // Wales (Welsh)
	"ga": {53.1424, -7.6921},    // Ireland (Irish)
	"is": {64.9631, -19.0208},   // Iceland (Icelandic)
	"sq": {41.1533, 20.1683},    // Albania (Albanian)
	"mk": {41.5122, 21.7453},    // North Macedonia (Macedonian)
	"hr": {45.1000, 15.2000},    // Croatia (Croatian)
	"sk": {48.6690, 19.6990},    // Slovakia (Slovak)
	"sl": {46.1512, 14.9955},    // Slovenia (Slovenian)
	"lt": {55.1694, 23.8813},    // Lithuania (Lithuanian)
	"lv": {56.8796, 24.6032},    // Latvia (Latvian)
	"et": {58.5953, 25.0136},    // Estonia (Estonian)
	"be": {53.7098, 27.9534},    // Belarus (Belarusian)
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
	{"Switzerland", 46.9480, 7.4474},
	{"Austria", 48.2082, 16.3738},
	{"Belgium", 50.8503, 4.3517},
	{"Portugal", 38.7223, -9.1393},
	{"Greece", 37.9838, 23.7275},
	{"Czech Republic", 50.0755, 14.4378},
	{"Ireland", 53.3498, -6.2603},
	{"New Zealand", -41.2865, 174.7762},

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
	{"Swiss", 46.9480, 7.4474},
	{"Austrian", 48.2082, 16.3738},
	{"Belgian", 50.8503, 4.3517},
	{"Portuguese", 38.7223, -9.1393},
	{"Greek", 37.9838, 23.7275},
}

// semanticGeocode attempts to extract a geographic location from an article
// title by matching known place/country keywords. Returns the coordinates of
// the first (most specific) match. Kept as a lightweight fallback within
// extractLocationNER.
func semanticGeocode(title string) (float64, float64, bool) {
	lower := strings.ToLower(title)
	for _, pc := range placeKeywords {
		if strings.Contains(lower, strings.ToLower(pc.Keyword)) {
			return pc.Lat, pc.Lng, true
		}
	}
	return 0, 0, false
}

// ---------------------------------------------------------------------------
// Rule-based NER location extractor
// ---------------------------------------------------------------------------
// Instead of blind keyword matching, this extracts locations by recognising
// natural-language patterns that indicate WHERE something is. This correctly
// resolves "Forest Hill High School" (on de.wiki) to the United States because
// the intro says "...is a public high school in Jackson, Mississippi".
//
// Strategy:
//  1. Contextual patterns — scan for "in [Place], [Place]" / "located in" / etc.
//     and resolve the captured place names to coordinates.
//  2. US state detection — articles mentioning a US state name in context are
//     placed there rather than falling through to wiki-language centroid.
//  3. Keyword fallback — if no contextual pattern matches, fall back to the
//     existing placeKeywords list (countries, regions, adjective forms).

// locationPattern defines a regex + group index that captures location text.
type locationPattern struct {
	re       *regexp.Regexp
	locGroup int // capture group index holding the location text
}

// locationPatterns are compiled once at init. Ordered by specificity.
var locationPatterns = func() []locationPattern {
	raw := []struct {
		pattern  string
		locGroup int
	}{
		// "in City, State, Country" / "in City, Country"
		{`\bin\s+([A-Z][\w\s-]+(?:,\s*[A-Z][\w\s-]+){1,3})`, 1},
		// "located in Place" / "situated in Place" / "based in Place"
		{`(?:located|situated|based|headquartered)\s+in\s+([A-Z][\w\s,-]+)`, 1},
		// "is a ... in Place, Place"
		{`is\s+(?:a|an|the)\s+[\w\s]+?\bin\s+([A-Z][\w\s-]+(?:,\s*[A-Z][\w\s-]+){0,3})`, 1},
		// "City, State" at sentence start (common in short descriptions)
		{`^([A-Z][\w\s-]+,\s+[A-Z][\w\s-]+)`, 1},
	}
	pats := make([]locationPattern, 0, len(raw))
	for _, r := range raw {
		pats = append(pats, locationPattern{
			re:       regexp.MustCompile(r.pattern),
			locGroup: r.locGroup,
		})
	}
	return pats
}()

// usStates maps US state names to approximate coordinates.
var usStates = map[string][2]float64{
	"alabama": {32.3182, -86.9023}, "alaska": {64.2008, -152.4937},
	"arizona": {34.0489, -111.0937}, "arkansas": {34.7465, -92.2896},
	"california": {36.7783, -119.4179}, "colorado": {39.5501, -105.7821},
	"connecticut": {41.6032, -73.0877}, "delaware": {38.9108, -75.5277},
	"florida": {27.6648, -81.5158}, "georgia": {33.7490, -84.3880},
	"hawaii": {19.8968, -155.5828}, "idaho": {44.0682, -114.7420},
	"illinois": {40.6331, -89.3985}, "indiana": {40.2672, -86.1349},
	"iowa": {41.8780, -93.0977}, "kansas": {39.0119, -98.4842},
	"kentucky": {37.8393, -84.2700}, "louisiana": {30.9843, -91.9623},
	"maine": {45.2538, -69.4455}, "maryland": {39.0458, -76.6413},
	"massachusetts": {42.4072, -71.3824}, "michigan": {44.3148, -85.6024},
	"minnesota": {46.7296, -94.6859}, "mississippi": {32.3547, -89.3985},
	"missouri": {37.9643, -91.8318}, "montana": {46.8797, -110.3626},
	"nebraska": {41.4925, -99.9018}, "nevada": {38.8026, -116.4194},
	"new hampshire": {43.1939, -71.5724}, "new jersey": {40.0583, -74.4057},
	"new mexico": {34.5199, -105.8701}, "new york": {40.7128, -74.0060},
	"north carolina": {35.7596, -79.0193}, "north dakota": {47.5515, -101.0020},
	"ohio": {40.4173, -82.9071}, "oklahoma": {35.4676, -97.5164},
	"oregon": {43.8041, -120.5542}, "pennsylvania": {41.2033, -77.1945},
	"rhode island": {41.5801, -71.4774}, "south carolina": {33.8361, -81.1637},
	"south dakota": {43.9695, -99.9018}, "tennessee": {35.5175, -86.5804},
	"texas": {31.9686, -99.9018}, "utah": {39.3210, -111.0937},
	"vermont": {44.5588, -72.5778}, "virginia": {37.4316, -78.6569},
	"washington": {47.7511, -120.7401}, "west virginia": {38.5976, -80.4549},
	"wisconsin": {43.7844, -88.7879}, "wyoming": {43.0760, -107.2903},
	"district of columbia": {38.9072, -77.0369},
}

// extractLocationNER uses rule-based NER to extract geographic locations from
// combined title + extract text. Falls back to keyword matching.
func extractLocationNER(text string) (float64, float64, bool) {
	// Try contextual patterns first (most accurate)
	for _, pat := range locationPatterns {
		if m := pat.re.FindStringSubmatch(text); m != nil && pat.locGroup < len(m) {
			locText := strings.TrimSpace(m[pat.locGroup])
			// Strip trailing punctuation from the captured location
			locText = strings.TrimRight(locText, ".,;:")
			if lat, lng, ok := resolveLocationText(locText); ok {
				return lat, lng, true
			}
		}
	}

	// Fall back to keyword matching on the full text
	return semanticGeocode(text)
}

// resolveLocationText takes a captured location string like "Jackson, Mississippi"
// or "Queens, New York, United States" and resolves it to coordinates.
// It splits on commas and tries the most specific segment first (leftmost =
// typically city/state), working rightward toward country.
func resolveLocationText(locText string) (float64, float64, bool) {
	parts := strings.Split(locText, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	// Try left-to-right: most specific (city/state) before least specific (country)
	for _, seg := range parts {
		if seg == "" {
			continue
		}
		// Check US states first (handles "Mississippi", "New York")
		if coords, ok := usStates[strings.ToLower(seg)]; ok {
			return coords[0], coords[1], true
		}
		// Check country/region keywords
		if lat, lng, ok := semanticGeocode(seg); ok {
			return lat, lng, true
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
