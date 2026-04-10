package main

import (
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("album", func() {
	Describe("findBestAlbumMatch", func() {
		It("returns exact match on album name", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "Other Album", ArtistName: "Taylor Swift", ArtworkURL100: "https://img1.jpg"},
				{WrapperType: "collection", CollectionName: "1989", ArtistName: "Taylor Swift", ArtworkURL100: "https://img2.jpg"},
			}
			match := findBestAlbumMatch("1989", results)
			Expect(match).ToNot(BeNil())
			Expect(match.ArtworkURL100).To(Equal("https://img2.jpg"))
		})

		It("matches case-insensitively", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "Midnights", ArtistName: "TAYLOR SWIFT", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("midnights", results)
			Expect(match).ToNot(BeNil())
			Expect(match.CollectionName).To(Equal("Midnights"))
		})

		It("returns nil when album name does not match", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "Wrong Album", ArtistName: "Taylor Swift", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("1989", results)
			Expect(match).To(BeNil())
		})

		It("skips non-collection results", func() {
			results := []itunesAlbumResult{
				{WrapperType: "artist", CollectionName: "1989", ArtistName: "Taylor Swift", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("1989", results)
			Expect(match).To(BeNil())
		})

		It("returns nil for empty results", func() {
			match := findBestAlbumMatch("1989", nil)
			Expect(match).To(BeNil())
		})

		It("matches by base name when iTunes adds ' - Single' suffix", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "Versions - Single", ArtistName: "Thievery Corporation", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("Versions", results)
			Expect(match).ToNot(BeNil())
			Expect(match.CollectionName).To(Equal("Versions - Single"))
		})

		It("matches by base name when iTunes adds '(Deluxe Edition)'", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "1989 (Deluxe Edition)", ArtistName: "Taylor Swift", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("1989", results)
			Expect(match).ToNot(BeNil())
		})

		It("matches by base name when input has decorations", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "The Dark Side of the Moon", ArtistName: "Pink Floyd", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("The Dark Side of the Moon (2020) - 7.1 Multichannel", results)
			Expect(match).ToNot(BeNil())
		})

		It("matches by containment when base names differ slightly", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "The Dark Side of the Moon", ArtistName: "Pink Floyd", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("Dark Side of the Moon", results)
			Expect(match).ToNot(BeNil())
		})

		It("matches by containment for Pompeii-style names", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "Pink Floyd at Pompeii - MCMLXXII (2025 Mix)", ArtistName: "Pink Floyd", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("Pink Floyd at Pompeii: MCMLXXII", results)
			Expect(match).ToNot(BeNil())
		})

		It("prefers exact match over base-name match", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "1989 (Deluxe Edition)", ArtistName: "Taylor Swift", ArtworkURL100: "https://deluxe.jpg"},
				{WrapperType: "collection", CollectionName: "1989", ArtistName: "Taylor Swift", ArtworkURL100: "https://exact.jpg"},
			}
			match := findBestAlbumMatch("1989", results)
			Expect(match).ToNot(BeNil())
			Expect(match.ArtworkURL100).To(Equal("https://exact.jpg"))
		})

		It("does not match by containment when base name is too short", func() {
			results := []itunesAlbumResult{
				{WrapperType: "collection", CollectionName: "The Wall", ArtistName: "Pink Floyd", ArtworkURL100: "https://img.jpg"},
			}
			match := findBestAlbumMatch("All", results)
			Expect(match).To(BeNil())
		})
	})

	Describe("resolveAlbumMatch", func() {
		It("returns cached match", func() {
			data := mustMarshal(cachedAlbumMatch{
				ArtworkURL:        "https://cached.jpg",
				CollectionViewURL: "https://music.apple.com/us/album/1989/1",
			})
			host.KVStoreMock.On("Get", "album:taylor swift:1989").Return(data, true, nil)

			match, err := resolveAlbumMatch("1989", "Taylor Swift")
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeNil())
			Expect(match.ArtworkURL).To(Equal("https://cached.jpg"))
			Expect(match.CollectionViewURL).To(Equal("https://music.apple.com/us/album/1989/1"))
		})

		It("returns nil on negative cache hit", func() {
			data := mustMarshal(cachedAlbumMatch{})
			host.KVStoreMock.On("Get", "album:taylor swift:1989").Return(data, true, nil)

			match, err := resolveAlbumMatch("1989", "Taylor Swift")
			Expect(err).ToNot(HaveOccurred())
			Expect(match).To(BeNil())
		})

		It("looks up albums via artist ID on cache miss", func() {
			host.KVStoreMock.On("Get", "album:taylor swift:1989").Return([]byte(nil), false, nil)
			// Mock resolveArtistID (cache hit) + config for cache TTL
			host.KVStoreMock.On("Get", "artist:taylor swift").Return(
				mustMarshal(cachedArtistID{ArtistID: taylorSwiftID}), true, nil,
			)
			host.ConfigMock.On("Get", configCountries).Return("us", true).Maybe()
			host.ConfigMock.On("GetInt", configCacheTTLDays).Return(int64(7), true)

			lookupResp := itunesAlbumSearchResponse{
				ResultCount: 2,
				Results: []itunesAlbumResult{
					{WrapperType: "artist", CollectionName: "", ArtistName: "Taylor Swift", ArtworkURL100: ""},
					{
						WrapperType:       "collection",
						CollectionName:    "1989",
						ArtistName:        "Taylor Swift",
						ArtworkURL100:     "https://is1-ssl.mzstatic.com/image/thumb/Music/100x100bb.jpg",
						CollectionViewURL: "https://music.apple.com/us/album/1989/1440935467?uo=4",
					},
				},
			}
			respBody := mustMarshal(lookupResp)
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return req.Method == "GET" && strings.Contains(req.URL, "itunes.apple.com/lookup") && strings.Contains(req.URL, "entity=album")
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			host.KVStoreMock.On("SetWithTTL", "album:taylor swift:1989", mock.Anything, int64(7*24*60*60)).Return(nil)

			match, err := resolveAlbumMatch("1989", "Taylor Swift")
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeNil())
			Expect(match.ArtworkURL).To(Equal("https://is1-ssl.mzstatic.com/image/thumb/Music/100x100bb.jpg"))
			Expect(match.CollectionViewURL).To(Equal("https://music.apple.com/us/album/1989/1440935467"))
		})

		It("caches negative result when no album matches", func() {
			host.KVStoreMock.On("Get", "album:taylor swift:unknown album").Return([]byte(nil), false, nil)
			host.KVStoreMock.On("Get", "artist:taylor swift").Return(
				mustMarshal(cachedArtistID{ArtistID: taylorSwiftID}), true, nil,
			)
			host.ConfigMock.On("Get", configCountries).Return("us", true).Maybe()

			lookupResp := itunesAlbumSearchResponse{
				ResultCount: 2,
				Results: []itunesAlbumResult{
					{WrapperType: "artist", CollectionName: "", ArtistName: "Taylor Swift", ArtworkURL100: ""},
					{WrapperType: "collection", CollectionName: "Different Album", ArtistName: "Taylor Swift", ArtworkURL100: "https://img.jpg"},
				},
			}
			respBody := mustMarshal(lookupResp)
			host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			negativeCacheData := mustMarshal(cachedAlbumMatch{})
			host.KVStoreMock.On("SetWithTTL", "album:taylor swift:unknown album", negativeCacheData, int64(negativeCacheTTLSeconds)).Return(nil)

			match, err := resolveAlbumMatch("Unknown Album", "Taylor Swift")
			Expect(err).ToNot(HaveOccurred())
			Expect(match).To(BeNil())
			host.KVStoreMock.AssertCalled(GinkgoT(), "SetWithTTL", "album:taylor swift:unknown album", negativeCacheData, int64(negativeCacheTTLSeconds))
		})

		It("caches negative result when zero albums returned", func() {
			host.KVStoreMock.On("Get", "album:taylor swift:nope").Return([]byte(nil), false, nil)
			host.KVStoreMock.On("Get", "artist:taylor swift").Return(
				mustMarshal(cachedArtistID{ArtistID: taylorSwiftID}), true, nil,
			)
			host.ConfigMock.On("Get", configCountries).Return("us", true).Maybe()

			lookupResp := itunesAlbumSearchResponse{ResultCount: 1, Results: []itunesAlbumResult{
				{WrapperType: "artist", CollectionName: "", ArtistName: "Taylor Swift", ArtworkURL100: ""},
			}}
			respBody := mustMarshal(lookupResp)
			host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			negativeCacheData := mustMarshal(cachedAlbumMatch{})
			host.KVStoreMock.On("SetWithTTL", "album:taylor swift:nope", negativeCacheData, int64(negativeCacheTTLSeconds)).Return(nil)

			match, err := resolveAlbumMatch("Nope", "Taylor Swift")
			Expect(err).ToNot(HaveOccurred())
			Expect(match).To(BeNil())
		})

		It("returns error on empty album name", func() {
			_, err := resolveAlbumMatch("", "Taylor Swift")
			Expect(err).To(MatchError("empty album name"))
		})

		It("returns error on empty artist name", func() {
			_, err := resolveAlbumMatch("1989", "")
			Expect(err).To(MatchError("empty artist name"))
		})
	})

	Describe("parseAlbumDescription", func() {
		It("extracts editorial notes from serialized-server-data", func() {
			html := []byte(`<html><body><script id="serialized-server-data" type="application/json">[{"data":[{"data":{"sections":[{"items":[{"modalPresentationDescriptor":{"paragraphText":"A real album description with <i>italic</i> text."}}]}]}}]}]</script></body></html>`)
			Expect(parseAlbumDescription(html)).To(Equal("A real album description with <i>italic</i> text."))
		})

		It("returns empty when paragraphText is empty", func() {
			html := []byte(`<html><body><script id="serialized-server-data" type="application/json">[{"data":[{"data":{"sections":[{"items":[{"modalPresentationDescriptor":{"paragraphText":""}}]}]}}]}]</script></body></html>`)
			Expect(parseAlbumDescription(html)).To(BeEmpty())
		})

		It("returns empty when no serialized-server-data block", func() {
			Expect(parseAlbumDescription([]byte(`<html><body>nothing here</body></html>`))).To(BeEmpty())
		})

		It("returns empty on malformed JSON", func() {
			html := []byte(`<html><body><script id="serialized-server-data" type="application/json">not json at all</script></body></html>`)
			Expect(parseAlbumDescription(html)).To(BeEmpty())
		})

		It("skips sections without editorial notes", func() {
			html := []byte(`<html><body><script id="serialized-server-data" type="application/json">[{"data":[{"data":{"sections":[{"items":[{"modalPresentationDescriptor":{"paragraphText":""}}]},{"items":[{"modalPresentationDescriptor":{"paragraphText":"Found it"}}]}]}}]}]</script></body></html>`)
			Expect(parseAlbumDescription(html)).To(Equal("Found it"))
		})
	})

	Describe("rewriteAlbumURLCountry", func() {
		It("rewrites the country segment", func() {
			Expect(rewriteAlbumURLCountry("https://music.apple.com/us/album/lover/1468058165", "fi")).
				To(Equal("https://music.apple.com/fi/album/lover/1468058165"))
		})

		It("leaves non-matching URLs unchanged", func() {
			Expect(rewriteAlbumURLCountry("https://example.com/foo", "fi")).To(Equal("https://example.com/foo"))
		})
	})

	Describe("stripTrackingParams", func() {
		It("removes query string", func() {
			Expect(stripTrackingParams("https://music.apple.com/us/album/1989/1?uo=4")).
				To(Equal("https://music.apple.com/us/album/1989/1"))
		})

		It("leaves URLs without query string unchanged", func() {
			Expect(stripTrackingParams("https://music.apple.com/us/album/1989/1")).
				To(Equal("https://music.apple.com/us/album/1989/1"))
		})
	})
})
