package main

import (
	"encoding/json"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/metadata"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const taylorSwiftID = int64(159260351)

func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	return data
}

// setupTaylorSwiftCache pre-caches the Taylor Swift artist ID and configures
// country/TTL mocks used by most capability method tests.
func setupTaylorSwiftCache() {
	host.KVStoreMock.On("Get", "artist:taylor swift").Return(
		mustMarshal(cachedArtistID{ArtistID: taylorSwiftID}), true, nil,
	)
	host.ConfigMock.On("Get", configCountries).Return("us", true)
	host.ConfigMock.On("GetInt", configCacheTTLDays).Return(int64(7), true)
}

var _ = Describe("appleMusicAgent", func() {
	BeforeEach(func() {
		pdk.ResetMock()
		host.ConfigMock.ExpectedCalls = nil
		host.ConfigMock.Calls = nil
		host.KVStoreMock.ExpectedCalls = nil
		host.KVStoreMock.Calls = nil
		host.HTTPMock.ExpectedCalls = nil
		host.HTTPMock.Calls = nil
		pdk.PDKMock.On("Log", mock.Anything, mock.Anything).Maybe()

		// Default all capabilities to enabled (not set = enabled)
		host.ConfigMock.On("Get", configArtistURL).Return("", false).Maybe()
		host.ConfigMock.On("Get", configArtistBiography).Return("", false).Maybe()
		host.ConfigMock.On("Get", configArtistImages).Return("", false).Maybe()
		host.ConfigMock.On("Get", configSimilarArtists).Return("", false).Maybe()
		host.ConfigMock.On("Get", configTopSongs).Return("", false).Maybe()
		host.ConfigMock.On("Get", configAlbumImages).Return("", false).Maybe()
	})

	Describe("getCountries", func() {
		It("returns default country when config not set", func() {
			host.ConfigMock.On("Get", configCountries).Return("", false)
			Expect(getCountries()).To(Equal([]string{"us"}))
		})

		It("returns default country when config is empty", func() {
			host.ConfigMock.On("Get", configCountries).Return("  ", true)
			Expect(getCountries()).To(Equal([]string{"us"}))
		})

		It("parses single country", func() {
			host.ConfigMock.On("Get", configCountries).Return("br", true)
			Expect(getCountries()).To(Equal([]string{"br"}))
		})

		It("parses multiple countries with spaces", func() {
			host.ConfigMock.On("Get", configCountries).Return(" br , us , de ", true)
			Expect(getCountries()).To(Equal([]string{"br", "us", "de"}))
		})

		It("normalizes to lowercase", func() {
			host.ConfigMock.On("Get", configCountries).Return("BR,US", true)
			Expect(getCountries()).To(Equal([]string{"br", "us"}))
		})

		It("skips empty entries", func() {
			host.ConfigMock.On("Get", configCountries).Return("br,,us,", true)
			Expect(getCountries()).To(Equal([]string{"br", "us"}))
		})
	})

	Describe("isEnabled", func() {
		BeforeEach(func() {
			// Clear default enable_* mocks so we can set specific expectations
			host.ConfigMock.ExpectedCalls = nil
			host.ConfigMock.Calls = nil
		})

		It("returns true when config not set (default enabled)", func() {
			host.ConfigMock.On("Get", configArtistURL).Return("", false)
			Expect(isEnabled(configArtistURL)).To(BeTrue())
		})

		It("returns true when config is true", func() {
			host.ConfigMock.On("Get", configArtistURL).Return("true", true)
			Expect(isEnabled(configArtistURL)).To(BeTrue())
		})

		It("returns false when config is false", func() {
			host.ConfigMock.On("Get", configArtistURL).Return("false", true)
			Expect(isEnabled(configArtistURL)).To(BeFalse())
		})
	})

	Describe("getCacheTTLSeconds", func() {
		It("returns default TTL when config not set", func() {
			host.ConfigMock.On("GetInt", configCacheTTLDays).Return(int64(0), false)
			Expect(getCacheTTLSeconds()).To(Equal(int64(7 * 24 * 60 * 60)))
		})

		It("returns default TTL when config is zero", func() {
			host.ConfigMock.On("GetInt", configCacheTTLDays).Return(int64(0), true)
			Expect(getCacheTTLSeconds()).To(Equal(int64(7 * 24 * 60 * 60)))
		})

		It("returns configured TTL in seconds", func() {
			host.ConfigMock.On("GetInt", configCacheTTLDays).Return(int64(14), true)
			Expect(getCacheTTLSeconds()).To(Equal(int64(14 * 24 * 60 * 60)))
		})
	})

	Describe("normalizeName", func() {
		It("lowercases and trims", func() {
			Expect(normalizeName("  Taylor Swift  ")).To(Equal("taylor swift"))
		})

		It("handles empty string", func() {
			Expect(normalizeName("")).To(Equal(""))
		})
	})

	Describe("kvGet", func() {
		It("returns cached value", func() {
			data := mustMarshal(cachedArtistID{ArtistID: 12345})
			host.KVStoreMock.On("Get", "artist:test").Return(data, true, nil)
			var result cachedArtistID
			ok := kvGet("artist:test", &result)
			Expect(ok).To(BeTrue())
			Expect(result.ArtistID).To(Equal(int64(12345)))
		})

		It("returns false when key not found", func() {
			host.KVStoreMock.On("Get", "artist:missing").Return([]byte(nil), false, nil)
			var result cachedArtistID
			ok := kvGet("artist:missing", &result)
			Expect(ok).To(BeFalse())
		})

		It("returns false on invalid JSON", func() {
			host.KVStoreMock.On("Get", "artist:bad").Return([]byte("invalid"), true, nil)
			var result cachedArtistID
			ok := kvGet("artist:bad", &result)
			Expect(ok).To(BeFalse())
		})
	})

	Describe("kvSet", func() {
		It("marshals and stores value", func() {
			expected := mustMarshal(cachedArtistID{ArtistID: 999})
			host.KVStoreMock.On("Set", "key", expected).Return(nil)
			err := kvSet("key", cachedArtistID{ArtistID: 999})
			Expect(err).ToNot(HaveOccurred())
			host.KVStoreMock.AssertCalled(GinkgoT(), "Set", "key", expected)
		})
	})

	Describe("kvSetWithTTL", func() {
		It("marshals and stores value with TTL", func() {
			expected := mustMarshal(cachedArtistID{ArtistID: 999})
			host.KVStoreMock.On("SetWithTTL", "key", expected, int64(3600)).Return(nil)
			err := kvSetWithTTL("key", cachedArtistID{ArtistID: 999}, 3600)
			Expect(err).ToNot(HaveOccurred())
			host.KVStoreMock.AssertCalled(GinkgoT(), "SetWithTTL", "key", expected, int64(3600))
		})
	})

	Describe("httpGet", func() {
		It("sends GET request with user agent", func() {
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return req.Method == "GET" &&
					req.URL == "https://example.com/test" &&
					req.Headers["User-Agent"] == userAgent &&
					req.TimeoutMs == httpTimeoutMs
			})).Return(&host.HTTPResponse{
				StatusCode: 200,
				Body:       []byte("response"),
			}, nil)

			body, status, err := httpGet("https://example.com/test")
			Expect(err).ToNot(HaveOccurred())
			Expect(status).To(Equal(int32(200)))
			Expect(body).To(Equal([]byte("response")))
		})
	})

	Describe("findBestArtistMatch", func() {
		It("returns exact case-insensitive match", func() {
			results := []itunesArtistResult{
				{WrapperType: "artist", ArtistName: "Taylor", ArtistID: 1},
				{WrapperType: "artist", ArtistName: "Taylor Swift", ArtistID: 2},
			}
			match := findBestArtistMatch("taylor swift", results)
			Expect(match).ToNot(BeNil())
			Expect(match.ArtistID).To(Equal(int64(2)))
		})

		It("falls back to first artist when no exact match", func() {
			results := []itunesArtistResult{
				{WrapperType: "collection", ArtistName: "Album", ArtistID: 1},
				{WrapperType: "artist", ArtistName: "Some Artist", ArtistID: 2},
			}
			match := findBestArtistMatch("query", results)
			Expect(match).ToNot(BeNil())
			Expect(match.ArtistID).To(Equal(int64(2)))
		})

		It("skips non-artist results", func() {
			results := []itunesArtistResult{
				{WrapperType: "collection", ArtistName: "Taylor Swift", ArtistID: 1},
			}
			match := findBestArtistMatch("Taylor Swift", results)
			Expect(match).To(BeNil())
		})

		It("returns nil for empty results", func() {
			match := findBestArtistMatch("anything", nil)
			Expect(match).To(BeNil())
		})
	})

	Describe("findBestAlbumMatch", func() {
		It("returns exact match on album name and artist name", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "Other Album", ArtistName: "Taylor Swift", ArtworkURL100: "https://img1.jpg"},
				{WrapperType: "collection", CollectionName: "1989", ArtistName: "Taylor Swift", ArtworkURL100: "https://img2.jpg"},
			}
			match := findBestAlbumMatch("1989", "Taylor Swift", results)
			Expect(match).ToNot(BeNil())
			Expect(match.ArtworkURL100).To(Equal("https://img2.jpg"))
		})

		It("matches case-insensitively", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "Midnights", ArtistName: "TAYLOR SWIFT", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("midnights", "taylor swift", results)
			Expect(match).ToNot(BeNil())
			Expect(match.CollectionName).To(Equal("Midnights"))
		})

		It("returns nil when album name matches but artist does not", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "1989", ArtistName: "Wrong Artist", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("1989", "Taylor Swift", results)
			Expect(match).To(BeNil())
		})

		It("returns nil when artist matches but album does not", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "Wrong Album", ArtistName: "Taylor Swift", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("1989", "Taylor Swift", results)
			Expect(match).To(BeNil())
		})

		It("skips non-collection results", func() {
			results := []itunesAlbumResult{
				{WrapperType: "artist", CollectionName: "1989", ArtistName: "Taylor Swift", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("1989", "Taylor Swift", results)
			Expect(match).To(BeNil())
		})

		It("returns nil for empty results", func() {
			match := findBestAlbumMatch("1989", "Taylor Swift", nil)
			Expect(match).To(BeNil())
		})

		It("matches by base name when iTunes adds ' - Single' suffix", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "Versions - Single", ArtistName: "Thievery Corporation", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("Versions", "Thievery Corporation", results)
			Expect(match).ToNot(BeNil())
			Expect(match.CollectionName).To(Equal("Versions - Single"))
		})

		It("matches by base name when iTunes adds '(Deluxe Edition)'", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "1989 (Deluxe Edition)", ArtistName: "Taylor Swift", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("1989", "Taylor Swift", results)
			Expect(match).ToNot(BeNil())
		})

		It("matches by base name when input has decorations", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "The Dark Side of the Moon", ArtistName: "Pink Floyd", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("The Dark Side of the Moon (2020) - 7.1 Multichannel", "Pink Floyd", results)
			Expect(match).ToNot(BeNil())
		})

		It("matches by containment when base names differ slightly", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "The Dark Side of the Moon", ArtistName: "Pink Floyd", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("Dark Side of the Moon", "Pink Floyd", results)
			Expect(match).ToNot(BeNil())
		})

		It("matches by containment for Pompeii-style names", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "Pink Floyd at Pompeii - MCMLXXII (2025 Mix)", ArtistName: "Pink Floyd", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("Pink Floyd at Pompeii: MCMLXXII", "Pink Floyd", results)
			Expect(match).ToNot(BeNil())
		})

		It("prefers exact match over base-name match", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "1989 (Deluxe Edition)", ArtistName: "Taylor Swift", ArtworkURL100: "https://deluxe.jpg"},
				{WrapperType: "collection", CollectionName: "1989", ArtistName: "Taylor Swift", ArtworkURL100: "https://exact.jpg"},
			}
			match := findBestAlbumMatch("1989", "Taylor Swift", results)
			Expect(match).ToNot(BeNil())
			Expect(match.ArtworkURL100).To(Equal("https://exact.jpg"))
		})

		It("does not match by containment when base name is too short", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "The Wall", ArtistName: "Pink Floyd", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("All", "Pink Floyd", results)
			Expect(match).To(BeNil())
		})
	})

	Describe("resolveArtistID", func() {
		It("returns cached artist ID", func() {
			data := mustMarshal(cachedArtistID{ArtistID: taylorSwiftID})
			host.KVStoreMock.On("Get", "artist:taylor swift").Return(data, true, nil)
			host.ConfigMock.On("Get", configCountries).Return("us", true)

			id, err := resolveArtistID("Taylor Swift")
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal(taylorSwiftID))
		})

		It("searches iTunes API on cache miss", func() {
			host.KVStoreMock.On("Get", "artist:taylor swift").Return([]byte(nil), false, nil)
			host.ConfigMock.On("Get", configCountries).Return("us", true)

			searchResp := itunesSearchResponse{
				ResultCount: 1,
				Results: []itunesArtistResult{
					{WrapperType: "artist", ArtistName: "Taylor Swift", ArtistID: taylorSwiftID},
				},
			}
			respBody := mustMarshal(searchResp)
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return req.Method == "GET" && strings.Contains(req.URL, "itunes.apple.com/search")
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			cachedData := mustMarshal(cachedArtistID{ArtistID: taylorSwiftID})
			host.KVStoreMock.On("Set", "artist:taylor swift", cachedData).Return(nil)

			id, err := resolveArtistID("Taylor Swift")
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(Equal(taylorSwiftID))
		})

		It("returns error for empty artist name", func() {
			_, err := resolveArtistID("")
			Expect(err).To(MatchError("empty artist name"))
		})

		It("returns error on negative cache hit (ArtistID == 0)", func() {
			data := mustMarshal(cachedArtistID{ArtistID: 0})
			host.KVStoreMock.On("Get", "artist:unknown artist").Return(data, true, nil)

			_, err := resolveArtistID("Unknown Artist")
			Expect(err).To(MatchError("no matching artist found"))
		})

		It("returns error when no results found", func() {
			host.KVStoreMock.On("Get", "artist:unknown").Return([]byte(nil), false, nil)
			host.ConfigMock.On("Get", configCountries).Return("us", true)

			searchResp := itunesSearchResponse{ResultCount: 0, Results: nil}
			respBody := mustMarshal(searchResp)
			host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			// Expect negative cache write
			host.KVStoreMock.On("SetWithTTL", "artist:unknown", mock.Anything, int64(negativeCacheTTLSeconds)).Return(nil)

			_, err := resolveArtistID("Unknown")
			Expect(err).To(MatchError("no artist found"))
		})

		It("caches negative result when no results found", func() {
			host.KVStoreMock.On("Get", "artist:unknown").Return([]byte(nil), false, nil)
			host.ConfigMock.On("Get", configCountries).Return("us", true)

			searchResp := itunesSearchResponse{ResultCount: 0, Results: nil}
			respBody := mustMarshal(searchResp)
			host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			negativeCacheData := mustMarshal(cachedArtistID{ArtistID: 0})
			host.KVStoreMock.On("SetWithTTL", "artist:unknown", negativeCacheData, int64(negativeCacheTTLSeconds)).Return(nil)

			_, err := resolveArtistID("Unknown")
			Expect(err).To(HaveOccurred())
			host.KVStoreMock.AssertCalled(GinkgoT(), "SetWithTTL", "artist:unknown", negativeCacheData, int64(negativeCacheTTLSeconds))
		})

		It("caches negative result when no matching artist found", func() {
			host.KVStoreMock.On("Get", "artist:unknown").Return([]byte(nil), false, nil)
			host.ConfigMock.On("Get", configCountries).Return("us", true)

			searchResp := itunesSearchResponse{
				ResultCount: 1,
				Results: []itunesArtistResult{
					{WrapperType: "collection", ArtistName: "Not An Artist", ArtistID: 1},
				},
			}
			respBody := mustMarshal(searchResp)
			host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			negativeCacheData := mustMarshal(cachedArtistID{ArtistID: 0})
			host.KVStoreMock.On("SetWithTTL", "artist:unknown", negativeCacheData, int64(negativeCacheTTLSeconds)).Return(nil)

			_, err := resolveArtistID("Unknown")
			Expect(err).To(MatchError("no matching artist found"))
			host.KVStoreMock.AssertCalled(GinkgoT(), "SetWithTTL", "artist:unknown", negativeCacheData, int64(negativeCacheTTLSeconds))
		})
	})

	Describe("resolveAlbumArtwork", func() {
		It("returns cached artwork URL", func() {
			data := mustMarshal(cachedAlbumArtwork{ArtworkURL: "https://cached.jpg"})
			host.KVStoreMock.On("Get", "album:taylor swift:1989").Return(data, true, nil)

			url, err := resolveAlbumArtwork("1989", "Taylor Swift")
			Expect(err).ToNot(HaveOccurred())
			Expect(url).To(Equal("https://cached.jpg"))
		})

		It("returns error on negative cache hit", func() {
			data := mustMarshal(cachedAlbumArtwork{ArtworkURL: ""})
			host.KVStoreMock.On("Get", "album:taylor swift:1989").Return(data, true, nil)

			_, err := resolveAlbumArtwork("1989", "Taylor Swift")
			Expect(err).To(MatchError("no matching album found"))
		})

		It("looks up albums via artist ID on cache miss", func() {
			host.KVStoreMock.On("Get", "album:taylor swift:1989").Return([]byte(nil), false, nil)
			// Mock resolveArtistID (cache hit)
			host.KVStoreMock.On("Get", "artist:taylor swift").Return(
				mustMarshal(cachedArtistID{ArtistID: taylorSwiftID}), true, nil,
			)
			host.ConfigMock.On("GetInt", configCacheTTLDays).Return(int64(7), true)

			lookupResp := itunesAlbumSearchResponse{
				ResultCount: 2,
				Results: []itunesAlbumResult{
					{WrapperType: "artist", CollectionName: "", ArtistName: "Taylor Swift", ArtworkURL100: ""},
					{WrapperType: "collection", CollectionName: "1989", ArtistName: "Taylor Swift", ArtworkURL100: "https://is1-ssl.mzstatic.com/image/thumb/Music/100x100bb.jpg"},
				},
			}
			respBody := mustMarshal(lookupResp)
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return req.Method == "GET" && strings.Contains(req.URL, "itunes.apple.com/lookup") && strings.Contains(req.URL, "entity=album")
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			host.KVStoreMock.On("SetWithTTL", "album:taylor swift:1989", mock.Anything, int64(7*24*60*60)).Return(nil)

			url, err := resolveAlbumArtwork("1989", "Taylor Swift")
			Expect(err).ToNot(HaveOccurred())
			Expect(url).To(Equal("https://is1-ssl.mzstatic.com/image/thumb/Music/100x100bb.jpg"))
		})

		It("caches negative result when no album matches", func() {
			host.KVStoreMock.On("Get", "album:taylor swift:unknown album").Return([]byte(nil), false, nil)
			host.KVStoreMock.On("Get", "artist:taylor swift").Return(
				mustMarshal(cachedArtistID{ArtistID: taylorSwiftID}), true, nil,
			)

			lookupResp := itunesAlbumSearchResponse{
				ResultCount: 2,
				Results: []itunesAlbumResult{
					{WrapperType: "artist", CollectionName: "", ArtistName: "Taylor Swift", ArtworkURL100: ""},
					{WrapperType: "collection", CollectionName: "Different Album", ArtistName: "Taylor Swift", ArtworkURL100: "https://img.jpg"},
				},
			}
			respBody := mustMarshal(lookupResp)
			host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			negativeCacheData := mustMarshal(cachedAlbumArtwork{ArtworkURL: ""})
			host.KVStoreMock.On("SetWithTTL", "album:taylor swift:unknown album", negativeCacheData, int64(negativeCacheTTLSeconds)).Return(nil)

			_, err := resolveAlbumArtwork("Unknown Album", "Taylor Swift")
			Expect(err).To(MatchError("no matching album found"))
			host.KVStoreMock.AssertCalled(GinkgoT(), "SetWithTTL", "album:taylor swift:unknown album", negativeCacheData, int64(negativeCacheTTLSeconds))
		})

		It("caches negative result when zero albums returned", func() {
			host.KVStoreMock.On("Get", "album:taylor swift:nope").Return([]byte(nil), false, nil)
			host.KVStoreMock.On("Get", "artist:taylor swift").Return(
				mustMarshal(cachedArtistID{ArtistID: taylorSwiftID}), true, nil,
			)

			lookupResp := itunesAlbumSearchResponse{ResultCount: 1, Results: []itunesAlbumResult{
				{WrapperType: "artist", CollectionName: "", ArtistName: "Taylor Swift", ArtworkURL100: ""},
			}}
			respBody := mustMarshal(lookupResp)
			host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			negativeCacheData := mustMarshal(cachedAlbumArtwork{ArtworkURL: ""})
			host.KVStoreMock.On("SetWithTTL", "album:taylor swift:nope", negativeCacheData, int64(negativeCacheTTLSeconds)).Return(nil)

			_, err := resolveAlbumArtwork("Nope", "Taylor Swift")
			Expect(err).To(MatchError("no matching album found"))
		})

		It("returns error on empty album name", func() {
			_, err := resolveAlbumArtwork("", "Taylor Swift")
			Expect(err).To(MatchError("empty album name"))
		})

		It("returns error on empty artist name", func() {
			_, err := resolveAlbumArtwork("1989", "")
			Expect(err).To(MatchError("empty artist name"))
		})
	})

	Describe("parseJSONLD", func() {
		It("extracts JSON-LD data from HTML", func() {
			html := `<html><head><script type="application/ld+json">{"@type":"MusicGroup","name":"Taylor Swift","description":"A bio","image":"https://example.com/img.jpg"}</script></head></html>`
			ld, err := parseJSONLD(html)
			Expect(err).ToNot(HaveOccurred())
			Expect(ld.Name).To(Equal("Taylor Swift"))
			Expect(ld.Description).To(Equal("A bio"))
			Expect(ld.Image).To(Equal("https://example.com/img.jpg"))
		})

		It("returns error when no JSON-LD found", func() {
			_, err := parseJSONLD("<html><head></head></html>")
			Expect(err).To(MatchError("no JSON-LD found"))
		})

		It("returns error for malformed JSON-LD", func() {
			html := `<html><script type="application/ld+json">{invalid`
			_, err := parseJSONLD(html)
			Expect(err).To(HaveOccurred())
		})

		It("handles script tag with id attribute before type", func() {
			html := `<html><script id=schema:music-group type="application/ld+json">{"@type":"MusicGroup","name":"Evanescence","description":"A rock band","image":"https://example.com/img.png"}</script></html>`
			ld, err := parseJSONLD(html)
			Expect(err).ToNot(HaveOccurred())
			Expect(ld.Name).To(Equal("Evanescence"))
			Expect(ld.Description).To(Equal("A rock band"))
		})
	})

	Describe("parseOpenGraphImage", func() {
		It("extracts og:image URL", func() {
			html := `<html><meta property="og:image" content="https://example.com/og.jpg"></html>`
			Expect(parseOpenGraphImage(html)).To(Equal("https://example.com/og.jpg"))
		})

		It("returns empty when no og:image found", func() {
			Expect(parseOpenGraphImage("<html></html>")).To(BeEmpty())
		})
	})

	Describe("rewriteImageSize", func() {
		It("rewrites dimension segment", func() {
			url := "https://is1-ssl.mzstatic.com/image/thumb/Music116/v4/ab/cd/ef/abcdef-12345/486x486bb.jpg"
			result := rewriteImageSize(url, 1000)
			Expect(result).To(ContainSubstring("/1000x1000bb."))
			Expect(result).ToNot(ContainSubstring("486x486"))
		})

		It("handles URLs without dimension segment", func() {
			url := "https://example.com/image.jpg"
			Expect(rewriteImageSize(url, 300)).To(Equal(url))
		})
	})

	Describe("parseSimilarArtists", func() {
		It("extracts artists from lockup title elements", func() {
			html := `<html><div data-testid="section-container" aria-label="Similar Artists">` +
				`<div data-testid="section-content">` +
				`<h3 data-testid="ellipse-lockup__title" class="title">Ed Sheeran</h3>` +
				`<h3 data-testid="ellipse-lockup__title" class="title">Adele</h3>` +
				`</div></div></html>`
			artists := parseSimilarArtists(html)
			Expect(artists).To(HaveLen(2))
			Expect(artists[0].Name).To(Equal("Ed Sheeran"))
			Expect(artists[1].Name).To(Equal("Adele"))
		})

		It("returns nil when no section found", func() {
			Expect(parseSimilarArtists("<html></html>")).To(BeNil())
		})

		It("deduplicates artist names", func() {
			html := `<html><div aria-label="Similar Artists">` +
				`<h3 data-testid="ellipse-lockup__title">Ed Sheeran</h3>` +
				`<h3 data-testid="ellipse-lockup__title">Ed Sheeran</h3>` +
				`</div></html>`
			artists := parseSimilarArtists(html)
			Expect(artists).To(HaveLen(1))
		})

		It("stops at next section boundary", func() {
			html := `<html><div aria-label="Similar Artists">` +
				`<h3 data-testid="ellipse-lockup__title">Amy Lee</h3>` +
				// pad to get past the 100 char offset for boundary detection
				strings.Repeat(" ", 100) +
				`<div data-testid="section-container">` +
				`<h3 data-testid="ellipse-lockup__title">Not Similar</h3>` +
				`</div></html>`
			artists := parseSimilarArtists(html)
			Expect(artists).To(HaveLen(1))
			Expect(artists[0].Name).To(Equal("Amy Lee"))
		})
	})

	Describe("hasField", func() {
		It("checks biography field", func() {
			page := &parsedPageData{Biography: "A bio"}
			Expect(hasField(page, fieldBiography)).To(BeTrue())
			Expect(hasField(&parsedPageData{}, fieldBiography)).To(BeFalse())
		})

		It("checks image field", func() {
			page := &parsedPageData{ImageURL: "https://example.com/img.jpg"}
			Expect(hasField(page, fieldImage)).To(BeTrue())
			Expect(hasField(&parsedPageData{}, fieldImage)).To(BeFalse())
		})

		It("checks similar field", func() {
			page := &parsedPageData{SimilarArtists: []similarArtistInfo{{Name: "X"}}}
			Expect(hasField(page, fieldSimilar)).To(BeTrue())
			Expect(hasField(&parsedPageData{}, fieldSimilar)).To(BeFalse())
		})

		It("checks any field by default", func() {
			Expect(hasField(&parsedPageData{Biography: "bio"}, fieldAny)).To(BeTrue())
			Expect(hasField(&parsedPageData{}, fieldAny)).To(BeFalse())
		})
	})

	Describe("fetchArtistPage", func() {
		It("returns cached page data", func() {
			host.ConfigMock.On("Get", configCountries).Return("us", true)
			host.ConfigMock.On("GetInt", configCacheTTLDays).Return(int64(7), true)

			pageData := parsedPageData{Biography: "A biography"}
			data := mustMarshal(pageData)
			host.KVStoreMock.On("Get", "page:12345:us").Return(data, true, nil)

			result, err := fetchArtistPage(12345, fieldBiography)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Biography).To(Equal("A biography"))
		})

		It("falls back to next country when first has no wanted field", func() {
			host.ConfigMock.On("Get", configCountries).Return("br,us", true)
			host.ConfigMock.On("GetInt", configCacheTTLDays).Return(int64(7), true)

			// BR page cached but no biography
			brData := mustMarshal(parsedPageData{ImageURL: "https://img.com/br.jpg"})
			host.KVStoreMock.On("Get", "page:12345:br").Return(brData, true, nil)

			// US page has biography
			usHTML := `<html><script type="application/ld+json">{"description":"English bio","image":"https://img.com/us.jpg"}</script></html>`
			host.KVStoreMock.On("Get", "page:12345:us").Return([]byte(nil), false, nil)
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return strings.Contains(req.URL, "/us/artist/-/12345")
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(usHTML)}, nil)
			host.KVStoreMock.On("SetWithTTL", "page:12345:us", mock.Anything, mock.Anything).Return(nil)

			result, err := fetchArtistPage(12345, fieldBiography)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Biography).To(Equal("English bio"))
		})
	})

	Describe("GetArtistURL", func() {
		var agent appleMusicAgent

		BeforeEach(func() {
			setupTaylorSwiftCache()
		})

		It("returns Apple Music URL", func() {
			resp, err := agent.GetArtistURL(metadata.ArtistRequest{Name: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.URL).To(Equal("https://music.apple.com/us/artist/-/159260351"))
		})

		It("returns nil when disabled", func() {
			host.ConfigMock.ExpectedCalls = nil
			host.ConfigMock.On("Get", configArtistURL).Return("false", true)
			resp, err := agent.GetArtistURL(metadata.ArtistRequest{Name: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(BeNil())
		})
	})

	Describe("GetArtistBiography", func() {
		var agent appleMusicAgent

		BeforeEach(func() {
			setupTaylorSwiftCache()
		})

		It("returns biography from cached page", func() {
			pageData := parsedPageData{Biography: "Taylor Swift biography"}
			pageBytes := mustMarshal(pageData)
			host.KVStoreMock.On("Get", "page:159260351:us").Return(pageBytes, true, nil)

			resp, err := agent.GetArtistBiography(metadata.ArtistRequest{Name: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Biography).To(Equal("Taylor Swift biography"))
		})

		It("returns error when no biography found", func() {
			pageData := parsedPageData{ImageURL: "https://img.com/img.jpg"}
			pageBytes := mustMarshal(pageData)
			host.KVStoreMock.On("Get", "page:159260351:us").Return(pageBytes, true, nil)

			_, err := agent.GetArtistBiography(metadata.ArtistRequest{Name: "Taylor Swift"})
			Expect(err).To(MatchError("no biography found"))
		})
	})

	Describe("GetArtistImages", func() {
		var agent appleMusicAgent

		BeforeEach(func() {
			setupTaylorSwiftCache()
		})

		It("returns images in multiple sizes", func() {
			pageData := parsedPageData{ImageURL: "https://is1-ssl.mzstatic.com/image/thumb/Music116/486x486bb.jpg"}
			pageBytes := mustMarshal(pageData)
			host.KVStoreMock.On("Get", "page:159260351:us").Return(pageBytes, true, nil)

			resp, err := agent.GetArtistImages(metadata.ArtistRequest{Name: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Images).To(HaveLen(3))
			Expect(resp.Images[0].Size).To(Equal(int32(1000)))
			Expect(resp.Images[0].URL).To(ContainSubstring("1000x1000bb"))
			Expect(resp.Images[1].Size).To(Equal(int32(600)))
			Expect(resp.Images[2].Size).To(Equal(int32(300)))
		})

		It("returns error when no image found", func() {
			pageData := parsedPageData{Biography: "A bio"}
			pageBytes := mustMarshal(pageData)
			host.KVStoreMock.On("Get", "page:159260351:us").Return(pageBytes, true, nil)

			_, err := agent.GetArtistImages(metadata.ArtistRequest{Name: "Taylor Swift"})
			Expect(err).To(MatchError("no artist image found"))
		})
	})

	Describe("GetSimilarArtists", func() {
		var agent appleMusicAgent

		BeforeEach(func() {
			setupTaylorSwiftCache()
		})

		It("returns similar artists", func() {
			pageData := parsedPageData{SimilarArtists: []similarArtistInfo{{Name: "Ed Sheeran"}, {Name: "Adele"}, {Name: "Lorde"}}}
			pageBytes := mustMarshal(pageData)
			host.KVStoreMock.On("Get", "page:159260351:us").Return(pageBytes, true, nil)

			resp, err := agent.GetSimilarArtists(metadata.SimilarArtistsRequest{Name: "Taylor Swift", Limit: 2})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Artists).To(HaveLen(2))
			Expect(resp.Artists[0].Name).To(Equal("Ed Sheeran"))
			Expect(resp.Artists[1].Name).To(Equal("Adele"))
		})

		It("returns all when limit is 0", func() {
			pageData := parsedPageData{SimilarArtists: []similarArtistInfo{{Name: "A"}, {Name: "B"}}}
			pageBytes := mustMarshal(pageData)
			host.KVStoreMock.On("Get", "page:159260351:us").Return(pageBytes, true, nil)

			resp, err := agent.GetSimilarArtists(metadata.SimilarArtistsRequest{Name: "Taylor Swift", Limit: 0})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Artists).To(HaveLen(2))
		})
	})

	Describe("GetArtistTopSongs", func() {
		var agent appleMusicAgent

		BeforeEach(func() {
			setupTaylorSwiftCache()
		})

		It("returns top songs from iTunes Lookup API", func() {
			host.KVStoreMock.On("Get", "topsongs:159260351:5").Return([]byte(nil), false, nil)

			lookupResp := itunesLookupResponse{
				ResultCount: 3,
				Results: []itunesLookupResult{
					{WrapperType: "artist", ArtistName: "Taylor Swift", ArtistID: taylorSwiftID},
					{WrapperType: "track", TrackName: "Anti-Hero", ArtistName: "Taylor Swift"},
					{WrapperType: "track", TrackName: "Shake It Off", ArtistName: "Taylor Swift"},
				},
			}
			respBody := mustMarshal(lookupResp)
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return strings.Contains(req.URL, "itunes.apple.com/lookup")
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			host.KVStoreMock.On("SetWithTTL", "topsongs:159260351:5", mock.Anything, mock.Anything).Return(nil)

			resp, err := agent.GetArtistTopSongs(metadata.TopSongsRequest{Name: "Taylor Swift", Count: 5})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Songs).To(HaveLen(2))
			Expect(resp.Songs[0].Name).To(Equal("Anti-Hero"))
			Expect(resp.Songs[1].Name).To(Equal("Shake It Off"))
		})

		It("returns cached top songs", func() {
			cached := metadata.TopSongsResponse{Songs: []metadata.SongRef{{Name: "Cached Song", Artist: "Taylor Swift"}}}
			cachedBytes := mustMarshal(cached)
			host.KVStoreMock.On("Get", "topsongs:159260351:10").Return(cachedBytes, true, nil)

			resp, err := agent.GetArtistTopSongs(metadata.TopSongsRequest{Name: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Songs).To(HaveLen(1))
			Expect(resp.Songs[0].Name).To(Equal("Cached Song"))
		})
	})

	Describe("GetAlbumImages", func() {
		var agent appleMusicAgent

		It("returns album images in multiple sizes", func() {
			data := mustMarshal(cachedAlbumArtwork{ArtworkURL: "https://is1-ssl.mzstatic.com/image/thumb/Music116/100x100bb.jpg"})
			host.KVStoreMock.On("Get", "album:taylor swift:1989").Return(data, true, nil)

			resp, err := agent.GetAlbumImages(metadata.AlbumRequest{Name: "1989", Artist: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Images).To(HaveLen(3))
			Expect(resp.Images[0].Size).To(Equal(int32(1000)))
			Expect(resp.Images[0].URL).To(ContainSubstring("1000x1000bb"))
			Expect(resp.Images[1].Size).To(Equal(int32(600)))
			Expect(resp.Images[1].URL).To(ContainSubstring("600x600bb"))
			Expect(resp.Images[2].Size).To(Equal(int32(300)))
			Expect(resp.Images[2].URL).To(ContainSubstring("300x300bb"))
		})

		It("returns error when album not found", func() {
			data := mustMarshal(cachedAlbumArtwork{ArtworkURL: ""})
			host.KVStoreMock.On("Get", "album:taylor swift:unknown").Return(data, true, nil)

			_, err := agent.GetAlbumImages(metadata.AlbumRequest{Name: "Unknown", Artist: "Taylor Swift"})
			Expect(err).To(HaveOccurred())
		})

		It("returns nil when disabled", func() {
			host.ConfigMock.ExpectedCalls = nil
			host.ConfigMock.Calls = nil
			host.ConfigMock.On("Get", configAlbumImages).Return("false", true)

			resp, err := agent.GetAlbumImages(metadata.AlbumRequest{Name: "1989", Artist: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(BeNil())
		})
	})
})
