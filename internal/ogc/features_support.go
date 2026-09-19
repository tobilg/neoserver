package ogc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/rastergrid"
	"github.com/tobilg/neoserver/internal/filter"
	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

const (
	queryablesRel  = "http://www.opengis.net/def/rel/ogc/1.0/queryables"
	queryablesType = "application/schema+json"
	gregorianTRS   = "http://www.opengis.net/def/uom/ISO-8601/0/Gregorian"
)

var itemsQueryParameterNames = []string{
	"limit", "offset", "bbox", "datetime", "crs", "bbox-crs", "filter",
	"filter-lang", "filter-crs", "properties", "sortby",
}

var (
	itemsQueryParameters = queryParameterSet(itemsQueryParameterNames...)
	itemQueryParameters  = queryParameterSet("crs")
)

func queryParameterSet(names ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(names))
	for _, name := range names {
		result[name] = struct{}{}
	}
	return result
}

func (h *workspaceHandler) validateQueryParameters(w http.ResponseWriter, r *http.Request, allowed map[string]struct{}) bool {
	for name := range r.URL.Query() {
		if _, ok := allowed[name]; ok {
			continue
		}
		if name == "apikey" && h.cfg.Auth.AllowAPIKeyInQuery {
			continue
		}
		writeErr(w, http.StatusBadRequest, "InvalidParameterValue", fmt.Sprintf("unsupported query parameter %q", name))
		return false
	}
	return true
}

func advertisedCRSs(layer *workspace.Layer) []string {
	crss := []string{query.CRS84URI}
	storage := query.CRSURIFromSRID(layer.CRSDefault)
	if layer.CRSDefault != 0 && storage != query.CRS84URI {
		crss = append(crss, storage)
	}
	return crss
}

func storageCRS(layer *workspace.Layer) string {
	if layer.CRSDefault == 0 {
		return query.CRS84URI
	}
	return query.CRSURIFromSRID(layer.CRSDefault)
}

func parseAdvertisedCRS(value string, layer *workspace.Layer) (int, error) {
	if value == "" {
		return 4326, nil
	}
	srid, err := query.ParseCRS(value)
	if err != nil {
		return 0, err
	}
	if srid != 4326 && srid != layer.CRSDefault {
		return 0, fmt.Errorf("crs %q is not advertised by this collection", value)
	}
	return srid, nil
}

func contentCRSValue(srid int) string {
	return "<" + query.CRSURIFromSRID(srid) + ">"
}

func timeDimension(layer *workspace.Layer) *workspace.Dimension {
	for _, dimension := range layer.Dimensions {
		if dimension != nil && strings.EqualFold(strings.TrimSpace(dimension.Name), "time") && strings.TrimSpace(dimension.SourceProperty) != "" {
			return dimension
		}
	}
	return nil
}

func collectionExtent(layer *workspace.Layer) *CollectionExtent {
	result := &CollectionExtent{Spatial: collectionSpatialExtent(layer)}
	dimension := timeDimension(layer)
	if dimension != nil && strings.TrimSpace(dimension.Extent) != "" {
		parts := strings.Split(strings.TrimSpace(dimension.Extent), "/")
		if len(parts) >= 2 {
			endpoint := func(value string) any {
				value = strings.TrimSpace(value)
				if value == "" || value == ".." {
					return nil
				}
				return value
			}
			result.Temporal = &TemporalExtent{
				Interval: [][]any{{endpoint(parts[0]), endpoint(parts[1])}},
				TRS:      gregorianTRS,
			}
		}
	}
	if result.Spatial == nil && result.Temporal == nil {
		return nil
	}
	return result
}

func collectionSpatialExtent(layer *workspace.Layer) *SpatialExtent {
	extent := layer.NativeExtent
	if extent == nil || extent.Stale || extent.MinX > extent.MaxX || extent.MinY > extent.MaxY {
		return nil
	}
	bbox := [4]float64{extent.MinX, extent.MinY, extent.MaxX, extent.MaxY}
	if extent.SRID != 0 && extent.SRID != 4326 {
		transformed, err := rastergrid.TransformBBox(bbox, fmt.Sprintf("EPSG:%d", extent.SRID), "EPSG:4326")
		if err != nil {
			return nil
		}
		bbox = transformed
	}
	return &SpatialExtent{
		BBox: [][]float64{{bbox[0], bbox[1], bbox[2], bbox[3]}},
		CRS:  query.CRS84URI,
	}
}

func parseDateTime(value string, dimension *workspace.Dimension) (*datasource.DateTimeFilter, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	selection := &datasource.DateTimeFilter{}
	if dimension != nil {
		selection.SourceProperty = strings.TrimSpace(dimension.SourceProperty)
		selection.EndProperty = strings.TrimSpace(dimension.EndProperty)
	}
	if !strings.Contains(value, "/") {
		instant, dateOnly, err := parseTemporalEndpoint(value, false)
		if err != nil {
			return nil, err
		}
		if dateOnly {
			end := instant.AddDate(0, 0, 1).Add(-time.Nanosecond)
			selection.Start, selection.End = instant, &end
			return selection, nil
		}
		selection.Start, selection.End, selection.Instant = instant, instant, true
		return selection, nil
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("datetime must be an instant or a single interval")
	}
	left, right := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	var start, end *time.Time
	var err error
	leftDuration, leftIsDuration := parseISODuration(left)
	rightDuration, rightIsDuration := parseISODuration(right)
	if leftIsDuration && rightIsDuration {
		return nil, fmt.Errorf("datetime interval cannot contain two durations")
	}
	switch {
	case leftIsDuration:
		if right == "" || right == ".." {
			return nil, fmt.Errorf("datetime duration requires a bounded endpoint")
		}
		end, _, err = parseTemporalEndpoint(right, true)
		if err == nil {
			value := leftDuration.add(*end, -1)
			start = &value
		}
	case rightIsDuration:
		if left == "" || left == ".." {
			return nil, fmt.Errorf("datetime duration requires a bounded endpoint")
		}
		start, _, err = parseTemporalEndpoint(left, false)
		if err == nil {
			value := rightDuration.add(*start, 1)
			end = &value
		}
	default:
		start, _, err = parseTemporalEndpoint(left, false)
		if err == nil {
			end, _, err = parseTemporalEndpoint(right, true)
		}
	}
	if err != nil {
		return nil, err
	}
	if start == nil && end == nil {
		return nil, fmt.Errorf("datetime interval must have at least one bounded endpoint")
	}
	if start != nil && end != nil && start.After(*end) {
		return nil, fmt.Errorf("datetime interval start must not be after its end")
	}
	selection.Start, selection.End = start, end
	return selection, nil
}

func parseTemporalEndpoint(raw string, upper bool) (*time.Time, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == ".." {
		return nil, false, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed, false, nil
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		parsed = parsed.UTC()
		if upper {
			parsed = parsed.AddDate(0, 0, 1).Add(-time.Nanosecond)
		}
		return &parsed, true, nil
	}
	return nil, false, fmt.Errorf("datetime endpoint %q must be an RFC 3339 date or timestamp", raw)
}

type isoDuration struct {
	years, months, days int
	clock               time.Duration
}

var isoDurationPattern = regexp.MustCompile(`^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

func parseISODuration(value string) (isoDuration, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	matches := isoDurationPattern.FindStringSubmatch(normalized)
	if matches == nil {
		return isoDuration{}, false
	}
	hasComponent := false
	for _, component := range matches[1:] {
		if component != "" {
			hasComponent = true
			break
		}
	}
	if !hasComponent {
		return isoDuration{}, false
	}
	parseInt := func(index int) int {
		parsed, _ := strconv.Atoi(matches[index])
		return parsed
	}
	seconds, _ := strconv.ParseFloat(matches[6], 64)
	duration := isoDuration{
		years: parseInt(1), months: parseInt(2), days: parseInt(3),
		clock: time.Duration(parseInt(4))*time.Hour + time.Duration(parseInt(5))*time.Minute + time.Duration(seconds*float64(time.Second)),
	}
	return duration, true
}

func (d isoDuration) add(base time.Time, direction int) time.Time {
	return base.AddDate(direction*d.years, direction*d.months, direction*d.days).Add(time.Duration(direction) * d.clock)
}

func dateTimeCQL(selection *datasource.DateTimeFilter) string {
	return selection.CQL2()
}

func combineCQL(userFilter, temporalFilter string) string {
	if userFilter == "" {
		return temporalFilter
	}
	if temporalFilter == "" {
		return userFilter
	}
	return "(" + userFilter + ") AND (" + temporalFilter + ")"
}

type queryableMetadata struct {
	Properties       []datasource.PropertyInfo
	GeometryProperty string
	GeometryType     string
	SRID             int
	IDProperty       string
}

func queryableMetadataForLayer(ctx context.Context, layer *workspace.Layer, svc *workspace.Service) (*queryableMetadata, error) {
	if layer.IsSQLView && layer.SQLViewConfig != nil {
		meta := &queryableMetadata{
			GeometryProperty: layer.SQLViewConfig.GeometryColumn,
			GeometryType:     layer.SQLViewConfig.GeometryType,
			SRID:             layer.SQLViewConfig.SRID,
			IDProperty:       layer.SQLViewConfig.IDColumn,
		}
		for i, property := range layer.SQLViewConfig.Properties {
			if property != nil {
				meta.Properties = append(meta.Properties, datasource.PropertyInfo{Name: property.Name, Type: property.Type, JSONType: jsonTypeForName(property.Type), Ordinal: i})
			}
		}
		return meta, nil
	}
	if svc == nil || svc.DataSource == nil {
		return nil, fmt.Errorf("data source not available")
	}
	info, err := svc.DataSource.GetLayerInfo(ctx, layer.SourceLayer)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("layer metadata not available")
	}
	return &queryableMetadata{
		Properties: info.Properties, GeometryProperty: info.GeometryColumn,
		GeometryType: info.GeometryType, SRID: info.SRID, IDProperty: info.IDColumn,
	}, nil
}

func jsonTypeForName(value string) datasource.JSONType {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "bool"):
		return datasource.JSONTypeBoolean
	case strings.Contains(lower, "int"):
		return datasource.JSONTypeInteger
	case strings.Contains(lower, "float"), strings.Contains(lower, "double"), strings.Contains(lower, "decimal"), strings.Contains(lower, "numeric"), strings.Contains(lower, "number"):
		return datasource.JSONTypeNumber
	case strings.Contains(lower, "array"), strings.HasSuffix(lower, "[]"):
		return datasource.JSONTypeArray
	default:
		return datasource.JSONTypeString
	}
}

func propertyJSONSchema(property datasource.PropertyInfo) map[string]any {
	typeName := string(property.JSONType)
	if typeName == "" {
		typeName = string(jsonTypeForName(property.Type))
	}
	schema := map[string]any{"type": typeName}
	if property.Description != "" {
		schema["description"] = property.Description
	}
	lower := strings.ToLower(property.Type)
	if strings.Contains(lower, "timestamp") || strings.Contains(lower, "datetime") || strings.Contains(lower, "timestamptz") {
		schema["type"] = "string"
		schema["format"] = "date-time"
	} else if lower == "date" {
		schema["type"] = "string"
		schema["format"] = "date"
	}
	return schema
}

func geometryFormat(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "Geometry"
	}
	value = strings.TrimPrefix(strings.ToUpper(value), "ST_")
	names := map[string]string{
		"POINT": "Point", "MULTIPOINT": "MultiPoint", "LINESTRING": "LineString",
		"MULTILINESTRING": "MultiLineString", "POLYGON": "Polygon",
		"MULTIPOLYGON": "MultiPolygon", "GEOMETRYCOLLECTION": "GeometryCollection",
		"GEOMETRY": "Geometry",
	}
	if normalized, ok := names[value]; ok {
		return "geometry-" + normalized
	}
	return "geometry-Geometry"
}

func (h *workspaceHandler) queryables(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusInternalServerError, "ServerError", "workspace not found in context")
		return
	}
	if ws.Settings == nil || !ws.Settings.OGCAPI.Enabled {
		writeErr(w, http.StatusForbidden, "ServiceDisabled", "OGC API Features is not enabled for this workspace")
		return
	}
	if h.requireAuth(w, r, ws, ws.Settings.OGCAPI.Public) || !h.validateQueryParameters(w, r, map[string]struct{}{}) {
		return
	}
	collectionID := pathParam(r, "collectionId")
	layer, svc := ws.GetLayer(collectionID)
	if layer == nil || !layer.VisibleToRole(workspaceRole(r, ws.ID)) {
		writeErr(w, http.StatusNotFound, "Not Found", "collection not found")
		return
	}
	meta, err := queryableMetadataForLayer(r.Context(), layer, svc)
	if err != nil {
		h.logger.Error("queryables metadata failed", "collection", collectionID, "err", err)
		writeErr(w, http.StatusInternalServerError, "ServerError", "failed to describe queryables")
		return
	}
	properties := make(map[string]map[string]any, len(meta.Properties)+2)
	for _, property := range meta.Properties {
		properties[property.Name] = propertyJSONSchema(property)
	}
	if meta.IDProperty != "" {
		if _, exists := properties[meta.IDProperty]; !exists {
			properties[meta.IDProperty] = map[string]any{"type": "string"}
		}
	}
	if meta.GeometryProperty != "" {
		properties[meta.GeometryProperty] = map[string]any{"$ref": "https://geojson.org/schema/Geometry.json", "format": geometryFormat(meta.GeometryType)}
	}
	base := h.workspaceBaseURL(r, ws.Name)
	id := fmt.Sprintf("%s/collections/%s/queryables", base, urlPathEscape(layer.PublicID))
	w.Header().Set("Content-Type", queryablesType)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Queryables{
		Schema: "https://json-schema.org/draft/2020-12/schema", ID: id, Type: "object",
		Title: layer.Title, Description: layer.Description, Properties: properties,
		AdditionalProperties: false,
	})
}

func itemsPageLinks(base string, layer *workspace.Layer, r *http.Request, limit, offset int, hasNext bool) []Link {
	path := fmt.Sprintf("%s/collections/%s/items", base, urlPathEscape(layer.PublicID))
	selfQuery := cloneValues(r.URL.Query())
	// Authentication material is transport state, not resource identity. Never
	// echo an opt-in query API key into cacheable response links.
	selfQuery.Del("apikey")
	links := []Link{
		{Href: withQuery(path, selfQuery), Rel: "self", Type: "application/geo+json"},
		{Href: fmt.Sprintf("%s/collections/%s/queryables", base, urlPathEscape(layer.PublicID)), Rel: queryablesRel, Type: queryablesType},
	}
	if hasNext {
		nextQuery := cloneValues(selfQuery)
		nextQuery.Set("limit", strconv.Itoa(limit))
		nextQuery.Set("offset", strconv.Itoa(offset+limit))
		links = append(links, Link{Href: withQuery(path, nextQuery), Rel: "next", Type: "application/geo+json"})
	}
	return links
}

func cloneValues(values url.Values) url.Values {
	clone := make(url.Values, len(values))
	for key, entries := range values {
		clone[key] = append([]string(nil), entries...)
	}
	return clone
}

func withQuery(path string, values url.Values) string {
	if encoded := values.Encode(); encoded != "" {
		return path + "?" + encoded
	}
	return path
}

func validateCQL2Text(meta *queryableMetadata, serviceType store.ServiceType, expression string, filterSRID int) error {
	if strings.TrimSpace(expression) == "" {
		return nil
	}
	allowed := make(map[string]struct{}, len(meta.Properties)+1)
	for _, property := range meta.Properties {
		allowed[property.Name] = struct{}{}
	}
	if meta.IDProperty != "" {
		allowed[meta.IDProperty] = struct{}{}
	}
	if serviceType == store.ServiceTypePostGIS {
		_, _, _, err := filter.Compile(expression, filter.Options{StartParamIndex: 1, FilterSRID: filterSRID, SourceSRID: meta.SRID, AllowedProperties: allowed, GeometryProperty: meta.GeometryProperty})
		return err
	}
	_, _, _, err := filter.CompileForDuckDB(expression, filter.DuckDBOptions{StartParamIndex: 1, FilterSRID: filterSRID, SourceSRID: meta.SRID, AllowedProperties: allowed, GeometryProperty: meta.GeometryProperty})
	return err
}
