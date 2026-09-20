package wfs

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/gdalmd"
)

// WriteGMLFeatureCollection writes a GML 3.2 feature collection to the response.
func WriteGMLFeatureCollection(w http.ResponseWriter, layerInfo *datasource.LayerInfo, features [][]byte,
	totalCount, startIndex, count int, namespace, nsPrefix string, srid int, baseURL, typeName string) {
	writeGMLFeatureCollection(w, layerInfo, features, strconv.Itoa(totalCount), startIndex, count, namespace, nsPrefix, srid, baseURL, typeName, "")
}

func WriteGMLFeatureCollectionMatched(w http.ResponseWriter, layerInfo *datasource.LayerInfo, features [][]byte,
	numberMatched string, startIndex, count int, namespace, nsPrefix string, srid int, baseURL, typeName string, requests ...*GetFeatureRequest) {
	writeGMLFeatureCollection(w, layerInfo, features, numberMatched, startIndex, count, namespace, nsPrefix, srid, baseURL, typeName, "", requests...)
}

// WriteGMLFeatureCollectionWithLock writes a GML 3.2 feature collection with optional lockId attribute.
func WriteGMLFeatureCollectionWithLock(w http.ResponseWriter, layerInfo *datasource.LayerInfo, features [][]byte,
	totalCount, startIndex, count int, namespace, nsPrefix string, srid int, baseURL, typeName string, lockId string, requests ...*GetFeatureRequest) {
	writeGMLFeatureCollection(w, layerInfo, features, strconv.Itoa(totalCount), startIndex, count, namespace, nsPrefix, srid, baseURL, typeName, lockId, requests...)
}

func writeGMLFeatureCollection(w http.ResponseWriter, layerInfo *datasource.LayerInfo, features [][]byte,
	numberMatched string, startIndex, count int, namespace, nsPrefix string, srid int, baseURL, typeName string, lockId string, requests ...*GetFeatureRequest) {

	// Determine effective namespace and prefix
	effectiveNS := namespace
	effectivePrefix := nsPrefix
	qn := ParseQName(typeName)
	if qn.Prefix != "" {
		if qn.Namespace != NSDefault {
			effectiveNS = qn.Namespace
		}
		effectivePrefix = qn.Prefix
	}
	if !validASCIIXMLName(effectivePrefix) {
		WriteException(w, ExceptionInvalidParameterValue, "namespace", "invalid XML namespace prefix")
		return
	}
	featureElementName := qn.LocalPart
	if featureElementName == "" {
		featureElementName = layerInfo.Name
	}
	featureElementName = sanitizeXMLName(featureElementName)
	if !validASCIIXMLName(featureElementName) {
		WriteException(w, ExceptionNoApplicableCode, "", "feature type cannot be represented as an XML name")
		return
	}
	propertyNames, err := propertyXMLNames(layerInfo)
	if err != nil {
		WriteException(w, ExceptionNoApplicableCode, "", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/gml+xml; version=3.2; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))

	// Build schema location
	schemaLoc := fmt.Sprintf("%s http://schemas.opengis.net/wfs/2.0/wfs.xsd %s %s", NSWfs, effectiveNS,
		wfsURL(baseURL, map[string]string{"service": "WFS", "version": featureRequestVersion(requests), "request": "DescribeFeatureType", "typeNames": typeName}))

	// Calculate pagination
	numReturned := len(features)
	nextStartIndex := startIndex + numReturned
	totalCount, countErr := strconv.Atoi(numberMatched)
	hasNext := (countErr == nil && nextStartIndex < totalCount) || (numberMatched == "unknown" && count > 0 && numReturned == count)
	hasPrevious := startIndex > 0

	// Build pagination attributes
	var paginationAttrs string
	if hasNext {
		nextURL := featurePageURL(baseURL, typeName, nextStartIndex, count, requests...)
		paginationAttrs += fmt.Sprintf("\n  next=\"%s\"", escapeXML(nextURL))
	}
	if hasPrevious {
		prevStartIndex := startIndex - count
		if prevStartIndex < 0 {
			prevStartIndex = 0
		}
		previousURL := featurePageURL(baseURL, typeName, prevStartIndex, count, requests...)
		paginationAttrs += fmt.Sprintf("\n  previous=\"%s\"", escapeXML(previousURL))
	}

	// Build lockId attribute if provided
	var lockIdAttr string
	if lockId != "" {
		lockIdAttr = fmt.Sprintf("\n  lockId=\"%s\"", escapeXML(lockId))
	}

	// Write opening tag
	fmt.Fprintf(w, `<wfs:FeatureCollection
  xmlns:wfs="%s"
  xmlns:gml="%s"
  xmlns:xsi="%s"
  xmlns:%s="%s"
  xsi:schemaLocation="%s"
  timeStamp="%s"
  numberMatched="%s"
  numberReturned="%d"%s%s>
`, NSWfs, NSGml, NSXsi, effectivePrefix, escapeXML(effectiveNS), escapeXML(schemaLoc),
		escapeXML(nowISO8601()), escapeXML(numberMatched), numReturned, paginationAttrs, lockIdAttr)

	// Write each feature
	for _, featureJSON := range features {
		writeGMLFeature(w, layerInfo, featureJSON, effectiveNS, effectivePrefix, srid, featureElementName, propertyNames)
	}

	// Close collection
	w.Write([]byte("</wfs:FeatureCollection>\n"))
}

func writeGMLFeature(w http.ResponseWriter, layerInfo *datasource.LayerInfo, featureJSON []byte,
	namespace, nsPrefix string, srid int, elementName string, propertyNames map[string]string) {

	// Parse GeoJSON feature
	feature, err := decodeFeatureJSON(featureJSON)
	if err != nil {
		slog.Error("GML: failed to unmarshal feature JSON", "error", err)
		return
	}

	// Get feature ID
	featureID := ""
	if id, ok := feature["id"]; ok {
		featureID = fmt.Sprintf("%s.%v", elementName, id)
	}

	// Get properties (may be nil for features without properties)
	properties, ok := feature["properties"].(map[string]interface{})
	if !ok {
		properties = make(map[string]interface{})
	}

	// Get geometry (may be nil for non-spatial features)
	geometry, _ := feature["geometry"].(map[string]interface{})

	// Write member wrapper
	fmt.Fprintf(w, "  <wfs:member>\n")

	// Write feature element
	fmt.Fprintf(w, "    <%s:%s gml:id=\"%s\">\n", nsPrefix, elementName, escapeXML(featureID))

	// Write GML standard properties first, in correct schema order: description, identifier, name
	// Per GML 3.2 AbstractGMLType schema order
	writeGMLStandardProperties(w, properties, "      ")

	// Write application-specific properties (non-GML standard properties)
	for _, prop := range layerInfo.Properties {
		if isStandardGMLProperty(prop.Name) {
			continue // Already written above
		}
		xmlName := propertyNames[prop.Name]
		if val, ok := properties[prop.Name]; ok {
			if val != nil {
				fmt.Fprintf(w, "      <%s:%s>%s</%s:%s>\n",
					nsPrefix, xmlName, escapeXML(formatValue(val)), nsPrefix, xmlName)
			} else {
				// For application-specific properties, output nil with xsi:nil="true"
				fmt.Fprintf(w, "      <%s:%s xsi:nil=\"true\"/>\n", nsPrefix, xmlName)
			}
		}
	}

	// Write geometry
	if geometry != nil && layerInfo.GeometryColumn != "" {
		geometryName := propertyNames[layerInfo.GeometryColumn]
		fmt.Fprintf(w, "      <%s:%s>\n", nsPrefix, geometryName)
		writeGMLGeometry(w, geometry, srid)
		fmt.Fprintf(w, "      </%s:%s>\n", nsPrefix, geometryName)
	}

	fmt.Fprintf(w, "    </%s:%s>\n", nsPrefix, elementName)
	fmt.Fprintf(w, "  </wfs:member>\n")
}

func writeGMLGeometry(w http.ResponseWriter, geometry map[string]interface{}, srid int) {
	geomType, ok := geometry["type"].(string)
	if !ok || geomType == "" {
		return // Invalid geometry, skip
	}
	coords := geometry["coordinates"]
	if coords == nil {
		return // No coordinates, skip
	}

	srsName := SRSNameFromSRID(srid)
	coords = authorityCoordinates(coords, srid)

	switch strings.ToUpper(geomType) {
	case "POINT":
		writeGMLPoint(w, coords, srsName)
	case "LINESTRING":
		writeGMLLineString(w, coords, srsName)
	case "POLYGON":
		writeGMLPolygon(w, coords, srsName)
	case "MULTIPOINT":
		writeGMLMultiPoint(w, coords, srsName)
	case "MULTILINESTRING":
		writeGMLMultiLineString(w, coords, srsName)
	case "MULTIPOLYGON":
		writeGMLMultiPolygon(w, coords, srsName)
	}
}

func writeGMLPoint(w http.ResponseWriter, coords interface{}, srsName string) {
	point, ok := coords.([]interface{})
	if !ok || len(point) < 2 {
		return // Invalid point coordinates
	}
	fmt.Fprintf(w, "        <gml:Point srsName=\"%s\">\n", srsName)
	fmt.Fprintf(w, "          <gml:pos>%v %v</gml:pos>\n", point[0], point[1])
	fmt.Fprintf(w, "        </gml:Point>\n")
}

func writeGMLLineString(w http.ResponseWriter, coords interface{}, srsName string) {
	line, ok := coords.([]interface{})
	if !ok {
		return // Invalid line coordinates
	}
	fmt.Fprintf(w, "        <gml:LineString srsName=\"%s\">\n", srsName)
	fmt.Fprintf(w, "          <gml:posList>%s</gml:posList>\n", coordsToPosList(line))
	fmt.Fprintf(w, "        </gml:LineString>\n")
}

func writeGMLPolygon(w http.ResponseWriter, coords interface{}, srsName string) {
	rings, ok := coords.([]interface{})
	if !ok {
		return // Invalid polygon coordinates
	}
	fmt.Fprintf(w, "        <gml:Polygon srsName=\"%s\">\n", srsName)

	for i, ring := range rings {
		ringCoords, ok := ring.([]interface{})
		if !ok {
			continue // Skip invalid ring
		}
		if i == 0 {
			fmt.Fprintf(w, "          <gml:exterior>\n")
			fmt.Fprintf(w, "            <gml:LinearRing>\n")
			fmt.Fprintf(w, "              <gml:posList>%s</gml:posList>\n", coordsToPosList(ringCoords))
			fmt.Fprintf(w, "            </gml:LinearRing>\n")
			fmt.Fprintf(w, "          </gml:exterior>\n")
		} else {
			fmt.Fprintf(w, "          <gml:interior>\n")
			fmt.Fprintf(w, "            <gml:LinearRing>\n")
			fmt.Fprintf(w, "              <gml:posList>%s</gml:posList>\n", coordsToPosList(ringCoords))
			fmt.Fprintf(w, "            </gml:LinearRing>\n")
			fmt.Fprintf(w, "          </gml:interior>\n")
		}
	}

	fmt.Fprintf(w, "        </gml:Polygon>\n")
}

func writeGMLMultiPoint(w http.ResponseWriter, coords interface{}, srsName string) {
	points, ok := coords.([]interface{})
	if !ok {
		return // Invalid multipoint coordinates
	}
	fmt.Fprintf(w, "        <gml:MultiPoint srsName=\"%s\">\n", srsName)

	for _, point := range points {
		fmt.Fprintf(w, "          <gml:pointMember>\n")
		writeGMLPoint(w, point, srsName)
		fmt.Fprintf(w, "          </gml:pointMember>\n")
	}

	fmt.Fprintf(w, "        </gml:MultiPoint>\n")
}

func writeGMLMultiLineString(w http.ResponseWriter, coords interface{}, srsName string) {
	lines, ok := coords.([]interface{})
	if !ok {
		return // Invalid multilinestring coordinates
	}
	fmt.Fprintf(w, "        <gml:MultiCurve srsName=\"%s\">\n", srsName)

	for _, line := range lines {
		fmt.Fprintf(w, "          <gml:curveMember>\n")
		writeGMLLineString(w, line, srsName)
		fmt.Fprintf(w, "          </gml:curveMember>\n")
	}

	fmt.Fprintf(w, "        </gml:MultiCurve>\n")
}

func writeGMLMultiPolygon(w http.ResponseWriter, coords interface{}, srsName string) {
	polygons, ok := coords.([]interface{})
	if !ok {
		return // Invalid multipolygon coordinates
	}
	fmt.Fprintf(w, "        <gml:MultiSurface srsName=\"%s\">\n", srsName)

	for _, polygon := range polygons {
		fmt.Fprintf(w, "          <gml:surfaceMember>\n")
		writeGMLPolygon(w, polygon, srsName)
		fmt.Fprintf(w, "          </gml:surfaceMember>\n")
	}

	fmt.Fprintf(w, "        </gml:MultiSurface>\n")
}

var authorityAxisOrder sync.Map

// Source GeoJSON uses XY regardless of CRS. GML uses the advertised EPSG URN's
// authority order. Return detached coordinates so other formats/readers retain XY.
func authorityCoordinates(value interface{}, srid int) interface{} {
	swap, ok := authorityAxisOrder.Load(srid)
	if !ok {
		resolved, err := gdalmd.AuthorityAxisSwap(srid)
		if err != nil {
			slog.Error("GML axis lookup failed", "srid", srid, "error", err)
			return value
		}
		swap = resolved
		authorityAxisOrder.Store(srid, resolved)
	}
	if !swap.(bool) {
		return value
	}
	var reorder func(interface{}) interface{}
	reorder = func(value interface{}) interface{} {
		items, ok := value.([]interface{})
		if !ok {
			return value
		}
		result := append([]interface{}(nil), items...)
		if len(items) >= 2 {
			if _, nested := items[0].([]interface{}); !nested {
				result[0], result[1] = items[1], items[0]
				return result
			}
		}
		for i, item := range items {
			result[i] = reorder(item)
		}
		return result
	}
	return reorder(value)
}

func coordsToPosList(coords []interface{}) string {
	var parts []string
	for _, coord := range coords {
		point, ok := coord.([]interface{})
		if !ok || len(point) < 2 {
			continue // Skip invalid coordinate
		}
		parts = append(parts, fmt.Sprintf("%v %v", point[0], point[1]))
	}
	return strings.Join(parts, " ")
}

// WriteGeoJSONFeatureCollection writes a GeoJSON feature collection.
func WriteGeoJSONFeatureCollection(w http.ResponseWriter, features [][]byte, totalCount int) {
	WriteGeoJSONFeatureCollectionMatched(w, features, strconv.Itoa(totalCount))
}

func WriteGeoJSONFeatureCollectionMatched(w http.ResponseWriter, features [][]byte, numberMatched string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// Write opening
	matchedJSON := numberMatched
	if numberMatched == "unknown" {
		matchedJSON = `"unknown"`
	}
	fmt.Fprintf(w, `{"type":"FeatureCollection","numberMatched":%s,"numberReturned":%d,"features":[`, matchedJSON, len(features))

	// Write features
	for i, feature := range features {
		if i > 0 {
			w.Write([]byte(","))
		}
		w.Write(feature)
	}

	// Write closing
	w.Write([]byte("]}"))
}

// WriteHitsResponse writes a hits-only response with just the count.
func WriteHitsResponse(w http.ResponseWriter, totalCount, startIndex, requestedCount int, baseURL, typeName string, requests ...*GetFeatureRequest) {
	w.Header().Set("Content-Type", "application/gml+xml; version=3.2; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	w.Write([]byte(xml.Header))

	// Hits do not consume a result page. Their continuation must retrieve the
	// first requested subset as results, even when all matches fit in one page
	// (WFS 2.0, 7.7.4.2). Copy the request so its effective query is preserved
	// without mutating the caller or leaking raw URL credentials into the link.
	hasNext := startIndex < totalCount && requestedCount > 0

	if hasNext {
		continuation := GetFeatureRequest{}
		if len(requests) > 0 && requests[0] != nil {
			continuation = *requests[0]
		}
		continuation.ResultType = ResultTypeResults
		nextURL := featurePageURL(baseURL, typeName, startIndex, requestedCount, &continuation)
		fmt.Fprintf(w, `<wfs:FeatureCollection
  xmlns:wfs="%s"
  xmlns:gml="%s"
  xmlns:xsi="%s"
  xsi:schemaLocation="%s"
  timeStamp="%s"
  numberMatched="%d"
  numberReturned="0"
  next="%s">
</wfs:FeatureCollection>
`, NSWfs, NSGml, NSXsi, WfsSchemaLocation, nowISO8601(), totalCount, escapeXML(nextURL))
	} else {
		fmt.Fprintf(w, `<wfs:FeatureCollection
  xmlns:wfs="%s"
  xmlns:gml="%s"
  xmlns:xsi="%s"
  xsi:schemaLocation="%s"
  timeStamp="%s"
  numberMatched="%d"
  numberReturned="0">
</wfs:FeatureCollection>
`, NSWfs, NSGml, NSXsi, WfsSchemaLocation, nowISO8601(), totalCount)
	}
}

// Helper functions

func nowISO8601() string {
	// Use +00:00 instead of Z for maximum compatibility with Java parsers
	return time.Now().UTC().Format("2006-01-02T15:04:05") + "+00:00"
}

func formatValue(val interface{}) string {
	if val == nil {
		return ""
	}
	// Handle time.Time values properly for xsd:dateTime
	if t, ok := val.(time.Time); ok {
		return formatDateTime(t)
	}
	// Handle *time.Time pointers
	if t, ok := val.(*time.Time); ok && t != nil {
		return formatDateTime(*t)
	}
	// Handle string timestamps that may have come from database
	if s, ok := val.(string); ok {
		if t := parseDateTime(s); t != nil {
			return formatDateTime(*t)
		}
	}
	return fmt.Sprintf("%v", val)
}

// formatDateTime formats a time.Time as xsd:dateTime compatible string.
// Uses +00:00 instead of Z for maximum compatibility with Java parsers.
func formatDateTime(t time.Time) string {
	// xsd:dateTime canonical format: YYYY-MM-DDThh:mm:ss+hh:mm
	// Using +00:00 instead of Z for better Java compatibility
	utc := t.UTC()
	return utc.Format("2006-01-02T15:04:05") + "+00:00"
}

// parseDateTime tries to parse a string as a datetime using common formats.
// Returns nil if the string doesn't look like a timestamp.
func parseDateTime(s string) *time.Time {
	// Quick check: timestamps should have 'T' or be in date format
	if len(s) < 10 {
		return nil
	}
	// Check if it looks like a timestamp (contains T or matches date pattern)
	hasT := false
	for i := 0; i < len(s); i++ {
		if s[i] == 'T' {
			hasT = true
			break
		}
	}
	// If no 'T' and doesn't start with a digit, it's not a timestamp
	if !hasT && (s[0] < '0' || s[0] > '9') {
		return nil
	}

	// Go time format reference: Mon Jan 2 15:04:05 -0700 MST 2006
	// -07:00 represents timezone offset, Z07:00 handles both Z and ±hh:mm
	formats := []string{
		time.RFC3339Nano,                      // 2006-01-02T15:04:05.999999999Z07:00
		time.RFC3339,                          // 2006-01-02T15:04:05Z07:00
		"2006-01-02T15:04:05.999999999-07:00", // with fractional seconds and offset
		"2006-01-02T15:04:05-07:00",           // ISO 8601 with offset (e.g., +00:00)
		"2006-01-02T15:04:05Z",                // ISO 8601 with Z
		"2006-01-02 15:04:05.999999999-07:00", // space separator with offset
		"2006-01-02 15:04:05-07:00",           // space separator with offset
		"2006-01-02 15:04:05.999999999",       // space separator, no timezone
		"2006-01-02 15:04:05",                 // space separator, no timezone
		"2006-01-02",                          // date only
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return &t
		}
	}
	return nil
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

func sanitizeXMLName(name string) string {
	if validASCIIXMLName(name) {
		return name
	}
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, " ", "_")
	var cleaned strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-' {
			cleaned.WriteByte(c)
		} else {
			cleaned.WriteByte('_')
		}
	}
	name = cleaned.String()
	if len(name) > 0 {
		first := name[0]
		if (first >= '0' && first <= '9') || first == '-' || first == '.' {
			name = "_" + name
		}
	}
	return name
}

func validASCIIXMLName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if i == 0 {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_') {
				return false
			}
		} else if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

func propertyXMLNames(layerInfo *datasource.LayerInfo) (map[string]string, error) {
	result, used := make(map[string]string), make(map[string]string)
	names := make([]string, 0, len(layerInfo.Properties)+1)
	for _, property := range layerInfo.Properties {
		names = append(names, property.Name)
	}
	if layerInfo.GeometryColumn != "" {
		names = append(names, layerInfo.GeometryColumn)
	}
	for _, name := range names {
		mapped := sanitizeXMLName(name)
		if !validASCIIXMLName(mapped) {
			return nil, fmt.Errorf("property %q cannot be represented as an XML name", name)
		}
		if prior, exists := used[mapped]; exists && prior != name {
			return nil, fmt.Errorf("property names %q and %q collide in XML", prior, name)
		}
		used[mapped], result[name] = name, mapped
	}
	return result, nil
}

func wfsURL(base string, values map[string]string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	query := u.Query()
	for key, value := range values {
		query.Set(key, value)
	}
	u.RawQuery = query.Encode()
	return u.String()
}

// isStandardGMLProperty checks if a property name is a standard GML property
// that should be output with the gml: namespace prefix.
func isStandardGMLProperty(name string) bool {
	// Standard GML properties defined in GML 3.2 AbstractGMLType
	// These should use gml: prefix in WFS output
	switch strings.ToLower(name) {
	case "name", "description", "identifier":
		return true
	default:
		return false
	}
}

// writeGMLStandardProperties writes GML standard properties in the correct schema order.
// Per GML 3.2 AbstractGMLType, the order is: description, identifier, name
// Note: gml:identifier requires a codeSpace attribute per GML schema.
func writeGMLStandardProperties(w http.ResponseWriter, properties map[string]interface{}, indent string) {
	// Output in GML schema order: description, identifier, name
	// Each is optional, so only output if present and non-null

	// 1. gml:description (optional)
	if val, ok := properties["description"]; ok && val != nil {
		fmt.Fprintf(w, "%s<gml:description>%s</gml:description>\n", indent, escapeXML(formatValue(val)))
	}

	// 2. gml:identifier (optional, but requires codeSpace attribute)
	if val, ok := properties["identifier"]; ok && val != nil {
		// codeSpace is required for gml:identifier per GML 3.2 schema (CodeWithAuthorityType)
		fmt.Fprintf(w, "%s<gml:identifier codeSpace=\"http://www.opengis.net/def/nil/OGC/0/unknown\">%s</gml:identifier>\n",
			indent, escapeXML(formatValue(val)))
	}

	// 3. gml:name (0..*, optional)
	if val, ok := properties["name"]; ok && val != nil {
		fmt.Fprintf(w, "%s<gml:name>%s</gml:name>\n", indent, escapeXML(formatValue(val)))
	}
}

// WriteGMLSingleFeature writes a single GML feature for GetFeatureById responses.
// Per WFS 2.0 spec (ISO 19142:2010, cl. 7.9.3.6), GetFeatureById returns just the feature,
// not wrapped in a FeatureCollection.
// The gmlID parameter should be the exact ID that was requested, to ensure the response
// gml:id matches the requested identifier.
func WriteGMLSingleFeature(w http.ResponseWriter, layerInfo *datasource.LayerInfo, featureJSON []byte,
	namespace, nsPrefix string, srid int, typeName string, baseURL string, gmlID string, requests ...*GetFeatureRequest) {

	// Parse GeoJSON feature
	feature, err := decodeFeatureJSON(featureJSON)
	if err != nil {
		WriteException(w, ExceptionNoApplicableCode, "", "Failed to parse feature")
		return
	}

	qn := ParseQName(typeName)
	elementName := qn.LocalPart
	if elementName == "" {
		elementName = sanitizeXMLName(layerInfo.Name)
	}

	// Use the provided gmlID if available, otherwise construct from feature ID
	featureID := gmlID
	if featureID == "" {
		if id, ok := feature["id"]; ok {
			featureID = fmt.Sprintf("%s.%v", elementName, id)
		}
	}

	// Determine effective namespace and prefix
	effectiveNS := namespace
	effectivePrefix := nsPrefix
	if qn.Prefix != "" {
		if qn.Namespace != NSDefault {
			effectiveNS = qn.Namespace
		}
		effectivePrefix = qn.Prefix
	}
	if !validASCIIXMLName(effectivePrefix) {
		WriteException(w, ExceptionInvalidParameterValue, "namespace", "invalid XML namespace prefix")
		return
	}
	elementName = sanitizeXMLName(elementName)
	if !validASCIIXMLName(elementName) {
		WriteException(w, ExceptionNoApplicableCode, "", "feature type cannot be represented as an XML name")
		return
	}
	propertyNames, err := propertyXMLNames(layerInfo)
	if err != nil {
		WriteException(w, ExceptionNoApplicableCode, "", err.Error())
		return
	}

	// Get properties (may be nil for features without properties)
	properties, ok := feature["properties"].(map[string]interface{})
	if !ok {
		properties = make(map[string]interface{})
	}

	// Get geometry (may be nil for non-spatial features)
	geometry, _ := feature["geometry"].(map[string]interface{})

	// Build schema location
	schemaLoc := fmt.Sprintf("%s %s", effectiveNS, wfsURL(baseURL, map[string]string{"service": "WFS", "version": featureRequestVersion(requests), "request": "DescribeFeatureType", "typeNames": typeName}))

	w.Header().Set("Content-Type", "application/gml+xml; version=3.2; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// Write XML declaration
	w.Write([]byte(xml.Header))

	// Write feature element with all required namespaces
	fmt.Fprintf(w, "<%s:%s gml:id=\"%s\"\n", effectivePrefix, elementName, escapeXML(featureID))
	fmt.Fprintf(w, "  xmlns:%s=\"%s\"\n", effectivePrefix, escapeXML(effectiveNS))
	fmt.Fprintf(w, "  xmlns:gml=\"%s\"\n", NSGml)
	fmt.Fprintf(w, "  xmlns:xsi=\"%s\"\n", NSXsi)
	fmt.Fprintf(w, "  xsi:schemaLocation=\"%s\">\n", escapeXML(schemaLoc))

	// Write GML standard properties first, in correct schema order: description, identifier, name
	// Per GML 3.2 AbstractGMLType schema order
	writeGMLStandardProperties(w, properties, "  ")

	// Write application-specific properties (non-GML standard properties)
	for _, prop := range layerInfo.Properties {
		if isStandardGMLProperty(prop.Name) {
			continue // Already written above
		}
		xmlName := propertyNames[prop.Name]
		if val, ok := properties[prop.Name]; ok {
			if val != nil {
				fmt.Fprintf(w, "  <%s:%s>%s</%s:%s>\n",
					effectivePrefix, xmlName, escapeXML(formatValue(val)), effectivePrefix, xmlName)
			} else {
				// For application-specific properties, output nil with xsi:nil="true"
				fmt.Fprintf(w, "  <%s:%s xsi:nil=\"true\"/>\n", effectivePrefix, xmlName)
			}
		}
	}

	// Write geometry
	if geometry != nil && layerInfo.GeometryColumn != "" {
		geometryName := propertyNames[layerInfo.GeometryColumn]
		fmt.Fprintf(w, "  <%s:%s>\n", effectivePrefix, geometryName)
		writeGMLGeometryNoIndent(w, geometry, srid)
		fmt.Fprintf(w, "  </%s:%s>\n", effectivePrefix, geometryName)
	}

	// Close feature element
	fmt.Fprintf(w, "</%s:%s>\n", effectivePrefix, elementName)
}

// writeGMLGeometryNoIndent writes GML geometry without extra indentation for single feature responses.
func writeGMLGeometryNoIndent(w http.ResponseWriter, geometry map[string]interface{}, srid int) {
	geomType, ok := geometry["type"].(string)
	if !ok || geomType == "" {
		return // Invalid geometry
	}
	coords := geometry["coordinates"]
	if coords == nil {
		return // No coordinates
	}
	srsName := SRSNameFromSRID(srid)
	coords = authorityCoordinates(coords, srid)

	switch strings.ToUpper(geomType) {
	case "POINT":
		point, ok := coords.([]interface{})
		if !ok || len(point) < 2 {
			return
		}
		fmt.Fprintf(w, "    <gml:Point srsName=\"%s\">\n", srsName)
		fmt.Fprintf(w, "      <gml:pos>%v %v</gml:pos>\n", point[0], point[1])
		fmt.Fprintf(w, "    </gml:Point>\n")
	case "LINESTRING":
		line, ok := coords.([]interface{})
		if !ok {
			return
		}
		fmt.Fprintf(w, "    <gml:LineString srsName=\"%s\">\n", srsName)
		fmt.Fprintf(w, "      <gml:posList>%s</gml:posList>\n", coordsToPosList(line))
		fmt.Fprintf(w, "    </gml:LineString>\n")
	case "POLYGON":
		rings, ok := coords.([]interface{})
		if !ok {
			return
		}
		fmt.Fprintf(w, "    <gml:Polygon srsName=\"%s\">\n", srsName)
		for i, ring := range rings {
			ringCoords, ok := ring.([]interface{})
			if !ok {
				continue
			}
			if i == 0 {
				fmt.Fprintf(w, "      <gml:exterior>\n")
				fmt.Fprintf(w, "        <gml:LinearRing>\n")
				fmt.Fprintf(w, "          <gml:posList>%s</gml:posList>\n", coordsToPosList(ringCoords))
				fmt.Fprintf(w, "        </gml:LinearRing>\n")
				fmt.Fprintf(w, "      </gml:exterior>\n")
			} else {
				fmt.Fprintf(w, "      <gml:interior>\n")
				fmt.Fprintf(w, "        <gml:LinearRing>\n")
				fmt.Fprintf(w, "          <gml:posList>%s</gml:posList>\n", coordsToPosList(ringCoords))
				fmt.Fprintf(w, "        </gml:LinearRing>\n")
				fmt.Fprintf(w, "      </gml:interior>\n")
			}
		}
		fmt.Fprintf(w, "    </gml:Polygon>\n")
	case "MULTIPOINT":
		points, ok := coords.([]interface{})
		if !ok {
			return
		}
		fmt.Fprintf(w, "    <gml:MultiPoint srsName=\"%s\">\n", srsName)
		for _, point := range points {
			pt, ok := point.([]interface{})
			if !ok || len(pt) < 2 {
				continue
			}
			fmt.Fprintf(w, "      <gml:pointMember>\n")
			fmt.Fprintf(w, "        <gml:Point>\n")
			fmt.Fprintf(w, "          <gml:pos>%v %v</gml:pos>\n", pt[0], pt[1])
			fmt.Fprintf(w, "        </gml:Point>\n")
			fmt.Fprintf(w, "      </gml:pointMember>\n")
		}
		fmt.Fprintf(w, "    </gml:MultiPoint>\n")
	case "MULTILINESTRING":
		lines, ok := coords.([]interface{})
		if !ok {
			return
		}
		fmt.Fprintf(w, "    <gml:MultiCurve srsName=\"%s\">\n", srsName)
		for _, line := range lines {
			lineCoords, ok := line.([]interface{})
			if !ok {
				continue
			}
			fmt.Fprintf(w, "      <gml:curveMember>\n")
			fmt.Fprintf(w, "        <gml:LineString>\n")
			fmt.Fprintf(w, "          <gml:posList>%s</gml:posList>\n", coordsToPosList(lineCoords))
			fmt.Fprintf(w, "        </gml:LineString>\n")
			fmt.Fprintf(w, "      </gml:curveMember>\n")
		}
		fmt.Fprintf(w, "    </gml:MultiCurve>\n")
	case "MULTIPOLYGON":
		polygons, ok := coords.([]interface{})
		if !ok {
			return
		}
		fmt.Fprintf(w, "    <gml:MultiSurface srsName=\"%s\">\n", srsName)
		for _, polygon := range polygons {
			rings, ok := polygon.([]interface{})
			if !ok {
				continue
			}
			fmt.Fprintf(w, "      <gml:surfaceMember>\n")
			fmt.Fprintf(w, "        <gml:Polygon>\n")
			for i, ring := range rings {
				ringCoords, ok := ring.([]interface{})
				if !ok {
					continue
				}
				if i == 0 {
					fmt.Fprintf(w, "          <gml:exterior>\n")
					fmt.Fprintf(w, "            <gml:LinearRing>\n")
					fmt.Fprintf(w, "              <gml:posList>%s</gml:posList>\n", coordsToPosList(ringCoords))
					fmt.Fprintf(w, "            </gml:LinearRing>\n")
					fmt.Fprintf(w, "          </gml:exterior>\n")
				} else {
					fmt.Fprintf(w, "          <gml:interior>\n")
					fmt.Fprintf(w, "            <gml:LinearRing>\n")
					fmt.Fprintf(w, "              <gml:posList>%s</gml:posList>\n", coordsToPosList(ringCoords))
					fmt.Fprintf(w, "            </gml:LinearRing>\n")
					fmt.Fprintf(w, "          </gml:interior>\n")
				}
			}
			fmt.Fprintf(w, "        </gml:Polygon>\n")
			fmt.Fprintf(w, "      </gml:surfaceMember>\n")
		}
		fmt.Fprintf(w, "    </gml:MultiSurface>\n")
	}
}

// WriteValueCollection writes a GetPropertyValue response as a wfs:ValueCollection.
func WriteValueCollection(w http.ResponseWriter, values []string, numberMatched, numberReturned int) {
	WriteValueCollectionMatched(w, values, strconv.Itoa(numberMatched), numberReturned)
}

func WriteValueCollectionMatched(w http.ResponseWriter, values []string, numberMatched string, numberReturned int) {
	writeValueCollectionMembers(w, numberMatched, numberReturned, func() {
		for _, v := range values {
			fmt.Fprintf(w, "  <wfs:member>%s</wfs:member>\n", escapeXML(v))
		}
	})
}

func writeValueCollectionMembers(w http.ResponseWriter, numberMatched string, numberReturned int, members func()) {
	w.Header().Set("Content-Type", "application/gml+xml; version=3.2; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	w.Write([]byte(xml.Header))

	fmt.Fprintf(w, `<wfs:ValueCollection
  xmlns:wfs="%s"
  xmlns:gml="%s"
  xmlns:xsi="%s"
  xsi:schemaLocation="%s"
  timeStamp="%s"
  numberMatched="%s"
  numberReturned="%d">
`, NSWfs, NSGml, NSXsi, WfsSchemaLocation, nowISO8601(), numberMatched, numberReturned)

	members()

	fmt.Fprintln(w, "</wfs:ValueCollection>")
}

func writeGeometryValueCollection(w http.ResponseWriter, geometries []map[string]interface{}, numberMatched string, srid int) {
	writeValueCollectionMembers(w, numberMatched, len(geometries), func() {
		for _, geometry := range geometries {
			if geometry == nil {
				fmt.Fprintln(w, `  <wfs:member xsi:nil="true"/>`)
				continue
			}
			fmt.Fprintln(w, "  <wfs:member>")
			writeGMLGeometry(w, geometry, srid)
			fmt.Fprintln(w, "  </wfs:member>")
		}
	})
}

// WriteValueCollectionHits writes a hits-only GetPropertyValue response.
func WriteValueCollectionHits(w http.ResponseWriter, numberMatched int) {
	w.Header().Set("Content-Type", "application/gml+xml; version=3.2; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	w.Write([]byte(xml.Header))

	fmt.Fprintf(w, `<wfs:ValueCollection
  xmlns:wfs="%s"
  xmlns:gml="%s"
  xmlns:xsi="%s"
  xsi:schemaLocation="%s"
  timeStamp="%s"
  numberMatched="%d"
  numberReturned="0"/>
`, NSWfs, NSGml, NSXsi, WfsSchemaLocation, nowISO8601(), numberMatched)
}

func featureRequestVersion(requests []*GetFeatureRequest) string {
	if len(requests) > 0 && requests[0] != nil {
		return transactionResponseVersion(requests[0].Version)
	}
	return Version200
}
