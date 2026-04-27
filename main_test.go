package main

import (
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/metadata"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const taylorSwiftID = int64(159260351)

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

		It("returns nil when no biography found", func() {
			pageData := parsedPageData{ImageURL: "https://img.com/img.jpg"}
			pageBytes := mustMarshal(pageData)
			host.KVStoreMock.On("Get", "page:159260351:us").Return(pageBytes, true, nil)

			resp, err := agent.GetArtistBiography(metadata.ArtistRequest{Name: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(BeNil())
		})

		It("returns nil when biography is a generic Apple Music promotional text", func() {
			pageData := parsedPageData{Biography: "Listen to music by Taylor Swift on Apple Music. Find top songs and albums by Taylor Swift."}
			pageBytes := mustMarshal(pageData)
			host.KVStoreMock.On("Get", "page:159260351:us").Return(pageBytes, true, nil)

			resp, err := agent.GetArtistBiography(metadata.ArtistRequest{Name: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(BeNil())
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
			Expect(resp.Images[0].Size).To(Equal(int32(1500)))
			Expect(resp.Images[0].URL).To(ContainSubstring("1500x1500bb"))
			Expect(resp.Images[1].Size).To(Equal(int32(600)))
			Expect(resp.Images[2].Size).To(Equal(int32(300)))
		})

		It("returns nil when no image found", func() {
			pageData := parsedPageData{Biography: "A bio"}
			pageBytes := mustMarshal(pageData)
			host.KVStoreMock.On("Get", "page:159260351:us").Return(pageBytes, true, nil)

			resp, err := agent.GetArtistImages(metadata.ArtistRequest{Name: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(BeNil())
		})

		It("returns nil when image is the generic Apple Music placeholder", func() {
			pageData := parsedPageData{ImageURL: "https://music.apple.com/assets/meta/apple-music.png"}
			pageBytes := mustMarshal(pageData)
			host.KVStoreMock.On("Get", "page:159260351:us").Return(pageBytes, true, nil)

			resp, err := agent.GetArtistImages(metadata.ArtistRequest{Name: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(BeNil())
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
			data := mustMarshal(cachedAlbumMatch{
				ArtworkURL:        "https://is1-ssl.mzstatic.com/image/thumb/Music116/100x100bb.jpg",
				CollectionViewURL: "https://music.apple.com/us/album/1989/1",
			})
			host.KVStoreMock.On("Get", "album:taylor swift:1989").Return(data, true, nil)

			resp, err := agent.GetAlbumImages(metadata.AlbumRequest{Name: "1989", Artist: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Images).To(HaveLen(3))
			Expect(resp.Images[0].Size).To(Equal(int32(1500)))
			Expect(resp.Images[0].URL).To(ContainSubstring("1500x1500bb"))
			Expect(resp.Images[1].Size).To(Equal(int32(600)))
			Expect(resp.Images[1].URL).To(ContainSubstring("600x600bb"))
			Expect(resp.Images[2].Size).To(Equal(int32(300)))
			Expect(resp.Images[2].URL).To(ContainSubstring("300x300bb"))
		})

		It("returns nil when album not found", func() {
			data := mustMarshal(cachedAlbumMatch{})
			host.KVStoreMock.On("Get", "album:taylor swift:unknown").Return(data, true, nil)

			resp, err := agent.GetAlbumImages(metadata.AlbumRequest{Name: "Unknown", Artist: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(BeNil())
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

	Describe("GetAlbumInfo", func() {
		var agent appleMusicAgent

		const albumPageHTML = `<html><body><script id="serialized-server-data" type="application/json">[{"data":[{"data":{"sections":[{"items":[{"modalPresentationDescriptor":{"paragraphText":"A lovely editorial review of the album."}}]}]}}]}]</script></body></html>`

		It("returns description and URL on happy path, and caches the entry", func() {
			host.KVStoreMock.On("Get", "album_info:taylor swift:1989").Return([]byte(nil), false, nil)
			matchData := mustMarshal(cachedAlbumMatch{
				ArtworkURL:        "https://img.jpg",
				CollectionViewURL: "https://music.apple.com/us/album/1989/1440935467",
			})
			host.KVStoreMock.On("Get", "album:taylor swift:1989").Return(matchData, true, nil)
			host.ConfigMock.On("Get", configCountries).Return("us", true)
			host.ConfigMock.On("GetInt", configCacheTTLDays).Return(int64(7), true)
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return strings.Contains(req.URL, "music.apple.com/us/album/1989/1440935467")
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(albumPageHTML)}, nil)

			expectedCache := mustMarshal(cachedAlbumInfo{
				URL:         "https://music.apple.com/us/album/1989/1440935467",
				Description: "A lovely editorial review of the album.",
			})
			host.KVStoreMock.On("SetWithTTL", "album_info:taylor swift:1989", expectedCache, int64(7*24*60*60)).Return(nil)

			resp, err := agent.GetAlbumInfo(metadata.AlbumRequest{Name: "1989", Artist: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			Expect(resp.Name).To(Equal("1989"))
			Expect(resp.URL).To(Equal("https://music.apple.com/us/album/1989/1440935467"))
			Expect(resp.Description).To(Equal("A lovely editorial review of the album."))
			Expect(resp.MBID).To(BeEmpty())
			host.KVStoreMock.AssertCalled(GinkgoT(), "SetWithTTL", "album_info:taylor swift:1989", expectedCache, int64(7*24*60*60))
		})

		It("returns cached entry without consulting album match or fetching page", func() {
			cached := mustMarshal(cachedAlbumInfo{
				URL:         "https://music.apple.com/us/album/1989/1",
				Description: "Previously cached notes.",
			})
			host.KVStoreMock.On("Get", "album_info:taylor swift:1989").Return(cached, true, nil)

			resp, err := agent.GetAlbumInfo(metadata.AlbumRequest{Name: "1989", Artist: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			Expect(resp.URL).To(Equal("https://music.apple.com/us/album/1989/1"))
			Expect(resp.Description).To(Equal("Previously cached notes."))
			host.KVStoreMock.AssertNotCalled(GinkgoT(), "Get", "album:taylor swift:1989")
			host.HTTPMock.AssertNotCalled(GinkgoT(), "Send", mock.Anything)
		})

		It("returns nil when cached entry is an empty sentinel (negative cache)", func() {
			host.KVStoreMock.On("Get", "album_info:taylor swift:1989").Return(mustMarshal(cachedAlbumInfo{}), true, nil)

			resp, err := agent.GetAlbumInfo(metadata.AlbumRequest{Name: "1989", Artist: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(BeNil())
		})

		It("caches entry with empty description when page has no editorial notes", func() {
			host.KVStoreMock.On("Get", "album_info:band:demo").Return([]byte(nil), false, nil)
			matchData := mustMarshal(cachedAlbumMatch{
				ArtworkURL:        "https://img.jpg",
				CollectionViewURL: "https://music.apple.com/us/album/demo/1",
			})
			host.KVStoreMock.On("Get", "album:band:demo").Return(matchData, true, nil)
			host.ConfigMock.On("Get", configCountries).Return("us", true)
			host.ConfigMock.On("GetInt", configCacheTTLDays).Return(int64(7), true)
			emptyHTML := `<html><body><script id="serialized-server-data" type="application/json">[{"data":[{"data":{"sections":[]}}]}]</script></body></html>`
			host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(emptyHTML)}, nil)

			expectedCache := mustMarshal(cachedAlbumInfo{URL: "https://music.apple.com/us/album/demo/1", Description: ""})
			host.KVStoreMock.On("SetWithTTL", "album_info:band:demo", expectedCache, int64(7*24*60*60)).Return(nil)

			resp, err := agent.GetAlbumInfo(metadata.AlbumRequest{Name: "Demo", Artist: "Band"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			Expect(resp.URL).To(Equal("https://music.apple.com/us/album/demo/1"))
			Expect(resp.Description).To(BeEmpty())
			host.KVStoreMock.AssertCalled(GinkgoT(), "SetWithTTL", "album_info:band:demo", expectedCache, int64(7*24*60*60))
		})

		It("falls through countries when first country has no description", func() {
			host.KVStoreMock.On("Get", "album_info:taylor swift:1989").Return([]byte(nil), false, nil)
			matchData := mustMarshal(cachedAlbumMatch{
				ArtworkURL:        "https://img.jpg",
				CollectionViewURL: "https://music.apple.com/us/album/1989/1",
			})
			host.KVStoreMock.On("Get", "album:taylor swift:1989").Return(matchData, true, nil)
			host.ConfigMock.On("Get", configCountries).Return("us,br", true)
			host.ConfigMock.On("GetInt", configCacheTTLDays).Return(int64(7), true)
			host.KVStoreMock.On("SetWithTTL", "album_info:taylor swift:1989", mock.Anything, int64(7*24*60*60)).Return(nil)

			emptyHTML := `<html><body><script id="serialized-server-data" type="application/json">[{"data":[{"data":{"sections":[]}}]}]</script></body></html>`
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return strings.Contains(req.URL, "/us/album/")
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(emptyHTML)}, nil)
			host.HTTPMock.On("Send", mock.MatchedBy(func(req host.HTTPRequest) bool {
				return strings.Contains(req.URL, "/br/album/")
			})).Return(&host.HTTPResponse{StatusCode: 200, Body: []byte(albumPageHTML)}, nil)

			resp, err := agent.GetAlbumInfo(metadata.AlbumRequest{Name: "1989", Artist: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.Description).To(Equal("A lovely editorial review of the album."))
		})

		It("returns nil when no album match", func() {
			host.KVStoreMock.On("Get", "album_info:taylor swift:unknown").Return([]byte(nil), false, nil)
			host.KVStoreMock.On("Get", "album:taylor swift:unknown").Return(mustMarshal(cachedAlbumMatch{}), true, nil)

			resp, err := agent.GetAlbumInfo(metadata.AlbumRequest{Name: "Unknown", Artist: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(BeNil())
		})

		It("returns nil when disabled", func() {
			host.ConfigMock.ExpectedCalls = nil
			host.ConfigMock.Calls = nil
			host.ConfigMock.On("Get", configAlbumInfo).Return("false", true)

			resp, err := agent.GetAlbumInfo(metadata.AlbumRequest{Name: "1989", Artist: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).To(BeNil())
		})

		It("returns response with URL but does not cache when page fetch fails", func() {
			host.KVStoreMock.On("Get", "album_info:taylor swift:1989").Return([]byte(nil), false, nil)
			matchData := mustMarshal(cachedAlbumMatch{
				ArtworkURL:        "https://img.jpg",
				CollectionViewURL: "https://music.apple.com/us/album/1989/1",
			})
			host.KVStoreMock.On("Get", "album:taylor swift:1989").Return(matchData, true, nil)
			host.ConfigMock.On("Get", configCountries).Return("us", true)
			host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 500, Body: nil}, nil)

			resp, err := agent.GetAlbumInfo(metadata.AlbumRequest{Name: "1989", Artist: "Taylor Swift"})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			Expect(resp.URL).To(Equal("https://music.apple.com/us/album/1989/1"))
			Expect(resp.Description).To(BeEmpty())
			host.KVStoreMock.AssertNotCalled(GinkgoT(), "SetWithTTL", "album_info:taylor swift:1989", mock.Anything, mock.Anything)
		})
	})
})
