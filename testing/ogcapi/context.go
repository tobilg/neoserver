package ogcapi

import (
	"fmt"
	"sync"
	"testing"
)

// Collection represents a collection discovered from the API.
type Collection struct {
	ID          string
	Title       string
	Description string
	ItemsURL    string
	Extent      *Extent
	CRS         []string
}

// Extent represents the spatial and temporal extent of a collection.
type Extent struct {
	Spatial  *SpatialExtent
	Temporal *TemporalExtent
}

// SpatialExtent represents the spatial extent as a bounding box.
type SpatialExtent struct {
	BBox [][]float64 // [[minLon, minLat, maxLon, maxLat], ...]
	CRS  string
}

// TemporalExtent represents the temporal extent.
type TemporalExtent struct {
	Interval [][]string // [["start", "end"], ...]
	TRS      string
}

// Link represents a link in OGC API responses.
type Link struct {
	Href  string `json:"href"`
	Rel   string `json:"rel"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
}

// TestContext holds shared state for OGC API conformance tests.
type TestContext struct {
	mu sync.RWMutex

	// Config is the test configuration.
	Config Config

	// Client is the HTTP client for making requests.
	Client *Client

	// ConformanceClasses lists the conformance classes declared by the server.
	ConformanceClasses []string

	// Collections holds discovered collections.
	Collections []Collection

	// CollectionByID maps collection ID to collection.
	CollectionByID map[string]Collection

	// FeatureIDs maps collection ID to a sample feature ID.
	FeatureIDs map[string]string
}

// NewTestContext creates a new test context.
func NewTestContext(cfg Config) *TestContext {
	return &TestContext{
		Config:         cfg,
		Client:         NewClient(cfg.BaseURL, cfg.Timeout, cfg.Verbose),
		CollectionByID: make(map[string]Collection),
		FeatureIDs:     make(map[string]string),
	}
}

// DiscoverCollections fetches and parses collections from the API.
func (ctx *TestContext) DiscoverCollections() error {
	resp, err := ctx.Client.GetJSON("/collections")
	if err != nil {
		return fmt.Errorf("failed to fetch collections: %w", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("collections endpoint returned status %d", resp.StatusCode)
	}

	if resp.JSON == nil {
		return fmt.Errorf("collections response is not valid JSON")
	}

	collectionsData, ok := resp.JSON["collections"].([]any)
	if !ok {
		return fmt.Errorf("collections property is missing or not an array")
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	for _, c := range collectionsData {
		colMap, ok := c.(map[string]any)
		if !ok {
			continue
		}

		col := Collection{
			ID: getString(colMap, "id"),
		}

		if col.ID == "" {
			continue
		}

		col.Title = getString(colMap, "title")
		col.Description = getString(colMap, "description")

		// Parse extent
		if extentData, ok := colMap["extent"].(map[string]any); ok {
			col.Extent = parseExtent(extentData)
		}

		// Parse CRS list
		if crsList, ok := colMap["crs"].([]any); ok {
			for _, crs := range crsList {
				if crsStr, ok := crs.(string); ok {
					col.CRS = append(col.CRS, crsStr)
				}
			}
		}

		// Find items link
		if links, ok := colMap["links"].([]any); ok {
			for _, l := range links {
				if linkMap, ok := l.(map[string]any); ok {
					if getString(linkMap, "rel") == RelItems {
						col.ItemsURL = getString(linkMap, "href")
						break
					}
				}
			}
		}

		ctx.Collections = append(ctx.Collections, col)
		ctx.CollectionByID[col.ID] = col

		// Limit collections if configured
		if ctx.Config.NoOfCollections > 0 && len(ctx.Collections) >= ctx.Config.NoOfCollections {
			break
		}
	}

	return nil
}

// DiscoverConformance fetches and parses conformance classes from the API.
func (ctx *TestContext) DiscoverConformance() error {
	resp, err := ctx.Client.GetJSON("/conformance")
	if err != nil {
		return fmt.Errorf("failed to fetch conformance: %w", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("conformance endpoint returned status %d", resp.StatusCode)
	}

	if resp.JSON == nil {
		return fmt.Errorf("conformance response is not valid JSON")
	}

	conformsTo, ok := resp.JSON["conformsTo"].([]any)
	if !ok {
		return fmt.Errorf("conformsTo property is missing or not an array")
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	for _, c := range conformsTo {
		if classStr, ok := c.(string); ok {
			ctx.ConformanceClasses = append(ctx.ConformanceClasses, classStr)
		}
	}

	return nil
}

// HasConformanceClass checks if the server declares a conformance class.
func (ctx *TestContext) HasConformanceClass(class string) bool {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	for _, c := range ctx.ConformanceClasses {
		if c == class {
			return true
		}
	}
	return false
}

// SetFeatureID stores a feature ID for a collection.
func (ctx *TestContext) SetFeatureID(collectionID, featureID string) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.FeatureIDs[collectionID] = featureID
}

// GetFeatureID retrieves a stored feature ID for a collection.
func (ctx *TestContext) GetFeatureID(collectionID string) (string, bool) {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	id, ok := ctx.FeatureIDs[collectionID]
	return id, ok
}

// GetCollection retrieves a collection by ID.
func (ctx *TestContext) GetCollection(id string) (Collection, bool) {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	col, ok := ctx.CollectionByID[id]
	return col, ok
}

// RequireSetup ensures the context is initialized, failing the test if not.
func (ctx *TestContext) RequireSetup(t *testing.T) {
	t.Helper()
	if ctx.Client == nil {
		t.Fatal("Test context not initialized. Ensure TestMain runs setup.")
	}
	if len(ctx.Collections) == 0 {
		t.Skip("No collections discovered, skipping test")
	}
}

// Helper functions

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func parseExtent(data map[string]any) *Extent {
	ext := &Extent{}

	if spatial, ok := data["spatial"].(map[string]any); ok {
		ext.Spatial = &SpatialExtent{}
		if bbox, ok := spatial["bbox"].([]any); ok {
			for _, b := range bbox {
				if bboxArr, ok := b.([]any); ok {
					var coords []float64
					for _, c := range bboxArr {
						if f, ok := c.(float64); ok {
							coords = append(coords, f)
						}
					}
					if len(coords) >= 4 {
						ext.Spatial.BBox = append(ext.Spatial.BBox, coords)
					}
				}
			}
		}
		ext.Spatial.CRS = getString(spatial, "crs")
	}

	if temporal, ok := data["temporal"].(map[string]any); ok {
		ext.Temporal = &TemporalExtent{}
		if interval, ok := temporal["interval"].([]any); ok {
			for _, i := range interval {
				if intArr, ok := i.([]any); ok {
					var times []string
					for _, t := range intArr {
						if s, ok := t.(string); ok {
							times = append(times, s)
						} else if t == nil {
							times = append(times, "")
						}
					}
					if len(times) >= 2 {
						ext.Temporal.Interval = append(ext.Temporal.Interval, times)
					}
				}
			}
		}
		ext.Temporal.TRS = getString(temporal, "trs")
	}

	return ext
}
