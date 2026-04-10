package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

// --- iTunes artist response types ---

type itunesSearchResponse struct {
	ResultCount int                  `json:"resultCount"`
	Results     []itunesArtistResult `json:"results"`
}

type itunesArtistResult struct {
	WrapperType string `json:"wrapperType"`
	ArtistName  string `json:"artistName"`
	ArtistID    int64  `json:"artistId"`
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

// --- Cached and parsed artist data ---

type cachedArtistID struct {
	ArtistID int64 `json:"artistId"`
}

type parsedPageData struct {
	Biography      string              `json:"biography,omitempty"`
	ImageURL       string              `json:"imageURL,omitempty"`
	SimilarArtists []similarArtistInfo `json:"similarArtists,omitempty"`
}

type similarArtistInfo struct {
	Name string `json:"name"`
}

type jsonLDData struct {
	Context     string `json:"@context"`
	Type        string `json:"@type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Image       string `json:"image"`
}

// pageField identifies which field of parsedPageData must be non-empty.
type pageField int

const (
	fieldAny       pageField = iota // any non-empty field
	fieldBiography                  // Biography
	fieldImage                      // ImageURL
	fieldSimilar                    // SimilarArtists
)

// --- Artist resolution ---

// resolveArtistID looks up an Apple Music artist ID by name.
// Uses KVStore cache for permanent storage of name→ID mappings.
func resolveArtistID(artistName string) (int64, error) {
	normalized := normalizeName(artistName)
	if normalized == "" {
		return 0, errors.New("empty artist name")
	}

	// Check cache
	cacheKey := "artist:" + normalized
	var cached cachedArtistID
	if kvGet(cacheKey, &cached) {
		if cached.ArtistID == 0 {
			pdk.Log(pdk.LogDebug, "artist ID negative cache hit: "+normalized)
			return 0, nil
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

	var searchResp itunesSearchResponse
	if err := httpGetJSON(searchURL, &searchResp); err != nil {
		return 0, fmt.Errorf("iTunes artist search: %w", err)
	}

	if searchResp.ResultCount == 0 {
		pdk.Log(pdk.LogDebug, "no artist found for: "+artistName)
		if err := kvSetWithTTL(cacheKey, cachedArtistID{ArtistID: 0}, negativeCacheTTLSeconds); err != nil {
			pdk.Log(pdk.LogWarn, "failed to cache negative artist result: "+err.Error())
		}
		return 0, nil
	}

	// Find best match by name similarity
	bestMatch := findBestArtistMatch(artistName, searchResp.Results)
	if bestMatch == nil {
		pdk.Log(pdk.LogDebug, "no matching artist found for: "+artistName)
		if err := kvSetWithTTL(cacheKey, cachedArtistID{ArtistID: 0}, negativeCacheTTLSeconds); err != nil {
			pdk.Log(pdk.LogWarn, "failed to cache negative artist result: "+err.Error())
		}
		return 0, nil
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
	normalized := normalizeName(query)
	var firstArtist *itunesArtistResult
	for i := range results {
		if results[i].WrapperType != "artist" {
			continue
		}
		if firstArtist == nil {
			firstArtist = &results[i]
		}
		if normalizeName(results[i].ArtistName) == normalized {
			return &results[i]
		}
	}
	return firstArtist
}

// --- JSON-LD parsing (used by artist pages) ---

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

// --- Placeholder detection ---

// placeholderImageURL is the generic Apple Music image that should be treated as "no image".
const placeholderImageURL = "https://music.apple.com/assets/meta/apple-music.png"

// isPlaceholderImage returns true if the URL is Apple Music's generic placeholder.
func isPlaceholderImage(url string) bool {
	return url == placeholderImageURL
}

// appleMusicRegex matches "Apple Music" with any whitespace between the words,
// including non-breaking spaces (U+00A0) used in some locales.
var appleMusicRegex = regexp.MustCompile(`Apple[\s\pZ]+Music`)

// isPlaceholderBiography returns true if the text is a generic Apple Music promotional
// description rather than a real artist biography. Apple Music returns these for artists
// without a curated bio. The placeholder always mentions "Apple Music" in its first
// sentence (e.g., "Listen to music by X on Apple Music."). Real biographies may mention
// "Apple Music" deeper in the text (e.g., awards), so we only check the first sentence.
func isPlaceholderBiography(text string) bool {
	firstSentence := text
	if idx := strings.Index(text, ". "); idx != -1 {
		firstSentence = text[:idx]
	}
	return appleMusicRegex.MatchString(firstSentence)
}

// --- OpenGraph image parsing (fallback for artist image) ---

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

// --- Similar artists parsing ---

const (
	similarSectionMaxBytes = 60000 // generous chunk after section marker to cover all artist lockups
	sectionBoundaryOffset  = 100   // skip initial chars before searching for next section boundary
)

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

// --- Artist page fetching ---

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
	pdk.Log(pdk.LogDebug, fmt.Sprintf("no page data found for artist %d in any country", artistID))
	return nil, nil
}

// parsePage extracts all metadata from an Apple Music artist HTML page.
// Always parses all fields so the cached result is complete for any future capability request.
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

	// Discard generic Apple Music promotional description (not a real biography)
	if isPlaceholderBiography(page.Biography) {
		pdk.Log(pdk.LogDebug, "discarding placeholder biography: "+page.Biography)
		page.Biography = ""
	}

	// Discard generic Apple Music placeholder image
	if isPlaceholderImage(page.ImageURL) {
		pdk.Log(pdk.LogDebug, "discarding placeholder image: "+page.ImageURL)
		page.ImageURL = ""
	}

	// Fallback to OpenGraph for image
	if page.ImageURL == "" {
		page.ImageURL = parseOpenGraphImage(html)
		if isPlaceholderImage(page.ImageURL) {
			pdk.Log(pdk.LogDebug, "discarding placeholder OpenGraph image: "+page.ImageURL)
			page.ImageURL = ""
		} else if page.ImageURL != "" {
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
