package wms

import (
	"bytes"
	"fmt"
	_ "golang.org/x/image/tiff"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Response represents a WMS HTTP response with parsed metadata.
type Response struct {
	// StatusCode is the HTTP status code.
	StatusCode int

	// Headers contains the response headers.
	Headers http.Header

	// Body is the raw response body.
	Body []byte

	// ContentType is the Content-Type header value.
	ContentType string

	// IsImage indicates if the response is an image.
	IsImage bool

	// IsXML indicates if the response is XML (capabilities or exception).
	IsXML bool

	// Image is the decoded image (if IsImage is true).
	Image image.Image

	// ImageFormat is the detected image format (png, jpeg, gif, tiff).
	ImageFormat string

	// RequestTime is how long the request took.
	RequestTime time.Duration
}

// Client is a WMS-specific HTTP client with caching.
type Client struct {
	baseURL    string
	httpClient *http.Client
	cache      map[string]*Response
	mu         sync.RWMutex
}

// NewClient creates a new WMS client.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		cache: make(map[string]*Response),
	}
}

// GetCapabilities sends a GetCapabilities request.
func (c *Client) GetCapabilities(params url.Values) (*Response, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("SERVICE", ServiceWMS)
	params.Set("REQUEST", RequestGetCapabilities)
	if params.Get("VERSION") == "" {
		params.Set("VERSION", Version130)
	}
	return c.doRequest(params)
}

// GetMap sends a GetMap request.
func (c *Client) GetMap(params url.Values) (*Response, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("SERVICE", ServiceWMS)
	params.Set("REQUEST", RequestGetMap)
	if params.Get("VERSION") == "" {
		params.Set("VERSION", Version130)
	}
	return c.doRequest(params)
}

// GetFeatureInfo sends a GetFeatureInfo request.
func (c *Client) GetFeatureInfo(params url.Values) (*Response, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("SERVICE", ServiceWMS)
	params.Set("REQUEST", RequestGetFeatureInfo)
	if params.Get("VERSION") == "" {
		params.Set("VERSION", Version130)
	}
	return c.doRequest(params)
}

// GetLegendGraphic sends a GetLegendGraphic request.
func (c *Client) GetLegendGraphic(params url.Values) (*Response, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("SERVICE", ServiceWMS)
	params.Set("REQUEST", RequestGetLegendGraphic)
	if params.Get("VERSION") == "" {
		params.Set("VERSION", Version130)
	}
	return c.doRequest(params)
}

// Request sends a raw WMS request with the given parameters.
func (c *Client) Request(params url.Values) (*Response, error) {
	return c.doRequest(params)
}

// GetCached returns a cached response for the given cache key, or nil if not found.
func (c *Client) GetCached(key string) *Response {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache[key]
}

// SetCached stores a response in the cache.
func (c *Client) SetCached(key string, resp *Response) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = resp
}

// ClearCache clears the response cache.
func (c *Client) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]*Response)
}

// doRequest performs the HTTP request and parses the response.
func (c *Client) doRequest(params url.Values) (*Response, error) {
	reqURL := c.baseURL
	if len(params) > 0 {
		if strings.Contains(reqURL, "?") {
			reqURL += "&" + params.Encode()
		} else {
			reqURL += "?" + params.Encode()
		}
	}

	start := time.Now()
	httpResp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	resp := &Response{
		StatusCode:  httpResp.StatusCode,
		Headers:     httpResp.Header,
		Body:        body,
		ContentType: httpResp.Header.Get("Content-Type"),
		RequestTime: time.Since(start),
	}

	// Detect response type
	ct := strings.ToLower(resp.ContentType)
	if strings.Contains(ct, "image/") {
		resp.IsImage = true
		// Try to decode the image
		if img, format, err := image.Decode(bytes.NewReader(body)); err == nil {
			resp.Image = img
			resp.ImageFormat = format
		}
	} else if strings.Contains(ct, "xml") || strings.Contains(ct, "text/xml") {
		resp.IsXML = true
	}

	return resp, nil
}

// BaseURL returns the base URL of the WMS server.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// ImageWidth returns the width of the decoded image, or 0 if not an image.
func (r *Response) ImageWidth() int {
	if r.Image == nil {
		return 0
	}
	return r.Image.Bounds().Dx()
}

// ImageHeight returns the height of the decoded image, or 0 if not an image.
func (r *Response) ImageHeight() int {
	if r.Image == nil {
		return 0
	}
	return r.Image.Bounds().Dy()
}

// BodyString returns the response body as a string.
func (r *Response) BodyString() string {
	return string(r.Body)
}
