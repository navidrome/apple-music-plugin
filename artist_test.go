package main

import (
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("artist", func() {
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

		It("returns zero on negative cache hit (ArtistID == 0)", func() {
			data := mustMarshal(cachedArtistID{ArtistID: 0})
			host.KVStoreMock.On("Get", "artist:unknown artist").Return(data, true, nil)

			id, err := resolveArtistID("Unknown Artist")
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(BeZero())
		})

		It("returns zero when no results found", func() {
			host.KVStoreMock.On("Get", "artist:unknown").Return([]byte(nil), false, nil)
			host.ConfigMock.On("Get", configCountries).Return("us", true)

			searchResp := itunesSearchResponse{ResultCount: 0, Results: nil}
			respBody := mustMarshal(searchResp)
			host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			// Expect negative cache write
			host.KVStoreMock.On("SetWithTTL", "artist:unknown", mock.Anything, int64(negativeCacheTTLSeconds)).Return(nil)

			id, err := resolveArtistID("Unknown")
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(BeZero())
		})

		It("caches negative result when no results found", func() {
			host.KVStoreMock.On("Get", "artist:unknown").Return([]byte(nil), false, nil)
			host.ConfigMock.On("Get", configCountries).Return("us", true)

			searchResp := itunesSearchResponse{ResultCount: 0, Results: nil}
			respBody := mustMarshal(searchResp)
			host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 200, Body: respBody}, nil)

			negativeCacheData := mustMarshal(cachedArtistID{ArtistID: 0})
			host.KVStoreMock.On("SetWithTTL", "artist:unknown", negativeCacheData, int64(negativeCacheTTLSeconds)).Return(nil)

			id, err := resolveArtistID("Unknown")
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(BeZero())
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

			id, err := resolveArtistID("Unknown")
			Expect(err).ToNot(HaveOccurred())
			Expect(id).To(BeZero())
			host.KVStoreMock.AssertCalled(GinkgoT(), "SetWithTTL", "artist:unknown", negativeCacheData, int64(negativeCacheTTLSeconds))
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

	Describe("isPlaceholderImage", func() {
		It("returns true for the generic Apple Music placeholder", func() {
			Expect(isPlaceholderImage("https://music.apple.com/assets/meta/apple-music.png")).To(BeTrue())
		})

		It("returns false for a real artist image URL", func() {
			Expect(isPlaceholderImage("https://is1-ssl.mzstatic.com/image/thumb/Music116/486x486bb.jpg")).To(BeFalse())
		})

		It("returns false for empty string", func() {
			Expect(isPlaceholderImage("")).To(BeFalse())
		})
	})

	Describe("isPlaceholderBiography", func() {
		It("returns true for English promotional text", func() {
			text := `Listen to music by Der Schlunz on Apple Music. Find top songs and albums by Der Schlunz including 09 - Der Schlunz and more.`
			Expect(isPlaceholderBiography(text)).To(BeTrue())
		})

		It("returns true for German promotional text", func() {
			text := "Hör dir Musik von Der Schlunz auf Apple\u00a0Music an. Finde Toptitel und -alben von Der Schlunz."
			Expect(isPlaceholderBiography(text)).To(BeTrue())
		})

		It("returns true for French promotional text", func() {
			text := `Écoutez la musique de Der Schlunz sur Apple Music. Découvrez les meilleurs titres et albums de Der Schlunz.`
			Expect(isPlaceholderBiography(text)).To(BeTrue())
		})

		It("returns true for Spanish promotional text", func() {
			text := `Escucha música de Der Schlunz en Apple Music. Busca canciones y álbumes de Der Schlunz.`
			Expect(isPlaceholderBiography(text)).To(BeTrue())
		})

		It("returns true for Portuguese promotional text", func() {
			text := `Escute as músicas de Der Schlunz no Apple Music. Encontre as melhores músicas e álbuns de Der Schlunz.`
			Expect(isPlaceholderBiography(text)).To(BeTrue())
		})

		It("returns true for Italian promotional text", func() {
			text := `Ascolta la musica di Der Schlunz su Apple Music. Trova i brani icons e icons albums di Der Schlunz.`
			Expect(isPlaceholderBiography(text)).To(BeTrue())
		})

		It("returns false for a real biography that mentions Apple Music later", func() {
			text := `Taylor Swift formally embraced pop on 2012's Red. She earned the Apple Music Award for Songwriter of the Year.`
			Expect(isPlaceholderBiography(text)).To(BeFalse())
		})

		It("returns false for a real biography", func() {
			text := `Taylor Alison Swift is an American singer-songwriter. Recognized for her genre-restless artistry, she is a prominent cultural figure of the 21st century.`
			Expect(isPlaceholderBiography(text)).To(BeFalse())
		})

		It("returns false for empty string", func() {
			Expect(isPlaceholderBiography("")).To(BeFalse())
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
})
