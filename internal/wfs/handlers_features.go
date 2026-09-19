package wfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/gdalcap"
	"github.com/tobilg/neoserver/internal/workspace"
)

func (h *workspaceHandler) handleGetFeature(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	maxFeatures, defaultCount, maxOffset := h.featureLimits(ws)
	req, err := ParseGetFeatureRequest(r, maxFeatures, defaultCount)
	if err != nil {
		WriteExceptionFromError(w, err)
		return
	}
	if maxOffset > 0 && req.StartIndex > maxOffset {
		WriteException(w, ExceptionInvalidParameterValue, "startIndex", fmt.Sprintf("STARTINDEX exceeds maximum of %d", maxOffset))
		return
	}
	// Stored queries resolve their target from the query definition/ID. Keep
	// them outside the response cache: their handlers authorize the resolved
	// publication on every request, including after a definition changes.
	if req.StoredQueryID != "" {
		h.handleStoredQuery(r.Context(), w, r, ws, req)
		return
	}
	// Authorization belongs before both cache hits and shared cache fills.
	// The same check inside the loader alone does not protect cache readers.
	if _, _, err := h.authorizeFeatureQuery(r, ws, req); err != nil {
		WriteExceptionFromError(w, err)
		return
	}
	capabilities := gdalcap.Get()
	if isGeoPackageOutput(req.OutputFormat) && !capabilities.GeoPackage {
		WriteException(w, ExceptionInvalidParameterValue, "outputFormat", "GeoPackage output is unavailable because the GDAL GPKG driver is missing")
		return
	}
	if isShapeZipOutput(req.OutputFormat) {
		if !capabilities.Shapefile {
			WriteException(w, ExceptionInvalidParameterValue, "outputFormat", "SHAPE-ZIP output is unavailable because the GDAL ESRI Shapefile driver is missing")
			return
		}
		if _, err := shapeZipFilename(r, "features"); err != nil {
			WriteException(w, ExceptionInvalidParameterValue, "FORMAT_OPTIONS", err.Error())
			return
		}
	}
	if isCSVOutput(req.OutputFormat) || isBinaryExportOutput(req.OutputFormat) {
		h.handleGetFeatureRequest(w, r, ws, req, "", false, 0)
		return
	}
	cacheEnabled := h.cache != nil
	cacheTTL := time.Duration(0)
	if h.cache != nil {
		cacheTTL = h.cache.FeatureTTL()
		if len(req.TypeNames) == 1 {
			_, service := resolveFeatureLayer(ws, req.TypeNames[0], h.cfg.WFS.AppNamespacePrefix)
			if service != nil {
				cacheEnabled, cacheTTL = service.FeatureCachePolicy(cacheTTL)
			}
		}
	}

	keyInput := NormalizeQuery(r).Encode()
	if r.Method == http.MethodPost {
		if body, bodyErr := GetBodyBytes(r); bodyErr == nil {
			sum := sha256.Sum256(body)
			keyInput += fmt.Sprintf("&body=%x", sum[:])
		}
	}
	keyHash := sha256.Sum256([]byte(keyInput))
	cacheKey := cache.FeaturesKey(cache.FeaturesParams{Workspace: ws.ID, Collection: fmt.Sprintf("wfs-%x", keyHash[:]), Limit: req.Count, Offset: req.StartIndex, CRS: req.SrsName, Filter: req.Filter, Properties: strings.Join(req.PropertyName, ",")})
	if cacheEnabled {
		body, source, loadErr := h.cache.LoadBytes(r.Context(), cache.CacheTypeFeatures, cacheKey, cacheTTL, func(loadCtx context.Context) ([]byte, error) {
			capture := newBufferedResponse()
			h.handleGetFeatureRequest(capture, r.Clone(loadCtx), ws, req, cacheKey, false, cacheTTL)
			if capture.statusCode() != http.StatusOK {
				return nil, &bufferedResponseError{status: capture.statusCode(), header: capture.header.Clone(), body: append([]byte(nil), capture.body.Bytes()...)}
			}
			return append([]byte(nil), capture.body.Bytes()...), nil
		})
		if loadErr != nil {
			if captured, ok := loadErr.(*bufferedResponseError); ok {
				copyResponse(w, captured.header, captured.status, captured.body)
				return
			}
			h.writeInternalError(w, "Feature load failed", loadErr)
			return
		}
		etag := cache.SetHTTPHeaders(w, r, ws.Settings.WFS.Public, cacheTTL, body)
		if cache.IsNotModified(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if isJSONOutput(req.OutputFormat) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "application/gml+xml; version=3.2; charset=utf-8")
		}
		w.Header().Set("X-Cache", strings.ToUpper(string(source)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return
	}

	capture := newBufferedResponse()
	h.handleGetFeatureRequest(capture, r, ws, req, cacheKey, cacheEnabled, cacheTTL)
	for k, values := range capture.header {
		for _, value := range values {
			w.Header().Add(k, value)
		}
	}
	if cacheEnabled && capture.statusCode() == http.StatusOK {
		cache.SetHTTPHeaders(w, r, ws.Settings.WFS.Public, cacheTTL, capture.body.Bytes())
	}
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(capture.statusCode())
	_, _ = w.Write(capture.body.Bytes())
}

type bufferedResponseError struct {
	status int
	header http.Header
	body   []byte
}

func (e *bufferedResponseError) Error() string { return "buffered response failed" }

func copyResponse(w http.ResponseWriter, header http.Header, status int, body []byte) {
	for name, values := range header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newBufferedResponse() *bufferedResponse    { return &bufferedResponse{header: make(http.Header)} }
func (b *bufferedResponse) Header() http.Header { return b.header }
func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}
func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}
func (b *bufferedResponse) statusCode() int {
	if b.status == 0 {
		return http.StatusOK
	}
	return b.status
}

func (h *workspaceHandler) handleGetFeatureRequest(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, req *GetFeatureRequest, responseCacheKey string, responseCacheEnabled bool, responseCacheTTL time.Duration) {
	cacheFill := h.cache.BeginFill(cache.CacheTypeFeatures, responseCacheKey)
	ctx := r.Context()
	var err error

	// Handle stored query (GetFeatureById)
	if req.StoredQueryID != "" {
		h.handleStoredQuery(ctx, w, r, ws, req)
		return
	}

	// Validate type names
	if len(req.TypeNames) == 0 {
		WriteException(w, ExceptionMissingParameterValue, "typeNames", "TYPENAMES parameter is required")
		return
	}

	// Spatial joins (multiple type names) are not yet supported
	if len(req.TypeNames) > 1 {
		WriteException(w, ExceptionOperationNotSupported, "typeNames",
			"Spatial joins (multiple type names in a single query) are not supported. Query each type separately.")
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
	responseTypeName := publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix)

	layerInfo, err := layer.FeatureInfo(ctx, service.DataSource)
	if err != nil {
		h.writeInternalError(w, "Failed to get layer info", err)
		return
	}

	// Determine output SRID
	outputSRID := req.SRID
	if outputSRID == 0 {
		outputSRID = layerInfo.SRID
		if outputSRID == 0 {
			outputSRID = 4326
		}
	}

	// Validate requested CRS is supported (if explicitly requested)
	// WFS 2.0 requires returning InvalidParameterValue for unsupported CRS
	if req.SRID != 0 {
		supportedCRS := map[int]bool{
			4326:  true, // WGS84
			3857:  true, // Web Mercator
			32632: true, // UTM zone 32N (common for Europe)
			32633: true, // UTM zone 33N
		}
		// Also allow the layer's native SRID
		if layerInfo.SRID != 0 {
			supportedCRS[layerInfo.SRID] = true
		}
		if !supportedCRS[req.SRID] {
			WriteException(w, ExceptionInvalidParameterValue,
				"SRSNAME",
				fmt.Sprintf("Unsupported CRS: %s. Supported CRS are EPSG:4326, EPSG:3857, and the layer's native CRS.", req.SrsName))
			return
		}
	}

	// Build query parameters
	params, err := h.buildQueryParams(req, layerInfo, outputSRID, typeName)
	if err != nil {
		if reqErr, ok := err.(*RequestError); ok {
			WriteException(w, reqErr.Code, reqErr.Locator, reqErr.Message)
		} else {
			WriteException(w, ExceptionNoApplicableCode, "filter", fmt.Sprintf("Filter error: %v", err))
		}
		return
	}

	// Handle hits-only request
	if req.ResultType == ResultTypeHits {
		count, err := h.countPublication(ctx, ws, layer, service, params, "")
		if err != nil {
			h.writeInternalError(w, "Count failed", err)
			return
		}
		baseURL := h.workspaceBaseURL(r, ws.Name)
		WriteHitsResponse(w, count, req.StartIndex, req.Count, baseURL, responseTypeName, req)
		return
	}

	// Query features
	features, err := layer.QueryFeatures(ctx, service.DataSource, params)
	if err != nil {
		h.writeInternalError(w, "Query failed", err)
		return
	}

	// Get total count for pagination
	countKey := ""
	if responseCacheKey != "" {
		countKey = responseCacheKey + ":count"
	}
	totalCount, err := h.countPublication(ctx, ws, layer, service, params, countKey)
	countTimedOut := errors.Is(err, context.DeadlineExceeded)
	if err != nil && !countTimedOut {
		h.writeInternalError(w, "Count failed", err)
		return
	}
	numberMatched := strconv.Itoa(totalCount)
	if countTimedOut {
		numberMatched = "unknown"
	}

	// Convert json.RawMessage to []byte for output functions
	featuresBytes := make([][]byte, len(features))
	for i, f := range features {
		featuresBytes[i] = []byte(f)
	}

	// Write response based on output format
	baseURL := h.workspaceBaseURL(r, ws.Name)

	switch {
	case isCSVOutput(req.OutputFormat):
		if err := h.writeCSV(w, featuresBytes, layer.PublicID); err != nil {
			WriteException(w, ExceptionOperationProcessingFailed, "outputFormat", err.Error())
		}
	case isGeoPackageOutput(req.OutputFormat):
		if err := h.writeGeoPackage(ctx, w, featuresBytes, layer.PublicID, layerInfo, outputSRID, req.PropertyName); err != nil {
			WriteException(w, ExceptionOperationProcessingFailed, "outputFormat", err.Error())
		}
	case isShapeZipOutput(req.OutputFormat):
		if err := h.writeShapeZip(ctx, w, r, featuresBytes, layer.PublicID, layerInfo, outputSRID, req.PropertyName); err != nil {
			WriteException(w, ExceptionOperationProcessingFailed, "outputFormat", err.Error())
		}
	case isJSONOutput(req.OutputFormat):
		WriteGeoJSONFeatureCollectionMatched(w, featuresBytes, numberMatched)
	default:
		// GML 3.2 output
		WriteGMLFeatureCollectionMatched(w, layerInfo, featuresBytes, numberMatched, req.StartIndex, req.Count,
			h.cfg.WFS.AppNamespace, h.cfg.WFS.AppNamespacePrefix, outputSRID, baseURL, responseTypeName, req)
	}
	if responseCacheEnabled {
		if captured, ok := w.(*bufferedResponse); ok && captured.statusCode() == http.StatusOK {
			h.cache.SetFeatures(responseCacheKey, append([]byte(nil), captured.body.Bytes()...), responseCacheTTL, cacheFill)
		}
	}
}

// handleStoredQuery handles stored query requests like GetFeatureById and custom stored queries.
func (h *workspaceHandler) handleStoredQuery(ctx context.Context, w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, req *GetFeatureRequest) {
	// Check if this is the built-in GetFeatureById query
	if IsGetFeatureByIdQuery(req.StoredQueryID) {
		h.handleGetFeatureById(ctx, w, r, ws, req)
		return
	}

	// Check if this is a custom stored query
	storedQuery, err := h.store.GetWFSStoredQuery(ctx, ws.ID, req.StoredQueryID)
	if err != nil || storedQuery == nil {
		WriteException(w, ExceptionInvalidParameterValue, "storedQueryId", fmt.Sprintf("Unknown stored query: %s", req.StoredQueryID))
		return
	}

	// Execute the custom stored query
	h.executeCustomStoredQuery(ctx, w, r, ws, req, storedQuery)
}

// handleGetFeatureById handles the built-in GetFeatureById stored query.
func (h *workspaceHandler) handleGetFeatureById(ctx context.Context, w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, req *GetFeatureRequest) {
	// Get ID parameter
	featureID := req.StoredQueryParams["ID"]
	if featureID == "" {
		WriteException(w, ExceptionMissingParameterValue, "ID", "ID parameter is required for GetFeatureById")
		return
	}

	// Parse the feature ID to extract layer and ID
	// Feature ID format: TypeName.localId (e.g., "cities.123" or "app:cities.123")
	// If no dot found, the ID might be an unknown ID - return 404 per WFS 2.0 spec
	typeName, localID, resolved := resolveFeatureIdentifier(ws, featureID, h.cfg.WFS.AppNamespacePrefix)
	if !resolved {
		// Per WFS 2.0 spec (cl. 11.3.5), unknown feature ID should return HTTP 404
		WriteException(w, ExceptionNotFound, "ID", fmt.Sprintf("Feature not found: %s", featureID))
		return
	}

	// Find layer and service. A layer the caller may not read is treated as an
	// unknown type name so its existence is not disclosed.
	layer, service := resolveFeatureLayer(ws, typeName, h.cfg.WFS.AppNamespacePrefix)
	if layer == nil || service == nil || !layer.VisibleToRole(workspaceRole(r, ws.ID)) {
		// Unknown type name also means feature not found
		WriteException(w, ExceptionNotFound, "ID", fmt.Sprintf("Feature not found: %s", featureID))
		return
	}

	if service.DataSource == nil {
		WriteException(w, ExceptionNoApplicableCode, "", fmt.Sprintf("Service not available for layer: %s", layer.PublicID))
		return
	}
	responseTypeName := publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix)

	// Get layer info
	layerInfo, err := layer.FeatureInfo(ctx, service.DataSource)
	if err != nil {
		h.writeInternalError(w, "Failed to get layer info", err)
		return
	}

	// Determine output SRID
	outputSRID := layerInfo.SRID
	if outputSRID == 0 {
		outputSRID = 4326
	}

	// Query by ID
	feature, found, err := layer.FeatureByID(ctx, service.DataSource, localID, outputSRID)
	if err != nil {
		h.writeInternalError(w, "Query failed", err)
		return
	}

	if !found || feature == nil {
		// Per WFS 2.0 spec, GetFeatureById with unknown ID should return HTTP 404
		WriteException(w, ExceptionNotFound, "ID", fmt.Sprintf("Feature not found: %s", featureID))
		return
	}

	// Write response
	baseURL := h.workspaceBaseURL(r, ws.Name)

	// Per WFS 2.0 spec (ISO 19142:2010, cl. 7.9.3.6), GetFeatureById returns just the feature
	// The gml:id in the response must match the requested featureID exactly
	WriteGMLSingleFeature(w, layerInfo, []byte(feature), h.cfg.WFS.AppNamespace, h.cfg.WFS.AppNamespacePrefix,
		outputSRID, responseTypeName, baseURL, featureID)
}

// executeCustomStoredQuery executes a custom stored query.
func (h *workspaceHandler) executeCustomStoredQuery(ctx context.Context, w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, req *GetFeatureRequest, storedQuery *StoredQuery) {
	// Substitute parameters in the query expression (case-insensitive)
	queryExpr := storedQuery.QueryExpression
	for paramName, paramValue := range req.StoredQueryParams {
		// WFS 2.0 uses ${paramName} syntax for parameter substitution
		// Parameter names in StoredQueryParams are uppercase, but the query may use any case
		// Try common variations: lowercase, uppercase, and original camelCase patterns
		variations := []string{
			"${" + strings.ToLower(paramName) + "}", // ${typename}
			"${" + paramName + "}",                  // ${TYPENAME}
			"${" + strings.ToUpper(string(paramName[0])) + strings.ToLower(paramName[1:]) + "}",     // ${Typename}
			"${" + strings.ToLower(string(paramName[0])) + strings.ToLower(paramName[1:]) + "Name}", // ${typeName} for TYPENAME
		}
		for _, placeholder := range variations {
			queryExpr = strings.ReplaceAll(queryExpr, placeholder, paramValue)
		}
		// Also do a case-insensitive search for ${paramName} pattern
		queryExpr = replacePlaceholderCaseInsensitive(queryExpr, paramName, paramValue)
	}

	// Parse the query expression to extract typeNames
	// The query expression is XML like: <Query typeNames="app:NamedPlaces"/>
	var typeName string
	if idx := strings.Index(queryExpr, "typeNames="); idx >= 0 {
		rest := queryExpr[idx+len("typeNames="):]
		// Find the quote character used
		if len(rest) > 0 {
			quote := rest[0]
			if quote == '"' || quote == '\'' {
				rest = rest[1:]
				endIdx := strings.IndexByte(rest, quote)
				if endIdx >= 0 {
					typeName = rest[:endIdx]
				}
			}
		}
	}

	if typeName == "" {
		WriteException(w, ExceptionOperationParsingFailed, "StoredQuery", "Could not extract typeNames from stored query expression")
		return
	}

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
	responseTypeName := publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix)

	// Get layer info
	layerInfo, err := layer.FeatureInfo(ctx, service.DataSource)
	if err != nil {
		h.writeInternalError(w, "Failed to get layer info", err)
		return
	}

	// Determine output SRID
	outputSRID := req.SRID
	if outputSRID == 0 {
		outputSRID = layerInfo.SRID
		if outputSRID == 0 {
			outputSRID = 4326
		}
	}

	// Build query parameters
	// Create a modified request with the extracted type name
	effective := *req
	modifiedReq := &effective
	modifiedReq.TypeNames = []string{typeName}
	modifiedReq.StoredQueryID, modifiedReq.StoredQueryParams = "", nil
	modifiedReq.SRID = outputSRID

	// Extract filter from query expression if present
	if filterXML, err := extractFilterFromXML(queryExpr); err != nil {
		WriteExceptionFromError(w, err)
		return
	} else if filterXML != "" {
		modifiedReq.Filter = filterXML
	}

	params, err := h.buildQueryParams(modifiedReq, layerInfo, outputSRID, typeName)
	if err != nil {
		WriteExceptionFromError(w, err)
		return
	}

	// Query features
	features, err := layer.QueryFeatures(ctx, service.DataSource, params)
	if err != nil {
		h.writeInternalError(w, "Query failed", err)
		return
	}

	// Get total count
	totalCount, err := layer.CountFeatures(ctx, service.DataSource, params)
	if err != nil {
		h.writeInternalError(w, "Count failed", err)
		return
	}

	// Convert to [][]byte
	featuresBytes := make([][]byte, len(features))
	for i, f := range features {
		featuresBytes[i] = []byte(f)
	}

	// Write response
	baseURL := h.workspaceBaseURL(r, ws.Name)

	if req.ResultType == ResultTypeHits {
		WriteHitsResponse(w, totalCount, req.StartIndex, req.Count, baseURL, responseTypeName, modifiedReq)
	} else {
		WriteGMLFeatureCollectionMatched(w, layerInfo, featuresBytes, strconv.Itoa(totalCount), req.StartIndex, req.Count,
			h.cfg.WFS.AppNamespace, h.cfg.WFS.AppNamespacePrefix, outputSRID, baseURL, responseTypeName, modifiedReq)
	}
}

// replacePlaceholderCaseInsensitive replaces ${paramName} patterns case-insensitively.
func replacePlaceholderCaseInsensitive(s, paramName, value string) string {
	// Look for ${...} patterns and check if the content matches paramName case-insensitively
	result := s
	start := 0
	for {
		idx := strings.Index(result[start:], "${")
		if idx < 0 {
			break
		}
		idx += start
		endIdx := strings.Index(result[idx:], "}")
		if endIdx < 0 {
			break
		}
		endIdx += idx
		placeholderName := result[idx+2 : endIdx]
		if strings.EqualFold(placeholderName, paramName) {
			result = result[:idx] + value + result[endIdx+1:]
			// Adjust start position for next iteration
			start = idx + len(value)
		} else {
			start = endIdx + 1
		}
	}
	return result
}

// buildQueryParams builds datasource query parameters from a WFS request.
func (h *workspaceHandler) buildQueryParams(req *GetFeatureRequest, layerInfo *datasource.LayerInfo, outputSRID int, typeName string) (datasource.QueryParams, error) {
	params := datasource.QueryParams{
		OutputSRID: outputSRID,
		Limit:      req.Count,
		Offset:     req.StartIndex,
	}

	// Extract collection ID from typeName for ResourceId validation
	collectionID := typeName
	if idx := strings.Index(typeName, ":"); idx >= 0 {
		collectionID = typeName[idx+1:]
	}

	// Build allowed properties set
	allowed := make(map[string]struct{})
	for _, prop := range layerInfo.Properties {
		allowed[prop.Name] = struct{}{}
	}
	if layerInfo.IDColumn != "" {
		allowed[layerInfo.IDColumn] = struct{}{}
	}

	// Sort by
	if len(req.SortBy) > 0 {
		for _, sf := range req.SortBy {
			params.SortBy = append(params.SortBy, datasource.SortField{
				Name: sf.Name,
				Desc: sf.Desc,
			})
		}
	}

	// Properties to return
	if len(req.PropertyName) > 0 {
		params.Properties = req.PropertyName
	}

	// Handle RESOURCEID parameter (WFS 2.0 KVP feature ID filter)
	// Per WFS 2.0 spec, RESOURCEID is mutually exclusive with FILTER and BBOX
	if len(req.ResourceID) > 0 {
		if req.BBox != nil || strings.TrimSpace(req.Filter) != "" {
			return params, &RequestError{Code: ExceptionInvalidParameterValue, Locator: "RESOURCEID", Message: "RESOURCEID cannot be combined with FILTER or BBOX"}
		}
		// Build FES filter XML from ResourceIDs
		var filterParts []string
		for _, rid := range req.ResourceID {
			filterParts = append(filterParts, fmt.Sprintf(`<fes:ResourceId rid="%s"/>`, escapeXML(rid)))
		}
		fesXML := fmt.Sprintf(`<fes:Filter xmlns:fes="%s">%s</fes:Filter>`, NSFes, strings.Join(filterParts, ""))

		fesFilter, err := ParseFESFilter(fesXML)
		if err != nil {
			return params, err
		}

		// Compile FES to SQL
		compileOptions := FESCompileOptions{
			StartParamIndex:   1,
			SourceSRID:        layerInfo.SRID,
			GeometryProperty:  layerInfo.GeometryColumn,
			AllowedProperties: allowed,
			CollectionID:      collectionID,
			IDColumn:          layerInfo.IDColumn,
		}
		filterSQL, filterArgs, _, err := CompileFES(fesFilter, compileOptions)
		params.Predicate = &featurePredicate{Filter: fesFilter, Options: compileOptions}
		if err != nil {
			return params, err
		}

		if filterSQL != "" {
			params.CompiledFilter = filterSQL
			params.CompiledFilterArgs = filterArgs
			params.CompiledFilterParamOffset = 1
		}

		return params, nil
	}

	// BBox filter
	if req.BBox != nil {
		params.BBox = &datasource.BBox{
			MinX: req.BBox.MinX,
			MinY: req.BBox.MinY,
			MaxX: req.BBox.MaxX,
			MaxY: req.BBox.MaxY,
		}
		params.BBoxSRID = req.BBoxSRID
		if params.BBoxSRID == 0 {
			params.BBoxSRID = 4326
		}
	}

	// Handle filter
	if req.Filter != "" {
		filterTrimmed := strings.TrimSpace(req.Filter)
		// Check if filter is FES XML (starts with '<')
		if strings.HasPrefix(filterTrimmed, "<") {
			// Parse FES XML filter
			fesFilter, err := ParseFESFilter(filterTrimmed)
			if err != nil {
				return params, err
			}

			// Compile FES to SQL
			// Starting param index depends on BBox (4 params if present)
			startParamIdx := 1
			if req.BBox != nil {
				startParamIdx = 5
			}

			compileOptions := FESCompileOptions{
				StartParamIndex:   startParamIdx,
				SourceSRID:        layerInfo.SRID,
				GeometryProperty:  layerInfo.GeometryColumn,
				AllowedProperties: allowed,
				CollectionID:      collectionID,
				IDColumn:          layerInfo.IDColumn,
			}
			filterSQL, filterArgs, _, err := CompileFES(fesFilter, compileOptions)
			params.Predicate = &featurePredicate{Filter: fesFilter, Options: compileOptions}
			if err != nil {
				return params, err
			}

			if filterSQL != "" {
				params.CompiledFilter = filterSQL
				params.CompiledFilterArgs = filterArgs
				params.CompiledFilterParamOffset = startParamIdx
			}
		} else {
			// CQL2 text filter
			params.Filter = req.Filter
			params.FilterSRID = outputSRID
		}
	}

	return params, nil
}
