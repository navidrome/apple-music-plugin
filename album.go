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

type itunesAlbumSearchResponse struct {
	ResultCount int                 `json:"resultCount"`
	Results     []itunesAlbumResult `json:"results"`
}

type itunesAlbumResult struct {
	WrapperType       string `json:"wrapperType"`
	CollectionName    string `json:"collectionName"`
	ArtistName        string `json:"artistName"`
	ArtworkURL100     string `json:"artworkUrl100"`
	CollectionViewURL string `json:"collectionViewUrl"`
}

type cachedAlbumMatch struct {
	ArtworkURL        string `json:"artworkUrl,omitempty"`
	CollectionViewURL string `json:"collectionViewUrl,omitempty"`
}

type cachedAlbumInfo struct {
	URL         string `json:"url,omitempty"`
	Description string `json:"description"`
}

var baseNameDelimiters = []string{" (", " [", " - ", ": "}

func extractBaseName(normalized string) string {
	for _, delim := range baseNameDelimiters {
		if idx := strings.Index(normalized, delim); idx > 0 {
			normalized = normalized[:idx]
		}
	}
	return strings.TrimSpace(normalized)
}

func findBestAlbumMatch(albumName string, results []itunesAlbumResult) *itunesAlbumResult {
	normalizedAlbum := normalizeName(albumName)
	baseAlbum := extractBaseName(normalizedAlbum)

	// Filter to collection entries
	type candidate struct {
		index          int
		normalizedName string
		baseName       string
	}
	var candidates []candidate
	for i := range results {
		if results[i].WrapperType != "collection" {
			continue
		}
		cn := normalizeName(results[i].CollectionName)
		candidates = append(candidates, candidate{
			index:          i,
			normalizedName: cn,
			baseName:       extractBaseName(cn),
		})
	}

	// Pass 1: exact match on full name
	for _, c := range candidates {
		if c.normalizedName == normalizedAlbum {
			return &results[c.index]
		}
	}

	// Pass 2: exact match on base names
	for _, c := range candidates {
		if c.baseName == baseAlbum {
			return &results[c.index]
		}
	}

	// Pass 3: containment — one base name contains the other.
	// Require the shorter name to be at least 4 characters to avoid false positives.
	if len(baseAlbum) >= 4 {
		for _, c := range candidates {
			if len(c.baseName) >= 4 &&
				(strings.Contains(c.baseName, baseAlbum) || strings.Contains(baseAlbum, c.baseName)) {
				return &results[c.index]
			}
		}
	}

	return nil
}

func resolveAlbumMatch(albumName, artistName string) (*cachedAlbumMatch, error) {
	normalizedAlbum := normalizeName(albumName)
	normalizedArtist := normalizeName(artistName)
	if normalizedAlbum == "" {
		return nil, errors.New("empty album name")
	}
	if normalizedArtist == "" {
		return nil, errors.New("empty artist name")
	}

	// Check cache
	cacheKey := fmt.Sprintf("album:%s:%s", normalizedArtist, normalizedAlbum)
	var cached cachedAlbumMatch
	if kvGet(cacheKey, &cached) {
		if cached.ArtworkURL == "" && cached.CollectionViewURL == "" {
			pdk.Log(pdk.LogDebug, "album negative cache hit: "+cacheKey)
			return nil, nil
		}
		// Pre-0.2.0 cache entries only stored artwork; treat as a miss so the
		// CollectionViewURL can be populated on re-fetch.
		if cached.CollectionViewURL != "" {
			pdk.Log(pdk.LogDebug, "album cache hit: "+cacheKey)
			return &cached, nil
		}
		pdk.Log(pdk.LogDebug, "album cache entry missing URL, refreshing: "+cacheKey)
	}

	// Resolve artist ID first
	artistID, err := resolveArtistID(artistName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve artist for album lookup: %w", err)
	}
	if artistID == 0 {
		pdk.Log(pdk.LogDebug, "artist not found for album lookup: "+artistName)
		return nil, nil
	}

	// Look up all albums by artist ID via the iTunes Lookup API
	lookupURL := fmt.Sprintf("%s?id=%d&entity=album&limit=200", iTunesLookupURL, artistID)

	pdk.Log(pdk.LogDebug, "looking up albums for artist: "+lookupURL)

	var lookupResp itunesAlbumSearchResponse
	if err := httpGetJSON(lookupURL, &lookupResp); err != nil {
		return nil, fmt.Errorf("iTunes album lookup: %w", err)
	}

	// Find match by album name (artist already matched via artist ID)
	bestMatch := findBestAlbumMatch(albumName, lookupResp.Results)

	if bestMatch == nil || (bestMatch.ArtworkURL100 == "" && bestMatch.CollectionViewURL == "") {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("no matching album found for '%s' by '%s'", albumName, artistName))
		if err := kvSetWithTTL(cacheKey, cachedAlbumMatch{}, negativeCacheTTLSeconds); err != nil {
			pdk.Log(pdk.LogWarn, "failed to cache negative album result: "+err.Error())
		}
		return nil, nil
	}

	match := &cachedAlbumMatch{
		ArtworkURL:        bestMatch.ArtworkURL100,
		CollectionViewURL: stripTrackingParams(bestMatch.CollectionViewURL),
	}

	// Cache with standard TTL
	ttl := getCacheTTLSeconds()
	if err := kvSetWithTTL(cacheKey, match, ttl); err != nil {
		pdk.Log(pdk.LogWarn, "failed to cache album match: "+err.Error())
	}

	pdk.Log(pdk.LogInfo, fmt.Sprintf("resolved album '%s' by '%s' → match", albumName, artistName))
	return match, nil
}

func stripTrackingParams(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

var serializedServerDataRegex = regexp.MustCompile(`(?is)<script[^>]*id="serialized-server-data"[^>]*>(.*?)</script>`)

// Mirrors: data[0].data.sections[*].items[*].modalPresentationDescriptor.paragraphText
type serverDataPage struct {
	Data []struct {
		Data struct {
			Sections []struct {
				Items []struct {
					ModalPresentationDescriptor struct {
						ParagraphText string `json:"paragraphText"`
					} `json:"modalPresentationDescriptor"`
				} `json:"items"`
			} `json:"sections"`
		} `json:"data"`
	} `json:"data"`
}

func parseAlbumDescription(html []byte) string {
	m := serializedServerDataRegex.FindSubmatch(html)
	if m == nil {
		return ""
	}

	// Apple wraps the page data in an array, but fall back to a single object for robustness.
	var pages []serverDataPage
	if err := json.Unmarshal(m[1], &pages); err != nil {
		var single serverDataPage
		if err2 := json.Unmarshal(m[1], &single); err2 != nil {
			pdk.Log(pdk.LogDebug, "failed to parse serialized-server-data: "+err2.Error())
			return ""
		}
		pages = []serverDataPage{single}
	}

	for _, p := range pages {
		for _, d := range p.Data {
			for _, s := range d.Data.Sections {
				for _, it := range s.Items {
					if text := strings.TrimSpace(it.ModalPresentationDescriptor.ParagraphText); text != "" {
						return text
					}
				}
			}
		}
	}
	return ""
}

// The second return value reports whether any fetch succeeded, so the caller
// can distinguish "no notes" (cacheable) from "all fetches failed" (retry later).
func fetchAlbumDescription(collectionViewURL string) (string, bool) {
	countries := getCountries()
	anySuccess := false
	for _, country := range countries {
		pageURL := rewriteAlbumURLCountry(collectionViewURL, country)
		pdk.Log(pdk.LogDebug, "fetching Apple Music album page: "+pageURL)

		body, statusCode, err := httpGet(pageURL)
		if err != nil {
			pdk.Log(pdk.LogWarn, fmt.Sprintf("failed to fetch album page for country %s: %s", country, err.Error()))
			continue
		}
		if statusCode != 200 {
			pdk.Log(pdk.LogWarn, fmt.Sprintf("album page returned %d for country %s", statusCode, country))
			continue
		}

		anySuccess = true
		if description := parseAlbumDescription(body); description != "" {
			return description, true
		}
	}
	return "", anySuccess
}

var albumURLCountryRegex = regexp.MustCompile(`^(https?://music\.apple\.com/)[a-z]{2}(/album/)`)

func rewriteAlbumURLCountry(albumURL, country string) string {
	return albumURLCountryRegex.ReplaceAllString(albumURL, "${1}"+country+"${2}")
}
