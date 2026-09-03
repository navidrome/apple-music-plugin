package main

import (
	"time"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/stretchr/testify/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("helpers", func() {
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

	Describe("normalizeText", func() {
		It("collapses tab characters between words into single spaces", func() {
			Expect(normalizeText("In\tHet\tMidden\tVan\tAlles")).To(Equal("In Het Midden Van Alles"))
		})

		It("collapses mixed runs of whitespace (tabs, newlines, NBSP, narrow NBSP)", func() {
			Expect(normalizeText("In\tHet\tMidden\tVan\tAlles\t  \t  Direct nadat BLØF")).
				To(Equal("In Het Midden Van Alles Direct nadat BLØF"))
		})

		It("trims leading and trailing whitespace", func() {
			Expect(normalizeText("  \t hello world \n ")).To(Equal("hello world"))
		})

		It("leaves already-clean text (including HTML tags) unchanged", func() {
			Expect(normalizeText("A real album description with <i>italic</i> text.")).
				To(Equal("A real album description with <i>italic</i> text."))
		})

		It("preserves paragraph breaks while collapsing in-line whitespace", func() {
			Expect(normalizeText("Para\tone\there.\n\nPara\ttwo\tends.")).
				To(Equal("Para one here.\n\nPara two ends."))
		})

		It("preserves single newlines between lines", func() {
			Expect(normalizeText("Line\tone\nLine\ttwo")).To(Equal("Line one\nLine two"))
		})

		It("normalizes CRLF/CR line endings to LF", func() {
			Expect(normalizeText("one\r\ntwo\rthree")).To(Equal("one\ntwo\nthree"))
		})

		It("reduces a whitespace-only line to a blank paragraph separator", func() {
			Expect(normalizeText("a\n  \t  \nb")).To(Equal("a\n\nb"))
		})

		It("handles empty string", func() {
			Expect(normalizeText("")).To(Equal(""))
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
})

var _ = Describe("throttleError", func() {
	It("asks for the delay iTunes named on a 429", func() {
		err := throttleError(429, map[string]string{"Retry-After": "11"})
		Expect(err).To(MatchError(ContainSubstring("agent(retry_later:11)")))
	})

	It("reads Retry-After regardless of header case", func() {
		err := throttleError(429, map[string]string{"retry-after": "11"})
		Expect(err).To(MatchError(ContainSubstring("agent(retry_later:11)")))
	})

	It("falls back to a default delay when a 429 names none", func() {
		err := throttleError(429, nil)
		Expect(err).To(MatchError(ContainSubstring("agent(retry_later:30)")))
	})

	It("ignores an unparseable Retry-After", func() {
		err := throttleError(429, map[string]string{"Retry-After": "Wed, 21 Oct 2026 07:28:00 GMT"})
		Expect(err).To(MatchError(ContainSubstring("agent(retry_later:30)")))
	})

	It("parks the agent for an hour on a 403 block", func() {
		err := throttleError(403, nil)
		Expect(err).To(MatchError(ContainSubstring("agent(retry_later:3600)")))
	})

	It("returns nil for a successful response", func() {
		Expect(throttleError(200, nil)).To(BeNil())
	})

	It("returns nil for a not-found, which is a real answer", func() {
		Expect(throttleError(404, nil)).To(BeNil())
	})
})

var _ = Describe("httpGetJSON throttling", func() {
	It("surfaces the retry_later token when iTunes throttles", func() {
		host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{
			StatusCode: 429,
			Headers:    map[string]string{"Retry-After": "11"},
		}, nil)

		err := httpGetJSON("https://itunes.apple.com/search?term=x", &struct{}{})
		Expect(err).To(MatchError(ContainSubstring("agent(retry_later:11)")))
	})

	It("surfaces the retry_later token when iTunes blocks us", func() {
		host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 403}, nil)

		err := httpGetJSON("https://itunes.apple.com/search?term=x", &struct{}{})
		Expect(err).To(MatchError(ContainSubstring("agent(retry_later:3600)")))
	})

	It("reports other failures without asking for a delay", func() {
		host.HTTPMock.On("Send", mock.Anything).Return(&host.HTTPResponse{StatusCode: 500}, nil)

		err := httpGetJSON("https://itunes.apple.com/search?term=x", &struct{}{})
		Expect(err).To(MatchError(ContainSubstring("returned status 500")))
		Expect(err.Error()).ToNot(ContainSubstring("retry_later"))
	})
})

var _ = Describe("apple cooldown", func() {
	const pages = "https://music.apple.com/us/artist/-/123"

	It("parks on an iTunes 429 for the delay Apple named", func() {
		host.CacheMock.ExpectedCalls = nil
		host.CacheMock.On("GetInt", cooldownKey).Return(int64(0), false, nil)
		host.CacheMock.On("SetInt", cooldownKey, mock.Anything, int64(11)).Return(nil)
		err := throttleError(429, map[string]string{"Retry-After": "11"})
		Expect(err).To(MatchError(ContainSubstring("agent(retry_later:11)")))
		host.CacheMock.AssertCalled(GinkgoT(), "SetInt", cooldownKey, mock.Anything, int64(11))
	})

	It("parks on an iTunes 403 for an hour", func() {
		host.CacheMock.ExpectedCalls = nil
		host.CacheMock.On("GetInt", cooldownKey).Return(int64(0), false, nil)
		host.CacheMock.On("SetInt", cooldownKey, mock.Anything, int64(3600)).Return(nil)
		err := throttleError(403, nil)
		Expect(err).To(MatchError(ContainSubstring("agent(retry_later:3600)")))
	})

	It("parks on a page 403 too, since missing content there is a 404", func() {
		host.CacheMock.ExpectedCalls = nil
		host.CacheMock.On("GetInt", cooldownKey).Return(int64(0), false, nil)
		host.CacheMock.On("SetInt", cooldownKey, mock.Anything, int64(3600)).Return(nil)
		Expect(throttleError(403, nil)).To(MatchError(ContainSubstring("agent(retry_later:3600)")))
	})

	It("refuses every request while parked, without touching the network", func() {
		host.CacheMock.ExpectedCalls = nil
		host.CacheMock.On("GetInt", cooldownKey).Return(time.Now().Add(90*time.Second).Unix(), true, nil)

		_, _, err := httpGet(pages)
		Expect(err).To(MatchError(ContainSubstring("agent(retry_later:")))
		Expect(host.HTTPMock.Calls).To(BeEmpty(), "parked means no request goes out")
	})

	It("reports the time left, not the original delay", func() {
		host.CacheMock.ExpectedCalls = nil
		host.CacheMock.On("GetInt", cooldownKey).Return(time.Now().Add(42*time.Second).Unix(), true, nil)
		Expect(cooldownRemaining()).To(BeNumerically("~", 42, 1))
	})

	It("is not parked once the deadline has passed", func() {
		host.CacheMock.ExpectedCalls = nil
		host.CacheMock.On("GetInt", cooldownKey).Return(time.Now().Add(-5*time.Second).Unix(), true, nil)
		Expect(cooldownRemaining()).To(BeZero())
	})
})
