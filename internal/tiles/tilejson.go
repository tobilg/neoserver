package tiles

import (
	"fmt"
	"strings"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

// TileJSON represents a TileJSON 3.0.0 response.
// See https://github.com/mapbox/tilejson-spec/tree/master/3.0.0
type TileJSON struct {
	TileJSON     string        `json:"tilejson"`
	Name         string        `json:"name,omitempty"`
	Description  string        `json:"description,omitempty"`
	Version      string        `json:"version,omitempty"`
	Attribution  string        `json:"attribution,omitempty"`
	Scheme       string        `json:"scheme,omitempty"`
	Tiles        []string      `json:"tiles"`
	Grids        []string      `json:"grids,omitempty"`
	Data         []string      `json:"data,omitempty"`
	MinZoom      int           `json:"minzoom"`
	MaxZoom      int           `json:"maxzoom"`
	Bounds       []float64     `json:"bounds,omitempty"`
	Center       []float64     `json:"center,omitempty"`
	FillZoom     int           `json:"fillzoom,omitempty"`
	Legend       string        `json:"legend,omitempty"`
	Template     string        `json:"template,omitempty"`
	VectorLayers []VectorLayer `json:"vector_layers,omitempty"`
}

// VectorLayer describes a layer in a vector tileset.
type VectorLayer struct {
	ID          string            `json:"id"`
	Description string            `json:"description,omitempty"`
	MinZoom     int               `json:"minzoom,omitempty"`
	MaxZoom     int               `json:"maxzoom,omitempty"`
	Fields      map[string]string `json:"fields"`
}

// TileJSONOptions contains options for generating TileJSON.
type TileJSONOptions struct {
	BaseURL       string
	WorkspaceName string
	CollectionID  string
	TileMatrixSet string
	DataType      string // "vector" or "map"
	MinZoom       int
	MaxZoom       int
	Format        string // "mvt", "png", "jpeg"
	Role          string // caller's workspace role, for per-layer read filtering
}

// GenerateTileJSON generates a TileJSON 3.0.0 response for a collection.
func GenerateTileJSON(layer *workspace.Layer, layerInfo *datasource.LayerInfo, opts TileJSONOptions) *TileJSON {
	// Build tile URL template
	tileURL := buildTileURL(opts)

	// Determine bounds
	var bounds []float64
	var center []float64
	if layerInfo != nil && layerInfo.Extent != nil {
		bounds = []float64{
			layerInfo.Extent.MinX,
			layerInfo.Extent.MinY,
			layerInfo.Extent.MaxX,
			layerInfo.Extent.MaxY,
		}
		centerX := (layerInfo.Extent.MinX + layerInfo.Extent.MaxX) / 2
		centerY := (layerInfo.Extent.MinY + layerInfo.Extent.MaxY) / 2
		center = []float64{centerX, centerY, float64(opts.MinZoom)}
	} else {
		// Default to world bounds
		bounds = []float64{-180, -85.051129, 180, 85.051129}
		center = []float64{0, 0, float64(opts.MinZoom)}
	}

	tj := &TileJSON{
		TileJSON:    "3.0.0",
		Name:        layer.Title,
		Description: layer.Description,
		Version:     "1.0.0",
		Scheme:      "xyz",
		Tiles:       []string{tileURL},
		MinZoom:     opts.MinZoom,
		MaxZoom:     opts.MaxZoom,
		Bounds:      bounds,
		Center:      center,
	}

	// Add vector layer info for MVT tiles
	if opts.DataType == DataTypeVector && layerInfo != nil {
		vl := VectorLayer{
			ID:          opts.CollectionID,
			Description: layer.Description,
			MinZoom:     opts.MinZoom,
			MaxZoom:     opts.MaxZoom,
			Fields:      make(map[string]string),
		}

		// Add field descriptions
		for _, prop := range layerInfo.Properties {
			fieldType := mapJSONTypeToTileJSON(prop.JSONType)
			vl.Fields[prop.Name] = fieldType
		}

		tj.VectorLayers = []VectorLayer{vl}
	}

	return tj
}

// buildTileURL builds the tile URL template.
func buildTileURL(opts TileJSONOptions) string {
	baseURL := strings.TrimRight(opts.BaseURL, "/")

	if opts.DataType == DataTypeVector {
		// Vector tile URL
		return fmt.Sprintf("%s/workspaces/%s/ogc-tiles/collections/%s/tiles/%s/{z}/{y}/{x}",
			baseURL, urlPathEscape(opts.WorkspaceName), urlPathEscape(opts.CollectionID), urlPathEscape(opts.TileMatrixSet))
	}

	// Map tile URL
	formatExt := "png"
	if opts.Format != "" {
		formatExt = opts.Format
	}
	return fmt.Sprintf("%s/workspaces/%s/ogc-tiles/collections/%s/map/tiles/%s/{z}/{y}/{x}?f=%s",
		baseURL, urlPathEscape(opts.WorkspaceName), urlPathEscape(opts.CollectionID), urlPathEscape(opts.TileMatrixSet), formatExt)
}

// mapJSONTypeToTileJSON maps datasource.JSONType to TileJSON field type.
func mapJSONTypeToTileJSON(jt datasource.JSONType) string {
	switch jt {
	case datasource.JSONTypeBoolean:
		return "Boolean"
	case datasource.JSONTypeInteger:
		return "Number"
	case datasource.JSONTypeNumber:
		return "Number"
	case datasource.JSONTypeString:
		return "String"
	default:
		return "String"
	}
}

// TileJSONForWorkspace generates TileJSON including all tile-enabled layers.
func TileJSONForWorkspace(ws *workspace.Workspace, opts TileJSONOptions) *TileJSON {
	baseURL := strings.TrimRight(opts.BaseURL, "/")

	// Build tile URL template for workspace-level tileset
	tileURL := fmt.Sprintf("%s/workspaces/%s/ogc-tiles/tiles/%s/{z}/{y}/{x}",
		baseURL, urlPathEscape(ws.Name), urlPathEscape(opts.TileMatrixSet))

	tj := &TileJSON{
		TileJSON:    "3.0.0",
		Name:        ws.Name,
		Description: ws.Description,
		Version:     "1.0.0",
		Scheme:      "xyz",
		Tiles:       []string{tileURL},
		MinZoom:     opts.MinZoom,
		MaxZoom:     opts.MaxZoom,
		Bounds:      []float64{-180, -85.051129, 180, 85.051129},
		Center:      []float64{0, 0, float64(opts.MinZoom)},
	}

	// Add vector layers for all enabled layers the caller may read
	if opts.DataType == DataTypeVector {
		var vectorLayers []VectorLayer
		for _, layer := range ws.GetAllLayers() {
			if !layer.Enabled || !layer.VisibleToRole(opts.Role) {
				continue
			}
			vl := VectorLayer{
				ID:          layer.PublicID,
				Description: layer.Description,
				MinZoom:     opts.MinZoom,
				MaxZoom:     opts.MaxZoom,
				Fields:      make(map[string]string),
			}
			vectorLayers = append(vectorLayers, vl)
		}
		tj.VectorLayers = vectorLayers
	}

	return tj
}
