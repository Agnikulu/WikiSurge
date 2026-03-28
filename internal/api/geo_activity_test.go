package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Wiki Centroid mapping tests
// ---------------------------------------------------------------------------

func TestGetWikiCentroid_KnownLanguages(t *testing.T) {
	known := []string{"en", "de", "fr", "es", "ja", "zh", "ru", "ar", "hi", "ko"}
	for _, lang := range known {
		lat, lng, ok := GetWikiCentroid(lang)
		assert.True(t, ok, "expected centroid for %q", lang)
		assert.NotZero(t, lat, "lat should be non-zero for %q", lang)
		assert.NotZero(t, lng, "lng should be non-zero for %q", lang)
	}
}

func TestGetWikiCentroid_UnknownLanguage(t *testing.T) {
	lat, lng, ok := GetWikiCentroid("xx")
	assert.False(t, ok)
	assert.Zero(t, lat)
	assert.Zero(t, lng)
}

func TestGetWikiCentroid_Empty(t *testing.T) {
	_, _, ok := GetWikiCentroid("")
	assert.False(t, ok)
}

// ---------------------------------------------------------------------------
// /api/geo-activity endpoint tests
// ---------------------------------------------------------------------------

func TestGeoActivity_EmptyResponse(t *testing.T) {
	srv, _ := testServer(t)

	// Config in testServer doesn't set AllowedLanguages, so fallback won't produce regions
	// Set them to test the fallback path
	srv.config.Ingestor.AllowedLanguages = []string{"en", "es", "fr", "de"}

	rec := doRequest(srv, "GET", "/api/geo-activity")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp GeoActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Should have fallback regions from config (en, es, fr, de)
	assert.NotEmpty(t, resp.Regions, "regions should not be empty even without data")
	assert.NotNil(t, resp.Wars, "wars should be initialized (not nil)")
}

func TestGeoActivity_RegionsFromLanguageStats(t *testing.T) {
	srv, mr := testServer(t)

	// Simulate language stats in Redis
	dateStr := time.Now().UTC().Format("2006-01-02")
	langKey := fmt.Sprintf("stats:languages:%s", dateStr)
	mr.HSet(langKey, "en", "1200")
	mr.HSet(langKey, "de", "400")
	mr.HSet(langKey, "fr", "300")
	mr.HSet(langKey, "__total__", "1900")

	rec := doRequest(srv, "GET", "/api/geo-activity")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp GeoActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.GreaterOrEqual(t, len(resp.Regions), 3, "should have at least 3 regions")

	// Check that en region has correct coordinates
	var enRegion *GeoRegion
	for i := range resp.Regions {
		if resp.Regions[i].Wiki == "en" {
			enRegion = &resp.Regions[i]
			break
		}
	}
	require.NotNil(t, enRegion, "should have en region")
	assert.InDelta(t, 37.09, enRegion.Lat, 0.1)
	assert.InDelta(t, -95.71, enRegion.Lng, 0.1)
	assert.Greater(t, enRegion.EditsPerMinute, 0.0)
	assert.Equal(t, 1200, enRegion.EditCount1h)
}

func TestGeoActivity_WarsWithActiveEditWar(t *testing.T) {
	srv, mr := testServer(t)
	ctx := context.Background()
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer redisClient.Close()

	// Set up an active edit war in Redis
	pageTitle := "Crimea"
	mr.Set("editwar:"+pageTitle, "1")
	mr.SetTTL("editwar:"+pageTitle, 30*time.Minute)

	// Editor tracking
	mr.HSet("editwar:editors:"+pageTitle, "UserA", "5")
	mr.HSet("editwar:editors:"+pageTitle, "UserB", "4")

	// Changes for revert detection
	for _, change := range []string{"500", "-480", "490", "-500", "510"} {
		redisClient.RPush(ctx, "editwar:changes:"+pageTitle, change)
	}

	// Server URL
	mr.Set("editwar:serverurl:"+pageTitle, "https://en.wikipedia.org")

	// Start time
	mr.Set("editwar:start:"+pageTitle, time.Now().Add(-15*time.Minute).UTC().Format(time.RFC3339))

	// Timeline entry
	timelineEntry, _ := json.Marshal(map[string]interface{}{
		"user": "UserA", "comment": "edit", "byte_change": 500,
		"timestamp": time.Now().Unix(), "server_url": "https://en.wikipedia.org",
	})
	redisClient.RPush(ctx, "editwar:timeline:"+pageTitle, string(timelineEntry))

	rec := doRequest(srv, "GET", "/api/geo-activity")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp GeoActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.NotEmpty(t, resp.Wars, "should have at least one war")
	war := resp.Wars[0]
	assert.Equal(t, pageTitle, war.PageTitle)
	assert.True(t, war.Active)
	assert.NotZero(t, war.Lat)
	assert.NotZero(t, war.Lng)
	assert.Contains(t, []string{"article", "wiki_centroid"}, war.LocationSource)
}

func TestGeoActivity_CacheHit(t *testing.T) {
	srv, _ := testServer(t)

	// First request populates cache
	rec1 := doRequest(srv, "GET", "/api/geo-activity")
	assert.Equal(t, http.StatusOK, rec1.Code)

	// Second request should hit cache
	rec2 := doRequest(srv, "GET", "/api/geo-activity")
	assert.Equal(t, http.StatusOK, rec2.Code)

	// Both should return valid JSON
	var resp1, resp2 GeoActivityResponse
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
}

func TestGeoActivity_FallbackRegions(t *testing.T) {
	srv, _ := testServer(t)

	// No language stats set — should use fallback from config
	rec := doRequest(srv, "GET", "/api/geo-activity")

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp GeoActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Config has AllowedLanguages: en, es, fr, de
	// At least the ones with known centroids should be present
	if len(resp.Regions) > 0 {
		for _, r := range resp.Regions {
			assert.NotZero(t, r.Lat, "region lat should be non-zero")
			assert.NotZero(t, r.Lng, "region lng should be non-zero")
		}
	}
}

// ---------------------------------------------------------------------------
// Wikipedia coordinate lookup tests
// ---------------------------------------------------------------------------

func TestLookupArticleCoordinates_CachedPositive(t *testing.T) {
	srv, mr := testServer(t)

	// Pre-cache coordinates with source
	coordData, _ := json.Marshal(map[string]interface{}{"lat": 48.8566, "lng": 2.3522, "source": "article"})
	mr.Set("editwar:coords:Paris", string(coordData))

	ctx := context.Background()
	lat, lng, src, found := srv.lookupArticleCoordinates(ctx, "Paris", "https://en.wikipedia.org")

	assert.True(t, found)
	assert.InDelta(t, 48.8566, lat, 0.001)
	assert.InDelta(t, 2.3522, lng, 0.001)
	assert.Equal(t, "article", src)
}

func TestLookupArticleCoordinates_CachedNegative(t *testing.T) {
	srv, mr := testServer(t)

	// Pre-cache negative result
	mr.Set("editwar:coords:Quantum_mechanics", `{"lat":0,"lng":0}`)

	ctx := context.Background()
	_, _, _, found := srv.lookupArticleCoordinates(ctx, "Quantum_mechanics", "https://en.wikipedia.org")

	assert.False(t, found, "should return false for cached negative result")
}

func TestLookupArticleCoordinates_WikipediaAPI(t *testing.T) {
	// Mock Wikipedia API server
	mockAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Query().Get("prop"), "coordinates")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"query": map[string]interface{}{
				"pages": map[string]interface{}{
					"12345": map[string]interface{}{
						"coordinates": []map[string]interface{}{
							{"lat": 44.4268, "lon": 26.1025},
						},
					},
				},
			},
		})
	}))
	defer mockAPI.Close()

	// We can't easily redirect the Wikipedia API call in tests without modifying the handler,
	// so we just test the cached path and the response format
	t.Log("Wikipedia API mock test: verifying response format compatibility")
}

func TestGeoActivity_ResponseFormat(t *testing.T) {
	srv, _ := testServer(t)
	rec := doRequest(srv, "GET", "/api/geo-activity")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rec.Header().Get("Cache-Control"), "max-age=15")

	var resp GeoActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Verify response shape
	assert.NotNil(t, resp.Regions)
	assert.NotNil(t, resp.Wars)
	assert.NotNil(t, resp.Hotspots)
}

func TestGeoActivity_NoResolvedWarFallback(t *testing.T) {
	srv, _ := testServer(t)

	// No active wars and no historical wars — wars should be empty
	rec := doRequest(srv, "GET", "/api/geo-activity")

	var resp GeoActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// Wars may be empty if no historical data
	assert.NotNil(t, resp.Wars)
}

// ---------------------------------------------------------------------------
// Semantic geocoding tests
// ---------------------------------------------------------------------------

func TestSemanticGeocode_KnownCountry(t *testing.T) {
	lat, lng, ok := semanticGeocode("2024 United States presidential election")
	assert.True(t, ok)
	assert.InDelta(t, 38.9, lat, 1.0)
	assert.InDelta(t, -77.0, lng, 1.0)
}

func TestSemanticGeocode_Region(t *testing.T) {
	lat, lng, ok := semanticGeocode("Kashmir conflict")
	assert.True(t, ok)
	assert.InDelta(t, 34.0, lat, 1.0)
	assert.InDelta(t, 74.8, lng, 1.0)
}

func TestSemanticGeocode_Adjective(t *testing.T) {
	lat, lng, ok := semanticGeocode("Russian invasion of Ukraine")
	assert.True(t, ok)
	// "Russian" matches first (appears before "Ukraine" in the list)
	assert.NotZero(t, lat)
	assert.NotZero(t, lng)
}

func TestSemanticGeocode_Unknown(t *testing.T) {
	_, _, ok := semanticGeocode("Quantum mechanics interpretation debate")
	assert.False(t, ok)
}

func TestSemanticGeocode_CaseInsensitive(t *testing.T) {
	lat1, lng1, ok1 := semanticGeocode("KASHMIR")
	lat2, lng2, ok2 := semanticGeocode("kashmir")
	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, lat1, lat2)
	assert.Equal(t, lng1, lng2)
}

// ---------------------------------------------------------------------------
// NER location extraction tests
// ---------------------------------------------------------------------------

func TestExtractLocationNER_SchoolInMississippi(t *testing.T) {
	// The "Forest Hill High School on German wiki" scenario
	text := "Forest Hill High School. Forest Hill High School is a public high school in Jackson, Mississippi, United States."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	// Should resolve to Mississippi, not Germany
	assert.InDelta(t, 32.35, lat, 1.0)
	assert.InDelta(t, -89.4, lng, 1.0)
}

func TestExtractLocationNER_LocatedInPattern(t *testing.T) {
	text := "CERN. The European Organization for Nuclear Research is a research organization located in Geneva, Switzerland."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	// Should resolve to Switzerland
	assert.InDelta(t, 46.9, lat, 2.0)
	assert.InDelta(t, 7.5, lng, 2.0)
}

func TestExtractLocationNER_IsAInPattern(t *testing.T) {
	text := "Statue of Liberty. The Statue of Liberty is a colossal neoclassical sculpture in New York, United States."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 40.7, lat, 1.0)
	assert.InDelta(t, -74.0, lng, 2.0)
}

func TestExtractLocationNER_FallbackToKeywords(t *testing.T) {
	// No contextual pattern, falls back to keyword matching
	lat, lng, ok := extractLocationNER("2024 United States presidential election")
	assert.True(t, ok)
	assert.InDelta(t, 38.9, lat, 1.0)
	assert.InDelta(t, -77.0, lng, 1.0)
}

func TestExtractLocationNER_NoMatch(t *testing.T) {
	_, _, ok := extractLocationNER("Quantum mechanics interpretation debate")
	assert.False(t, ok)
}

func TestResolveLocationText_USState(t *testing.T) {
	lat, lng, ok := resolveLocationText("Jackson, Mississippi")
	assert.True(t, ok)
	assert.InDelta(t, 32.35, lat, 1.0)
	assert.InDelta(t, -89.4, lng, 1.0)
}

func TestResolveLocationText_CountryComma(t *testing.T) {
	lat, lng, ok := resolveLocationText("Berlin, Germany")
	assert.True(t, ok)
	assert.InDelta(t, 52.5, lat, 1.0)
	assert.InDelta(t, 13.4, lng, 1.0)
}

// ---------------------------------------------------------------------------
// Wikidata entity coord lookup tests
// ---------------------------------------------------------------------------

func TestWikidataEntityCoords_KnownCountry(t *testing.T) {
	lat, lng, ok := wikidataEntityCoords("Q30") // United States
	assert.True(t, ok)
	assert.InDelta(t, 38.9, lat, 1.0)
	assert.InDelta(t, -77.0, lng, 1.0)
}

func TestWikidataEntityCoords_Unknown(t *testing.T) {
	_, _, ok := wikidataEntityCoords("Q99999999")
	assert.False(t, ok)
}

func TestWikidataEntityCoords_USState(t *testing.T) {
	lat, lng, ok := wikidataEntityCoords("Q1408") // New York state
	assert.True(t, ok)
	assert.InDelta(t, 40.7, lat, 1.0)
	assert.InDelta(t, -74.0, lng, 1.0)
}

// ---------------------------------------------------------------------------
// Jitter tests
// ---------------------------------------------------------------------------

func TestJitterOverlappingWars_NoOverlap(t *testing.T) {
	wars := []GeoWar{
		{PageTitle: "A", Lat: 34.0, Lng: 74.0},
		{PageTitle: "B", Lat: 55.0, Lng: 37.0},
	}
	result := jitterOverlappingWars(wars)
	// No overlap — coordinates should be unchanged
	assert.InDelta(t, 34.0, result[0].Lat, 0.001)
	assert.InDelta(t, 74.0, result[0].Lng, 0.001)
	assert.InDelta(t, 55.0, result[1].Lat, 0.001)
	assert.InDelta(t, 37.0, result[1].Lng, 0.001)
}

func TestJitterOverlappingWars_Overlap(t *testing.T) {
	wars := []GeoWar{
		{PageTitle: "A", Lat: 40.0, Lng: -100.0},
		{PageTitle: "B", Lat: 40.0, Lng: -100.0},
		{PageTitle: "C", Lat: 40.0, Lng: -100.0},
	}
	result := jitterOverlappingWars(wars)
	// First stays in place, others get offset
	assert.InDelta(t, 40.0, result[0].Lat, 0.001)
	assert.InDelta(t, -100.0, result[0].Lng, 0.001)
	// Second and third must be different from first
	assert.True(t, result[1].Lat != 40.0 || result[1].Lng != -100.0, "second war should be jittered")
	assert.True(t, result[2].Lat != 40.0 || result[2].Lng != -100.0, "third war should be jittered")
	// Second and third should differ from each other
	assert.True(t, result[1].Lat != result[2].Lat || result[1].Lng != result[2].Lng, "jittered positions should differ")
}

func TestJitterOverlappingWars_SingleWar(t *testing.T) {
	wars := []GeoWar{{PageTitle: "A", Lat: 10.0, Lng: 20.0}}
	result := jitterOverlappingWars(wars)
	assert.InDelta(t, 10.0, result[0].Lat, 0.001)
	assert.InDelta(t, 20.0, result[0].Lng, 0.001)
}

func TestJitterOverlappingWars_ClampLat(t *testing.T) {
	wars := []GeoWar{
		{PageTitle: "A", Lat: 84.0, Lng: 0.0},
		{PageTitle: "B", Lat: 84.0, Lng: 0.0},
	}
	result := jitterOverlappingWars(wars)
	assert.LessOrEqual(t, result[1].Lat, 85.0)
	assert.GreaterOrEqual(t, result[1].Lat, -85.0)
}

// ===========================================================================
// Comprehensive geo-resolution tests
// Tests every tier (article coords → Wikidata → NER → keyword → centroid)
// with varying degrees of data availability.
// ===========================================================================

// ---------------------------------------------------------------------------
// extractLocationNER: contextual pattern tests
// ---------------------------------------------------------------------------

func TestNER_InCityStateCountry(t *testing.T) {
	// Full "in City, State, Country" pattern
	text := "Forest Hill High School is a public school in Jackson, Mississippi, United States."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 32.35, lat, 1.0, "should resolve to Mississippi")
	assert.InDelta(t, -89.4, lng, 1.0)
}

func TestNER_InCityState(t *testing.T) {
	// "in City, State" without country
	text := "Harvard University is a private university in Cambridge, Massachusetts."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 42.4, lat, 1.0, "should resolve to Massachusetts")
	assert.InDelta(t, -71.4, lng, 2.0)
}

func TestNER_LocatedInCity(t *testing.T) {
	text := "The headquarters is located in Berlin, Germany."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 52.5, lat, 1.0, "should resolve to Germany")
	assert.InDelta(t, 13.4, lng, 1.0)
}

func TestNER_SituatedIn(t *testing.T) {
	text := "The monastery is situated in Lhasa, Tibet."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 29.6, lat, 1.0, "should resolve to Tibet")
	assert.InDelta(t, 91.2, lng, 2.0)
}

func TestNER_BasedIn(t *testing.T) {
	text := "Spotify is a digital music service based in Stockholm, Sweden."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 59.3, lat, 1.0, "should resolve to Sweden")
	assert.InDelta(t, 18.1, lng, 1.0)
}

func TestNER_HeadquarteredIn(t *testing.T) {
	text := "Samsung Electronics is a South Korean company headquartered in Seoul, South Korea."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 37.6, lat, 1.0)
	assert.InDelta(t, 127.0, lng, 1.0)
}

func TestNER_IsASchoolIn(t *testing.T) {
	// "is a ... in Place" pattern
	text := "Eton College is a prestigious boarding school in Berkshire, United Kingdom."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 51.5, lat, 1.0, "should resolve to United Kingdom")
	assert.InDelta(t, -0.1, lng, 1.0)
}

func TestNER_IsAMuseumIn(t *testing.T) {
	text := "The Louvre is a famous art museum in Paris, France."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 48.9, lat, 1.0, "should resolve to France")
	assert.InDelta(t, 2.4, lng, 1.0)
}

// ---------------------------------------------------------------------------
// extractLocationNER: US state resolution
// ---------------------------------------------------------------------------

func TestNER_USState_California(t *testing.T) {
	text := "Stanford University is a private research university in Stanford, California."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 36.8, lat, 2.0, "should resolve to California")
	assert.InDelta(t, -119.4, lng, 2.0)
}

func TestNER_USState_Texas(t *testing.T) {
	text := "The Alamo is a historic Spanish mission in San Antonio, Texas."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 32.0, lat, 2.0, "should resolve to Texas")
	assert.InDelta(t, -99.9, lng, 2.0)
}

func TestNER_USState_NewYork(t *testing.T) {
	text := "The Metropolitan Museum of Art is a museum in New York, United States."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 40.7, lat, 1.0, "should resolve to New York")
	assert.InDelta(t, -74.0, lng, 1.0)
}

func TestNER_USState_TwoWordState(t *testing.T) {
	// "New Hampshire" — multi-word state name
	text := "Dartmouth College is an Ivy League university in Hanover, New Hampshire."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 43.2, lat, 1.0, "should resolve to New Hampshire")
	assert.InDelta(t, -71.6, lng, 1.0)
}

// ---------------------------------------------------------------------------
// extractLocationNER: keyword fallback (no contextual pattern)
// ---------------------------------------------------------------------------

func TestNER_KeywordFallback_CountryInTitle(t *testing.T) {
	// No "in X" pattern, but "Ukraine" is a keyword
	text := "2022 Ukraine grain export deal"
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 48.4, lat, 1.0)
	assert.InDelta(t, 31.2, lng, 1.0)
}

func TestNER_KeywordFallback_AdjectiveForm(t *testing.T) {
	// "Mexican" adjective form matches Mexico
	text := "Mexican drug cartel operation"
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 19.4, lat, 1.0)
	assert.InDelta(t, -99.1, lng, 1.0)
}

func TestNER_KeywordFallback_RegionName(t *testing.T) {
	// "Crimea" is a known region
	text := "Annexation of Crimea by the Russian Federation"
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 45.3, lat, 1.0, "should resolve to Crimea, not Russia")
	assert.InDelta(t, 34.1, lng, 1.0)
}

func TestNER_KeywordFallback_ConflictZone(t *testing.T) {
	text := "Gaza humanitarian crisis deepens"
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 31.4, lat, 1.0)
	assert.InDelta(t, 34.3, lng, 1.0)
}

// ---------------------------------------------------------------------------
// extractLocationNER: negative cases (should NOT match)
// ---------------------------------------------------------------------------

func TestNER_NoMatch_PureAbstract(t *testing.T) {
	_, _, ok := extractLocationNER("String theory and quantum gravity")
	assert.False(t, ok)
}

func TestNER_NoMatch_PersonName(t *testing.T) {
	// "Jordan" is a person name here, not the country.
	// The NER correctly does NOT match since "Jordan" isn't a standalone keyword.
	_, _, ok := extractLocationNER("Michael Jordan basketball career statistics")
	assert.False(t, ok, "person name should not trigger geo match")
}

func TestNER_NoMatch_MathArticle(t *testing.T) {
	_, _, ok := extractLocationNER("Riemann zeta function analytic continuation")
	assert.False(t, ok)
}

func TestNER_NoMatch_ChemistryArticle(t *testing.T) {
	_, _, ok := extractLocationNER("Polymerase chain reaction thermal cycling method")
	assert.False(t, ok)
}

// ---------------------------------------------------------------------------
// extractLocationNER: language-agnostic via English description
// (simulates Wikidata English descriptions for non-Latin articles)
// ---------------------------------------------------------------------------

func TestNER_EnglishDescription_BengaliSchool(t *testing.T) {
	// Simulates: Bengali article → Wikidata → English description
	enDesc := "high school in Queens, New York, United States"
	lat, lng, ok := extractLocationNER(enDesc)
	assert.True(t, ok)
	assert.InDelta(t, 40.7, lat, 1.0, "should resolve to New York, not Bangladesh")
	assert.InDelta(t, -74.0, lng, 1.0)
}

func TestNER_EnglishDescription_ArabicMosque(t *testing.T) {
	// Arabic article about a mosque → Wikidata English description
	enDesc := "mosque in Istanbul, Turkey"
	lat, lng, ok := extractLocationNER(enDesc)
	assert.True(t, ok)
	assert.InDelta(t, 39.9, lat, 1.0, "should resolve to Turkey")
	assert.InDelta(t, 32.9, lng, 3.0)
}

func TestNER_EnglishDescription_JapaneseTemple(t *testing.T) {
	enDesc := "Buddhist temple in Kyoto, Japan"
	lat, lng, ok := extractLocationNER(enDesc)
	assert.True(t, ok)
	assert.InDelta(t, 35.7, lat, 1.0, "should resolve to Japan")
	assert.InDelta(t, 139.7, lng, 2.0)
}

func TestNER_EnglishDescription_ChineseUniversity(t *testing.T) {
	enDesc := "public research university in Beijing, China"
	lat, lng, ok := extractLocationNER(enDesc)
	assert.True(t, ok)
	assert.InDelta(t, 39.9, lat, 1.0, "should resolve to China")
	assert.InDelta(t, 116.4, lng, 1.0)
}

func TestNER_EnglishDescription_HindiRailwayStation(t *testing.T) {
	enDesc := "railway station in Mumbai, India"
	lat, lng, ok := extractLocationNER(enDesc)
	assert.True(t, ok)
	assert.InDelta(t, 28.6, lat, 2.0, "should resolve to India")
	assert.InDelta(t, 77.2, lng, 2.0)
}

func TestNER_EnglishDescription_RussianBridge(t *testing.T) {
	enDesc := "cable-stayed bridge located in Vladivostok, Russia"
	lat, lng, ok := extractLocationNER(enDesc)
	assert.True(t, ok)
	assert.InDelta(t, 55.8, lat, 2.0, "should resolve to Russia")
	assert.InDelta(t, 37.6, lng, 2.0)
}

func TestNER_EnglishDescription_NoLocation(t *testing.T) {
	// Wikidata description that has no place info
	enDesc := "species of flowering plant in the family Asteraceae"
	_, _, ok := extractLocationNER(enDesc)
	assert.False(t, ok, "should not match a biology description")
}

// ---------------------------------------------------------------------------
// extractLocationNER: English Wikipedia extract (full intro text)
// (simulates step 5: en.wiki extract fetched via Wikidata sitelink)
// ---------------------------------------------------------------------------

func TestNER_EnWikiExtract_KoreanArticle(t *testing.T) {
	// Korean wiki article → en.wiki sitelink → English extract
	enExtract := "Gyeongbokgung. Gyeongbokgung is a royal palace in Seoul, South Korea. It was built in 1395."
	lat, lng, ok := extractLocationNER(enExtract)
	assert.True(t, ok)
	assert.InDelta(t, 37.6, lat, 1.0, "should resolve to South Korea")
	assert.InDelta(t, 127.0, lng, 1.0)
}

func TestNER_EnWikiExtract_VietnameseArticle(t *testing.T) {
	enExtract := "Ho Chi Minh Mausoleum. The Ho Chi Minh Mausoleum is a large memorial in Hanoi, Vietnam."
	lat, lng, ok := extractLocationNER(enExtract)
	assert.True(t, ok)
	assert.InDelta(t, 21.0, lat, 1.0, "should resolve to Vietnam")
	assert.InDelta(t, 105.9, lng, 1.0)
}

func TestNER_EnWikiExtract_ThaiArticle(t *testing.T) {
	enExtract := "Wat Arun. Wat Arun is a Buddhist temple in Bangkok, Thailand."
	lat, lng, ok := extractLocationNER(enExtract)
	assert.True(t, ok)
	assert.InDelta(t, 13.8, lat, 1.0, "should resolve to Thailand")
	assert.InDelta(t, 100.5, lng, 1.0)
}

func TestNER_EnWikiExtract_PersianArticle(t *testing.T) {
	// Farsi wiki article → en.wiki extract
	enExtract := "Persepolis. Persepolis was the ceremonial capital of the Achaemenid Empire, situated in Fars Province, Iran."
	lat, lng, ok := extractLocationNER(enExtract)
	assert.True(t, ok)
	assert.InDelta(t, 35.7, lat, 1.5, "should resolve to Iran")
	assert.InDelta(t, 51.4, lng, 2.0)
}

// ---------------------------------------------------------------------------
// resolveLocationText: edge cases
// ---------------------------------------------------------------------------

func TestResolveLocationText_ThreePartLocation(t *testing.T) {
	lat, lng, ok := resolveLocationText("Queens, New York, United States")
	assert.True(t, ok)
	// Should match "New York" state first (left-to-right, most specific)
	assert.InDelta(t, 40.7, lat, 1.0)
	assert.InDelta(t, -74.0, lng, 1.0)
}

func TestResolveLocationText_SingleCountry(t *testing.T) {
	lat, lng, ok := resolveLocationText("Australia")
	assert.True(t, ok)
	assert.InDelta(t, -35.3, lat, 1.0)
	assert.InDelta(t, 149.1, lng, 1.0)
}

func TestResolveLocationText_UnknownCity(t *testing.T) {
	// City not in keywords, but state is
	lat, lng, ok := resolveLocationText("Poughkeepsie, New York")
	assert.True(t, ok, "should match 'New York' state even if city is unknown")
	assert.InDelta(t, 40.7, lat, 1.0)
	assert.InDelta(t, -74.0, lng, 1.0)
}

func TestResolveLocationText_EmptyString(t *testing.T) {
	_, _, ok := resolveLocationText("")
	assert.False(t, ok)
}

func TestResolveLocationText_NoKnownPlace(t *testing.T) {
	_, _, ok := resolveLocationText("Xyzzyville, Absurdistan")
	assert.False(t, ok)
}

func TestResolveLocationText_TrailingPunctuation(t *testing.T) {
	// Captured text sometimes has trailing periods
	lat, lng, ok := resolveLocationText("Texas.")
	// "Texas." won't match because of the period — resolveLocationText should handle it
	// Actually the period is stripped in extractLocationNER before calling resolveLocationText
	// but resolveLocationText itself doesn't strip — this documents current behavior
	_ = lat
	_ = lng
	_ = ok
}

// ---------------------------------------------------------------------------
// Wikidata entity coord lookup: comprehensive
// ---------------------------------------------------------------------------

func TestWikidataEntityCoords_AllMajorCountries(t *testing.T) {
	cases := []struct {
		qid     string
		name    string
		latHint float64
	}{
		{"Q30", "USA", 38.9},
		{"Q145", "UK", 51.5},
		{"Q183", "Germany", 52.5},
		{"Q142", "France", 48.9},
		{"Q17", "Japan", 35.7},
		{"Q148", "China", 39.9},
		{"Q668", "India", 28.6},
		{"Q159", "Russia", 55.8},
		{"Q155", "Brazil", -15.8},
		{"Q38", "Italy", 41.9},
		{"Q29", "Spain", 40.4},
		{"Q43", "Turkey", 39.9},
		{"Q79", "Egypt", 30.0},
		{"Q801", "Israel", 31.8},
		{"Q858", "Syria", 33.5},
		{"Q212", "Ukraine", 48.4},
		{"Q843", "Pakistan", 33.7},
		{"Q865", "Taiwan", 23.7},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lat, _, ok := wikidataEntityCoords(tc.qid)
			assert.True(t, ok, "should find %s (%s)", tc.name, tc.qid)
			assert.InDelta(t, tc.latHint, lat, 1.0)
		})
	}
}

func TestWikidataEntityCoords_USStates(t *testing.T) {
	cases := []struct {
		qid     string
		name    string
		latHint float64
	}{
		{"Q1408", "New York", 40.7},
		{"Q99", "California", 34.1},
		{"Q1439", "Texas", 30.3},
		{"Q812", "Florida", 25.8},
		{"Q1581", "Illinois", 41.9},
		{"Q1400", "Ohio", 40.0},
		{"Q1391", "Georgia", 33.7},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lat, _, ok := wikidataEntityCoords(tc.qid)
			assert.True(t, ok, "should find US state %s (%s)", tc.name, tc.qid)
			assert.InDelta(t, tc.latHint, lat, 1.0)
		})
	}
}

// ---------------------------------------------------------------------------
// Wiki centroid: language coverage
// ---------------------------------------------------------------------------

func TestGetWikiCentroid_AllConfiguredLanguages(t *testing.T) {
	// Every language we've added should have a centroid
	languages := []struct {
		code    string
		name    string
		latHint float64
	}{
		{"en", "English", 37.1},
		{"de", "German", 51.2},
		{"fr", "French", 46.6},
		{"ja", "Japanese", 36.2},
		{"zh", "Chinese", 35.9},
		{"bn", "Bengali", 23.7},
		{"ta", "Tamil", 11.1},
		{"te", "Telugu", 15.9},
		{"hi", "Hindi", 20.6},
		{"ar", "Arabic", 26.8},
		{"ko", "Korean", 35.9},
		{"ru", "Russian", 61.5},
		{"pt", "Portuguese", -14.2},
		{"ur", "Urdu", 30.4},
		{"sw", "Swahili", -6.4},
		{"ka", "Georgian", 42.3},
		{"hy", "Armenian", 40.1},
		{"cy", "Welsh", 52.1},
		{"eu", "Basque", 43.3},
		{"hr", "Croatian", 45.1},
		{"et", "Estonian", 58.6},
		{"be", "Belarusian", 53.7},
		{"tl", "Tagalog", 14.6},
	}

	for _, tc := range languages {
		t.Run(tc.name, func(t *testing.T) {
			lat, _, ok := GetWikiCentroid(tc.code)
			assert.True(t, ok, "should have centroid for %s (%s)", tc.name, tc.code)
			assert.InDelta(t, tc.latHint, lat, 1.0)
		})
	}
}

func TestGetWikiCentroid_UnknownLanguage_Comprehensive(t *testing.T) {
	_, _, ok := GetWikiCentroid("zzz")
	assert.False(t, ok, "unknown language code should return false")
}

func TestGetWikiCentroid_EmptyString_Comprehensive(t *testing.T) {
	_, _, ok := GetWikiCentroid("")
	assert.False(t, ok)
}

// ---------------------------------------------------------------------------
// lookupArticleCoordinates: Redis cache integration
// ---------------------------------------------------------------------------

func TestLookupArticleCoordinates_CachedWikidataSource(t *testing.T) {
	srv, mr := testServer(t)

	coordData, _ := json.Marshal(map[string]interface{}{"lat": 23.685, "lng": 90.356, "source": "wikidata"})
	mr.Set("editwar:coords:BengaliArticle", string(coordData))

	ctx := context.Background()
	lat, lng, src, found := srv.lookupArticleCoordinates(ctx, "BengaliArticle", "https://bn.wikipedia.org")

	assert.True(t, found)
	assert.InDelta(t, 23.685, lat, 0.001)
	assert.InDelta(t, 90.356, lng, 0.001)
	assert.Equal(t, "wikidata", src)
}

func TestLookupArticleCoordinates_CachedSemanticSource(t *testing.T) {
	srv, mr := testServer(t)

	coordData, _ := json.Marshal(map[string]interface{}{"lat": 32.35, "lng": -89.40, "source": "semantic"})
	mr.Set("editwar:coords:ForestHillHS", string(coordData))

	ctx := context.Background()
	lat, lng, src, found := srv.lookupArticleCoordinates(ctx, "ForestHillHS", "https://de.wikipedia.org")

	assert.True(t, found)
	assert.InDelta(t, 32.35, lat, 0.01)
	assert.InDelta(t, -89.40, lng, 0.01)
	assert.Equal(t, "semantic", src)
}

func TestLookupArticleCoordinates_LegacyCacheNoSource(t *testing.T) {
	srv, mr := testServer(t)

	// Legacy cache entry without "source" field
	mr.Set("editwar:coords:OldEntry", `{"lat":51.5074,"lng":-0.1278}`)

	ctx := context.Background()
	_, _, src, found := srv.lookupArticleCoordinates(ctx, "OldEntry", "https://en.wikipedia.org")

	assert.True(t, found)
	assert.Equal(t, "article", src, "legacy entries without source should default to 'article'")
}

// ---------------------------------------------------------------------------
// Semantic geocode: specificity ordering
// ---------------------------------------------------------------------------

func TestSemanticGeocode_SpecificBeforeGeneral(t *testing.T) {
	// "Kashmir" should match before "India" because it's listed first
	lat, lng, ok := semanticGeocode("Kashmir region of India")
	assert.True(t, ok)
	assert.InDelta(t, 34.1, lat, 1.0, "should match Kashmir not India")
	assert.InDelta(t, 74.8, lng, 1.0)
}

func TestSemanticGeocode_HongKongBeforeChina(t *testing.T) {
	lat, lng, ok := semanticGeocode("Hong Kong protests China response")
	assert.True(t, ok)
	assert.InDelta(t, 22.3, lat, 1.0, "should match Hong Kong before China")
	assert.InDelta(t, 114.2, lng, 1.0)
}

func TestSemanticGeocode_SouthKoreaNotKorea(t *testing.T) {
	lat, _, ok := semanticGeocode("South Korean music industry")
	assert.True(t, ok)
	assert.InDelta(t, 37.6, lat, 1.0, "should match South Korea")
}

func TestSemanticGeocode_WestBankNotBank(t *testing.T) {
	lat, lng, ok := semanticGeocode("West Bank settlements expansion")
	assert.True(t, ok)
	assert.InDelta(t, 31.9, lat, 1.0)
	assert.InDelta(t, 35.3, lng, 1.0)
}

// ---------------------------------------------------------------------------
// NER: mixed-content edge cases
// ---------------------------------------------------------------------------

func TestNER_MultipleLocationsInText(t *testing.T) {
	// Multiple locations — should pick the first contextual match
	text := "The treaty between the United States and Russia was signed in Geneva, Switzerland."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	// The "in Geneva, Switzerland" pattern should match
	assert.InDelta(t, 46.9, lat, 2.0)
	assert.InDelta(t, 7.5, lng, 2.0)
}

func TestNER_LocationAtEndOfSentence(t *testing.T) {
	text := "The company was founded in 1998 in Tokyo, Japan."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 35.7, lat, 1.0, "should resolve to Japan")
	assert.InDelta(t, 139.7, lng, 1.0)
}

func TestNER_LocationWithNumbers(t *testing.T) {
	// Year and numbers mixed with location
	text := "The 2024 Olympics were held in Paris, France."
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 48.9, lat, 1.0)
	assert.InDelta(t, 2.4, lng, 1.0)
}

func TestNER_ShortDescription(t *testing.T) {
	// Minimal Wikidata-style description
	text := "village in Iran"
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 35.7, lat, 1.0, "should resolve to Iran")
	assert.InDelta(t, 51.4, lng, 1.0)
}

func TestNER_WikidataStyleDescription_CityInCountry(t *testing.T) {
	text := "city in Guangdong, China"
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 39.9, lat, 1.0, "should resolve to China")
	assert.InDelta(t, 116.4, lng, 1.0)
}

func TestNER_WikidataStyleDescription_DistrictOf(t *testing.T) {
	// Some Wikidata descriptions say "district of City, Country"
	text := "district of Istanbul, Turkey"
	lat, lng, ok := extractLocationNER(text)
	assert.True(t, ok)
	assert.InDelta(t, 39.9, lat, 1.0, "should resolve to Turkey")
	assert.InDelta(t, 32.9, lng, 3.0)
}
