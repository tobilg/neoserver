package wfs

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/gdalmd"
)

// Special marker for standard GML properties that don't map to actual columns
const gmlPropertyNotMapped = "__GML_NOT_MAPPED__"

// Special marker for geometry properties that can't be used in comparisons
const gmlGeometryProperty = "__GML_GEOMETRY__"

func (c *fesCompiler) compileComparison(comp *FESComparison, op string) (string, error) {
	valueReference := strings.TrimSpace(comp.ValueReference)
	if strings.EqualFold(valueReference, "@gml:id") || strings.EqualFold(valueReference, "gml:id") {
		featureID := strings.TrimSpace(comp.Literal)
		if local, ok := featureIDForType(featureID, c.collectionID); ok {
			featureID = local
		} else if dot := strings.LastIndex(featureID, "."); dot >= 0 {
			identifierType := featureID[:dot]
			if c.collectionID != "" && !strings.EqualFold(stripNSPrefix(identifierType), stripNSPrefix(c.collectionID)) {
				return "", &RequestError{
					Code:    ExceptionInvalidParameterValue,
					Locator: "filter",
					Message: fmt.Sprintf("Feature identifier type '%s' does not match requested type '%s'", identifierType, c.collectionID),
				}
			}
			featureID = featureID[dot+1:]
		}
		c.args = append(c.args, featureID)
		sql := fmt.Sprintf("%s::text %s $%d", c.column(c.idColumn), op, c.paramIdx)
		c.paramIdx++
		return sql, nil
	}

	prop := c.validateProperty(comp.ValueReference)
	if prop == "" {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: fmt.Sprintf("Unknown property: %s", comp.ValueReference),
		}
	}

	// If the property is a standard GML property but not mapped to a column,
	// return FALSE for comparisons (no match possible)
	if prop == gmlPropertyNotMapped {
		return "FALSE", nil
	}

	// If the property is a geometry column, comparison operators are not valid
	// Per WFS spec, return OperationProcessingFailed - the comparison cannot be performed
	// on a geometry (geometry comparisons need spatial operators like BBOX, Intersects)
	if prop == gmlGeometryProperty {
		propName := stripNSPrefix(comp.ValueReference)
		return "", &RequestError{
			Code:    ExceptionOperationProcessingFailed,
			Locator: "",
			Message: fmt.Sprintf("invalid operand: property '%s' is a geometry and cannot be used in comparison operators", propName),
		}
	}

	c.args = append(c.args, comp.Literal)
	sql := fmt.Sprintf("%s %s $%d", c.column(prop), op, c.paramIdx)
	c.paramIdx++

	return sql, nil
}

func (c *fesCompiler) compileLike(like *FESPropertyIsLike) (string, error) {
	prop := c.validateProperty(like.ValueReference)
	if prop == "" {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: fmt.Sprintf("Unknown property: %s", like.ValueReference),
		}
	}

	// If the property is a standard GML property but not mapped to a column,
	// return FALSE for LIKE comparisons (no match possible)
	if prop == gmlPropertyNotMapped {
		return "FALSE", nil
	}

	// If the property is a geometry column, LIKE is not valid
	// Per WFS spec, return OperationProcessingFailed - the comparison cannot be performed
	if prop == gmlGeometryProperty {
		propName := stripNSPrefix(like.ValueReference)
		return "", &RequestError{
			Code:    ExceptionOperationProcessingFailed,
			Locator: "",
			Message: fmt.Sprintf("invalid operand: property '%s' is a geometry and cannot be used in LIKE comparisons", propName),
		}
	}

	// Convert FES wildcards to SQL LIKE wildcards
	pattern := like.Literal
	wildCard := like.WildCard
	if wildCard == "" {
		wildCard = "*"
	}
	singleChar := like.SingleChar
	if singleChar == "" {
		singleChar = "?"
	}

	// Replace wildcards
	pattern = strings.ReplaceAll(pattern, wildCard, "%")
	pattern = strings.ReplaceAll(pattern, singleChar, "_")

	c.args = append(c.args, pattern)

	// Handle case sensitivity
	if like.MatchCase == "false" {
		sql := fmt.Sprintf("%s ILIKE $%d", c.column(prop), c.paramIdx)
		c.paramIdx++
		return sql, nil
	}

	sql := fmt.Sprintf("%s LIKE $%d", c.column(prop), c.paramIdx)
	c.paramIdx++
	return sql, nil
}

func (c *fesCompiler) compileIsNull(isNull *FESPropertyIsNull) (string, error) {
	prop := c.validateProperty(isNull.ValueReference)
	if prop == "" {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: fmt.Sprintf("Unknown property: %s", isNull.ValueReference),
		}
	}

	// If the property is a standard GML property but not mapped to a column,
	// return TRUE for IS NULL (property doesn't exist = is null)
	if prop == gmlPropertyNotMapped {
		return "TRUE", nil
	}

	// For geometry properties, IS NULL should check if the geometry is null
	// This is actually a valid operation, so use the actual geometry column
	if prop == gmlGeometryProperty {
		return fmt.Sprintf("%s IS NULL", c.column(c.geomProp)), nil
	}

	return fmt.Sprintf("%s IS NULL", c.column(prop)), nil
}

// compileIsNil compiles a PropertyIsNil predicate.
// PropertyIsNil checks if a property has xsi:nil="true", which in our database
// model is equivalent to IS NULL.
func (c *fesCompiler) compileIsNil(isNil *FESPropertyIsNil) (string, error) {
	prop := c.validateProperty(isNil.ValueReference)
	if prop == "" {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: fmt.Sprintf("Unknown property: %s", isNil.ValueReference),
		}
	}

	// If the property is a standard GML property but not mapped to a column,
	// return TRUE for IS NIL (property doesn't exist = is nil)
	if prop == gmlPropertyNotMapped {
		return "TRUE", nil
	}

	// For geometry properties, IS NIL should check if the geometry is null
	if prop == gmlGeometryProperty {
		return fmt.Sprintf("%s IS NULL", c.column(c.geomProp)), nil
	}

	return fmt.Sprintf("%s IS NULL", c.column(prop)), nil
}

func (c *fesCompiler) compileBetween(between *FESPropertyIsBetween) (string, error) {
	prop := c.validateProperty(between.ValueReference)
	if prop == "" {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: fmt.Sprintf("Unknown property: %s", between.ValueReference),
		}
	}

	// If the property is a standard GML property but not mapped to a column,
	// return FALSE for BETWEEN comparisons (no match possible)
	if prop == gmlPropertyNotMapped {
		return "FALSE", nil
	}

	// If the property is a geometry column, BETWEEN is not valid
	// Per WFS spec, return OperationProcessingFailed - the comparison cannot be performed
	if prop == gmlGeometryProperty {
		propName := stripNSPrefix(between.ValueReference)
		return "", &RequestError{
			Code:    ExceptionOperationProcessingFailed,
			Locator: "",
			Message: fmt.Sprintf("invalid operand: property '%s' is a geometry and cannot be used in BETWEEN comparisons", propName),
		}
	}

	c.args = append(c.args, between.LowerBoundary.Literal, between.UpperBoundary.Literal)
	sql := fmt.Sprintf("%s BETWEEN $%d AND $%d", c.column(prop), c.paramIdx, c.paramIdx+1)
	c.paramIdx += 2

	return sql, nil
}

func (c *fesCompiler) compileBBOX(bbox *FESBBOX) (string, error) {
	// Parse envelope
	lower := strings.Fields(bbox.Envelope.LowerCorner)
	upper := strings.Fields(bbox.Envelope.UpperCorner)

	if len(lower) < 2 || len(upper) < 2 {
		return "", fmt.Errorf("invalid BBOX envelope")
	}

	srid, swap, err := inputGeometryCRS(bbox.Envelope.SrsName, c.sourceSRID)
	if err != nil {
		return "", err
	}

	geomCol := c.geomProp
	if bbox.ValueReference != "" {
		// Validate that the value reference is a geometry property
		geomCol = c.validateGeometryProperty(bbox.ValueReference)
		if geomCol == "" {
			// Not a valid geometry property - per WFS spec (ISO 19142 section 8.3),
			// return InvalidParameterValue with HTTP 400 for invalid filter operands
			propName := stripNSPrefix(bbox.ValueReference)
			return "", &RequestError{
				Code:    ExceptionInvalidParameterValue,
				Locator: "filter",
				Message: fmt.Sprintf("invalid geometry operand: property '%s' is not a geometry", propName),
			}
		}
	}

	coordinates := make([]float64, 4)
	for i, value := range []string{lower[0], lower[1], upper[0], upper[1]} {
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return "", fmt.Errorf("invalid BBOX coordinate")
		}
		coordinates[i] = n
	}
	if coordinates[0] > coordinates[2] || coordinates[1] > coordinates[3] {
		return "", fmt.Errorf("invalid BBOX envelope bounds")
	}
	if swap {
		coordinates[0], coordinates[1] = coordinates[1], coordinates[0]
		coordinates[2], coordinates[3] = coordinates[3], coordinates[2]
	}
	for _, n := range coordinates {
		c.args = append(c.args, n)
	}
	envelope := fmt.Sprintf("ST_MakeEnvelope($%d, $%d, $%d, $%d, %d)", c.paramIdx, c.paramIdx+1, c.paramIdx+2, c.paramIdx+3, srid)
	if c.dialect == datasource.SQLDuckDB {
		envelope = fmt.Sprintf("ST_MakeEnvelope($%d, $%d, $%d, $%d)", c.paramIdx, c.paramIdx+1, c.paramIdx+2, c.paramIdx+3)
	}
	sql := fmt.Sprintf("ST_Intersects(%s, %s)", c.column(geomCol), c.transformGeometry(envelope, srid))

	c.paramIdx += 4
	return sql, nil
}

func (c *fesCompiler) compileSpatial(spatial *FESSpatial, function string) (string, error) {
	geomCol := c.geomProp
	if spatial.ValueReference != "" {
		// Validate that the value reference is a geometry property
		geomCol = c.validateGeometryProperty(spatial.ValueReference)
		if geomCol == "" {
			// Not a valid geometry property - per WFS spec (ISO 19142 section 8.3),
			// return InvalidParameterValue with HTTP 400 for invalid filter operands
			propName := stripNSPrefix(spatial.ValueReference)
			return "", &RequestError{
				Code:    ExceptionInvalidParameterValue,
				Locator: "filter",
				Message: fmt.Sprintf("invalid geometry operand: property '%s' is not a geometry", propName),
			}
		}
	}

	// Build geometry from filter
	geomWKT, geomSRID, err := c.buildGeometry(spatial)
	if err != nil {
		return "", err
	}

	c.args = append(c.args, geomWKT)

	sql := fmt.Sprintf("%s(%s, %s)", function, c.column(geomCol), c.geometryLiteral(c.paramIdx, geomSRID))

	c.paramIdx++
	return sql, nil
}

func (c *fesCompiler) compileDWithin(dwithin *FESDWithin) (string, error) {
	geomCol := c.geomProp
	if dwithin.ValueReference != "" {
		// Validate that the value reference is a geometry property
		geomCol = c.validateGeometryProperty(dwithin.ValueReference)
		if geomCol == "" {
			// Not a valid geometry property - per WFS spec (ISO 19142 section 8.3),
			// return InvalidParameterValue with HTTP 400 for invalid filter operands
			propName := stripNSPrefix(dwithin.ValueReference)
			return "", &RequestError{
				Code:    ExceptionInvalidParameterValue,
				Locator: "filter",
				Message: fmt.Sprintf("invalid geometry operand: property '%s' is not a geometry", propName),
			}
		}
	}

	if dwithin.Point == nil {
		return "", fmt.Errorf("DWithin requires a Point geometry")
	}

	wkt, srid, err := c.buildGeometry(&FESSpatial{Point: dwithin.Point})
	if err != nil {
		return "", err
	}
	c.args = append(c.args, wkt, dwithin.Distance.Value)

	sql := fmt.Sprintf("ST_DWithin(%s, %s, $%d)", c.column(geomCol), c.geometryLiteral(c.paramIdx, srid), c.paramIdx+1)

	c.paramIdx += 2
	return sql, nil
}

func (c *fesCompiler) compileResourceIds(ids []FESResourceId) (string, error) {
	if len(ids) == 0 {
		return "", nil
	}

	var placeholders []string
	for _, id := range ids {
		// Extract the feature ID from the rid
		// CITE test format: cite_RoadSegments.1 (namespace_TypeName.featureId)
		// Also handles: schema.table.id, table.id, etc.
		rid := id.Rid
		var featureID string
		var ridCollectionID string

		// Prefer the actual advertised name, preserving underscores and dots
		// in both publication names and local IDs. Only recognize the legacy
		// namespace spelling when no actual publication prefix matches.
		if local, ok := featureIDForType(rid, c.collectionID); ok {
			ridCollectionID, featureID = c.collectionID, local
		} else if underscoreIdx := strings.Index(rid, "_"); underscoreIdx > 0 && (rid[:underscoreIdx] == "cite" || rid[:underscoreIdx] == "app" || strings.HasPrefix(c.collectionID, rid[:underscoreIdx]+":")) {
			// Format: prefix_TypeName.id (e.g., "cite_RoadSegments.1")
			dotIdx := strings.LastIndex(rid, ".")
			if dotIdx > underscoreIdx {
				// Extract collection part (cite_RoadSegments -> cite:RoadSegments)
				prefix := rid[:underscoreIdx]
				typePart := rid[underscoreIdx+1 : dotIdx]
				ridCollectionID = prefix + ":" + typePart
				featureID = rid[dotIdx+1:]
			} else {
				// No dot after underscore, use whole thing as ID
				featureID = rid
			}
		} else {
			// Standard dot-separated format
			parts := strings.Split(rid, ".")
			if len(parts) >= 3 {
				// Format: schema.table.id (e.g., "public.places.3")
				ridCollectionID = parts[0] + "." + parts[1]
				featureID = strings.Join(parts[2:], ".")
			} else if len(parts) == 2 {
				// Format: table.id or TypeName.id
				ridCollectionID = parts[0]
				featureID = parts[1]
			} else {
				// Just use the whole rid
				featureID = rid
			}
		}

		// If we have a collection ID to validate against, check for mismatch
		if c.collectionID != "" && ridCollectionID != "" {
			// Normalize both for comparison (strip namespace prefix if present)
			ridLocal := stripNSPrefix(ridCollectionID)
			queryLocal := stripNSPrefix(c.collectionID)

			// Per WFS 2.0 spec, if ResourceId type doesn't match the requested type, return an error
			if !strings.EqualFold(ridLocal, queryLocal) {
				return "", &RequestError{
					Code:    ExceptionInvalidParameterValue,
					Locator: "RESOURCEID",
					Message: fmt.Sprintf("ResourceId type '%s' does not match requested type '%s'", ridCollectionID, c.collectionID),
				}
			}
		}

		c.args = append(c.args, featureID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", c.paramIdx))
		c.paramIdx++
	}

	// If no valid IDs remain (all were skipped due to type mismatch), return FALSE to get empty results
	if len(placeholders) == 0 {
		return "FALSE", nil
	}

	// Use table alias 't' - cast id column to text to handle both integer and string IDs
	// This prevents "invalid input syntax for type integer" errors when ID is a UUID/string
	return fmt.Sprintf("%s::text IN (%s)", c.column(c.idColumn), strings.Join(placeholders, ", ")), nil
}

// Formal EPSG URNs/URLs use authority order, just like GML output and WFS
// transactions. The legacy EPSG:code spelling and CRS84 explicitly remain XY.
func inputGeometryCRS(value string, defaultSRID int) (int, bool, error) {
	if strings.TrimSpace(value) == "" {
		return defaultSRID, false, nil
	}
	normalized, srid, err := normalizedInputCRS(value)
	if err != nil {
		return 0, false, err
	}
	if strings.HasPrefix(normalized, "EPSG:") {
		return srid, false, nil
	}
	swap, err := gdalmd.AuthorityAxisSwap(srid)
	return srid, swap, err
}

func (c *fesCompiler) buildGeometry(spatial *FESSpatial) (string, int, error) {
	var srsName string
	switch {
	case spatial.Point != nil:
		srsName = spatial.Point.SrsName
	case spatial.LineString != nil:
		srsName = spatial.LineString.SrsName
	case spatial.Polygon != nil:
		srsName = spatial.Polygon.SrsName
	case spatial.Envelope != nil:
		srsName = spatial.Envelope.SrsName
	}
	srid, swap, err := inputGeometryCRS(srsName, c.sourceSRID)
	if err != nil {
		return "", 0, err
	}

	if spatial.Point != nil {
		coords := strings.Fields(spatial.Point.Pos)
		if len(coords) < 2 {
			return "", 0, fmt.Errorf("invalid Point: requires 2 coordinates")
		}
		// Validate coordinates are numeric
		if !validateNumericCoords(coords[:2]) {
			return "", 0, fmt.Errorf("invalid Point: non-numeric coordinates")
		}
		if swap {
			coords[0], coords[1] = coords[1], coords[0]
		}
		return fmt.Sprintf("POINT(%s %s)", coords[0], coords[1]), srid, nil
	}

	if spatial.LineString != nil {
		posList := strings.TrimSpace(spatial.LineString.PosList)
		if posList == "" {
			return "", 0, fmt.Errorf("invalid LineString: empty posList")
		}
		// LineString requires at least 2 points (4 coordinate values)
		fields := strings.Fields(posList)
		if len(fields) < 4 {
			return "", 0, fmt.Errorf("invalid LineString: requires at least 2 points (got %d coordinate values)", len(fields))
		}
		coords := c.posListToWKTCoords(posList, false, swap) // LineString doesn't need ring closure
		if coords == "" {
			return "", 0, fmt.Errorf("invalid LineString: could not parse coordinates")
		}
		return fmt.Sprintf("LINESTRING(%s)", coords), srid, nil
	}

	if spatial.Polygon != nil {
		posList := strings.TrimSpace(spatial.Polygon.Exterior.LinearRing.PosList)
		if posList == "" {
			return "", 0, fmt.Errorf("invalid Polygon: empty posList")
		}
		// Polygon requires at least 3 unique points (6 coordinate values)
		fields := strings.Fields(posList)
		if len(fields) < 6 {
			return "", 0, fmt.Errorf("invalid Polygon: requires at least 3 points (got %d coordinate values)", len(fields))
		}
		coords := c.posListToWKTCoords(posList, true, swap) // Polygon ring needs closure
		if coords == "" {
			return "", 0, fmt.Errorf("invalid Polygon: could not parse coordinates")
		}
		return fmt.Sprintf("POLYGON((%s))", coords), srid, nil
	}

	if spatial.Envelope != nil {
		lower := strings.Fields(spatial.Envelope.LowerCorner)
		upper := strings.Fields(spatial.Envelope.UpperCorner)
		if len(lower) < 2 || len(upper) < 2 {
			return "", 0, fmt.Errorf("invalid Envelope: requires 2 coordinates for each corner")
		}
		// Validate coordinates are numeric
		if !validateNumericCoords(lower[:2]) || !validateNumericCoords(upper[:2]) {
			return "", 0, fmt.Errorf("invalid Envelope: non-numeric coordinates")
		}
		if swap {
			lower[0], lower[1] = lower[1], lower[0]
			upper[0], upper[1] = upper[1], upper[0]
		}
		// Convert envelope to polygon
		wkt := fmt.Sprintf("POLYGON((%s %s, %s %s, %s %s, %s %s, %s %s))",
			lower[0], lower[1], upper[0], lower[1], upper[0], upper[1], lower[0], upper[1], lower[0], lower[1])
		return wkt, srid, nil
	}

	return "", 0, fmt.Errorf("unsupported geometry type in spatial filter")
}

func (c *fesCompiler) posListToWKTCoords(posList string, closeRing, swap bool) string {
	fields := strings.Fields(posList)
	var coords []string
	for i := 0; i < len(fields)-1; i += 2 {
		// Validate that coordinates are numeric
		if _, err := strconv.ParseFloat(fields[i], 64); err != nil {
			return "" // Invalid coordinate
		}
		if _, err := strconv.ParseFloat(fields[i+1], 64); err != nil {
			return "" // Invalid coordinate
		}
		if swap {
			fields[i], fields[i+1] = fields[i+1], fields[i]
		}
		coords = append(coords, fmt.Sprintf("%s %s", fields[i], fields[i+1]))
	}

	// For polygon rings, ensure the ring is closed (first point == last point)
	if closeRing && len(coords) > 0 {
		if coords[0] != coords[len(coords)-1] {
			coords = append(coords, coords[0])
		}
	}

	return strings.Join(coords, ", ")
}

// validateNumericCoords checks if coordinate strings are valid numbers
func validateNumericCoords(coords []string) bool {
	for _, c := range coords {
		if _, err := strconv.ParseFloat(c, 64); err != nil {
			return false
		}
	}
	return true
}

func (c *fesCompiler) validateProperty(prop string) string {
	// Strip namespace prefix
	prop = stripNSPrefix(prop)

	// Handle standard GML properties
	// gml:boundedBy represents the bounding box - for comparison operators, this is invalid
	// (geometry comparisons like boundedBy <= 'literal' don't make sense)
	// Spatial operators use validateGeometryProperty instead
	if prop == "boundedBy" && c.geomProp != "" {
		return gmlGeometryProperty
	}

	// Check if this is the geometry column - geometry properties can't be used
	// in comparison operators (=, <, >, LIKE, BETWEEN, etc.)
	// They should only be used in spatial operators (BBOX, Intersects, etc.)
	if c.geomProp != "" && prop == c.geomProp {
		return gmlGeometryProperty
	}

	// gml:name and gml:description are standard GML properties
	// Map them to the corresponding property name if it exists in the collection
	if prop == "name" || prop == "description" {
		// Check if this property exists in allowed properties (case-insensitive)
		if c.allowedProperties != nil {
			// Try exact match first
			if _, ok := c.allowedProperties[prop]; ok {
				return prop
			}
			// Try uppercase
			upperProp := strings.ToUpper(prop)
			if _, ok := c.allowedProperties[upperProp]; ok {
				return upperProp
			}
			// Try title case
			titleProp := strings.Title(prop)
			if _, ok := c.allowedProperties[titleProp]; ok {
				return titleProp
			}
			// Standard GML property but not mapped to a column - return marker
			// This allows PropertyIsNull to return TRUE and comparisons to return FALSE
			return gmlPropertyNotMapped
		}
		return prop
	}

	// Check if property is allowed
	if c.allowedProperties != nil {
		if _, ok := c.allowedProperties[prop]; !ok {
			return ""
		}
	}

	return prop
}

// validateGeometryProperty validates that a property reference refers to a geometry column.
// Returns the geometry column name if valid, empty string if not a geometry property.
func (c *fesCompiler) validateGeometryProperty(prop string) string {
	// Strip namespace prefix
	prop = stripNSPrefix(prop)

	// gml:boundedBy maps to the geometry column
	if prop == "boundedBy" && c.geomProp != "" {
		return c.geomProp
	}

	// If it matches the geometry column, it's valid
	if c.geomProp != "" && prop == c.geomProp {
		return c.geomProp
	}

	// Standard GML properties like name, description are NOT geometry
	if prop == "name" || prop == "description" {
		return ""
	}

	// If the property is in the allowed properties list, it's a regular property (not geometry)
	if c.allowedProperties != nil {
		if _, ok := c.allowedProperties[prop]; ok {
			return "" // It's a known non-geometry property
		}
	}

	// Unknown property - return empty (could be an error, but we return FALSE for CITE)
	return ""
}

// stripNSPrefix strips the namespace prefix from a property name (e.g., "tns:geom" -> "geom")
func stripNSPrefix(name string) string {
	if idx := strings.LastIndex(name, ":"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// quoteIdent quotes an identifier for use in SQL queries.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// Temporal predicate compilation methods

// validateTemporalProperty validates a temporal property and returns the column name.
func (c *fesCompiler) validateTemporalProperty(valueRef string) (string, error) {
	prop := c.validateProperty(valueRef)
	if prop == "" {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: fmt.Sprintf("Unknown property: %s", valueRef),
		}
	}
	if prop == gmlPropertyNotMapped {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: fmt.Sprintf("Property '%s' is not mapped to a column", valueRef),
		}
	}
	if prop == gmlGeometryProperty {
		propName := stripNSPrefix(valueRef)
		return "", &RequestError{
			Code:    ExceptionOperationProcessingFailed,
			Locator: "",
			Message: fmt.Sprintf("invalid operand: property '%s' is a geometry and cannot be used in temporal operators", propName),
		}
	}
	return prop, nil
}

// getTimeValue extracts the time value from either TimeInstant or TimePeriod.
// For TimeInstant, returns the timePosition.
// For TimePeriod, returns beginPosition for "start" mode and endPosition for "end" mode.
func getTimeValue(instant *GMLTimeInstant, period *GMLTimePeriod, mode string) string {
	if instant != nil {
		return instant.TimePosition
	}
	if period != nil {
		if mode == "start" {
			return period.BeginPosition
		}
		return period.EndPosition
	}
	return ""
}

// compileAfter compiles fes:After - property value is after the given instant/period end
func (c *fesCompiler) compileAfter(after *FESTemporalAfter) (string, error) {
	prop, err := c.validateTemporalProperty(after.ValueReference)
	if err != nil {
		return "", err
	}

	timeValue := getTimeValue(after.TimeInstant, after.TimePeriod, "end")
	if timeValue == "" {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "After requires TimeInstant or TimePeriod",
		}
	}

	c.args = append(c.args, timeValue)
	sql := fmt.Sprintf("%s > $%d::timestamptz", c.column(prop), c.paramIdx)
	c.paramIdx++
	return sql, nil
}

// compileBefore compiles fes:Before - property value is before the given instant/period begin
func (c *fesCompiler) compileBefore(before *FESTemporalBefore) (string, error) {
	prop, err := c.validateTemporalProperty(before.ValueReference)
	if err != nil {
		return "", err
	}

	timeValue := getTimeValue(before.TimeInstant, before.TimePeriod, "start")
	if timeValue == "" {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "Before requires TimeInstant or TimePeriod",
		}
	}

	c.args = append(c.args, timeValue)
	sql := fmt.Sprintf("%s < $%d::timestamptz", c.column(prop), c.paramIdx)
	c.paramIdx++
	return sql, nil
}

// compileDuring compiles fes:During - property value is within the given period
func (c *fesCompiler) compileDuring(during *FESTemporalDuring) (string, error) {
	prop, err := c.validateTemporalProperty(during.ValueReference)
	if err != nil {
		return "", err
	}

	if during.TimePeriod == nil {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "During requires TimePeriod",
		}
	}

	c.args = append(c.args, during.TimePeriod.BeginPosition, during.TimePeriod.EndPosition)
	sql := fmt.Sprintf("%s > $%d::timestamptz AND %s < $%d::timestamptz",
		c.column(prop), c.paramIdx, c.column(prop), c.paramIdx+1)
	c.paramIdx += 2
	return "(" + sql + ")", nil
}

// compileBegins compiles fes:Begins - property instant equals the start of the given period
func (c *fesCompiler) compileBegins(begins *FESTemporalBegins) (string, error) {
	prop, err := c.validateTemporalProperty(begins.ValueReference)
	if err != nil {
		return "", err
	}

	if begins.TimePeriod == nil {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "Begins requires TimePeriod",
		}
	}

	c.args = append(c.args, begins.TimePeriod.BeginPosition)
	sql := fmt.Sprintf("%s = $%d::timestamptz", c.column(prop), c.paramIdx)
	c.paramIdx++
	return sql, nil
}

// compileBegunBy compiles fes:BegunBy - the given instant/period begins the property period
func (c *fesCompiler) compileBegunBy(begunBy *FESTemporalBegunBy) (string, error) {
	prop, err := c.validateTemporalProperty(begunBy.ValueReference)
	if err != nil {
		return "", err
	}

	if begunBy.TimePeriod == nil {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "BegunBy requires TimePeriod",
		}
	}

	c.args = append(c.args, begunBy.TimePeriod.BeginPosition)
	sql := fmt.Sprintf("%s = $%d::timestamptz", c.column(prop), c.paramIdx)
	c.paramIdx++
	return sql, nil
}

// compileTContains compiles fes:TContains - property period contains the given instant/period
func (c *fesCompiler) compileTContains(tcontains *FESTemporalTContains) (string, error) {
	prop, err := c.validateTemporalProperty(tcontains.ValueReference)
	if err != nil {
		return "", err
	}

	if tcontains.TimeInstant != nil {
		c.args = append(c.args, tcontains.TimeInstant.TimePosition)
		// For a range column to contain an instant, we'd need range types
		// For a single timestamp column, this doesn't make sense
		// We'll interpret it as equality
		sql := fmt.Sprintf("%s = $%d::timestamptz", c.column(prop), c.paramIdx)
		c.paramIdx++
		return sql, nil
	}

	if tcontains.TimePeriod != nil {
		// Property value should be between begin and end of the given period
		// (interpreting as: does the instant fall within the period)
		c.args = append(c.args, tcontains.TimePeriod.BeginPosition, tcontains.TimePeriod.EndPosition)
		sql := fmt.Sprintf("%s >= $%d::timestamptz AND %s <= $%d::timestamptz",
			c.column(prop), c.paramIdx, c.column(prop), c.paramIdx+1)
		c.paramIdx += 2
		return "(" + sql + ")", nil
	}

	return "", &RequestError{
		Code:    ExceptionInvalidParameterValue,
		Locator: "filter",
		Message: "TContains requires TimeInstant or TimePeriod",
	}
}

// compileTEquals compiles fes:TEquals - property value equals the given instant/period
func (c *fesCompiler) compileTEquals(tequals *FESTemporalTEquals) (string, error) {
	prop, err := c.validateTemporalProperty(tequals.ValueReference)
	if err != nil {
		return "", err
	}

	timeValue := getTimeValue(tequals.TimeInstant, tequals.TimePeriod, "start")
	if timeValue == "" {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "TEquals requires TimeInstant or TimePeriod",
		}
	}

	c.args = append(c.args, timeValue)
	sql := fmt.Sprintf("%s = $%d::timestamptz", c.column(prop), c.paramIdx)
	c.paramIdx++
	return sql, nil
}

// compileTOverlaps compiles fes:TOverlaps - property period overlaps the given period
func (c *fesCompiler) compileTOverlaps(toverlaps *FESTemporalTOverlaps) (string, error) {
	prop, err := c.validateTemporalProperty(toverlaps.ValueReference)
	if err != nil {
		return "", err
	}

	if toverlaps.TimePeriod == nil {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "TOverlaps requires TimePeriod",
		}
	}

	// For single timestamp column, overlaps if within range
	c.args = append(c.args, toverlaps.TimePeriod.BeginPosition, toverlaps.TimePeriod.EndPosition)
	sql := fmt.Sprintf("%s >= $%d::timestamptz AND %s <= $%d::timestamptz",
		c.column(prop), c.paramIdx, c.column(prop), c.paramIdx+1)
	c.paramIdx += 2
	return "(" + sql + ")", nil
}

// compileMeets compiles fes:Meets - property period meets (ends at the start of) the given period
func (c *fesCompiler) compileMeets(meets *FESTemporalMeets) (string, error) {
	prop, err := c.validateTemporalProperty(meets.ValueReference)
	if err != nil {
		return "", err
	}

	if meets.TimePeriod == nil {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "Meets requires TimePeriod",
		}
	}

	c.args = append(c.args, meets.TimePeriod.BeginPosition)
	sql := fmt.Sprintf("%s = $%d::timestamptz", c.column(prop), c.paramIdx)
	c.paramIdx++
	return sql, nil
}

// compileOverlappedBy compiles fes:OverlappedBy - property period is overlapped by the given period
func (c *fesCompiler) compileOverlappedBy(overlappedBy *FESTemporalOverlappedBy) (string, error) {
	prop, err := c.validateTemporalProperty(overlappedBy.ValueReference)
	if err != nil {
		return "", err
	}

	if overlappedBy.TimePeriod == nil {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "OverlappedBy requires TimePeriod",
		}
	}

	// For single timestamp column, overlappedBy if within range
	c.args = append(c.args, overlappedBy.TimePeriod.BeginPosition, overlappedBy.TimePeriod.EndPosition)
	sql := fmt.Sprintf("%s >= $%d::timestamptz AND %s <= $%d::timestamptz",
		c.column(prop), c.paramIdx, c.column(prop), c.paramIdx+1)
	c.paramIdx += 2
	return "(" + sql + ")", nil
}

// compileMetBy compiles fes:MetBy - property period is met by (starts at the end of) the given period
func (c *fesCompiler) compileMetBy(metBy *FESTemporalMetBy) (string, error) {
	prop, err := c.validateTemporalProperty(metBy.ValueReference)
	if err != nil {
		return "", err
	}

	if metBy.TimePeriod == nil {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "MetBy requires TimePeriod",
		}
	}

	c.args = append(c.args, metBy.TimePeriod.EndPosition)
	sql := fmt.Sprintf("%s = $%d::timestamptz", c.column(prop), c.paramIdx)
	c.paramIdx++
	return sql, nil
}

// compileEnds compiles fes:Ends - property instant equals the end of the given period
func (c *fesCompiler) compileEnds(ends *FESTemporalEnds) (string, error) {
	prop, err := c.validateTemporalProperty(ends.ValueReference)
	if err != nil {
		return "", err
	}

	if ends.TimePeriod == nil {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "Ends requires TimePeriod",
		}
	}

	c.args = append(c.args, ends.TimePeriod.EndPosition)
	sql := fmt.Sprintf("%s = $%d::timestamptz", c.column(prop), c.paramIdx)
	c.paramIdx++
	return sql, nil
}

// compileEndedBy compiles fes:EndedBy - the given instant/period ends the property period
func (c *fesCompiler) compileEndedBy(endedBy *FESTemporalEndedBy) (string, error) {
	prop, err := c.validateTemporalProperty(endedBy.ValueReference)
	if err != nil {
		return "", err
	}

	if endedBy.TimePeriod == nil {
		return "", &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "filter",
			Message: "EndedBy requires TimePeriod",
		}
	}

	c.args = append(c.args, endedBy.TimePeriod.EndPosition)
	sql := fmt.Sprintf("%s = $%d::timestamptz", c.column(prop), c.paramIdx)
	c.paramIdx++
	return sql, nil
}

// compileAnyInteracts compiles fes:AnyInteracts - property has any temporal interaction with given instant/period
func (c *fesCompiler) compileAnyInteracts(anyInteracts *FESTemporalAnyInteracts) (string, error) {
	prop, err := c.validateTemporalProperty(anyInteracts.ValueReference)
	if err != nil {
		return "", err
	}

	if anyInteracts.TimeInstant != nil {
		// Any interaction with an instant - just check if equal or in same time frame
		c.args = append(c.args, anyInteracts.TimeInstant.TimePosition)
		// For AnyInteracts with an instant, we check equality (or approximate equality)
		sql := fmt.Sprintf("%s = $%d::timestamptz", c.column(prop), c.paramIdx)
		c.paramIdx++
		return sql, nil
	}

	if anyInteracts.TimePeriod != nil {
		// Any interaction with a period - check if value is within or overlaps the range
		c.args = append(c.args, anyInteracts.TimePeriod.BeginPosition, anyInteracts.TimePeriod.EndPosition)
		sql := fmt.Sprintf("%s >= $%d::timestamptz AND %s <= $%d::timestamptz",
			c.column(prop), c.paramIdx, c.column(prop), c.paramIdx+1)
		c.paramIdx += 2
		return "(" + sql + ")", nil
	}

	return "", &RequestError{
		Code:    ExceptionInvalidParameterValue,
		Locator: "filter",
		Message: "AnyInteracts requires TimeInstant or TimePeriod",
	}
}
