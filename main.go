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
	defaultTopSongs   = 10
	httpTimeoutMs              = 10000
	negativeCacheTTLSeconds    = 7200 // 2 hours
	iTunesSearchURL   = "https://itunes.apple.com/search"
	iTunesLookupURL   = "https://itunes.apple.com/lookup"
	appleMusicBaseURL = "https://music.apple.com"

	// HTML parsing limits
	similarSectionMaxBytes = 60000 // generous chunk after section marker to cover all artist lockups
	sectionBoundaryOffset  = 100   // skip initial chars before searching for next section boundary

	// Config keys
	configCountries       = "countries"
	configCacheTTLDays    = "cache_ttl_days"
	configArtistURL       = "enable_artist_url"
	configArtistBiography = "enable_artist_biography"
	configArtistImages    = "enable_artist_images"
	configSimilarArtists  = "enable_similar_artists"
	configTopSongs        = "enable_top_songs"
)

// Compile-time interface assertions
var (
	_ metadata.ArtistURLProvider       = (*appleMusicAgent)(nil)
	_ metadata.ArtistBiographyProvider = (*appleMusicAgent)(nil)
	_ metadata.ArtistImagesProvider    = (*appleMusicAgent)(nil)
	_ metadata.SimilarArtistsProvider  = (*appleMusicAgent)(nil)
	_ metadata.ArtistTopSongsProvider  = (*appleMusicAgent)(nil)
)

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
	val, exists := host.ConfigGet(configCountries)
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

// isEnabled returns whether a capability is enabled via config.
// Capabilities default to enabled; set the config value to "false" to disable.
func isEnabled(key string) bool {
	val, exists := host.ConfigGet(key)
	return !exists || val != "false"
}

// getCacheTTLSeconds returns the cache TTL in seconds from config.
func getCacheTTLSeconds() int64 {
	days, exists := host.ConfigGetInt(configCacheTTLDays)
	if !exists || days <= 0 {
		days = defaultCacheTTL
	}
	return days * 24 * 60 * 60
}

// --- KVStore helpers ---

// kvGet retrieves and unmarshals a JSON value from KVStore.
func kvGet(key string, target any) bool {
	data, exists, err := host.KVStoreGet(key)
	if err != nil || !exists {
		return false
	}
	if err := json.Unmarshal(data, target); err != nil {
		return false
	}
	return true
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

// clampLimit returns limit clamped to [1, total], or total when limit <= 0.
func clampLimit(limit, total int) int {
	if limit <= 0 || limit > total {
		return total
	}
	return limit
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
	var cached cachedArtistID
	if kvGet(cacheKey, &cached) {
		if cached.ArtistID == 0 {
			pdk.Log(pdk.LogDebug, "artist ID negative cache hit: "+normalized)
			return 0, errors.New("no matching artist found")
		}
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
		if err := kvSetWithTTL(cacheKey, cachedArtistID{ArtistID: 0}, negativeCacheTTLSeconds); err != nil {
			pdk.Log(pdk.LogWarn, "failed to cache negative artist result: "+err.Error())
		}
		return 0, errors.New("no artist found")
	}

	// Find best match by name similarity
	bestMatch := findBestArtistMatch(artistName, searchResp.Results)
	if bestMatch == nil {
		if err := kvSetWithTTL(cacheKey, cachedArtistID{ArtistID: 0}, negativeCacheTTLSeconds); err != nil {
			pdk.Log(pdk.LogWarn, "failed to cache negative artist result: "+err.Error())
		}
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
	normalized := normalizeArtistName(query)
	var firstArtist *itunesArtistResult
	for i := range results {
		if results[i].WrapperType != "artist" {
			continue
		}
		if firstArtist == nil {
			firstArtist = &results[i]
		}
		if normalizeArtistName(results[i].ArtistName) == normalized {
			return &results[i]
		}
	}
	return firstArtist
}

// --- HTML parsing helpers ---

// jsonLDRegex matches a <script> tag containing type="application/ld+json", regardless of
// other attributes (e.g. id=...) that may appear before or after the type attribute.
var jsonLDRegex = regexp.MustCompile(`(?i)<script[^>]*type="application/ld\+json"[^>]*>`)

// parseJSONLD extracts and parses JSON-LD data from an HTML page.
func parseJSONLD(html string) (*jsonLDData, error) {
	const endTag = `</script>`

	loc := jsonLDRegex.FindStringIndex(html)
	if loc == nil {
		return nil, errors.New("no JSON-LD found")
	}
	startIdx := loc[1] // position right after the opening tag

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

// similarSectionMarkers contains localized aria-label values for the "Similar Artists" section.
var similarSectionMarkers = []string{
	`aria-label="Similar Artists"`,
	`aria-label="Artistas semelhantes"`,
	`aria-label="Ähnliche Künstler"`,
	`aria-label="Artistes similaires"`,
	`aria-label="Artistas similares"`,
}

// lockupTitleRegex matches artist names inside the ellipse-lockup title elements.
// Apple Music uses: <h3 data-testid="ellipse-lockup__title" ...>Artist Name</h3>
var lockupTitleRegex = regexp.MustCompile(`data-testid="ellipse-lockup__title"[^>]*>([^<]+)<`)

// parseSimilarArtists extracts similar artist names from the Apple Music HTML page.
func parseSimilarArtists(html string) []similarArtistInfo {
	// Find the similar artists section by looking for localized aria-label markers.
	sectionStart := -1
	for _, marker := range similarSectionMarkers {
		idx := strings.Index(html, marker)
		if idx != -1 {
			pdk.Log(pdk.LogDebug, fmt.Sprintf("similar artists: found marker %q at position %d", marker, idx))
			sectionStart = idx
			break
		}
	}

	if sectionStart == -1 {
		pdk.Log(pdk.LogDebug, "similar artists: no section markers found in HTML")
		return nil
	}

	// Extract a generous chunk after the section marker to cover all artist lockups.
	sectionEnd := min(sectionStart+similarSectionMaxBytes, len(html))
	section := html[sectionStart:sectionEnd]

	// Limit to the current section by finding the next section boundary.
	if nextSection := strings.Index(section[sectionBoundaryOffset:], `data-testid="section-container"`); nextSection != -1 {
		section = section[:sectionBoundaryOffset+nextSection]
	}
	pdk.Log(pdk.LogDebug, fmt.Sprintf("similar artists: extracting from section (%d chars)", len(section)))

	// Extract artist names from ellipse-lockup title elements.
	var artists []similarArtistInfo
	seen := make(map[string]bool)

	matches := lockupTitleRegex.FindAllStringSubmatch(section, -1)
	pdk.Log(pdk.LogDebug, fmt.Sprintf("similar artists: found %d lockup titles in section", len(matches)))
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		artists = append(artists, similarArtistInfo{Name: name})
		pdk.Log(pdk.LogDebug, fmt.Sprintf("similar artists: found artist %q", name))
	}

	pdk.Log(pdk.LogDebug, fmt.Sprintf("similar artists: total found=%d", len(artists)))
	return artists
}

// --- Web page fetching ---

// pageField identifies which field of parsedPageData must be non-empty.
type pageField int

const (
	fieldAny       pageField = iota // any non-empty field
	fieldBiography                  // Biography
	fieldImage                      // ImageURL
	fieldSimilar                    // SimilarArtists
)

// fetchArtistPage fetches and parses the Apple Music artist page.
// Tries each country code in order until content is found.
func fetchArtistPage(artistID int64, wantField pageField) (*parsedPageData, error) {
	countries := getCountries()
	ttl := getCacheTTLSeconds()
	var firstResult *parsedPageData

	for _, country := range countries {
		cacheKey := fmt.Sprintf("page:%d:%s", artistID, country)

		// Check cache
		var cached parsedPageData
		if kvGet(cacheKey, &cached) {
			pdk.Log(pdk.LogDebug, fmt.Sprintf("page cache hit: %s", cacheKey))
			if firstResult == nil {
				firstResult = &cached
			}
			if hasField(&cached, wantField) {
				return &cached, nil
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
		pdk.Log(pdk.LogDebug, fmt.Sprintf("received page for country %s: %d bytes, status %d", country, len(body), statusCode))
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

	pdk.Log(pdk.LogDebug, fmt.Sprintf("parsing page HTML (%d bytes)", len(html)))

	// Parse JSON-LD for biography and image
	ld, err := parseJSONLD(html)
	if err == nil {
		page.Biography = ld.Description
		page.ImageURL = ld.Image
		pdk.Log(pdk.LogDebug, fmt.Sprintf("JSON-LD parsed: type=%s, name=%s, bio=%d chars, image=%s",
			ld.Type, ld.Name, len(ld.Description), ld.Image))
	} else {
		pdk.Log(pdk.LogDebug, "JSON-LD parsing failed: "+err.Error())
	}

	// Fallback to OpenGraph for image
	if page.ImageURL == "" {
		page.ImageURL = parseOpenGraphImage(html)
		if page.ImageURL != "" {
			pdk.Log(pdk.LogDebug, "OpenGraph image found: "+page.ImageURL)
		} else {
			pdk.Log(pdk.LogDebug, "no OpenGraph image found")
		}
	}

	// Parse similar artists
	page.SimilarArtists = parseSimilarArtists(html)
	pdk.Log(pdk.LogDebug, fmt.Sprintf("parsed page result: bio=%d chars, image=%v, similar=%d",
		len(page.Biography), page.ImageURL != "", len(page.SimilarArtists)))

	return page
}

// hasField checks if the parsed page has the requested field populated.
func hasField(page *parsedPageData, field pageField) bool {
	switch field {
	case fieldBiography:
		return page.Biography != ""
	case fieldImage:
		return page.ImageURL != ""
	case fieldSimilar:
		return len(page.SimilarArtists) > 0
	default:
		return page.Biography != "" || page.ImageURL != "" || len(page.SimilarArtists) > 0
	}
}

// --- Capability methods ---

// GetArtistURL returns the Apple Music URL for the artist.
func (a *appleMusicAgent) GetArtistURL(input metadata.ArtistRequest) (*metadata.ArtistURLResponse, error) {
	if !isEnabled(configArtistURL) {
		return nil, nil
	}
	artistID, err := resolveArtistID(input.Name)
	if err != nil {
		return nil, err
	}

	countries := getCountries()
	artistURL := fmt.Sprintf("%s/%s/artist/-/%d", appleMusicBaseURL, countries[0], artistID)
	return &metadata.ArtistURLResponse{URL: artistURL}, nil
}

// GetArtistBiography returns the artist biography from Apple Music.
func (a *appleMusicAgent) GetArtistBiography(input metadata.ArtistRequest) (*metadata.ArtistBiographyResponse, error) {
	if !isEnabled(configArtistBiography) {
		return nil, nil
	}
	artistID, err := resolveArtistID(input.Name)
	if err != nil {
		pdk.Log(pdk.LogWarn, "GetArtistBiography: resolve failed: "+err.Error())
		return nil, err
	}

	page, err := fetchArtistPage(artistID, fieldBiography)
	if err != nil {
		pdk.Log(pdk.LogWarn, "GetArtistBiography: fetchArtistPage failed: "+err.Error())
		return nil, err
	}

	if page.Biography == "" {
		pdk.Log(pdk.LogDebug, "GetArtistBiography: no biography found in any country page")
		return nil, errors.New("no biography found")
	}

	pdk.Log(pdk.LogDebug, fmt.Sprintf("GetArtistBiography: returning biography (%d chars)", len(page.Biography)))
	return &metadata.ArtistBiographyResponse{Biography: page.Biography}, nil
}

// GetArtistImages returns artist images from Apple Music in multiple sizes.
func (a *appleMusicAgent) GetArtistImages(input metadata.ArtistRequest) (*metadata.ArtistImagesResponse, error) {
	if !isEnabled(configArtistImages) {
		return nil, nil
	}
	artistID, err := resolveArtistID(input.Name)
	if err != nil {
		return nil, err
	}

	page, err := fetchArtistPage(artistID, fieldImage)
	if err != nil {
		return nil, err
	}

	if page.ImageURL == "" {
		return nil, errors.New("no artist image found")
	}

	// Generate multiple sizes from the base image URL
	sizes := []int{1000, 600, 300}
	images := make([]metadata.ImageInfo, 0, len(sizes))
	for _, size := range sizes {
		images = append(images, metadata.ImageInfo{
			URL:  rewriteImageSize(page.ImageURL, size),
			Size: int32(size),
		})
	}

	return &metadata.ArtistImagesResponse{Images: images}, nil
}

// GetSimilarArtists returns similar artists scraped from the Apple Music page.
func (a *appleMusicAgent) GetSimilarArtists(input metadata.SimilarArtistsRequest) (*metadata.SimilarArtistsResponse, error) {
	if !isEnabled(configSimilarArtists) {
		return nil, nil
	}
	artistID, err := resolveArtistID(input.Name)
	if err != nil {
		pdk.Log(pdk.LogWarn, "GetSimilarArtists: resolve failed: "+err.Error())
		return nil, err
	}

	page, err := fetchArtistPage(artistID, fieldSimilar)
	if err != nil {
		pdk.Log(pdk.LogWarn, "GetSimilarArtists: fetchArtistPage failed: "+err.Error())
		return nil, err
	}

	if len(page.SimilarArtists) == 0 {
		pdk.Log(pdk.LogDebug, "GetSimilarArtists: no similar artists found in any country page")
		return nil, errors.New("no similar artists found")
	}
	pdk.Log(pdk.LogDebug, fmt.Sprintf("GetSimilarArtists: found %d similar artists", len(page.SimilarArtists)))

	limit := clampLimit(int(input.Limit), len(page.SimilarArtists))

	artists := make([]metadata.ArtistRef, 0, limit)
	for i := 0; i < limit; i++ {
		artists = append(artists, metadata.ArtistRef{
			Name: page.SimilarArtists[i].Name,
		})
	}

	return &metadata.SimilarArtistsResponse{Artists: artists}, nil
}

// GetArtistTopSongs returns the artist's top songs via the iTunes Lookup API.
func (a *appleMusicAgent) GetArtistTopSongs(input metadata.TopSongsRequest) (*metadata.TopSongsResponse, error) {
	if !isEnabled(configTopSongs) {
		return nil, nil
	}
	artistID, err := resolveArtistID(input.Name)
	if err != nil {
		return nil, err
	}

	count := int(input.Count)
	if count <= 0 {
		count = defaultTopSongs
	}

	// Check cache
	cacheKey := fmt.Sprintf("topsongs:%d:%d", artistID, count)
	var cached metadata.TopSongsResponse
	if kvGet(cacheKey, &cached) {
		pdk.Log(pdk.LogDebug, "top songs cache hit: "+cacheKey)
		return &cached, nil
	}

	// Fetch from iTunes Lookup API
	lookupURL := fmt.Sprintf("%s?id=%d&entity=song&sort=popular&limit=%d",
		iTunesLookupURL, artistID, count)

	pdk.Log(pdk.LogDebug, "fetching top songs: "+lookupURL)

	body, statusCode, err := httpGet(lookupURL)
	if err != nil {
		return nil, fmt.Errorf("iTunes lookup failed: %w", err)
	}
	if statusCode != 200 {
		return nil, fmt.Errorf("iTunes lookup returned status %d", statusCode)
	}

	var lookupResp itunesLookupResponse
	if err := json.Unmarshal(body, &lookupResp); err != nil {
		return nil, fmt.Errorf("failed to parse iTunes lookup response: %w", err)
	}

	// First result is the artist itself, skip it
	songs := make([]metadata.SongRef, 0, len(lookupResp.Results))
	for _, r := range lookupResp.Results {
		if r.WrapperType == "track" && r.TrackName != "" {
			songs = append(songs, metadata.SongRef{
				Name:   r.TrackName,
				Artist: r.ArtistName,
			})
		}
	}

	if len(songs) == 0 {
		return nil, errors.New("no top songs found")
	}

	result := &metadata.TopSongsResponse{Songs: songs}

	// Cache with TTL
	ttl := getCacheTTLSeconds()
	if err := kvSetWithTTL(cacheKey, result, ttl); err != nil {
		pdk.Log(pdk.LogWarn, "failed to cache top songs: "+err.Error())
	}

	return result, nil
}
