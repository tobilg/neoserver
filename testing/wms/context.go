package wms

import (
	"fmt"
	"net/url"
	"sync"

	"github.com/tobilg/neoserver/testing/wms/capabilities"
)

// Layer is an alias for capabilities.Layer for convenience.
type Layer = capabilities.Layer

// TestContext holds shared state for WMS conformance tests.
type TestContext struct {
	// Config holds the test configuration.
	Config Config

	// Client is the WMS HTTP client.
	Client *Client

	// Capabilities is the parsed WMS capabilities document.
	Capabilities *capabilities.Capabilities

	// Layers is the list of named layers from capabilities.
	Layers []*capabilities.Layer

	// LayerByName provides quick layer lookup by name.
	LayerByName map[string]*capabilities.Layer

	mu sync.RWMutex
}

// NewTestContext creates a new test context with the given configuration.
func NewTestContext(cfg Config) *TestContext {
	return &TestContext{
		Config:      cfg,
		Client:      NewClient(cfg.BaseURL, cfg.Timeout),
		LayerByName: make(map[string]*capabilities.Layer),
	}
}

// DiscoverCapabilities fetches and parses the WMS capabilities document.
func (ctx *TestContext) DiscoverCapabilities() error {
	resp, err := ctx.Client.GetCapabilities(nil)
	if err != nil {
		return fmt.Errorf("fetching capabilities: %w", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("GetCapabilities returned status %d", resp.StatusCode)
	}

	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		return fmt.Errorf("parsing capabilities: %w", err)
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.Capabilities = caps
	ctx.Layers = caps.GetNamedLayers()

	// Build layer lookup map
	ctx.LayerByName = make(map[string]*capabilities.Layer)
	for _, layer := range ctx.Layers {
		ctx.LayerByName[layer.Name] = layer
	}

	return nil
}

// GetLayer returns a layer by name.
func (ctx *TestContext) GetLayer(name string) *capabilities.Layer {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.LayerByName[name]
}

// GetFirstNamedLayer returns the first named layer.
func (ctx *TestContext) GetFirstNamedLayer() *capabilities.Layer {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	if len(ctx.Layers) > 0 {
		return ctx.Layers[0]
	}
	return nil
}

// GetFirstQueryableLayer returns the first queryable layer.
func (ctx *TestContext) GetFirstQueryableLayer() *capabilities.Layer {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	for _, layer := range ctx.Layers {
		if layer.IsQueryable() {
			return layer
		}
	}
	return nil
}

// GetQueryableLayers returns all queryable layers.
func (ctx *TestContext) GetQueryableLayers() []*capabilities.Layer {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	var result []*capabilities.Layer
	for _, layer := range ctx.Layers {
		if layer.IsQueryable() {
			result = append(result, layer)
		}
	}
	return result
}

// GetNonQueryableLayer returns a non-queryable layer if one exists.
func (ctx *TestContext) GetNonQueryableLayer() *capabilities.Layer {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	for _, layer := range ctx.Layers {
		if !layer.IsQueryable() {
			return layer
		}
	}
	return nil
}

// GetLayerWithTimeDimension returns a layer with a TIME dimension.
func (ctx *TestContext) GetLayerWithTimeDimension() *capabilities.Layer {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	for _, layer := range ctx.Layers {
		if layer.HasTimeDimension() {
			return layer
		}
	}
	return nil
}

// SupportsGetFeatureInfo returns true if the server supports GetFeatureInfo.
func (ctx *TestContext) SupportsGetFeatureInfo() bool {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	if ctx.Capabilities == nil {
		return false
	}
	return ctx.Capabilities.SupportsGetFeatureInfo()
}

// GetMapFormats returns supported GetMap formats.
func (ctx *TestContext) GetMapFormats() []string {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	if ctx.Capabilities == nil {
		return nil
	}
	return ctx.Capabilities.GetMapFormats()
}

// GetFeatureInfoFormats returns supported GetFeatureInfo formats.
func (ctx *TestContext) GetFeatureInfoFormats() []string {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	if ctx.Capabilities == nil {
		return nil
	}
	return ctx.Capabilities.GetFeatureInfoFormats()
}

// GetExceptionFormats returns supported exception formats.
func (ctx *TestContext) GetExceptionFormats() []string {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	if ctx.Capabilities == nil {
		return nil
	}
	return ctx.Capabilities.GetExceptionFormats()
}

// BuildGetMapParams builds a basic GetMap request for a layer.
func (ctx *TestContext) BuildGetMapParams(layer *capabilities.Layer) url.Values {
	crs := layer.GetFirstCRS()
	if crs == "" {
		crs = CRSCRS84
	}

	// Get bounding box for the CRS
	bbox := "-180,-90,180,90" // default
	if bb := layer.GetBoundingBox(crs); bb != nil {
		bbox = fmt.Sprintf("%f,%f,%f,%f", bb.MinX, bb.MinY, bb.MaxX, bb.MaxY)
	} else if layer.EXGeographicBoundingBox != nil {
		ex := layer.EXGeographicBoundingBox
		bbox = fmt.Sprintf("%f,%f,%f,%f", ex.WestBoundLongitude, ex.SouthBoundLatitude, ex.EastBoundLongitude, ex.NorthBoundLatitude)
	}

	return url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {Version130},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {crs},
		"BBOX":    {bbox},
		"WIDTH":   {fmt.Sprintf("%d", DefaultWidth)},
		"HEIGHT":  {fmt.Sprintf("%d", DefaultHeight)},
		"FORMAT":  {FormatPNG},
		"STYLES":  {""},
	}
}

// BuildGetFeatureInfoParams builds a basic GetFeatureInfo request for a layer.
func (ctx *TestContext) BuildGetFeatureInfoParams(layer *capabilities.Layer) url.Values {
	params := ctx.BuildGetMapParams(layer)
	params.Set("REQUEST", "GetFeatureInfo")
	params.Set("QUERY_LAYERS", layer.Name)
	params.Set("INFO_FORMAT", InfoFormatXML)
	params.Set("I", "128")
	params.Set("J", "128")
	return params
}

// HasLayers returns true if there are any named layers.
func (ctx *TestContext) HasLayers() bool {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return len(ctx.Layers) > 0
}

// LayerCount returns the number of named layers.
func (ctx *TestContext) LayerCount() int {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return len(ctx.Layers)
}
