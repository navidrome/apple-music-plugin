package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/metadata"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

const (
	userAgent         = "NavidromeAppleMusicPlugin/0.1"
	defaultCountry    = "us"
	defaultCacheTTL   = 7 // days
	httpTimeoutMs     = 10000
	iTunesSearchURL   = "https://itunes.apple.com/search"
	iTunesLookupURL   = "https://itunes.apple.com/lookup"
	appleMusicBaseURL = "https://music.apple.com"
)

// Compile-time interface assertions (uncomment after all methods are added in Tasks 11-15)
// var (
// 	_ metadata.ArtistURLProvider       = (*appleMusicAgent)(nil)
// 	_ metadata.ArtistBiographyProvider = (*appleMusicAgent)(nil)
// 	_ metadata.ArtistImagesProvider    = (*appleMusicAgent)(nil)
// 	_ metadata.SimilarArtistsProvider  = (*appleMusicAgent)(nil)
// 	_ metadata.ArtistTopSongsProvider  = (*appleMusicAgent)(nil)
// )

func init() {
	metadata.Register(&appleMusicAgent{})
}

func main() {}

type appleMusicAgent struct{}

// --- iTunes API response types ---

type itunesSearchResponse struct {
	ResultCount int                  `json:"resultCount"`
	Results     []itunesArtistResult `json:"results"`
}

type itunesArtistResult struct {
	WrapperType   string `json:"wrapperType"`
	ArtistType    string `json:"artistType"`
	ArtistName    string `json:"artistName"`
	ArtistLinkURL string `json:"artistLinkUrl"`
	ArtistID      int64  `json:"artistId"`
	PrimaryGenre  string `json:"primaryGenreName"`
}

type itunesLookupResponse struct {
	ResultCount int                  `json:"resultCount"`
	Results     []itunesLookupResult `json:"results"`
}

type itunesLookupResult struct {
	WrapperType string `json:"wrapperType"`
	ArtistName  string `json:"artistName"`
	TrackName   string `json:"trackName"`
	ArtistID    int64  `json:"artistId"`
}

// --- Scraped page data ---

type parsedPageData struct {
	Biography      string              `json:"biography,omitempty"`
	ImageURL       string              `json:"imageURL,omitempty"`
	SimilarArtists []similarArtistInfo `json:"similarArtists,omitempty"`
}

type similarArtistInfo struct {
	Name string `json:"name"`
}

// --- Cached artist ID ---

type cachedArtistID struct {
	ArtistID int64 `json:"artistId"`
}

// --- JSON-LD structure ---

type jsonLDData struct {
	Context     string `json:"@context"`
	Type        string `json:"@type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Image       string `json:"image"`
}

// --- Config helpers ---

// getCountries returns the ordered list of country codes from config.
func getCountries() []string {
	val, exists := host.ConfigGet("countries")
	if !exists || strings.TrimSpace(val) == "" {
		return []string{defaultCountry}
	}
	parts := strings.Split(val, ",")
	countries := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			countries = append(countries, p)
		}
	}
	if len(countries) == 0 {
		return []string{defaultCountry}
	}
	return countries
}

// getCacheTTLSeconds returns the cache TTL in seconds from config.
func getCacheTTLSeconds() int64 {
	days, exists := host.ConfigGetInt("cache_ttl_days")
	if !exists || days <= 0 {
		days = defaultCacheTTL
	}
	return days * 24 * 60 * 60
}

// --- KVStore helpers ---
// Note: We avoid generics since TinyGo's support is limited.
// Instead, we use concrete unmarshal helpers for each cached type.

// kvGetArtistID retrieves a cached artist ID from KVStore.
func kvGetArtistID(key string) (*cachedArtistID, bool) {
	data, exists, err := host.KVStoreGet(key)
	if err != nil || !exists {
		return nil, false
	}
	var val cachedArtistID
	if err := json.Unmarshal(data, &val); err != nil {
		return nil, false
	}
	return &val, true
}

// kvGetPageData retrieves cached page data from KVStore.
func kvGetPageData(key string) (*parsedPageData, bool) {
	data, exists, err := host.KVStoreGet(key)
	if err != nil || !exists {
		return nil, false
	}
	var val parsedPageData
	if err := json.Unmarshal(data, &val); err != nil {
		return nil, false
	}
	return &val, true
}

// kvGetTopSongs retrieves cached top songs from KVStore.
func kvGetTopSongs(key string) (*metadata.TopSongsResponse, bool) {
	data, exists, err := host.KVStoreGet(key)
	if err != nil || !exists {
		return nil, false
	}
	var val metadata.TopSongsResponse
	if err := json.Unmarshal(data, &val); err != nil {
		return nil, false
	}
	return &val, true
}

// kvSet stores a JSON value in KVStore with no TTL (permanent).
func kvSet(key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return host.KVStoreSet(key, data)
}

// kvSetWithTTL stores a JSON value in KVStore with a TTL in seconds.
func kvSetWithTTL(key string, value any, ttlSeconds int64) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return host.KVStoreSetWithTTL(key, data, ttlSeconds)
}

// --- HTTP helper ---

// httpGet performs a GET request and returns the response body.
func httpGet(rawURL string) ([]byte, int32, error) {
	resp, err := host.HTTPSend(host.HTTPRequest{
		Method:    "GET",
		URL:       rawURL,
		Headers:   map[string]string{"User-Agent": userAgent},
		TimeoutMs: httpTimeoutMs,
	})
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, resp.StatusCode, nil
}

// --- Name normalization ---

// normalizeArtistName normalizes an artist name for cache key use.
func normalizeArtistName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// --- Artist resolution ---

// resolveArtistID looks up an Apple Music artist ID by name.
// Uses KVStore cache for permanent storage of name→ID mappings.
func resolveArtistID(artistName string) (int64, error) {
	normalized := normalizeArtistName(artistName)
	if normalized == "" {
		return 0, errors.New("empty artist name")
	}

	// Check cache
	cacheKey := "artist:" + normalized
	if cached, ok := kvGetArtistID(cacheKey); ok {
		pdk.Log(pdk.LogDebug, "artist ID cache hit: "+normalized)
		return cached.ArtistID, nil
	}

	// Search iTunes API
	countries := getCountries()
	country := countries[0]

	searchURL := fmt.Sprintf("%s?term=%s&entity=musicArtist&limit=5&country=%s",
		iTunesSearchURL, url.QueryEscape(artistName), url.QueryEscape(country))

	pdk.Log(pdk.LogDebug, "searching iTunes API: "+searchURL)

	body, statusCode, err := httpGet(searchURL)
	if err != nil {
		return 0, fmt.Errorf("iTunes search failed: %w", err)
	}
	if statusCode != 200 {
		return 0, fmt.Errorf("iTunes search returned status %d", statusCode)
	}

	var searchResp itunesSearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return 0, fmt.Errorf("failed to parse iTunes response: %w", err)
	}

	if searchResp.ResultCount == 0 {
		return 0, errors.New("no artist found")
	}

	// Find best match by name similarity
	bestMatch := findBestArtistMatch(artistName, searchResp.Results)
	if bestMatch == nil {
		return 0, errors.New("no matching artist found")
	}

	// Cache permanently
	if err := kvSet(cacheKey, cachedArtistID{ArtistID: bestMatch.ArtistID}); err != nil {
		pdk.Log(pdk.LogWarn, "failed to cache artist ID: "+err.Error())
	}

	pdk.Log(pdk.LogInfo, fmt.Sprintf("resolved artist '%s' → ID %d", artistName, bestMatch.ArtistID))
	return bestMatch.ArtistID, nil
}

// findBestArtistMatch finds the best matching artist from search results.
// Uses case-insensitive exact match first, then falls back to first result.
func findBestArtistMatch(query string, results []itunesArtistResult) *itunesArtistResult {
	queryLower := strings.ToLower(strings.TrimSpace(query))
	for i := range results {
		if results[i].WrapperType != "artist" {
			continue
		}
		if strings.ToLower(results[i].ArtistName) == queryLower {
			return &results[i]
		}
	}
	// Fall back to first artist result
	for i := range results {
		if results[i].WrapperType == "artist" {
			return &results[i]
		}
	}
	return nil
}

// --- HTML parsing helpers ---

// parseJSONLD extracts and parses JSON-LD data from an HTML page.
func parseJSONLD(html string) (*jsonLDData, error) {
	const startTag = `<script type="application/ld+json">`
	const endTag = `</script>`

	startIdx := strings.Index(html, startTag)
	if startIdx == -1 {
		return nil, errors.New("no JSON-LD found")
	}
	startIdx += len(startTag)

	endIdx := strings.Index(html[startIdx:], endTag)
	if endIdx == -1 {
		return nil, errors.New("malformed JSON-LD")
	}

	jsonStr := strings.TrimSpace(html[startIdx : startIdx+endIdx])

	var ld jsonLDData
	if err := json.Unmarshal([]byte(jsonStr), &ld); err != nil {
		return nil, fmt.Errorf("failed to parse JSON-LD: %w", err)
	}

	return &ld, nil
}

// parseOpenGraphImage extracts the og:image URL from an HTML page.
func parseOpenGraphImage(html string) string {
	// Look for <meta property="og:image" content="...">
	pattern := `<meta property="og:image" content="`
	idx := strings.Index(html, pattern)
	if idx == -1 {
		return ""
	}
	idx += len(pattern)
	endIdx := strings.Index(html[idx:], `"`)
	if endIdx == -1 {
		return ""
	}
	return html[idx : idx+endIdx]
}

// imageURLRegex matches Apple's mzstatic.com image dimension segments like "486x486bb".
var imageURLRegex = regexp.MustCompile(`/\d+x\d+[a-z]*\.`)

// rewriteImageSize rewrites an Apple mzstatic.com image URL to the given size.
func rewriteImageSize(imageURL string, size int) string {
	return imageURLRegex.ReplaceAllString(imageURL, fmt.Sprintf("/%dx%dbb.", size, size))
}

// similarArtistLinkRegex matches Apple Music artist links in the similar artists section.
// Pattern: /XX/artist/artist-name/12345 where XX is a country code.
var similarArtistLinkRegex = regexp.MustCompile(`/[a-z]{2}/artist/[^/]+/(\d+)`)

// parseSimilarArtists extracts similar artist names from the Apple Music HTML page.
// The section is identified by structural patterns since the label is localized.
func parseSimilarArtists(html string) []similarArtistInfo {
	// Find the similar artists section by looking for the section ID pattern.
	sectionMarkers := []string{
		`aria-label="Similar Artists"`,
		`aria-label="Artistas semelhantes"`,
		`aria-label="Ähnliche Künstler"`,
		`aria-label="Artistes similaires"`,
		`data-testid="section-content"`,
	}

	sectionStart := -1
	for _, marker := range sectionMarkers {
		idx := strings.Index(html, marker)
		if idx != -1 {
			// Verify this is actually a similar artists section by checking context
			contextStart := idx
			if idx > 500 {
				contextStart = idx - 500
			}
			context := html[contextStart:idx]
			if strings.Contains(context, "svelte-") || strings.Contains(context, "section") {
				sectionStart = idx
				break
			}
			// Fallback: use this marker position
			if sectionStart == -1 {
				sectionStart = idx
			}
		}
	}

	if sectionStart == -1 {
		return nil
	}

	// Extract artist info from the section (next ~5000 chars should cover it)
	sectionEnd := sectionStart + 5000
	if sectionEnd > len(html) {
		sectionEnd = len(html)
	}
	section := html[sectionStart:sectionEnd]

	// Find all artist names via aria-label on lockup elements
	var artists []similarArtistInfo
	seen := make(map[string]bool)

	// Look for artist name patterns: lockup elements with aria-label containing artist names
	namePattern := regexp.MustCompile(`aria-label="([^"]+?)(?:,\s|")`)
	matches := namePattern.FindAllStringSubmatch(section, -1)
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		// Skip section-level labels and non-artist labels
		if name == "" || strings.Contains(strings.ToLower(name), "similar") ||
			strings.Contains(strings.ToLower(name), "artist") ||
			strings.Contains(strings.ToLower(name), "section") ||
			seen[name] {
			continue
		}
		seen[name] = true
		artists = append(artists, similarArtistInfo{Name: name})
	}

	return artists
}

// --- Web page fetching ---

// fetchArtistPage fetches and parses the Apple Music artist page.
// Tries each country code in order until content is found.
// The `wantField` parameter indicates which field must be non-empty:
// "biography", "image", "similar", or "" for any content.
func fetchArtistPage(artistID int64, wantField string) (*parsedPageData, error) {
	countries := getCountries()
	ttl := getCacheTTLSeconds()
	var firstResult *parsedPageData

	for _, country := range countries {
		cacheKey := fmt.Sprintf("page:%d:%s", artistID, country)

		// Check cache
		if cached, ok := kvGetPageData(cacheKey); ok {
			pdk.Log(pdk.LogDebug, fmt.Sprintf("page cache hit: %s", cacheKey))
			if firstResult == nil {
				firstResult = cached
			}
			if hasField(cached, wantField) {
				return cached, nil
			}
			continue
		}

		// Fetch page
		pageURL := fmt.Sprintf("%s/%s/artist/-/%d", appleMusicBaseURL, country, artistID)
		pdk.Log(pdk.LogDebug, "fetching Apple Music page: "+pageURL)

		body, statusCode, err := httpGet(pageURL)
		if err != nil {
			pdk.Log(pdk.LogWarn, fmt.Sprintf("failed to fetch page for country %s: %s", country, err.Error()))
			continue
		}
		if statusCode != 200 {
			pdk.Log(pdk.LogWarn, fmt.Sprintf("Apple Music page returned %d for country %s", statusCode, country))
			continue
		}

		html := string(body)
		page := parsePage(html)

		// Cache the result
		if err := kvSetWithTTL(cacheKey, page, ttl); err != nil {
			pdk.Log(pdk.LogWarn, "failed to cache page data: "+err.Error())
		}

		if firstResult == nil {
			firstResult = page
		}
		if hasField(page, wantField) {
			return page, nil
		}
	}

	if firstResult != nil {
		return firstResult, nil
	}
	return nil, errors.New("no page data found for any country")
}

// parsePage extracts all metadata from an Apple Music artist HTML page.
func parsePage(html string) *parsedPageData {
	page := &parsedPageData{}

	// Parse JSON-LD for biography and image
	ld, err := parseJSONLD(html)
	if err == nil {
		page.Biography = ld.Description
		page.ImageURL = ld.Image
	}

	// Fallback to OpenGraph for image
	if page.ImageURL == "" {
		page.ImageURL = parseOpenGraphImage(html)
	}

	// Parse similar artists
	page.SimilarArtists = parseSimilarArtists(html)

	return page
}

// hasField checks if the parsed page has the requested field populated.
func hasField(page *parsedPageData, field string) bool {
	switch field {
	case "biography":
		return page.Biography != ""
	case "image":
		return page.ImageURL != ""
	case "similar":
		return len(page.SimilarArtists) > 0
	default:
		return page.Biography != "" || page.ImageURL != "" || len(page.SimilarArtists) > 0
	}
}
