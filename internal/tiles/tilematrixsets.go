package tiles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// TileMatrixSetID constants for supported tile matrix sets.
const (
	TMSWebMercatorQuad = "WebMercatorQuad"
	TMSWorldCRS84Quad  = "WorldCRS84Quad"
)

var customTileMatrixSets = struct {
	sync.RWMutex
	items map[string]*TileMatrixSetDefinition
}{items: make(map[string]*TileMatrixSetDefinition)}

// ReplaceCustomTileMatrixSets atomically replaces the process-wide custom grid
// registry. Definitions are cloned so management requests cannot mutate grids
// currently used by renderers.
func ReplaceCustomTileMatrixSets(definitions []*TileMatrixSetDefinition) error {
	next := make(map[string]*TileMatrixSetDefinition, len(definitions))
	for _, definition := range definitions {
		if err := ValidateTileMatrixSetDefinition(definition); err != nil {
			return err
		}
		if definition.ID == TMSWebMercatorQuad || definition.ID == TMSWorldCRS84Quad {
			return fmt.Errorf("built-in tile matrix set %q cannot be replaced", definition.ID)
		}
		copy := cloneTileMatrixSet(definition)
		if err := setTileMatrixSetCacheDigest(copy); err != nil {
			return fmt.Errorf("digest tile matrix set %q: %w", copy.ID, err)
		}
		next[copy.ID] = copy
	}
	customTileMatrixSets.Lock()
	customTileMatrixSets.items = next
	customTileMatrixSets.Unlock()
	return nil
}

func setTileMatrixSetCacheDigest(definition *TileMatrixSetDefinition) error {
	canonical, err := json.Marshal(definition)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonical)
	definition.cacheDigest = hex.EncodeToString(digest[:])
	return nil
}

func cloneTileMatrixSet(definition *TileMatrixSetDefinition) *TileMatrixSetDefinition {
	copy := *definition
	copy.OrderedAxes = append([]string(nil), definition.OrderedAxes...)
	copy.TileMatrices = append([]TileMatrix(nil), definition.TileMatrices...)
	for i := range copy.TileMatrices {
		copy.TileMatrices[i].PointOfOrigin = append([]float64(nil), copy.TileMatrices[i].PointOfOrigin...)
	}
	if definition.BoundingBox != nil {
		bbox := *definition.BoundingBox
		bbox.LowerCorner = append([]float64(nil), bbox.LowerCorner...)
		bbox.UpperCorner = append([]float64(nil), bbox.UpperCorner...)
		copy.BoundingBox = &bbox
	}
	return &copy
}

// ValidateTileMatrixSetDefinition validates the bounded custom-grid profile.
// neoserver currently renders only EPSG:3857 and CRS84 grids.
func ValidateTileMatrixSetDefinition(definition *TileMatrixSetDefinition) error {
	if definition == nil || strings.TrimSpace(definition.ID) == "" {
		return errorsNew("tile matrix set id is required")
	}
	if definition.CRS != CRS3857URI && definition.CRS != CRS4326URI {
		return fmt.Errorf("unsupported tile matrix set CRS %q", definition.CRS)
	}
	if len(definition.TileMatrices) == 0 || len(definition.TileMatrices) > 64 {
		return errorsNew("tile matrix set must contain between 1 and 64 matrices")
	}
	seen := make(map[string]bool, len(definition.TileMatrices))
	for _, matrix := range definition.TileMatrices {
		if strings.TrimSpace(matrix.ID) == "" || seen[matrix.ID] {
			return errorsNew("tile matrix ids must be non-empty and unique")
		}
		zoom, err := strconv.Atoi(matrix.ID)
		if err != nil || zoom < 0 || strconv.Itoa(zoom) != matrix.ID {
			return fmt.Errorf("tile matrix id %q must be a canonical non-negative integer", matrix.ID)
		}
		seen[matrix.ID] = true
		if matrix.CellSize <= 0 || math.IsNaN(matrix.CellSize) || math.IsInf(matrix.CellSize, 0) ||
			matrix.ScaleDenominator <= 0 || matrix.TileWidth <= 0 || matrix.TileHeight <= 0 ||
			matrix.MatrixWidth <= 0 || matrix.MatrixHeight <= 0 || len(matrix.PointOfOrigin) != 2 {
			return fmt.Errorf("invalid tile matrix %q", matrix.ID)
		}
		if matrix.CornerOfOrigin != "" && matrix.CornerOfOrigin != "topLeft" {
			return fmt.Errorf("tile matrix %q must use topLeft origin", matrix.ID)
		}
	}
	return nil
}

func errorsNew(message string) error { return fmt.Errorf("%s", message) }

// CRS URIs
const (
	CRS3857URI = "http://www.opengis.net/def/crs/EPSG/0/3857"
	CRS4326URI = "http://www.opengis.net/def/crs/OGC/1.3/CRS84"
)

// TileMatrixSet URIs
const (
	TMSWebMercatorQuadURI = "http://www.opengis.net/def/tilematrixset/OGC/1.0/WebMercatorQuad"
	TMSWorldCRS84QuadURI  = "http://www.opengis.net/def/tilematrixset/OGC/1.0/WorldCRS84Quad"
)

// Constants for WebMercatorQuad
const (
	WebMercatorOriginX     = -20037508.3427892
	WebMercatorOriginY     = 20037508.3427892
	WebMercatorExtent      = 20037508.3427892 * 2
	WebMercatorTileSize    = 256
	WebMercatorScaleDenom0 = 559082264.028717
	WebMercatorCellSize0   = 156543.033928041
)

// Constants for WorldCRS84Quad
const (
	CRS84OriginX     = -180.0
	CRS84OriginY     = 90.0
	CRS84TileSize    = 256
	CRS84ScaleDenom0 = 279541132.014358
	CRS84CellSize0   = 0.703125
)

// GetTileMatrixSetDefinition returns the definition for a tile matrix set.
func GetTileMatrixSetDefinition(id string) (*TileMatrixSetDefinition, error) {
	var definition *TileMatrixSetDefinition
	switch id {
	case TMSWebMercatorQuad:
		definition = getWebMercatorQuad()
	case TMSWorldCRS84Quad:
		definition = getWorldCRS84Quad()
	default:
		customTileMatrixSets.RLock()
		definition = customTileMatrixSets.items[id]
		customTileMatrixSets.RUnlock()
		if definition == nil {
			return nil, fmt.Errorf("unknown tile matrix set: %s", id)
		}
		return cloneTileMatrixSet(definition), nil
	}
	if err := setTileMatrixSetCacheDigest(definition); err != nil {
		return nil, fmt.Errorf("digest tile matrix set %q: %w", id, err)
	}
	return definition, nil
}

// GetSupportedTileMatrixSets returns all supported tile matrix sets.
func GetSupportedTileMatrixSets() []TileMatrixSetItem {
	items := []TileMatrixSetItem{
		{
			ID:    TMSWebMercatorQuad,
			Title: "Google Maps Compatible for the World",
			URI:   TMSWebMercatorQuadURI,
			CRS:   CRS3857URI,
		},
		{
			ID:    TMSWorldCRS84Quad,
			Title: "CRS84 for the World",
			URI:   TMSWorldCRS84QuadURI,
			CRS:   CRS4326URI,
		},
	}
	customTileMatrixSets.RLock()
	for _, definition := range customTileMatrixSets.items {
		items = append(items, TileMatrixSetItem{ID: definition.ID, Title: definition.Title, URI: definition.URI, CRS: definition.CRS})
	}
	customTileMatrixSets.RUnlock()
	sort.Slice(items[2:], func(i, j int) bool { return items[i+2].ID < items[j+2].ID })
	return items
}

// getWebMercatorQuad returns the WebMercatorQuad tile matrix set definition.
func getWebMercatorQuad() *TileMatrixSetDefinition {
	matrices := make([]TileMatrix, 25) // Zoom levels 0-24

	for z := 0; z <= 24; z++ {
		scale := 1 << z // 2^z
		matrices[z] = TileMatrix{
			ID:               fmt.Sprintf("%d", z),
			ScaleDenominator: WebMercatorScaleDenom0 / float64(scale),
			CellSize:         WebMercatorCellSize0 / float64(scale),
			CornerOfOrigin:   "topLeft",
			PointOfOrigin:    []float64{WebMercatorOriginX, WebMercatorOriginY},
			TileWidth:        WebMercatorTileSize,
			TileHeight:       WebMercatorTileSize,
			MatrixWidth:      scale,
			MatrixHeight:     scale,
		}
	}

	return &TileMatrixSetDefinition{
		ID:                TMSWebMercatorQuad,
		Title:             "Google Maps Compatible for the World",
		Description:       "The widely used 'Web Mercator' tile matrix set, compatible with Google Maps, Bing Maps, OpenStreetMap, and many other services.",
		URI:               TMSWebMercatorQuadURI,
		CRS:               CRS3857URI,
		OrderedAxes:       []string{"X", "Y"},
		WellKnownScaleSet: "http://www.opengis.net/def/wkss/OGC/1.0/GoogleMapsCompatible",
		BoundingBox: &BoundingBox{
			CRS:         CRS3857URI,
			LowerCorner: []float64{-20037508.3427892, -20037508.3427892},
			UpperCorner: []float64{20037508.3427892, 20037508.3427892},
		},
		TileMatrices: matrices,
	}
}

// getWorldCRS84Quad returns the WorldCRS84Quad tile matrix set definition.
func getWorldCRS84Quad() *TileMatrixSetDefinition {
	matrices := make([]TileMatrix, 25) // Zoom levels 0-24

	for z := 0; z <= 24; z++ {
		scale := 1 << z    // 2^z
		width := scale * 2 // 2 tiles wide at zoom 0
		height := scale

		matrices[z] = TileMatrix{
			ID:               fmt.Sprintf("%d", z),
			ScaleDenominator: CRS84ScaleDenom0 / float64(scale),
			CellSize:         CRS84CellSize0 / float64(scale),
			CornerOfOrigin:   "topLeft",
			PointOfOrigin:    []float64{CRS84OriginX, CRS84OriginY},
			TileWidth:        CRS84TileSize,
			TileHeight:       CRS84TileSize,
			MatrixWidth:      width,
			MatrixHeight:     height,
		}
	}

	return &TileMatrixSetDefinition{
		ID:                TMSWorldCRS84Quad,
		Title:             "CRS84 for the World",
		Description:       "The tile matrix set for the World using CRS84 (WGS84 geographic coordinate system with longitude/latitude axes).",
		URI:               TMSWorldCRS84QuadURI,
		CRS:               CRS4326URI,
		OrderedAxes:       []string{"Lon", "Lat"},
		WellKnownScaleSet: "http://www.opengis.net/def/wkss/OGC/1.0/GoogleCRS84Quad",
		BoundingBox: &BoundingBox{
			CRS:         CRS4326URI,
			LowerCorner: []float64{-180, -90},
			UpperCorner: []float64{180, 90},
		},
		TileMatrices: matrices,
	}
}

// TileBounds holds the bounding box coordinates for a tile.
type TileBounds struct {
	MinX float64
	MinY float64
	MaxX float64
	MaxY float64
}

// TileBBox calculates the bounding box for a tile in the tile matrix set's native CRS.
func TileBBox(tms string, z, x, y int) (*TileBounds, error) {
	switch tms {
	case TMSWebMercatorQuad:
		return webMercatorTileBBox(z, x, y), nil
	case TMSWorldCRS84Quad:
		return worldCRS84TileBBox(z, x, y), nil
	default:
		definition, err := GetTileMatrixSetDefinition(tms)
		if err != nil {
			return nil, err
		}
		matrix := matrixForZoom(definition, z)
		if matrix == nil {
			return nil, fmt.Errorf("unknown tile matrix %d for %s", z, tms)
		}
		width := float64(matrix.TileWidth) * matrix.CellSize
		height := float64(matrix.TileHeight) * matrix.CellSize
		minX := matrix.PointOfOrigin[0] + float64(x)*width
		maxY := matrix.PointOfOrigin[1] - float64(y)*height
		return &TileBounds{MinX: minX, MinY: maxY - height, MaxX: minX + width, MaxY: maxY}, nil
	}
}

// TileBBoxWGS84 calculates the bounding box for a tile in WGS84 (EPSG:4326).
func TileBBoxWGS84(tms string, z, x, y int) (*TileBounds, error) {
	switch tms {
	case TMSWebMercatorQuad:
		return webMercatorTileBBoxWGS84(z, x, y), nil
	case TMSWorldCRS84Quad:
		return worldCRS84TileBBox(z, x, y), nil // Already in WGS84
	default:
		definition, err := GetTileMatrixSetDefinition(tms)
		if err != nil {
			return nil, err
		}
		if definition.CRS == CRS4326URI {
			return TileBBox(tms, z, x, y)
		}
		if definition.CRS == CRS3857URI {
			bounds, boundsErr := TileBBox(tms, z, x, y)
			if boundsErr != nil {
				return nil, boundsErr
			}
			return mercatorBoundsToWGS84(bounds), nil
		}
		return nil, fmt.Errorf("tile matrix set %s cannot be transformed to WGS84", tms)
	}
}

func matrixForZoom(definition *TileMatrixSetDefinition, z int) *TileMatrix {
	wanted := strconv.Itoa(z)
	for i := range definition.TileMatrices {
		if definition.TileMatrices[i].ID == wanted {
			return &definition.TileMatrices[i]
		}
	}
	return nil
}

func mercatorBoundsToWGS84(bounds *TileBounds) *TileBounds {
	toLon := func(x float64) float64 { return x / WebMercatorOriginX * -180 }
	toLat := func(y float64) float64 {
		return (2*math.Atan(math.Exp(y/6378137)) - math.Pi/2) * 180 / math.Pi
	}
	return &TileBounds{MinX: toLon(bounds.MinX), MinY: toLat(bounds.MinY), MaxX: toLon(bounds.MaxX), MaxY: toLat(bounds.MaxY)}
}

// webMercatorTileBBox calculates the bounding box for a WebMercatorQuad tile.
func webMercatorTileBBox(z, x, y int) *TileBounds {
	n := float64(int(1) << z)
	tileSize := WebMercatorExtent / n

	minX := WebMercatorOriginX + float64(x)*tileSize
	maxX := minX + tileSize
	maxY := WebMercatorOriginY - float64(y)*tileSize
	minY := maxY - tileSize

	return &TileBounds{
		MinX: minX,
		MinY: minY,
		MaxX: maxX,
		MaxY: maxY,
	}
}

// webMercatorTileBBoxWGS84 calculates the WGS84 bounding box for a WebMercatorQuad tile.
func webMercatorTileBBoxWGS84(z, x, y int) *TileBounds {
	n := float64(int(1) << z)

	// Calculate longitude
	minLon := float64(x)/n*360.0 - 180.0
	maxLon := float64(x+1)/n*360.0 - 180.0

	// Calculate latitude (Web Mercator uses inverted Y)
	minLat := math.Atan(math.Sinh(math.Pi*(1-2*float64(y+1)/n))) * 180.0 / math.Pi
	maxLat := math.Atan(math.Sinh(math.Pi*(1-2*float64(y)/n))) * 180.0 / math.Pi

	return &TileBounds{
		MinX: minLon,
		MinY: minLat,
		MaxX: maxLon,
		MaxY: maxLat,
	}
}

// worldCRS84TileBBox calculates the bounding box for a WorldCRS84Quad tile.
func worldCRS84TileBBox(z, x, y int) *TileBounds {
	scale := 1 << z
	tileWidth := 360.0 / float64(scale*2) // 2 tiles wide at zoom 0
	tileHeight := 180.0 / float64(scale)

	minLon := CRS84OriginX + float64(x)*tileWidth
	maxLon := minLon + tileWidth
	maxLat := CRS84OriginY - float64(y)*tileHeight
	minLat := maxLat - tileHeight

	return &TileBounds{
		MinX: minLon,
		MinY: minLat,
		MaxX: maxLon,
		MaxY: maxLat,
	}
}

// ValidateTileCoords validates that tile coordinates are within valid range.
func ValidateTileCoords(tms string, z, x, y int) error {
	if z < 0 || z > 24 {
		return fmt.Errorf("zoom level %d out of range [0, 24]", z)
	}

	switch tms {
	case TMSWebMercatorQuad:
		max := 1 << z
		if x < 0 || x >= max {
			return fmt.Errorf("x coordinate %d out of range [0, %d)", x, max)
		}
		if y < 0 || y >= max {
			return fmt.Errorf("y coordinate %d out of range [0, %d)", y, max)
		}
	case TMSWorldCRS84Quad:
		maxX := (1 << z) * 2
		maxY := 1 << z
		if x < 0 || x >= maxX {
			return fmt.Errorf("x coordinate %d out of range [0, %d)", x, maxX)
		}
		if y < 0 || y >= maxY {
			return fmt.Errorf("y coordinate %d out of range [0, %d)", y, maxY)
		}
	default:
		definition, err := GetTileMatrixSetDefinition(tms)
		if err != nil {
			return err
		}
		matrix := matrixForZoom(definition, z)
		if matrix == nil {
			return fmt.Errorf("tile matrix %d does not exist in %s", z, tms)
		}
		if x < 0 || x >= matrix.MatrixWidth || y < 0 || y >= matrix.MatrixHeight {
			return fmt.Errorf("tile coordinate (%d,%d) is outside matrix %s", x, y, matrix.ID)
		}
	}

	return nil
}

// GetTMSSRID returns the SRID for a tile matrix set.
func GetTMSSRID(tms string) int {
	switch tms {
	case TMSWebMercatorQuad:
		return 3857
	case TMSWorldCRS84Quad:
		return 4326
	default:
		definition, err := GetTileMatrixSetDefinition(tms)
		if err == nil && definition.CRS == CRS3857URI {
			return 3857
		}
		return 4326
	}
}
