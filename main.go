package main

import (
	"fmt"

	"github.com/navidrome/navidrome/plugins/pdk/go/metadata"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

const (
	userAgent               = "NavidromeAppleMusicPlugin/0.1"
	defaultCountry          = "us"
	defaultCacheTTL         = 7 // days
	defaultTopSongs         = 10
	httpTimeoutMs           = 10000
	negativeCacheTTLSeconds = 7200 // 2 hours
	iTunesSearchURL         = "https://itunes.apple.com/search"
	iTunesLookupURL         = "https://itunes.apple.com/lookup"
	appleMusicBaseURL       = "https://music.apple.com"

	// Config keys
	configCountries       = "countries"
	configCacheTTLDays    = "cache_ttl_days"
	configArtistURL       = "enable_artist_url"
	configArtistBiography = "enable_artist_biography"
	configArtistImages    = "enable_artist_images"
	configSimilarArtists  = "enable_similar_artists"
	configTopSongs        = "enable_top_songs"
	configAlbumImages     = "enable_album_images"
	configAlbumInfo       = "enable_album_info"
)

// Compile-time interface assertions
var (
	_ metadata.ArtistURLProvider       = (*appleMusicAgent)(nil)
	_ metadata.ArtistBiographyProvider = (*appleMusicAgent)(nil)
	_ metadata.ArtistImagesProvider    = (*appleMusicAgent)(nil)
	_ metadata.SimilarArtistsProvider  = (*appleMusicAgent)(nil)
	_ metadata.ArtistTopSongsProvider  = (*appleMusicAgent)(nil)
	_ metadata.AlbumImagesProvider     = (*appleMusicAgent)(nil)
	_ metadata.AlbumInfoProvider       = (*appleMusicAgent)(nil)
)

func init() {
	metadata.Register(&appleMusicAgent{})
}

func main() {}

type appleMusicAgent struct{}

// GetArtistURL returns the Apple Music URL for the artist.
func (a *appleMusicAgent) GetArtistURL(input metadata.ArtistRequest) (*metadata.ArtistURLResponse, error) {
	if !isEnabled(configArtistURL) {
		return nil, nil
	}
	artistID, err := resolveArtistID(input.Name)
	if err != nil {
		return nil, err
	}
	if artistID == 0 {
		return nil, nil
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
		return nil, err
	}
	if artistID == 0 {
		return nil, nil
	}

	page, err := fetchArtistPage(artistID, fieldBiography)
	if err != nil {
		return nil, err
	}
	if page == nil || page.Biography == "" || isPlaceholderBiography(page.Biography) {
		pdk.Log(pdk.LogDebug, "no biography found for: "+input.Name)
		return nil, nil
	}

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
	if artistID == 0 {
		return nil, nil
	}

	page, err := fetchArtistPage(artistID, fieldImage)
	if err != nil {
		return nil, err
	}
	if page == nil || page.ImageURL == "" || isPlaceholderImage(page.ImageURL) {
		pdk.Log(pdk.LogDebug, "no artist image found for: "+input.Name)
		return nil, nil
	}

	return &metadata.ArtistImagesResponse{Images: buildImageList(page.ImageURL)}, nil
}

// GetSimilarArtists returns similar artists scraped from the Apple Music page.
func (a *appleMusicAgent) GetSimilarArtists(input metadata.SimilarArtistsRequest) (*metadata.SimilarArtistsResponse, error) {
	if !isEnabled(configSimilarArtists) {
		return nil, nil
	}
	artistID, err := resolveArtistID(input.Name)
	if err != nil {
		return nil, err
	}
	if artistID == 0 {
		return nil, nil
	}

	page, err := fetchArtistPage(artistID, fieldSimilar)
	if err != nil {
		return nil, err
	}
	if page == nil || len(page.SimilarArtists) == 0 {
		pdk.Log(pdk.LogDebug, "no similar artists found for: "+input.Name)
		return nil, nil
	}

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
	if artistID == 0 {
		return nil, nil
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

	var lookupResp itunesLookupResponse
	if err := httpGetJSON(lookupURL, &lookupResp); err != nil {
		return nil, fmt.Errorf("iTunes top songs lookup: %w", err)
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
		pdk.Log(pdk.LogDebug, "no top songs found for: "+input.Name)
		return nil, nil
	}

	result := &metadata.TopSongsResponse{Songs: songs}

	// Cache with TTL
	ttl := getCacheTTLSeconds()
	if err := kvSetWithTTL(cacheKey, result, ttl); err != nil {
		pdk.Log(pdk.LogWarn, "failed to cache top songs: "+err.Error())
	}

	return result, nil
}

// GetAlbumImages returns album artwork from Apple Music in multiple sizes.
func (a *appleMusicAgent) GetAlbumImages(input metadata.AlbumRequest) (*metadata.AlbumImagesResponse, error) {
	if !isEnabled(configAlbumImages) {
		return nil, nil
	}

	match, err := resolveAlbumMatch(input.Name, input.Artist)
	if err != nil {
		return nil, err
	}
	if match == nil || match.ArtworkURL == "" {
		pdk.Log(pdk.LogDebug, fmt.Sprintf("no album artwork found for '%s' by '%s'", input.Name, input.Artist))
		return nil, nil
	}

	return &metadata.AlbumImagesResponse{Images: buildImageList(match.ArtworkURL)}, nil
}

// GetAlbumInfo returns the Apple Music URL and editorial description for an album.
// Uses a dedicated album_info cache (separate from the album-match cache) so that
// a cache hit avoids both the iTunes Lookup KV read and the album page fetch.
func (a *appleMusicAgent) GetAlbumInfo(input metadata.AlbumRequest) (*metadata.AlbumInfoResponse, error) {
	if !isEnabled(configAlbumInfo) {
		return nil, nil
	}

	cacheKey := fmt.Sprintf("album_info:%s:%s", normalizeName(input.Artist), normalizeName(input.Name))
	var cachedInfo cachedAlbumInfo
	if kvGet(cacheKey, &cachedInfo) {
		if cachedInfo.URL == "" {
			return nil, nil
		}
		return &metadata.AlbumInfoResponse{
			Name:        input.Name,
			URL:         cachedInfo.URL,
			Description: cachedInfo.Description,
		}, nil
	}

	match, err := resolveAlbumMatch(input.Name, input.Artist)
	if err != nil {
		return nil, err
	}
	if match == nil || match.CollectionViewURL == "" {
		return nil, nil
	}

	resp := &metadata.AlbumInfoResponse{
		Name: input.Name,
		URL:  match.CollectionViewURL,
	}

	description, fetched := fetchAlbumDescription(match.CollectionViewURL)
	resp.Description = description
	if !fetched {
		// All country fetches failed: return URL but don't cache, so the next call retries.
		return resp, nil
	}

	entry := cachedAlbumInfo{URL: match.CollectionViewURL, Description: description}
	if err := kvSetWithTTL(cacheKey, entry, getCacheTTLSeconds()); err != nil {
		pdk.Log(pdk.LogWarn, "failed to cache album info: "+err.Error())
	}
	return resp, nil
}
