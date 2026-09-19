package cache

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strconv"
)

// scopedKey uses a versioned, unambiguous scope and a length-prefixed tuple
// encoding. Hashing delimiter-joined strings would not remove their ambiguity.
// Scope remains a separate field for workspace invalidation.
func scopedKey(kind, workspace string, fields ...string) string {
	h := sha256.New()
	for _, field := range fields {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(field)))
		h.Write(size[:])
		h.Write([]byte(field))
	}
	return "v2:" + base64.RawURLEncoding.EncodeToString([]byte(workspace)) + ":" + kind + ":" + hex.EncodeToString(h.Sum(nil))
}

func CapabilitiesKey(workspace, service string) string {
	return scopedKey("capabilities", workspace, service)
}
func WMSCapabilitiesKey(workspace string) string { return CapabilitiesKey(workspace, "wms") }
func WFSCapabilitiesKey(workspace string) string { return CapabilitiesKey(workspace, "wfs") }
func WCSCapabilitiesKey(workspace string) string { return CapabilitiesKey(workspace, "wcs") }
func CollectionsKey(workspace string) string     { return scopedKey("collections", workspace) }
func ConformanceKey(workspace string) string     { return scopedKey("conformance", workspace) }
func CollectionKey(workspace, collectionID string) string {
	return scopedKey("collection", workspace, collectionID)
}

type FeaturesParams struct {
	Workspace  string
	Collection string
	Limit      int
	Offset     int
	BBox       string
	BBoxCRS    string
	Filter     string
	FilterLang string
	FilterCRS  string
	DateTime   string
	CRS        string
	SortBy     string
	Properties string
}

func FeaturesKey(params FeaturesParams) string {
	return scopedKey("features", params.Workspace, params.Collection, strconv.Itoa(params.Limit), strconv.Itoa(params.Offset), params.BBox, params.BBoxCRS, params.Filter, params.FilterLang, params.FilterCRS, params.DateTime, params.CRS, params.SortBy, params.Properties)
}

func SingleFeatureKey(workspace, collection, featureID, crs string) string {
	return scopedKey("feature", workspace, collection, featureID, crs)
}

type TileParams struct {
	Workspace   string
	Layers      string
	CRS         string
	BBox        string
	Width       int
	Height      int
	Format      string
	Styles      string
	Transparent string
	BgColor     string
	Time        string
	Elevation   string
}

func TileKey(params TileParams) string {
	return scopedKey("tiles", params.Workspace, params.Layers, params.CRS, params.BBox, strconv.Itoa(params.Width), strconv.Itoa(params.Height), params.Format, params.Styles, params.Transparent, params.BgColor, params.Time, params.Elevation)
}

type WFSGetFeatureParams struct {
	Workspace    string
	TypeNames    string
	Count        int
	StartIndex   int
	BBox         string
	SRSName      string
	Filter       string
	SortBy       string
	PropertyName string
	ResourceID   string
	OutputFormat string
	ResultType   string
}

func WFSGetFeatureKey(params WFSGetFeatureParams) string {
	return scopedKey("wfs-features", params.Workspace, params.TypeNames, strconv.Itoa(params.Count), strconv.Itoa(params.StartIndex), params.BBox, params.SRSName, params.Filter, params.SortBy, params.PropertyName, params.ResourceID, params.OutputFormat, params.ResultType)
}

func MVTTileKey(workspace, collection, tileMatrixSet string, z, x, y int) string {
	return scopedKey("mvt", workspace, collection, tileMatrixSet, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y))
}

func MapTileKey(workspace, collection, tileMatrixSet string, z, x, y int, style, format string) string {
	return scopedKey("maptile", workspace, collection, tileMatrixSet, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y), style, format)
}
