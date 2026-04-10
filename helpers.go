package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/navidrome/navidrome/plugins/pdk/go/host"
	"github.com/navidrome/navidrome/plugins/pdk/go/metadata"
	"github.com/navidrome/navidrome/plugins/pdk/go/pdk"
)

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
	if err != nil {
		pdk.Log(pdk.LogWarn, "KVStore error for key "+key+": "+err.Error())
		return false
	}
	if !exists {
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

// --- HTTP helpers ---

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

// httpGetJSON performs a GET request, checks for 200 status, and unmarshals the JSON response.
func httpGetJSON(rawURL string, target any) error {
	body, statusCode, err := httpGet(rawURL)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	if statusCode != 200 {
		return fmt.Errorf("returned status %d", statusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	return nil
}

// normalizeName normalizes an artist or album name for cache key use.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// --- Image URL rewriting (shared by artist and album image capabilities) ---

// imageURLRegex matches Apple's mzstatic.com image dimension segments like "486x486bb".
var imageURLRegex = regexp.MustCompile(`/\d+x\d+[a-z]*\.`)

// rewriteImageSize rewrites an Apple mzstatic.com image URL to the given size.
func rewriteImageSize(imageURL string, size int) string {
	return imageURLRegex.ReplaceAllString(imageURL, fmt.Sprintf("/%dx%dbb.", size, size))
}

// buildImageList generates ImageInfo entries in multiple sizes from a base artwork URL.
func buildImageList(baseURL string) []metadata.ImageInfo {
	sizes := []int{1500, 600, 300}
	images := make([]metadata.ImageInfo, 0, len(sizes))
	for _, size := range sizes {
		images = append(images, metadata.ImageInfo{
			URL:  rewriteImageSize(baseURL, size),
			Size: int32(size),
		})
	}
	return images
}
