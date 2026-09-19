package mockserver

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Handler holds the mock WFS request handlers.
type Handler struct {
	baseURL      string
	featureTypes []MockFeatureType
	features     []MockFeature
}

// NewHandler creates a new WFS handler.
func NewHandler(baseURL string) *Handler {
	return &Handler{
		baseURL:      baseURL,
		featureTypes: DefaultFeatureTypes(),
		features:     DefaultFeatures(),
	}
}

// ServeHTTP handles all WFS requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters (case-insensitive)
	q := make(url.Values)
	for k, v := range r.URL.Query() {
		q[strings.ToUpper(k)] = v
	}
	if r.Method == http.MethodPost && q.Get("REQUEST") == "" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			h.writeException(w, "InvalidParameterValue", "Unable to read XML request", "request")
			return
		}
		var request struct {
			XMLName      xml.Name
			Service      string `xml:"service,attr"`
			Version      string `xml:"version,attr"`
			OutputFormat string `xml:"outputFormat,attr"`
			Count        string `xml:"count,attr"`
			StartIndex   string `xml:"startIndex,attr"`
			Queries      []struct {
				TypeNames string `xml:"typeNames,attr"`
				Filter    struct {
					ResourceIDs []struct {
						RID string `xml:"rid,attr"`
					} `xml:"ResourceId"`
				} `xml:"Filter"`
			} `xml:"Query"`
		}
		if err := xml.Unmarshal(body, &request); err != nil || request.XMLName.Local == "" {
			h.writeException(w, "InvalidParameterValue", "Invalid XML request", "request")
			return
		}
		q.Set("REQUEST", request.XMLName.Local)
		q.Set("SERVICE", request.Service)
		q.Set("VERSION", request.Version)
		q.Set("OUTPUTFORMAT", request.OutputFormat)
		q.Set("COUNT", request.Count)
		q.Set("STARTINDEX", request.StartIndex)
		var typeNames, resourceIDs []string
		for _, query := range request.Queries {
			typeNames = append(typeNames, strings.Fields(query.TypeNames)...)
			for _, identifier := range query.Filter.ResourceIDs {
				resourceIDs = append(resourceIDs, identifier.RID)
			}
		}
		q.Set("TYPENAMES", strings.Join(typeNames, ","))
		q.Set("RESOURCEID", strings.Join(resourceIDs, ","))
	}

	// Check SERVICE parameter
	service := strings.ToUpper(q.Get("SERVICE"))
	if service != "" && service != "WFS" {
		h.writeException(w, "InvalidParameterValue", "SERVICE must be WFS", "SERVICE")
		return
	}

	// Route based on REQUEST parameter
	request := strings.ToUpper(q.Get("REQUEST"))
	switch request {
	case "GETCAPABILITIES":
		h.handleGetCapabilities(w, r, q)
	case "DESCRIBEFEATURETYPE":
		h.handleDescribeFeatureType(w, r, q)
	case "GETFEATURE":
		h.handleGetFeature(w, r, q)
	case "GETPROPERTYVALUE":
		h.handleGetPropertyValue(w, r, q)
	case "LISTSTOREDQUERIES":
		h.handleListStoredQueries(w, r, q)
	case "DESCRIBESTOREDQUERIES":
		h.handleDescribeStoredQueries(w, r, q)
	case "":
		h.writeException(w, "MissingParameterValue", "REQUEST parameter is required", "REQUEST")
	default:
		h.writeException(w, "OperationNotSupported", "Unsupported REQUEST: "+request, "REQUEST")
	}
}

// handleGetCapabilities handles GetCapabilities requests.
func (h *Handler) handleGetCapabilities(w http.ResponseWriter, _ *http.Request, q url.Values) {
	// Version negotiation
	version := q.Get("VERSION")
	acceptVersions := q.Get("ACCEPTVERSIONS")
	if version == "" && acceptVersions == "" {
		// Use default version
		version = "2.0.0"
	}

	if version != "" && version != "2.0.0" && version != "2.0.2" {
		h.writeException(w, "InvalidParameterValue", "Unsupported VERSION: "+version, "VERSION")
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(BuildCapabilities(h.baseURL, h.featureTypes)))
}

// isValidCRS checks if a CRS is valid/supported.
func (h *Handler) isValidCRS(crs string) bool {
	validCRS := map[string]bool{
		"urn:ogc:def:crs:EPSG::4326": true,
		"urn:ogc:def:crs:CRS84":      true,
		"urn:ogc:def:crs:EPSG::3857": true,
		"EPSG:4326":                  true,
		"EPSG:3857":                  true,
		"CRS84":                      true,
	}
	return validCRS[crs]
}

// isValidOutputFormat checks if an output format is valid for GetFeature.
func (h *Handler) isValidOutputFormat(format string) bool {
	validFormats := map[string]bool{
		"":                                 true, // default
		"application/gml+xml; version=3.2": true,
		"text/xml; subtype=gml/3.2.1":      true,
		"application/json":                 true,
	}
	return validFormats[format]
}

// isValidDescribeOutputFormat checks if an output format is valid for DescribeFeatureType.
func (h *Handler) isValidDescribeOutputFormat(format string) bool {
	validFormats := map[string]bool{
		"":                                 true, // default
		"application/gml+xml; version=3.2": true,
		"text/xml; subtype=gml/3.2.1":      true,
	}
	return validFormats[format]
}

// validateBBOX validates BBOX format.
// Supports formats:
// - minx,miny,maxx,maxy (comma-separated)
// - minx,miny,maxx,maxy,crs (comma-separated with CRS)
// - "minx miny maxx maxy" (space-separated)
// - "minx,miny maxx,maxy" (WFS style - two corner pairs)
func (h *Handler) validateBBOX(bbox string) error {
	// Try comma-separated format first
	parts := strings.Split(bbox, ",")
	if len(parts) >= 4 {
		// Could be minx,miny,maxx,maxy or minx,miny,maxx,maxy,crs
		// But also handles "minx,miny maxx,maxy" where last two have spaces
		for i := 0; i < 4 && i < len(parts); i++ {
			// Handle space-separated pairs like "-100,35 -90,45"
			value := strings.TrimSpace(parts[i])
			// If it has a space, split and take first part
			if idx := strings.Index(value, " "); idx > 0 {
				value = value[:idx]
			}
			if _, err := strconv.ParseFloat(value, 64); err != nil {
				return fmt.Errorf("invalid coordinate: %s", parts[i])
			}
		}
		return nil
	}

	// Try space-separated format
	spaceParts := strings.Fields(bbox)
	if len(spaceParts) >= 4 {
		for i := 0; i < 4; i++ {
			if _, err := strconv.ParseFloat(spaceParts[i], 64); err != nil {
				return fmt.Errorf("invalid coordinate: %s", spaceParts[i])
			}
		}
		return nil
	}

	return fmt.Errorf("invalid BBOX format")
}

// handleDescribeFeatureType handles DescribeFeatureType requests.
func (h *Handler) handleDescribeFeatureType(w http.ResponseWriter, _ *http.Request, q url.Values) {
	// Validate output format
	outputFormat := q.Get("OUTPUTFORMAT")
	if !h.isValidDescribeOutputFormat(outputFormat) {
		h.writeException(w, "InvalidParameterValue", "Unsupported output format: "+outputFormat, "OUTPUTFORMAT")
		return
	}

	typeNames := q.Get("TYPENAMES")
	if typeNames == "" {
		typeNames = q.Get("TYPENAME") // WFS 1.x compatibility
	}

	var types []MockFeatureType
	if typeNames == "" {
		// Describe all types
		types = h.featureTypes
	} else {
		// Parse specific type names
		for _, name := range strings.Split(typeNames, ",") {
			name = strings.TrimSpace(name)
			found := false
			for _, ft := range h.featureTypes {
				if ft.Name == name {
					types = append(types, ft)
					found = true
					break
				}
			}
			if !found {
				h.writeException(w, "InvalidParameterValue", "Unknown type name: "+name, "TYPENAMES")
				return
			}
		}
	}

	w.Header().Set("Content-Type", "application/gml+xml; version=3.2; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(h.buildDescribeFeatureType(types)))
}

// handleGetFeature handles GetFeature requests.
func (h *Handler) handleGetFeature(w http.ResponseWriter, _ *http.Request, q url.Values) {
	// Validate VERSION first
	version := q.Get("VERSION")
	if version != "" && version != "2.0.0" && version != "2.0.2" {
		h.writeException(w, "InvalidParameterValue", "Unsupported VERSION: "+version, "VERSION")
		return
	}

	typeNames := q.Get("TYPENAMES")
	if typeNames == "" {
		typeNames = q.Get("TYPENAME")
	}

	// Check for stored query
	storedQueryID := q.Get("STOREDQUERY_ID")
	if storedQueryID != "" {
		h.handleStoredQuery(w, q, storedQueryID)
		return
	}

	if typeNames == "" {
		h.writeException(w, "MissingParameterValue", "TYPENAMES parameter is required", "TYPENAMES")
		return
	}

	// Validate output format
	outputFormat := q.Get("OUTPUTFORMAT")
	if !h.isValidOutputFormat(outputFormat) {
		h.writeException(w, "InvalidParameterValue", "Unsupported output format: "+outputFormat, "OUTPUTFORMAT")
		return
	}

	// Validate SRSNAME (CRS)
	srsName := q.Get("SRSNAME")
	if srsName != "" && !h.isValidCRS(srsName) {
		h.writeException(w, "InvalidParameterValue", "Unsupported CRS: "+srsName, "SRSNAME")
		return
	}

	// Validate BBOX format - supports comma-separated or space-separated pairs
	bbox := q.Get("BBOX")
	if bbox != "" {
		if err := h.validateBBOX(bbox); err != nil {
			h.writeException(w, "InvalidParameterValue", "Invalid BBOX format", "BBOX")
			return
		}
	}

	// Parse type names
	typeNameList := strings.Split(typeNames, ",")
	for _, name := range typeNameList {
		found := false
		for _, ft := range h.featureTypes {
			if ft.Name == strings.TrimSpace(name) {
				found = true
				break
			}
		}
		if !found {
			h.writeException(w, "InvalidParameterValue", "Unknown type name: "+name, "TYPENAMES")
			return
		}
	}

	// Get count and startIndex
	count := -1
	if countStr := q.Get("COUNT"); countStr != "" {
		var err error
		count, err = strconv.Atoi(countStr)
		if err != nil || count < 0 {
			h.writeException(w, "InvalidParameterValue", "Invalid COUNT value", "COUNT")
			return
		}
	}

	startIndex := 0
	if startStr := q.Get("STARTINDEX"); startStr != "" {
		var err error
		startIndex, err = strconv.Atoi(startStr)
		if err != nil || startIndex < 0 {
			h.writeException(w, "InvalidParameterValue", "Invalid STARTINDEX value", "STARTINDEX")
			return
		}
	}

	// Get resultType
	resultType := "results"
	if rt := q.Get("RESULTTYPE"); rt != "" {
		resultType = strings.ToLower(rt)
		if resultType != "results" && resultType != "hits" {
			h.writeException(w, "InvalidParameterValue", "Invalid RESULTTYPE value", "RESULTTYPE")
			return
		}
	}

	// Filter features by type
	var filtered []MockFeature
	for _, f := range h.features {
		for _, typeName := range typeNameList {
			if f.TypeName == strings.TrimSpace(typeName) {
				filtered = append(filtered, f)
				break
			}
		}
	}

	// Apply ResourceId filter if present
	resourceID := q.Get("RESOURCEID")
	if resourceID != "" {
		var byID []MockFeature
		ids := strings.Split(resourceID, ",")
		for _, f := range filtered {
			for _, id := range ids {
				if f.ID == strings.TrimSpace(id) {
					byID = append(byID, f)
					break
				}
			}
		}
		filtered = byID
	}

	// Calculate totals before paging
	numberMatched := len(filtered)

	// Apply paging
	if startIndex > 0 {
		if startIndex >= len(filtered) {
			filtered = nil
		} else {
			filtered = filtered[startIndex:]
		}
	}
	if count >= 0 && count < len(filtered) {
		filtered = filtered[:count]
	}

	numberReturned := len(filtered)
	if resultType == "hits" {
		numberReturned = 0
		filtered = nil
	}

	// Build response
	w.Header().Set("Content-Type", "application/gml+xml; version=3.2; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(h.buildFeatureCollection(filtered, numberMatched, numberReturned, startIndex, count)))
}

// handleStoredQuery handles stored query requests.
func (h *Handler) handleStoredQuery(w http.ResponseWriter, q url.Values, queryID string) {
	// Accept both URN and HTTP formats for GetFeatureById
	if queryID != "urn:ogc:def:query:OGC-WFS::GetFeatureById" &&
		queryID != "http://www.opengis.net/def/query/OGC-WFS/0/GetFeatureById" {
		h.writeException(w, "InvalidParameterValue", "Unknown stored query: "+queryID, "STOREDQUERY_ID")
		return
	}

	featureID := q.Get("ID")
	if featureID == "" {
		h.writeException(w, "MissingParameterValue", "ID parameter is required for GetFeatureById", "ID")
		return
	}

	var feature *MockFeature
	for _, f := range h.features {
		if f.ID == featureID {
			feature = &f
			break
		}
	}

	if feature == nil {
		// Return empty collection for unknown ID
		w.Header().Set("Content-Type", "application/gml+xml; version=3.2; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(h.buildFeatureCollection(nil, 0, 0, 0, -1)))
		return
	}

	w.Header().Set("Content-Type", "application/gml+xml; version=3.2; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(h.buildFeatureCollection([]MockFeature{*feature}, 1, 1, 0, -1)))
}

// handleGetPropertyValue handles GetPropertyValue requests.
func (h *Handler) handleGetPropertyValue(w http.ResponseWriter, _ *http.Request, q url.Values) {
	typeNames := q.Get("TYPENAMES")
	if typeNames == "" {
		typeNames = q.Get("TYPENAME")
	}
	if typeNames == "" {
		h.writeException(w, "MissingParameterValue", "TYPENAMES parameter is required", "TYPENAMES")
		return
	}

	// Validate type name exists
	found := false
	for _, ft := range h.featureTypes {
		if ft.Name == typeNames {
			found = true
			break
		}
	}
	if !found {
		h.writeException(w, "InvalidParameterValue", "Unknown type name: "+typeNames, "TYPENAMES")
		return
	}

	valueRef := q.Get("VALUEREFERENCE")
	if valueRef == "" {
		h.writeException(w, "MissingParameterValue", "VALUEREFERENCE parameter is required", "VALUEREFERENCE")
		return
	}

	// Get resultType
	resultType := "results"
	if rt := q.Get("RESULTTYPE"); rt != "" {
		resultType = strings.ToLower(rt)
		if resultType != "results" && resultType != "hits" {
			h.writeException(w, "InvalidParameterValue", "Invalid RESULTTYPE value", "RESULTTYPE")
			return
		}
	}

	// Get features for the type
	var features []MockFeature
	for _, f := range h.features {
		if f.TypeName == typeNames {
			features = append(features, f)
		}
	}

	// Extract values
	var values []string
	for _, f := range features {
		if valueRef == "@gml:id" || valueRef == "gml:id" {
			values = append(values, f.ID)
		} else if v, ok := f.Properties[valueRef]; ok {
			values = append(values, fmt.Sprintf("%v", v))
		}
	}

	// For hits, return count only
	numberMatched := len(values)
	numberReturned := len(values)
	if resultType == "hits" {
		numberReturned = 0
		values = nil
	}

	w.Header().Set("Content-Type", "application/gml+xml; version=3.2; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(h.buildValueCollectionWithCounts(values, numberMatched, numberReturned)))
}

// handleListStoredQueries handles ListStoredQueries requests.
func (h *Handler) handleListStoredQueries(w http.ResponseWriter, _ *http.Request, _ url.Values) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(BuildStoredQueriesList()))
}

// handleDescribeStoredQueries handles DescribeStoredQueries requests.
func (h *Handler) handleDescribeStoredQueries(w http.ResponseWriter, _ *http.Request, q url.Values) {
	// Check if specific stored query ID is requested
	// Accept both URN and HTTP formats for GetFeatureById
	storedQueryID := q.Get("STOREDQUERY_ID")
	if storedQueryID != "" &&
		storedQueryID != "urn:ogc:def:query:OGC-WFS::GetFeatureById" &&
		storedQueryID != "http://www.opengis.net/def/query/OGC-WFS/0/GetFeatureById" {
		h.writeException(w, "InvalidParameterValue", "Unknown stored query: "+storedQueryID, "STOREDQUERY_ID")
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(BuildStoredQueryDescription()))
}

// writeException writes an OWS exception report.
func (h *Handler) writeException(w http.ResponseWriter, code, text, locator string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<ows:ExceptionReport
  xmlns:ows="http://www.opengis.net/ows/1.1"
  version="2.0.0">
  <ows:Exception exceptionCode="%s" locator="%s">
    <ows:ExceptionText>%s</ows:ExceptionText>
  </ows:Exception>
</ows:ExceptionReport>
`, code, locator, text)
}

// buildDescribeFeatureType builds a DescribeFeatureType response.
func (h *Handler) buildDescribeFeatureType(types []MockFeatureType) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<xsd:schema
  xmlns:xsd="http://www.w3.org/2001/XMLSchema"
  xmlns:gml="http://www.opengis.net/gml/3.2"
  xmlns:mock="http://mock.test/wfs"
  targetNamespace="http://mock.test/wfs"
  elementFormDefault="qualified">

  <xsd:import namespace="http://www.opengis.net/gml/3.2" schemaLocation="http://schemas.opengis.net/gml/3.2.1/gml.xsd"/>

`)

	for _, ft := range types {
		localName := strings.TrimPrefix(ft.Name, "mock:")
		sb.WriteString(fmt.Sprintf(`  <xsd:element name="%s" type="mock:%sType" substitutionGroup="gml:AbstractFeature"/>
  <xsd:complexType name="%sType">
    <xsd:complexContent>
      <xsd:extension base="gml:AbstractFeatureType">
        <xsd:sequence>
`, localName, localName, localName))

		for _, p := range ft.Properties {
			xsdType := "xsd:string"
			switch p.Type {
			case "integer":
				xsdType = "xsd:integer"
			case "double":
				xsdType = "xsd:double"
			case "date":
				xsdType = "xsd:date"
			case "dateTime":
				xsdType = "xsd:dateTime"
			case "boolean":
				xsdType = "xsd:boolean"
			}
			sb.WriteString(fmt.Sprintf(`          <xsd:element name="%s" type="%s" minOccurs="0"/>
`, p.Name, xsdType))
		}

		// Add geometry property
		gmlType := "gml:PointPropertyType"
		switch ft.GeomType {
		case "LineString":
			gmlType = "gml:CurvePropertyType"
		case "Polygon":
			gmlType = "gml:SurfacePropertyType"
		case "MultiPoint":
			gmlType = "gml:MultiPointPropertyType"
		case "MultiLineString":
			gmlType = "gml:MultiCurvePropertyType"
		case "MultiPolygon":
			gmlType = "gml:MultiSurfacePropertyType"
		}
		sb.WriteString(fmt.Sprintf(`          <xsd:element name="%s" type="%s"/>
`, ft.GeomColumn, gmlType))

		sb.WriteString(`        </xsd:sequence>
      </xsd:extension>
    </xsd:complexContent>
  </xsd:complexType>

`)
	}

	sb.WriteString(`</xsd:schema>
`)
	return sb.String()
}

// buildFeatureCollection builds a GML feature collection.
func (h *Handler) buildFeatureCollection(features []MockFeature, numberMatched, numberReturned, startIndex, count int) string {
	var sb strings.Builder
	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Build next/previous links for paging
	nextLink := ""
	prevLink := ""
	if count > 0 && startIndex+count < numberMatched {
		nextLink = fmt.Sprintf(` next="%s?SERVICE=WFS&amp;REQUEST=GetFeature&amp;STARTINDEX=%d&amp;COUNT=%d"`,
			h.baseURL, startIndex+count, count)
	}
	if startIndex > 0 {
		prevStart := startIndex - count
		if prevStart < 0 {
			prevStart = 0
		}
		prevLink = fmt.Sprintf(` previous="%s?SERVICE=WFS&amp;REQUEST=GetFeature&amp;STARTINDEX=%d&amp;COUNT=%d"`,
			h.baseURL, prevStart, count)
	}

	sb.WriteString(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<wfs:FeatureCollection
  xmlns:wfs="http://www.opengis.net/wfs/2.0"
  xmlns:gml="http://www.opengis.net/gml/3.2"
  xmlns:mock="http://mock.test/wfs"
  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
  timeStamp="%s"
  numberMatched="%d"
  numberReturned="%d"%s%s>
`, timestamp, numberMatched, numberReturned, nextLink, prevLink))

	for _, f := range features {
		localName := strings.TrimPrefix(f.TypeName, "mock:")
		sb.WriteString(fmt.Sprintf(`  <wfs:member>
    <mock:%s gml:id="%s">
`, localName, f.ID))

		for k, v := range f.Properties {
			sb.WriteString(fmt.Sprintf(`      <mock:%s>%v</mock:%s>
`, k, v, k))
		}

		// Get geometry column name from feature type
		ft := GetFeatureType(f.TypeName)
		geomCol := "geom"
		if ft != nil {
			geomCol = ft.GeomColumn
		}

		sb.WriteString(fmt.Sprintf(`      <mock:%s>
        %s
      </mock:%s>
`, geomCol, f.Geometry, geomCol))

		sb.WriteString(fmt.Sprintf(`    </mock:%s>
  </wfs:member>
`, localName))
	}

	sb.WriteString(`</wfs:FeatureCollection>
`)
	return sb.String()
}

// buildValueCollection builds a GetPropertyValue response.
func (h *Handler) buildValueCollection(values []string) string {
	return h.buildValueCollectionWithCounts(values, len(values), len(values))
}

// buildValueCollectionWithCounts builds a GetPropertyValue response with explicit counts.
func (h *Handler) buildValueCollectionWithCounts(values []string, numberMatched, numberReturned int) string {
	var sb strings.Builder
	timestamp := time.Now().UTC().Format(time.RFC3339)

	sb.WriteString(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<wfs:ValueCollection
  xmlns:wfs="http://www.opengis.net/wfs/2.0"
  timeStamp="%s"
  numberMatched="%d"
  numberReturned="%d">
`, timestamp, numberMatched, numberReturned))

	for _, v := range values {
		sb.WriteString(fmt.Sprintf(`  <wfs:member>
    <wfs:Value>%s</wfs:Value>
  </wfs:member>
`, v))
	}

	sb.WriteString(`</wfs:ValueCollection>
`)
	return sb.String()
}
