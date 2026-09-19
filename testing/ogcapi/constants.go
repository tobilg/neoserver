package ogcapi

// MIME types for OGC API Features.
const (
	// GeoJSONMediaType is the media type for GeoJSON responses.
	GeoJSONMediaType = "application/geo+json"

	// JSONMediaType is the media type for JSON responses.
	JSONMediaType = "application/json"

	// HTMLMediaType is the media type for HTML responses.
	HTMLMediaType = "text/html"

	// OpenAPIMediaType is the media type for OpenAPI documents.
	OpenAPIMediaType = "application/vnd.oai.openapi+json;version=3.0"
)

// CRS URIs for coordinate reference systems.
const (
	// CRS84 is the default CRS for OGC API Features (WGS84 longitude/latitude).
	CRS84 = "http://www.opengis.net/def/crs/OGC/1.3/CRS84"

	// CRS84h is CRS84 with ellipsoidal height.
	CRS84h = "http://www.opengis.net/def/crs/OGC/0/CRS84h"

	// EPSG4326URI is the EPSG:4326 CRS URI.
	EPSG4326URI = "http://www.opengis.net/def/crs/EPSG/0/4326"
)

// Conformance class URIs for OGC API Features Part 1 (Core).
const (
	// ConformanceCore is the Core conformance class.
	ConformanceCore = "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/core"

	// ConformanceOAS30 is the OpenAPI 3.0 conformance class.
	ConformanceOAS30 = "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/oas30"

	// ConformanceGeoJSON is the GeoJSON conformance class.
	ConformanceGeoJSON = "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/geojson"

	// ConformanceHTML is the HTML conformance class.
	ConformanceHTML = "http://www.opengis.net/spec/ogcapi-features-1/1.0/conf/html"
)

// Conformance class URIs for OGC API Features Part 2 (CRS).
const (
	// ConformanceCRS is the CRS conformance class from Part 2.
	ConformanceCRS = "http://www.opengis.net/spec/ogcapi-features-2/1.0/conf/crs"
)

// Conformance class URIs for OGC API Features Part 3 (Filtering).
const (
	// ConformanceFilter is the Filter conformance class from Part 3.
	ConformanceFilter = "http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/filter"
)

// Link relation types used in OGC API Features.
const (
	// RelSelf is the link relation for the current document.
	RelSelf = "self"

	// RelAlternate is the link relation for alternate representations.
	RelAlternate = "alternate"

	// RelServiceDesc is the link relation for the API definition.
	RelServiceDesc = "service-desc"

	// RelServiceDoc is the link relation for API documentation.
	RelServiceDoc = "service-doc"

	// RelConformance is the link relation for the conformance declaration.
	RelConformance = "conformance"

	// RelData is the link relation for the collections (data).
	RelData = "data"

	// RelItems is the link relation for collection items.
	RelItems = "items"

	// RelNext is the link relation for the next page.
	RelNext = "next"

	// RelPrev is the link relation for the previous page.
	RelPrev = "prev"
)

// CRS84 coordinate bounds for validation.
const (
	// CRS84MinLon is the minimum valid longitude for CRS84.
	CRS84MinLon = -180.0

	// CRS84MaxLon is the maximum valid longitude for CRS84.
	CRS84MaxLon = 180.0

	// CRS84MinLat is the minimum valid latitude for CRS84.
	CRS84MinLat = -90.0

	// CRS84MaxLat is the maximum valid latitude for CRS84.
	CRS84MaxLat = 90.0
)

// DefaultFeaturesLimit is the default limit for features when validating responses.
const DefaultFeaturesLimit = 100
