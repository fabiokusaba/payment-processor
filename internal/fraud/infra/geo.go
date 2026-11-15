package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/huncoding/payment-processor-course/pkg/memcache"
)

// GeoResolver resolves a client IP to an ISO 3166 alpha-2 country code.
type GeoResolver interface {
	CountryCode(ctx context.Context, ip string) (string, error)
}

// StaticGeoResolver responds from a fixed table, acting as the default offline resolver and serving local demos.
type StaticGeoResolver struct{}

// NewStaticGeoResolver creates a new StaticGeoResolver.
func NewStaticGeoResolver() *StaticGeoResolver {
	return &StaticGeoResolver{}
}

// HTTPGeoResolver calls the free ipapi.co endpoint with an in-process cache in front, since the free tier is strictly
// rate limited and IPs repeat frequently.
type HTTPGeoResolver struct {
	client *http.Client
	cache *memcache.Cache[string]
}

// NewHTTPGeoResolver creates an HTTPGeoResolver with a 3-second client timeout and cache with 6-hour TTL and 1-hour 
// janitor sweep interval
func NewHTTPGeoResolver() *HTTPGeoResolver {
	return &HTTPGeoResolver{
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
		cache: memcache.New[string](6 * time.Hour, time.Hour),
	}
}

// CountryCode resolves documentation ranges to fixed countries so demos can trigger the impossible travel signal
// deterministically.
func (r *HTTPGeoResolver) CountryCode(ctx context.Context, ip string) (string, error) {
	if code, ok := r.cache.Get(ip); ok {
		return code, nil
	}

	url := fmt.Sprintf("https://ipapi.co/%s/json/", ip)
	response, err := r.client.Get(url)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	resp, err := r.client.Do(response.Request)
	if err != nil {
		return "", fmt.Errorf("geo: failed to get response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("geo: unexpected status code: %d", resp.StatusCode)
	}

	var body struct {
		CountryCode string `json:"country_code"` 
	}

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("geo: failed to decode response: %w", err)
	}

	r.cache.Set(ip, body.CountryCode)
	return body.CountryCode, nil
}