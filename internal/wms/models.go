// Package wms implements an OGC WMS 1.3.0 server.
package wms

import (
	"fmt"
	"image/color"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tobilg/neoserver/internal/crs"
	"github.com/tobilg/neoserver/internal/query"
)

// normalizeQuery returns a copy of the query values with all parameter names uppercased.
// WMS 1.3.0 requires parameter names to be case-insensitive.
func normalizeQuery(r *http.Request) url.Values {
	q := make(url.Values)
	for k, v := range r.URL.Query() {
		q[strings.ToUpper(k)] = v
	}
	return q
}

// Request types
const (
	RequestGetCapabilities  = "GetCapabilities"
	RequestGetMap           = "GetMap"
	RequestGetFeatureInfo   = "GetFeatureInfo"
	RequestGetLegendGraphic = "GetLegendGraphic"
	RequestDescribeLayer    = "DescribeLayer"
)

// WMS version
const (
	Version130 = "1.3.0"
)

// Output formats
const (
	FormatPNG     = "image/png"
	FormatPNG8    = "image/png8"
	FormatJPEG    = "image/jpeg"
	FormatGIF     = "image/gif"
	FormatTIFF    = "image/tiff"
	FormatTIFF8   = "image/tiff8"
	FormatGeoTIFF = "image/geotiff"
	FormatSVG     = "image/svg+xml"
	FormatPDF     = "application/pdf"
	FormatKML     = "application/vnd.google-earth.kml+xml"
	FormatKMZ     = "application/vnd.google-earth.kmz"
	FormatMapML   = "text/mapml"
	FormatUTFGrid = "application/json;type=utfgrid"
)

// Info formats for GetFeatureInfo
const (
	InfoFormatXML  = "text/xml"
	InfoFormatJSON = "application/json"
	InfoFormatHTML = "text/html"
	InfoFormatText = "text/plain"
)

// Exception format constants
const (
	ExceptionsXML     = "XML"
	ExceptionsINIMAGE = "INIMAGE"
	ExceptionsBLANK   = "BLANK"
)

// GetMapRequest represents a parsed GetMap request.
type GetMapRequest struct {
	Version        string
	Layers         []string
	Styles         []string
	CRS            string
	SRID           int
	BBox           query.BBox
	Width          int
	Height         int
	Format         string
	Transparent    bool
	BgColor        color.RGBA
	Exceptions     string // Exception format: XML, INIMAGE, or BLANK
	SLD            string // URL to external SLD
	SLDBody        string // Inline SLD content
	Time           string // TIME dimension parameter (ISO 8601)
	Elevation      string // ELEVATION dimension parameter
	Environment    map[string]string
	EnvironmentKey string // Canonical form used by response caches.
}

// GetFeatureInfoRequest represents a parsed GetFeatureInfo request.
type GetFeatureInfoRequest struct {
	GetMapRequest
	QueryLayers  []string
	InfoFormat   string
	I            int // Pixel X coordinate
	J            int // Pixel Y coordinate
	FeatureCount int
}

// GetLegendGraphicRequest represents a parsed GetLegendGraphic request.
type GetLegendGraphicRequest struct {
	Layer       string
	Style       string
	Format      string
	Width       int
	Height      int
	SLD         string
	SLDBody     string
	Environment map[string]string
}

var environmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// ParseGetMapRequest parses a GetMap request from query parameters.
func ParseGetMapRequest(r *http.Request, maxWidth, maxHeight int) (*GetMapRequest, error) {
	return parseGetMapRequestWithLimits(r, maxWidth, maxHeight, 32, 256)
}

func parseGetMapRequestWithLimits(r *http.Request, maxWidth, maxHeight, maxEnvVariables, maxEnvValueBytes int) (*GetMapRequest, error) {
	q := normalizeQuery(r)

	req := &GetMapRequest{
		Version:     q.Get("VERSION"),
		Format:      strings.ToLower(q.Get("FORMAT")),
		Transparent: strings.ToLower(q.Get("TRANSPARENT")) == "true",
		SLD:         q.Get("SLD"),
		SLDBody:     q.Get("SLD_BODY"),
	}
	if definition, ok := lookupGetMapFormat(req.Format); ok {
		req.Format = definition.Token
	}

	// VERSION is required for GetMap (per WMS 1.3.0 spec)
	if req.Version == "" {
		return nil, &RequestError{Code: ExceptionMissingParameterValue, Message: "VERSION parameter is required"}
	}

	// Parse layers (LAYERS parameter is required but may be empty for testing purposes)
	layers := q.Get("LAYERS")
	// Check if LAYERS parameter exists in the query (even if empty)
	if _, hasLayers := q["LAYERS"]; !hasLayers {
		return nil, &RequestError{Code: ExceptionMissingParameterValue, Message: "LAYERS parameter is required"}
	}
	if layers != "" {
		req.Layers = strings.Split(layers, ",")
	}

	// Parse styles (STYLES parameter must be present per WMS 1.3.0, but may be empty for default styles)
	if _, hasStyles := q["STYLES"]; !hasStyles {
		return nil, &RequestError{Code: ExceptionMissingParameterValue, Message: "STYLES parameter is required"}
	}
	styles := q.Get("STYLES")
	if styles != "" {
		req.Styles = strings.Split(styles, ",")
	}

	// Parse CRS (WMS 1.3.0) or SRS (WMS 1.1.1)
	crs := q.Get("CRS")
	if crs == "" {
		crs = q.Get("SRS") // Fallback for WMS 1.1.1
	}
	if crs == "" {
		return nil, &RequestError{Code: ExceptionMissingParameterValue, Message: "CRS parameter is required"}
	}
	req.CRS = crs

	// Parse SRID from CRS
	srid, err := parseCRS(crs)
	if err != nil {
		return nil, &RequestError{Code: "InvalidCRS", Message: err.Error()}
	}
	req.SRID = srid

	// Parse BBOX
	bboxStr := q.Get("BBOX")
	if bboxStr == "" {
		return nil, &RequestError{Code: ExceptionMissingParameterValue, Message: "BBOX parameter is required"}
	}
	bbox, err := parseBBox(bboxStr, crs)
	if err != nil {
		return nil, &RequestError{Code: "InvalidBBox", Message: err.Error()}
	}
	req.BBox = bbox

	// Parse WIDTH
	widthStr := q.Get("WIDTH")
	if widthStr == "" {
		return nil, &RequestError{Code: ExceptionMissingParameterValue, Message: "WIDTH parameter is required"}
	}
	width, err := strconv.Atoi(widthStr)
	if err != nil || width <= 0 {
		return nil, &RequestError{Code: "InvalidWidth", Message: "WIDTH must be a positive integer"}
	}
	if width > maxWidth {
		return nil, &RequestError{Code: "InvalidWidth", Message: fmt.Sprintf("WIDTH exceeds maximum of %d", maxWidth)}
	}
	req.Width = width

	// Parse HEIGHT
	heightStr := q.Get("HEIGHT")
	if heightStr == "" {
		return nil, &RequestError{Code: ExceptionMissingParameterValue, Message: "HEIGHT parameter is required"}
	}
	height, err := strconv.Atoi(heightStr)
	if err != nil || height <= 0 {
		return nil, &RequestError{Code: "InvalidHeight", Message: "HEIGHT must be a positive integer"}
	}
	if height > maxHeight {
		return nil, &RequestError{Code: "InvalidHeight", Message: fmt.Sprintf("HEIGHT exceeds maximum of %d", maxHeight)}
	}
	req.Height = height

	// Parse FORMAT
	if req.Format == "" {
		return nil, &RequestError{Code: ExceptionMissingParameterValue, Message: "FORMAT parameter is required"}
	}

	// Parse BGCOLOR
	bgColor := q.Get("BGCOLOR")
	if bgColor != "" {
		req.BgColor = parseHexColor(bgColor)
	} else {
		req.BgColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}

	// Parse EXCEPTIONS (default to XML)
	exceptions := strings.ToUpper(q.Get("EXCEPTIONS"))
	if exceptions == "" {
		req.Exceptions = ExceptionsXML
	} else {
		switch exceptions {
		case ExceptionsXML, ExceptionsINIMAGE, ExceptionsBLANK:
			req.Exceptions = exceptions
		default:
			req.Exceptions = ExceptionsXML
		}
	}

	// Parse dimensions and the optional, extension-gated rendering environment.
	req.Time = q.Get("TIME")
	req.Elevation = q.Get("ELEVATION")
	req.Environment, req.EnvironmentKey, err = parseRenderingEnvironment(q.Get("ENV"), maxEnvVariables, maxEnvValueBytes)
	if err != nil {
		return nil, &RequestError{Code: ExceptionInvalidParameterValue, Message: err.Error()}
	}

	return req, nil
}

// ParseGetFeatureInfoRequest parses a GetFeatureInfo request.
func ParseGetFeatureInfoRequest(r *http.Request, maxWidth, maxHeight int) (*GetFeatureInfoRequest, error) {
	return parseGetFeatureInfoRequestWithLimits(r, maxWidth, maxHeight, 32, 256)
}

func parseGetFeatureInfoRequestWithLimits(r *http.Request, maxWidth, maxHeight, maxEnvVariables, maxEnvValueBytes int) (*GetFeatureInfoRequest, error) {
	// First parse the base GetMap parameters
	getMapReq, err := parseGetMapRequestWithLimits(r, maxWidth, maxHeight, maxEnvVariables, maxEnvValueBytes)
	if err != nil {
		return nil, err
	}

	q := normalizeQuery(r)

	req := &GetFeatureInfoRequest{
		GetMapRequest: *getMapReq,
		InfoFormat:    q.Get("INFO_FORMAT"),
		FeatureCount:  10, // Default
	}

	// Parse QUERY_LAYERS
	// QUERY_LAYERS parameter must exist but may be empty (returns empty result)
	if _, hasQueryLayers := q["QUERY_LAYERS"]; !hasQueryLayers {
		return nil, &RequestError{Code: ExceptionMissingParameterValue, Message: "QUERY_LAYERS parameter is required"}
	}
	queryLayers := q.Get("QUERY_LAYERS")
	if queryLayers != "" {
		req.QueryLayers = strings.Split(queryLayers, ",")
	}

	// Parse I (X pixel coordinate in WMS 1.3.0)
	iStr := q.Get("I")
	if iStr == "" {
		iStr = q.Get("X") // Fallback for WMS 1.1.1
	}
	if iStr == "" {
		return nil, &RequestError{Code: ExceptionMissingParameterValue, Message: "I parameter is required"}
	}
	i, err := strconv.Atoi(iStr)
	if err != nil || i < 0 || i >= req.Width {
		return nil, &RequestError{Code: "InvalidPoint", Message: "I must be a valid pixel coordinate"}
	}
	req.I = i

	// Parse J (Y pixel coordinate in WMS 1.3.0)
	jStr := q.Get("J")
	if jStr == "" {
		jStr = q.Get("Y") // Fallback for WMS 1.1.1
	}
	if jStr == "" {
		return nil, &RequestError{Code: ExceptionMissingParameterValue, Message: "J parameter is required"}
	}
	j, err := strconv.Atoi(jStr)
	if err != nil || j < 0 || j >= req.Height {
		return nil, &RequestError{Code: "InvalidPoint", Message: "J must be a valid pixel coordinate"}
	}
	req.J = j

	// Parse FEATURE_COUNT
	featureCountStr := q.Get("FEATURE_COUNT")
	if featureCountStr != "" {
		if fc, err := strconv.Atoi(featureCountStr); err == nil && fc > 0 {
			req.FeatureCount = fc
		}
	}

	// Default INFO_FORMAT
	if req.InfoFormat == "" {
		req.InfoFormat = InfoFormatXML
	} else {
		// Validate INFO_FORMAT against supported formats
		switch req.InfoFormat {
		case InfoFormatXML, InfoFormatJSON, InfoFormatHTML, InfoFormatText:
			// Valid format
		default:
			return nil, &RequestError{Code: ExceptionInvalidFormat, Message: fmt.Sprintf("Invalid INFO_FORMAT: %s", req.InfoFormat)}
		}
	}

	return req, nil
}

// ParseGetLegendGraphicRequest parses a GetLegendGraphic request.
func ParseGetLegendGraphicRequest(r *http.Request) (*GetLegendGraphicRequest, error) {
	return ParseGetLegendGraphicRequestWithLimits(r, 4096, 4096, 4096*4096)
}

func ParseGetLegendGraphicRequestWithLimits(r *http.Request, maxWidth, maxHeight, maxPixels int) (*GetLegendGraphicRequest, error) {
	return parseGetLegendGraphicRequestWithLimits(r, maxWidth, maxHeight, maxPixels, 32, 256)
}

func parseGetLegendGraphicRequestWithLimits(r *http.Request, maxWidth, maxHeight, maxPixels, maxEnvVariables, maxEnvValueBytes int) (*GetLegendGraphicRequest, error) {
	q := normalizeQuery(r)

	req := &GetLegendGraphicRequest{
		Layer:   q.Get("LAYER"),
		Style:   q.Get("STYLE"),
		Format:  strings.ToLower(q.Get("FORMAT")),
		Width:   20, // Default
		Height:  20, // Default
		SLD:     q.Get("SLD"),
		SLDBody: q.Get("SLD_BODY"),
	}
	var err error
	req.Environment, _, err = parseRenderingEnvironment(q.Get("ENV"), maxEnvVariables, maxEnvValueBytes)
	if err != nil {
		return nil, &RequestError{Code: ExceptionInvalidParameterValue, Message: err.Error()}
	}

	if req.Layer == "" {
		return nil, &RequestError{Code: "LayerNotDefined", Message: "LAYER parameter is required"}
	}

	// Parse optional WIDTH
	if widthStr := q.Get("WIDTH"); widthStr != "" {
		w, err := strconv.Atoi(widthStr)
		if err != nil || w <= 0 || w > maxWidth {
			return nil, &RequestError{Code: "InvalidWidth", Message: "WIDTH is outside the allowed range"}
		}
		req.Width = w
	}

	// Parse optional HEIGHT
	if heightStr := q.Get("HEIGHT"); heightStr != "" {
		h, err := strconv.Atoi(heightStr)
		if err != nil || h <= 0 || h > maxHeight {
			return nil, &RequestError{Code: "InvalidHeight", Message: "HEIGHT is outside the allowed range"}
		}
		req.Height = h
	}
	if maxPixels > 0 && req.Height > maxPixels/req.Width {
		return nil, &RequestError{Code: ExceptionInvalidParameterValue, Message: "requested image exceeds maximum pixel count"}
	}

	// Default format
	if req.Format == "" {
		req.Format = FormatPNG
	}

	return req, nil
}

// parseRenderingEnvironment accepts GeoServer-compatible key:value pairs while
// deliberately excluding expansion, escaping, and nested request syntax. Values
// are request data only and are never interpreted as XML or SQL.
func parseRenderingEnvironment(raw string, maxVariables, maxValueBytes int) (map[string]string, string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, "", nil
	}
	result := make(map[string]string)
	for _, pair := range strings.Split(raw, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		name, value, ok := strings.Cut(pair, ":")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || !environmentNamePattern.MatchString(name) {
			return nil, "", fmt.Errorf("ENV contains an invalid variable")
		}
		if _, exists := result[name]; exists {
			return nil, "", fmt.Errorf("ENV variable %q is repeated", name)
		}
		if maxValueBytes > 0 && len(value) > maxValueBytes {
			return nil, "", fmt.Errorf("ENV variable %q exceeds %d bytes", name, maxValueBytes)
		}
		result[name] = value
		if maxVariables > 0 && len(result) > maxVariables {
			return nil, "", fmt.Errorf("ENV exceeds the maximum of %d variables", maxVariables)
		}
	}
	keys := make([]string, 0, len(result))
	for key := range result {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	canonical := make([]string, 0, len(keys))
	for _, key := range keys {
		canonical = append(canonical, key+":"+result[key])
	}
	return result, strings.Join(canonical, ";"), nil
}

// parseCRS extracts the SRID from a CRS string.
// Delegates to the unified crs.Parse function.
func parseCRS(crsStr string) (int, error) {
	return crs.Parse(crsStr)
}

// parseBBox parses a BBOX string and handles axis order based on CRS.
func parseBBox(bboxStr, crs string) (query.BBox, error) {
	parts := strings.Split(bboxStr, ",")
	if len(parts) != 4 {
		return query.BBox{}, fmt.Errorf("BBOX must have 4 comma-separated values")
	}

	values := make([]float64, 4)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return query.BBox{}, fmt.Errorf("invalid BBOX value %s: %w", p, err)
		}
		values[i] = v
	}

	// WMS 1.3.0: For geographic CRS (EPSG:4326), axis order is lat,lon
	// For CRS:84, axis order is lon,lat
	// For projected CRS, axis order is typically x,y (easting,northing)

	var bbox query.BBox
	srid, _ := parseCRS(crs)
	if srid == 4326 && !strings.EqualFold(crs, "CRS:84") && !strings.EqualFold(crs, "OGC:CRS84") {
		// EPSG:4326 in WMS 1.3.0 uses lat,lon order
		// Swap to lon,lat (x,y) for internal use
		bbox = query.BBox{
			MinX: values[1], // lon
			MinY: values[0], // lat
			MaxX: values[3], // lon
			MaxY: values[2], // lat
		}
	} else {
		// Standard x,y order
		bbox = query.BBox{
			MinX: values[0],
			MinY: values[1],
			MaxX: values[2],
			MaxY: values[3],
		}
	}

	// Validate BBOX - WMS 1.3.0 requires valid extent with non-zero width and height
	if bbox.MinX > bbox.MaxX {
		return query.BBox{}, fmt.Errorf("BBOX minx must be less than maxx")
	}
	if bbox.MinY > bbox.MaxY {
		return query.BBox{}, fmt.Errorf("BBOX miny must be less than maxy")
	}
	// Zero-width or zero-height bounding boxes are invalid per WMS 1.3.0 CITE tests
	if bbox.MinX == bbox.MaxX {
		return query.BBox{}, fmt.Errorf("BBOX has zero width (minx equals maxx)")
	}
	if bbox.MinY == bbox.MaxY {
		return query.BBox{}, fmt.Errorf("BBOX has zero height (miny equals maxy)")
	}

	return bbox, nil
}

// parseHexColor parses a hex color string (0xRRGGBB or RRGGBB).
func parseHexColor(s string) color.RGBA {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "#")
	if len(s) != 6 {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}

	r, _ := strconv.ParseUint(s[0:2], 16, 8)
	g, _ := strconv.ParseUint(s[2:4], 16, 8)
	b, _ := strconv.ParseUint(s[4:6], 16, 8)

	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
}

// RequestError represents a WMS request error.
type RequestError struct {
	Code    string
	Message string
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
