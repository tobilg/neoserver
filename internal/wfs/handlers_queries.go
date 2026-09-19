package wfs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func (h *workspaceHandler) handleGetPropertyValue(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	ctx := r.Context()

	// Parse request
	maxFeatures, defaultCount, maxOffset := h.featureLimits(ws)
	req, err := ParseGetPropertyValueRequest(r, maxFeatures, defaultCount)
	if err != nil {
		WriteExceptionFromError(w, err)
		return
	}

	if maxOffset > 0 && req.StartIndex > maxOffset {
		WriteException(w, ExceptionInvalidParameterValue, "startIndex", fmt.Sprintf("STARTINDEX exceeds maximum of %d", maxOffset))
		return
	}

	// Validate type names
	if len(req.TypeNames) == 0 {
		WriteException(w, ExceptionMissingParameterValue, "typeNames", "TYPENAMES parameter is required")
		return
	}

	// Spatial joins (multiple type names) are not supported for GetPropertyValue
	if len(req.TypeNames) > 1 {
		WriteException(w, ExceptionOperationNotSupported, "typeNames",
			"Spatial joins (multiple type names) are not supported for GetPropertyValue.")
		return
	}

	typeName := req.TypeNames[0]

	// Find layer and service. A layer the caller may not read is treated as an
	// unknown type name so its existence is not disclosed.
	layer, service := resolveFeatureLayer(ws, typeName, h.cfg.WFS.AppNamespacePrefix)
	if layer == nil || service == nil || !layer.VisibleToRole(workspaceRole(r, ws.ID)) {
		WriteException(w, ExceptionInvalidParameterValue, "typeNames", fmt.Sprintf("Unknown type name: %s", typeName))
		return
	}

	if service.DataSource == nil {
		WriteException(w, ExceptionNoApplicableCode, "", fmt.Sprintf("Service not available for layer: %s", layer.PublicID))
		return
	}

	// Get layer info
	layerInfo, err := layer.FeatureInfo(ctx, service.DataSource)
	if err != nil {
		h.writeInternalError(w, "Failed to get layer info", err)
		return
	}

	// Check if valueReference is @gml:id (special case for feature IDs)
	valueRef := req.ValueReference
	isGmlId := valueRef == "@gml:id" || valueRef == "gml:id"

	// Validate valueReference if not gml:id
	if !isGmlId {
		// Strip namespace prefix if present
		propName := valueRef
		if idx := strings.Index(propName, ":"); idx >= 0 {
			propName = propName[idx+1:]
		}
		valueRef = propName

		// Check if property exists
		found := false
		for _, prop := range layerInfo.Properties {
			if prop.Name == propName {
				found = true
				valueRef = propName // Use normalized name
				break
			}
		}
		if !found && propName != layerInfo.GeometryColumn {
			WriteException(w, ExceptionInvalidParameterValue,
				"valueReference", fmt.Sprintf("Unknown property: %s", req.ValueReference))
			return
		}
	}

	// Determine output SRID
	outputSRID := req.SRID
	if outputSRID == 0 {
		outputSRID = layerInfo.SRID
		if outputSRID == 0 {
			outputSRID = 4326
		}
	}

	effective := &GetFeatureRequest{TypeNames: req.TypeNames, Count: req.Count, StartIndex: req.StartIndex, BBox: req.BBox, BBoxSRID: req.BBoxSRID, Filter: req.Filter, SortBy: req.SortBy, ResourceID: req.ResourceID, SRID: req.SRID}
	params, err := h.buildQueryParams(effective, layerInfo, outputSRID, typeName)
	if err != nil {
		WriteExceptionFromError(w, err)
		return
	}

	// Handle resultType=hits
	if req.ResultType == ResultTypeHits {
		count, err := h.countPublication(ctx, ws, layer, service, params, "")
		if err != nil {
			h.writeInternalError(w, "Count failed", err)
			return
		}
		WriteValueCollectionHits(w, count)
		return
	}

	// Query features
	features, err := layer.QueryFeatures(ctx, service.DataSource, params)
	if err != nil {
		h.writeInternalError(w, "Query failed", err)
		return
	}

	// Get total count
	totalCount, err := h.countPublication(ctx, ws, layer, service, params, "")
	numberMatched := strconv.Itoa(totalCount)
	if errors.Is(err, context.DeadlineExceeded) {
		numberMatched = "unknown"
	} else if err != nil {
		h.writeInternalError(w, "Count failed", err)
		return
	}

	// Extract property values from features
	var values []string
	var geometries []map[string]interface{}
	isGeometry := !isGmlId && valueRef == layerInfo.GeometryColumn
	for _, featureJSON := range features {
		feature, err := decodeFeatureJSON(featureJSON)
		if err != nil {
			h.writeInternalError(w, "Invalid feature response", err)
			return
		}
		if isGeometry {
			geometry, _ := feature["geometry"].(map[string]interface{})
			geometries = append(geometries, geometry)
		} else if isGmlId {
			// Extract gml:id (format: TypeName.localId)
			if id, ok := feature["id"]; ok {
				values = append(values, fmt.Sprintf("%s.%v", ParseQName(publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix)).LocalPart, id))
			}
		} else {
			// Extract property value
			if props, ok := feature["properties"].(map[string]interface{}); ok {
				if v, ok := props[valueRef]; ok && v != nil {
					values = append(values, fmt.Sprintf("%v", v))
				}
			}
		}
	}

	// Write response
	if isGeometry {
		writeGeometryValueCollection(w, geometries, numberMatched, outputSRID)
		return
	}
	WriteValueCollectionMatched(w, values, numberMatched, len(values))
}

func (h *workspaceHandler) handleListStoredQueries(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	// Build list of return feature types from the layers the caller may read
	var returnTypes []string
	for _, layer := range ws.VisibleLayers(workspaceRole(r, ws.ID)) {
		returnTypes = append(returnTypes, publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix))
	}

	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	namespaceDeclarations := applicationNamespaceDeclarations(h.cfg.WFS.AppNamespace, h.cfg.WFS.AppNamespacePrefix)
	// Use HTTP URI format as tests check for:
	// @id='http://www.opengis.net/def/query/OGC-WFS/0/GetFeatureById' or @id='urn:ogc:def:query:OGC-WFS::GetFeatureById'
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<wfs:ListStoredQueriesResponse xmlns:wfs="http://www.opengis.net/wfs/2.0" %s>
  <wfs:StoredQuery id="%s">
    <wfs:Title>Get feature by identifier</wfs:Title>
`, namespaceDeclarations, StoredQueryGetFeatureByIdHTTP)
	for _, rt := range returnTypes {
		fmt.Fprintf(w, "    <wfs:ReturnFeatureType>%s</wfs:ReturnFeatureType>\n", escapeXML(rt))
	}
	fmt.Fprintf(w, "  </wfs:StoredQuery>\n")

	// Include custom stored queries
	customQueries, _ := h.store.ListWFSStoredQueries(r.Context(), ws.ID)
	for _, sq := range customQueries {
		fmt.Fprintf(w, "  <wfs:StoredQuery id=\"%s\">\n", escapeXML(sq.QueryID))
		if sq.Title != "" {
			fmt.Fprintf(w, "    <wfs:Title>%s</wfs:Title>\n", escapeXML(sq.Title))
		}
		for _, rt := range sq.ReturnTypes {
			fmt.Fprintf(w, "    <wfs:ReturnFeatureType>%s</wfs:ReturnFeatureType>\n", escapeXML(rt))
		}
		fmt.Fprintf(w, "  </wfs:StoredQuery>\n")
	}

	fmt.Fprintf(w, "</wfs:ListStoredQueriesResponse>")
}

func (h *workspaceHandler) handleDescribeStoredQueries(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	ctx := r.Context()

	// Parse request to get requested stored query IDs
	req, _ := ParseDescribeStoredQueriesRequest(r)

	// Build list of return feature types from the layers the caller may read
	var returnTypes []string
	for _, layer := range ws.VisibleLayers(workspaceRole(r, ws.ID)) {
		returnTypes = append(returnTypes, publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix))
	}
	returnFeatureTypes := strings.Join(returnTypes, " ")

	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	namespaceDeclarations := applicationNamespaceDeclarations(h.cfg.WFS.AppNamespace, h.cfg.WFS.AppNamespacePrefix)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<wfs:DescribeStoredQueriesResponse xmlns:wfs="http://www.opengis.net/wfs/2.0" xmlns:xsd="http://www.w3.org/2001/XMLSchema" %s>
`, namespaceDeclarations)

	// If no specific IDs requested, describe all queries
	if len(req.StoredQueryIds) == 0 {
		// Describe built-in GetFeatureById - use HTTP URI format for consistency
		fmt.Fprintf(w, `  <wfs:StoredQueryDescription id="%s">
    <wfs:Title>Get feature by identifier</wfs:Title>
    <wfs:Abstract>Retrieves a feature by its gml:id</wfs:Abstract>
    <wfs:Parameter name="ID" type="xsd:string"/>
    <wfs:QueryExpressionText returnFeatureTypes="%s" language="urn:ogc:def:queryLanguage:OGC-WFS::WFSQueryExpression" isPrivate="false"/>
  </wfs:StoredQueryDescription>
`, StoredQueryGetFeatureByIdHTTP, escapeXML(returnFeatureTypes))

		// Describe custom stored queries
		customQueries, _ := h.store.ListWFSStoredQueries(ctx, ws.ID)
		for _, sq := range customQueries {
			h.writeStoredQueryDescription(w, sq, returnFeatureTypes)
		}
	} else {
		// Describe only requested queries
		for _, requestedID := range req.StoredQueryIds {
			if IsGetFeatureByIdQuery(requestedID) {
				fmt.Fprintf(w, `  <wfs:StoredQueryDescription id="%s">
    <wfs:Title>Get feature by identifier</wfs:Title>
    <wfs:Abstract>Retrieves a feature by its gml:id</wfs:Abstract>
    <wfs:Parameter name="ID" type="xsd:string"/>
    <wfs:QueryExpressionText returnFeatureTypes="%s" language="urn:ogc:def:queryLanguage:OGC-WFS::WFSQueryExpression" isPrivate="false"/>
  </wfs:StoredQueryDescription>
`, requestedID, returnFeatureTypes)
			} else {
				// Check for custom stored query
				sq, err := h.store.GetWFSStoredQuery(ctx, ws.ID, requestedID)
				if err != nil || sq == nil {
					WriteException(w, ExceptionInvalidParameterValue, "storedQueryId",
						fmt.Sprintf("Unknown stored query: %s", requestedID))
					return
				}
				h.writeStoredQueryDescription(w, sq, returnFeatureTypes)
			}
		}
	}

	fmt.Fprintf(w, "</wfs:DescribeStoredQueriesResponse>")
}

// writeStoredQueryDescription writes a StoredQueryDescription element for a custom stored query
func (h *workspaceHandler) writeStoredQueryDescription(w http.ResponseWriter, sq *store.WFSStoredQuery, defaultReturnTypes string) {
	fmt.Fprintf(w, `  <wfs:StoredQueryDescription id="%s">
`, escapeXML(sq.QueryID))
	if sq.Title != "" {
		fmt.Fprintf(w, "    <wfs:Title>%s</wfs:Title>\n", escapeXML(sq.Title))
	}
	if sq.Abstract != "" {
		fmt.Fprintf(w, "    <wfs:Abstract>%s</wfs:Abstract>\n", escapeXML(sq.Abstract))
	}
	for _, param := range sq.Parameters {
		paramType := param.Type
		if paramType == "" {
			paramType = "xsd:string"
		}
		fmt.Fprintf(w, `    <wfs:Parameter name="%s" type="%s"/>
`, escapeXML(param.Name), escapeXML(paramType))
	}
	// Use stored return types if available, otherwise use default
	retTypes := defaultReturnTypes
	if len(sq.ReturnTypes) > 0 {
		retTypes = strings.Join(sq.ReturnTypes, " ")
	}
	lang := sq.Language
	if lang == "" {
		lang = QueryLanguageWFS
	}
	fmt.Fprintf(w, `    <wfs:QueryExpressionText returnFeatureTypes="%s" language="%s" isPrivate="false"/>
  </wfs:StoredQueryDescription>
`, escapeXML(retTypes), escapeXML(lang))
}
