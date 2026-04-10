package main

import (
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
