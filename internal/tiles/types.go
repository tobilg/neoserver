package tiles

// Link represents an OGC API link.
type Link struct {
	Href     string `json:"href"`
	Rel      string `json:"rel,omitempty"`
	Type     string `json:"type,omitempty"`
	Title    string `json:"title,omitempty"`
	Hreflang string `json:"hreflang,omitempty"`
}

// LandingPage represents the OGC API Tiles landing page response.
type LandingPage struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Links       []Link `json:"links"`
}

// Conformance represents the conformance declaration response.
type Conformance struct {
	ConformsTo []string `json:"conformsTo"`
}

// TileMatrixSetItem represents a tile matrix set in the list response.
type TileMatrixSetItem struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	URI   string `json:"uri,omitempty"`
	CRS   string `json:"crs,omitempty"`
	Links []Link `json:"links,omitempty"`
}

// TileMatrixSetsResponse represents the list of tile matrix sets.
type TileMatrixSetsResponse struct {
	TileMatrixSets []TileMatrixSetItem `json:"tileMatrixSets"`
}

// TileMatrix represents a single tile matrix (zoom level).
type TileMatrix struct {
	ID               string    `json:"id"`
	ScaleDenominator float64   `json:"scaleDenominator"`
	CellSize         float64   `json:"cellSize"`
	CornerOfOrigin   string    `json:"cornerOfOrigin,omitempty"`
	PointOfOrigin    []float64 `json:"pointOfOrigin"`
	TileWidth        int       `json:"tileWidth"`
	TileHeight       int       `json:"tileHeight"`
	MatrixWidth      int       `json:"matrixWidth"`
	MatrixHeight     int       `json:"matrixHeight"`
}

// TileMatrixSetDefinition represents a complete TileMatrixSet definition.
type TileMatrixSetDefinition struct {
	ID                string       `json:"id"`
	Title             string       `json:"title,omitempty"`
	Description       string       `json:"description,omitempty"`
	URI               string       `json:"uri,omitempty"`
	CRS               string       `json:"crs"`
	OrderedAxes       []string     `json:"orderedAxes,omitempty"`
	WellKnownScaleSet string       `json:"wellKnownScaleSet,omitempty"`
	BoundingBox       *BoundingBox `json:"boundingBox,omitempty"`
	TileMatrices      []TileMatrix `json:"tileMatrices"`
	cacheDigest       string
}

// BoundingBox represents a bounding box in the tile matrix set.
type BoundingBox struct {
	CRS         string    `json:"crs,omitempty"`
	LowerCorner []float64 `json:"lowerCorner"`
	UpperCorner []float64 `json:"upperCorner"`
}

// Collection represents a tile-enabled collection.
type Collection struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	CRS         []string `json:"crs,omitempty"`
	Links       []Link   `json:"links"`
}

// CollectionsResponse represents the list of collections.
type CollectionsResponse struct {
	Collections []Collection `json:"collections"`
	Links       []Link       `json:"links,omitempty"`
}

// TileSetMetadata represents metadata for a tileset.
type TileSetMetadata struct {
	Title           string   `json:"title,omitempty"`
	Description     string   `json:"description,omitempty"`
	DataType        string   `json:"dataType"`
	TileMatrixSetID string   `json:"tileMatrixSetId"`
	CRS             string   `json:"crs"`
	Epoch           int      `json:"epoch,omitempty"`
	Layers          []Layer  `json:"layers,omitempty"`
	BoundingBox     *GeoBBox `json:"boundingBox,omitempty"`
	CenterPoint     *Point   `json:"centerPoint,omitempty"`
	Links           []Link   `json:"links"`
}

// Layer represents a layer in the tileset.
type Layer struct {
	ID           string `json:"id"`
	Title        string `json:"title,omitempty"`
	Description  string `json:"description,omitempty"`
	DataType     string `json:"dataType,omitempty"`
	GeometryType string `json:"geometryType,omitempty"`
	MinZoom      int    `json:"minZoom,omitempty"`
	MaxZoom      int    `json:"maxZoom,omitempty"`
}

// GeoBBox represents a geographic bounding box.
type GeoBBox struct {
	LowerLeft  []float64 `json:"lowerLeft"`
	UpperRight []float64 `json:"upperRight"`
	CRS        string    `json:"crs,omitempty"`
}

// Point represents a geographic point.
type Point struct {
	Coordinates []float64 `json:"coordinates"`
	CRS         string    `json:"crs,omitempty"`
}

// TileSetList represents a list of tilesets.
type TileSetList struct {
	TileSets []TileSetMetadata `json:"tilesets"`
	Links    []Link            `json:"links,omitempty"`
}

// TileMatrixSetLimits defines the limits of tile matrix for a tileset.
type TileMatrixSetLimits struct {
	TileMatrixID string `json:"tileMatrix"`
	MinTileRow   int    `json:"minTileRow"`
	MaxTileRow   int    `json:"maxTileRow"`
	MinTileCol   int    `json:"minTileCol"`
	MaxTileCol   int    `json:"maxTileCol"`
}

// ContentMediaType constants
const (
	MediaTypeMVT     = "application/vnd.mapbox-vector-tile"
	MediaTypePNG     = "image/png"
	MediaTypeJPEG    = "image/jpeg"
	MediaTypeWEBP    = "image/webp"
	MediaTypeJSON    = "application/json"
	MediaTypeGeoJSON = "application/geo+json"
	MediaTypeOpenAPI = "application/vnd.oai.openapi+json;version=3.0"
)

// DataType constants
const (
	DataTypeVector = "vector"
	DataTypeMap    = "map"
)
