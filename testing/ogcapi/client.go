package ogcapi

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Response represents an HTTP response with parsed JSON.
type Response struct {
	StatusCode  int
	Headers     http.Header
	Body        []byte
	JSON        map[string]any
	RequestTime time.Time
}

// Client is an HTTP client for OGC API Features testing.
type Client struct {
	baseURL    string
	httpClient *http.Client
	verbose    bool

	// Response cache for reuse across related tests
	mu    sync.RWMutex
	cache map[string]*Response
}

// NewClient creates a new OGC API test client.
func NewClient(baseURL string, timeout time.Duration, verbose bool) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		verbose: verbose,
		cache:   make(map[string]*Response),
	}
}

// Get performs a GET request to the specified path with the given Accept header.
func (c *Client) Get(path string, accept string) (*Response, error) {
	return c.GetWithParams(path, nil, accept)
}

// GetWithParams performs a GET request with query parameters.
func (c *Client) GetWithParams(path string, params url.Values, accept string) (*Response, error) {
	return c.GetWithHeaders(path, params, accept, nil)
}

// GetWithHeaders allows tests to distinguish origin generation from cache reuse.
func (c *Client) GetWithHeaders(path string, params url.Values, accept string, headers http.Header) (*Response, error) {
	fullURL := c.baseURL + path
	if params != nil && len(params) > 0 {
		fullURL += "?" + params.Encode()
	}

	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	for name, values := range headers {
		req.Header[name] = append([]string(nil), values...)
	}

	requestTime := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	response := &Response{
		StatusCode:  resp.StatusCode,
		Headers:     resp.Header,
		Body:        body,
		RequestTime: requestTime,
	}

	// Try to parse JSON if content type suggests it
	contentType := resp.Header.Get("Content-Type")
	if isJSONContentType(contentType) {
		var jsonData map[string]any
		if err := json.Unmarshal(body, &jsonData); err == nil {
			response.JSON = jsonData
		}
	}

	return response, nil
}

// GetJSON performs a GET request expecting JSON response.
func (c *Client) GetJSON(path string) (*Response, error) {
	return c.Get(path, JSONMediaType)
}

// GetGeoJSON performs a GET request expecting GeoJSON response.
func (c *Client) GetGeoJSON(path string) (*Response, error) {
	return c.Get(path, GeoJSONMediaType)
}

// GetCached returns a cached response or fetches and caches it.
func (c *Client) GetCached(cacheKey, path, accept string) (*Response, error) {
	c.mu.RLock()
	if cached, ok := c.cache[cacheKey]; ok {
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	resp, err := c.Get(path, accept)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[cacheKey] = resp
	c.mu.Unlock()

	return resp, nil
}

// GetCachedWithParams returns a cached response or fetches with params and caches it.
func (c *Client) GetCachedWithParams(cacheKey, path string, params url.Values, accept string) (*Response, error) {
	c.mu.RLock()
	if cached, ok := c.cache[cacheKey]; ok {
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	resp, err := c.GetWithParams(path, params, accept)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[cacheKey] = resp
	c.mu.Unlock()

	return resp, nil
}

// CacheResponse stores a response in the cache.
func (c *Client) CacheResponse(key string, resp *Response) {
	c.mu.Lock()
	c.cache[key] = resp
	c.mu.Unlock()
}

// GetFromCache retrieves a response from the cache.
func (c *Client) GetFromCache(key string) (*Response, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	resp, ok := c.cache[key]
	return resp, ok
}

// ClearCache clears the response cache.
func (c *Client) ClearCache() {
	c.mu.Lock()
	c.cache = make(map[string]*Response)
	c.mu.Unlock()
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// isJSONContentType checks if a content type indicates JSON.
func isJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return mediaType == JSONMediaType || mediaType == GeoJSONMediaType || strings.HasSuffix(mediaType, "+json")
}
