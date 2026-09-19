// Package conformance contains the canonical requirement-class identifiers
// advertised by neoserver's OGC services. Protocol handlers select from this
// catalog using their effective workspace settings, so declarations and test
// coverage cannot silently drift apart.
package conformance

import (
	"fmt"
	"sort"
)

// Class describes one advertised OGC requirement class and the automated
// evidence profiles that exercise it. OfficialProfile is reserved for
// unmodified published ETS execution; DerivedProfile identifies transparent
// local compatibility adaptations of a published suite.
type Class struct {
	Key                string
	URI                string
	IntegrationProfile string
	OfficialProfile    string
	DerivedProfile     string
}

const (
	FeaturesCore         = "features-core"
	FeaturesOAS30        = "features-oas30"
	FeaturesGeoJSON      = "features-geojson"
	FeaturesCRS          = "features-crs"
	FeaturesFilter       = "features-filter"
	FeaturesQueryables   = "features-queryables"
	FeaturesFilterAction = "features-features-filter"
	CQL2Basic            = "cql2-basic"
	CQL2Spatial          = "cql2-basic-spatial"
	CQL2Text             = "cql2-text"

	TilesCore         = "tiles-core"
	TilesTileSet      = "tiles-tileset"
	TilesTileSetsList = "tiles-tilesets-list"
	TilesGeoData      = "tiles-geodata-tilesets"
	TilesMVT          = "tiles-mvt"
	TilesPNG          = "tiles-png"
	TilesJPEG         = "tiles-jpeg"

	WCS21Core          = "wcs21-core"
	WCS21KVP           = "wcs21-kvp"
	WCS20Core          = "wcs20-core"
	WCS20KVP           = "wcs20-kvp"
	WCS20GML           = "wcs20-gml"
	WCS20GeoTIFF       = "wcs20-geotiff"
	WCS20Multipart     = "wcs20-multipart"
	WCSXMLPost         = "wcs-xml-post"
	WCSRangeSubsetting = "wcs-range-subsetting"
	WCSScaling         = "wcs-scaling"
	WCSCRS             = "wcs-crs"
	WCSCRSGrid         = "wcs-crs-gridded"
	WCSInterpolation10 = "wcs-interpolation-1.0"
	WCSNearest10       = "wcs-nearest-1.0"
	WCSLinear10        = "wcs-linear-1.0"
	WCSInterpolation11 = "wcs-interpolation-1.1"
	WCSNearest11       = "wcs-nearest-1.1"
	WCSLinear11        = "wcs-linear-1.1"
)

var classes = map[string]Class{
	FeaturesCore:         {Key: FeaturesCore, URI: "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core", IntegrationProfile: "ogcapi-features/core", OfficialProfile: "ogcapi-features10/core"},
	FeaturesOAS30:        {Key: FeaturesOAS30, URI: "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/oas30", IntegrationProfile: "ogcapi-features/core", OfficialProfile: "ogcapi-features10/core"},
	FeaturesGeoJSON:      {Key: FeaturesGeoJSON, URI: "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/geojson", IntegrationProfile: "ogcapi-features/core", OfficialProfile: "ogcapi-features10/core"},
	FeaturesCRS:          {Key: FeaturesCRS, URI: "http://www.opengis.net/spec/ogcapi-features-2/1.0/conf/crs", IntegrationProfile: "ogcapi-features/crs", OfficialProfile: "ogcapi-features10/core"},
	FeaturesFilter:       {Key: FeaturesFilter, URI: "http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/filter", IntegrationProfile: "ogcapi-features/filtering"},
	FeaturesQueryables:   {Key: FeaturesQueryables, URI: "http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/queryables", IntegrationProfile: "ogcapi-features/filtering"},
	FeaturesFilterAction: {Key: FeaturesFilterAction, URI: "http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/features-filter", IntegrationProfile: "ogcapi-features/filtering"},
	CQL2Basic:            {Key: CQL2Basic, URI: "http://www.opengis.net/spec/cql2/1.0/conf/basic-cql2", IntegrationProfile: "ogcapi-features/filtering"},
	CQL2Spatial:          {Key: CQL2Spatial, URI: "http://www.opengis.net/spec/cql2/1.0/conf/basic-spatial-functions", IntegrationProfile: "ogcapi-features/filtering"},
	CQL2Text:             {Key: CQL2Text, URI: "http://www.opengis.net/spec/cql2/1.0/conf/cql2-text", IntegrationProfile: "ogcapi-features/filtering"},

	TilesCore:         {Key: TilesCore, URI: "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/core", IntegrationProfile: "ogcapi-tiles/core", OfficialProfile: "ogcapi-tiles10/core"},
	TilesTileSet:      {Key: TilesTileSet, URI: "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/tileset", IntegrationProfile: "ogcapi-tiles/tilesets", OfficialProfile: "ogcapi-tiles10/core"},
	TilesTileSetsList: {Key: TilesTileSetsList, URI: "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/tilesets-list", IntegrationProfile: "ogcapi-tiles/tilesets", OfficialProfile: "ogcapi-tiles10/core"},
	TilesGeoData:      {Key: TilesGeoData, URI: "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/geodata-tilesets", IntegrationProfile: "ogcapi-tiles/tilesets", OfficialProfile: "ogcapi-tiles10/core"},
	TilesMVT:          {Key: TilesMVT, URI: "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/mvt", IntegrationProfile: "ogcapi-tiles/formats"},
	TilesPNG:          {Key: TilesPNG, URI: "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/png", IntegrationProfile: "ogcapi-tiles/formats"},
	TilesJPEG:         {Key: TilesJPEG, URI: "http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/jpeg", IntegrationProfile: "ogcapi-tiles/formats"},

	WCS21Core:          {Key: WCS21Core, URI: "http://www.opengis.net/spec/WCS/2.1/conf/core", IntegrationProfile: "wcs21/core"},
	WCS21KVP:           {Key: WCS21KVP, URI: "http://www.opengis.net/spec/WCS/2.1/conf/kvp-protocol-binding", IntegrationProfile: "wcs21/kvp"},
	WCS20Core:          {Key: WCS20Core, URI: "http://www.opengis.net/spec/WCS/2.0/conf/core", IntegrationProfile: "wcs20/core", OfficialProfile: "wcs20/core"},
	WCS20KVP:           {Key: WCS20KVP, URI: "http://www.opengis.net/spec/WCS_protocol-binding_get-kvp/1.0/conf/get-kvp", IntegrationProfile: "wcs20/core", OfficialProfile: "wcs20/core"},
	WCS20GML:           {Key: WCS20GML, URI: "http://www.opengis.net/spec/WCS/2.0/conf/gml-coverage", IntegrationProfile: "wcs20/core", OfficialProfile: "wcs20/core"},
	WCS20GeoTIFF:       {Key: WCS20GeoTIFF, URI: "http://www.opengis.net/spec/WCS/2.0/conf/geotiff-coverage", IntegrationProfile: "wcs20/core", OfficialProfile: "wcs20/core"},
	WCS20Multipart:     {Key: WCS20Multipart, URI: "http://www.opengis.net/spec/WCS/2.0/conf/multipart", IntegrationProfile: "wcs20/core", OfficialProfile: "wcs20/core"},
	WCSXMLPost:         {Key: WCSXMLPost, URI: "http://www.opengis.net/spec/WCS_protocol-binding_post-xml/1.0/conf/post-xml", IntegrationProfile: "wcs/extensions", OfficialProfile: "wcs20/post"},
	WCSRangeSubsetting: {Key: WCSRangeSubsetting, URI: "http://www.opengis.net/spec/WCS_service-extension_range-subsetting/1.0/conf/record-subsetting", IntegrationProfile: "wcs/extensions", OfficialProfile: "wcs20/range-subsetting"},
	WCSScaling:         {Key: WCSScaling, URI: "http://www.opengis.net/spec/WCS_service-extension_scaling/1.0/conf/scaling", IntegrationProfile: "wcs/extensions", OfficialProfile: "wcs20/scaling"},
	WCSCRS:             {Key: WCSCRS, URI: "http://www.opengis.net/spec/WCS_service-extension_crs/1.0/conf/crs", IntegrationProfile: "wcs/extensions", OfficialProfile: "wcs20/crs"},
	WCSCRSGrid:         {Key: WCSCRSGrid, URI: "http://www.opengis.net/spec/WCS_service-extension_crs/1.0/conf/crs-gridded-coverage", IntegrationProfile: "wcs/extensions", OfficialProfile: "wcs20/crs"},
	WCSInterpolation10: {Key: WCSInterpolation10, URI: "http://www.opengis.net/spec/WCS_service-extension_interpolation/1.0/conf/interpolation", IntegrationProfile: "wcs/extensions", DerivedProfile: "wcs20/interpolation"},
	WCSNearest10:       {Key: WCSNearest10, URI: "http://www.opengis.net/spec/WCS_service-extension_interpolation/1.0/conf/interpolation-nearest-neighbor", IntegrationProfile: "wcs/extensions", DerivedProfile: "wcs20/interpolation"},
	WCSLinear10:        {Key: WCSLinear10, URI: "http://www.opengis.net/spec/WCS_service-extension_interpolation/1.0/conf/interpolation-linear", IntegrationProfile: "wcs/extensions", DerivedProfile: "wcs20/interpolation"},
	WCSInterpolation11: {Key: WCSInterpolation11, URI: "http://www.opengis.net/spec/WCS_service-extension_interpolation/1.1/conf/interpolation", IntegrationProfile: "wcs21/extensions"},
	WCSNearest11:       {Key: WCSNearest11, URI: "http://www.opengis.net/spec/WCS_service-extension_interpolation/1.1/conf/interpolation-nearest-neighbor", IntegrationProfile: "wcs21/extensions"},
	WCSLinear11:        {Key: WCSLinear11, URI: "http://www.opengis.net/spec/WCS_service-extension_interpolation/1.1/conf/interpolation-linear", IntegrationProfile: "wcs21/extensions"},
}

var OGCAPIFeaturesKeys = []string{
	FeaturesCore, FeaturesOAS30, FeaturesGeoJSON, FeaturesCRS,
	FeaturesFilter, FeaturesQueryables, FeaturesFilterAction,
	CQL2Basic, CQL2Spatial, CQL2Text,
}

// ClassForKey returns a catalog class.
func ClassForKey(key string) (Class, bool) {
	class, ok := classes[key]
	return class, ok
}

// URIs resolves catalog keys to requirement-class URIs. Unknown keys are a
// programmer error and panic so a deployment cannot silently under-declare.
func URIs(keys ...string) []string {
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		class, ok := classes[key]
		if !ok {
			panic(fmt.Sprintf("unknown conformance class %q", key))
		}
		result = append(result, class.URI)
	}
	return result
}

// All returns a stable snapshot of the complete catalog.
func All() []Class {
	result := make([]Class, 0, len(classes))
	for _, class := range classes {
		result = append(result, class)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}
