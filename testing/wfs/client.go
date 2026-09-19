package wfs

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Response represents a WFS HTTP response with parsed metadata.
type Response struct {
	// StatusCode is the HTTP status code.
	StatusCode int

	// Headers contains the response headers.
	Headers http.Header

	// Body is the raw response body.
	Body []byte

	// ContentType is the Content-Type header value.
	ContentType string

	// IsXML indicates if the response is XML.
	IsXML bool

	// IsGML indicates if the response is GML.
	IsGML bool

	// IsJSON indicates if the response is JSON.
	IsJSON bool

	// RequestTime is how long the request took.
	RequestTime time.Duration
}

// Client is a WFS-specific HTTP client with caching.
type Client struct {
	baseURL    string
	httpClient *http.Client
	cache      map[string]*Response
	mu         sync.RWMutex
	Timeout    time.Duration
}

// NewClient creates a new WFS client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cache:   make(map[string]*Response),
		Timeout: 30 * time.Second,
	}
}

// Get sends a raw GET request with the given parameters.
func (c *Client) Get(params url.Values) (*Response, error) {
	return c.doRequest("GET", params, nil)
}

// GetCapabilities sends a GetCapabilities request.
func (c *Client) GetCapabilities(params url.Values) (*Response, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("SERVICE", ServiceWFS)
	params.Set("REQUEST", RequestGetCapabilities)
	if params.Get("VERSION") == "" {
		params.Set("VERSION", Version200)
	}
	return c.doRequest("GET", params, nil)
}

// DescribeFeatureType sends a DescribeFeatureType request.
func (c *Client) DescribeFeatureType(params url.Values) (*Response, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("SERVICE", ServiceWFS)
	params.Set("REQUEST", RequestDescribeFeatureType)
	if params.Get("VERSION") == "" {
		params.Set("VERSION", Version200)
	}
	return c.doRequest("GET", params, nil)
}

// GetFeature sends a GetFeature request.
func (c *Client) GetFeature(params url.Values) (*Response, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("SERVICE", ServiceWFS)
	params.Set("REQUEST", RequestGetFeature)
	if params.Get("VERSION") == "" {
		params.Set("VERSION", Version200)
	}
	return c.doRequest("GET", params, nil)
}

// GetFeaturePost sends a GetFeature request using POST with XML body.
func (c *Client) GetFeaturePost(xmlBody []byte) (*Response, error) {
	return c.doRequest("POST", nil, xmlBody)
}

// GetPropertyValue sends a GetPropertyValue request.
func (c *Client) GetPropertyValue(params url.Values) (*Response, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("SERVICE", ServiceWFS)
	params.Set("REQUEST", RequestGetPropertyValue)
	if params.Get("VERSION") == "" {
		params.Set("VERSION", Version200)
	}
	return c.doRequest("GET", params, nil)
}

// ListStoredQueries sends a ListStoredQueries request.
func (c *Client) ListStoredQueries(params url.Values) (*Response, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("SERVICE", ServiceWFS)
	params.Set("REQUEST", RequestListStoredQueries)
	if params.Get("VERSION") == "" {
		params.Set("VERSION", Version200)
	}
	return c.doRequest("GET", params, nil)
}

// DescribeStoredQueries sends a DescribeStoredQueries request.
func (c *Client) DescribeStoredQueries(params url.Values) (*Response, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("SERVICE", ServiceWFS)
	params.Set("REQUEST", RequestDescribeStoredQueries)
	if params.Get("VERSION") == "" {
		params.Set("VERSION", Version200)
	}
	return c.doRequest("GET", params, nil)
}

// Request sends a raw WFS request with the given parameters.
func (c *Client) Request(params url.Values) (*Response, error) {
	return c.doRequest("GET", params, nil)
}

// RequestPost sends a raw WFS POST request with XML body.
func (c *Client) RequestPost(xmlBody []byte) (*Response, error) {
	return c.doRequest("POST", nil, xmlBody)
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
func (c *Client) doRequest(method string, params url.Values, body []byte) (*Response, error) {
	reqURL := c.baseURL
	if len(params) > 0 {
		if strings.Contains(reqURL, "?") {
			reqURL += "&" + params.Encode()
		} else {
			reqURL += "?" + params.Encode()
		}
	}

	var req *http.Request
	var err error

	if method == "POST" && body != nil {
		req, err = http.NewRequest("POST", reqURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("creating POST request: %w", err)
		}
		req.Header.Set("Content-Type", "application/xml")
	} else {
		req, err = http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating GET request: %w", err)
		}
	}

	start := time.Now()
	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	resp := &Response{
		StatusCode:  httpResp.StatusCode,
		Headers:     httpResp.Header,
		Body:        respBody,
		ContentType: httpResp.Header.Get("Content-Type"),
		RequestTime: time.Since(start),
	}

	// Detect response type
	ct := strings.ToLower(resp.ContentType)
	if strings.Contains(ct, "xml") || strings.Contains(ct, "gml") {
		resp.IsXML = true
		if strings.Contains(ct, "gml") {
			resp.IsGML = true
		}
	}
	if strings.Contains(ct, "json") {
		resp.IsJSON = true
	}

	return resp, nil
}

// BaseURL returns the base URL of the WFS server.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// BodyString returns the response body as a string.
func (r *Response) BodyString() string {
	return string(r.Body)
}
