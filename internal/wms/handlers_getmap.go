package wms

import (
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/workspace"
)

type capturedGetMapError struct {
	status int
	header http.Header
	body   []byte
}

type skipGetMapCacheKey struct{}

func (e *capturedGetMapError) Error() string { return "GetMap generation failed" }

func (h *workspaceHandler) handleGetMap(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	maxWidth, maxHeight, _, _, _ := h.wmsLimits(ws)
	maxEnvVariables, maxEnvValueBytes := h.environmentLimits()
	req, err := parseGetMapRequestWithLimits(r, maxWidth, maxHeight, maxEnvVariables, maxEnvValueBytes)
	if err == nil && strings.TrimSpace(req.SLD) != "" {
		h.writeGetMapExceptionWithLocator(w, req, ExceptionInvalidParameterValue, "SLD", remoteSLDUnsupportedMessage)
		return
	}
	if err == nil {
		if !h.authorizeMapResources(w, r, ws, req) {
			return
		}
		addDefaultDimensionWarnings(w, req, ws)
	}
	if h.cache == nil {
		h.renderGetMap(w, r, ws)
		return
	}
	if err != nil {
		h.renderGetMap(w, r, ws)
		return
	}
	cacheKey := getMapCacheKey(ws, req)
	cacheEnabled, cacheTTL := h.tileCachePolicy(ws, req)
	if !cacheEnabled {
		h.renderGetMap(w, r, ws)
		return
	}
	body, source, err := h.cache.LoadBytes(r.Context(), cache.CacheTypeTiles, cacheKey, cacheTTL, func(loadCtx context.Context) ([]byte, error) {
		recorder := httptest.NewRecorder()
		loadCtx = context.WithValue(loadCtx, skipGetMapCacheKey{}, true)
		h.renderGetMap(recorder, r.Clone(loadCtx), ws)
		result := recorder.Result()
		defer result.Body.Close()
		if result.StatusCode != http.StatusOK {
			return nil, &capturedGetMapError{status: result.StatusCode, header: result.Header.Clone(), body: append([]byte(nil), recorder.Body.Bytes()...)}
		}
		return append([]byte(nil), recorder.Body.Bytes()...), nil
	})
	if err != nil {
		if captured, ok := err.(*capturedGetMapError); ok {
			for name, values := range captured.header {
				for _, value := range values {
					w.Header().Add(name, value)
				}
			}
			w.WriteHeader(captured.status)
			_, _ = w.Write(captured.body)
			return
		}
		h.writeError(w, err)
		return
	}
	etag := cache.SetHTTPHeaders(w, r, ws.Settings.WMS.Public, cacheTTL, body)
	if cache.IsNotModified(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	contentType := getMapContentType(req.Format)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Cache", strings.ToUpper(string(source)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// addDefaultDimensionWarnings implements the WMS 1.3.0 dimension requirement
// for an omitted TIME or ELEVATION parameter. It runs outside cache generation
// so cache hits and misses expose identical response metadata.
func addDefaultDimensionWarnings(w http.ResponseWriter, req *GetMapRequest, ws *workspace.Workspace) {
	if w == nil || req == nil || ws == nil {
		return
	}
	seen := make(map[string]struct{})
	for _, layerName := range req.Layers {
		resource := ws.GetResource(layerName)
		if resource == nil {
			continue
		}
		var dimensions []*workspace.Dimension
		switch resource.Kind {
		case workspace.ResourceFeature:
			if resource.Layer != nil {
				dimensions = resource.Layer.Dimensions
			}
		case workspace.ResourceCoverage:
			if resource.Coverage != nil {
				dimensions = resource.Coverage.Dimensions
			}
		}
		for _, dimension := range dimensions {
			if dimension == nil || dimension.Default == "" {
				continue
			}
			name := strings.ToLower(dimension.Name)
			if (name == "time" && req.Time != "") || (name == "elevation" && req.Elevation != "") {
				continue
			}
			if name != "time" && name != "elevation" {
				continue
			}
			warning := fmt.Sprintf("99 Default value used: %s=%s", name, dimension.Default)
			if _, exists := seen[warning]; exists {
				continue
			}
			seen[warning] = struct{}{}
			w.Header().Add("Warning", warning)
		}
	}
}

func (h *workspaceHandler) renderGetMap(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	ctx := r.Context()
	maxWidth, maxHeight, maxPixels, maxFeatures, maxVertices := h.wmsLimits(ws)
	simplify, simplifyPixels := h.wmsSimplification(ws)

	// Parse request
	maxEnvVariables, maxEnvValueBytes := h.environmentLimits()
	req, err := parseGetMapRequestWithLimits(r, maxWidth, maxHeight, maxEnvVariables, maxEnvValueBytes)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if strings.TrimSpace(req.SLD) != "" {
		h.writeGetMapExceptionWithLocator(w, req, ExceptionInvalidParameterValue, "SLD", remoteSLDUnsupportedMessage)
		return
	}
	if maxPixels > 0 && req.Height > maxPixels/req.Width {
		h.writeError(w, &RequestError{Code: ExceptionInvalidParameterValue, Message: "requested image exceeds maximum pixel count"})
		return
	}

	// Validate format
	if !supportedGetMapFormat(req.Format) {
		h.writeGetMapException(w, req, ExceptionInvalidFormat, fmt.Sprintf("Unsupported format: %s", req.Format))
		return
	}

	if !h.authorizeMapResources(w, r, ws, req) {
		return
	}
	cacheKey := getMapCacheKey(ws, req)
	cacheFill := h.cache.BeginFill(cache.CacheTypeTiles, cacheKey)
	cacheEnabled, cacheTTL := h.tileCachePolicy(ws, req)
	if skip, _ := r.Context().Value(skipGetMapCacheKey{}).(bool); skip {
		cacheEnabled = false
	}
	if cacheEnabled {
		if body, ok := h.cache.GetTile(cacheKey); ok {
			etag := cache.SetHTTPHeaders(w, r, ws.Settings.WMS.Public, cacheTTL, body)
			if cache.IsNotModified(r, etag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			setMapContentHeaders(w, req.Format)
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
	}

	queueTimeout := time.Duration(h.cfg.WMS.RenderQueueTimeoutMS) * time.Millisecond
	if queueTimeout <= 0 {
		queueTimeout = 2 * time.Second
	}
	if h.renderSlots == nil {
		h.renderSlots = make(chan struct{}, 1)
	}
	select {
	case h.renderSlots <- struct{}{}:
		defer func() { <-h.renderSlots }()
	case <-time.After(queueTimeout):
		WriteException(w, ExceptionServerBusy, "render queue is full")
		return
	case <-ctx.Done():
		return
	}

	// Create transform
	transform := renderer.NewTransform(req.BBox, req.Width, req.Height)

	// Create renderer
	mapRenderer := h.newMapRenderer(transform, req.Transparent, req.BgColor, ws)

	// Get default style from config
	defaultStyleCfg := h.cfg.WMS.Styles[h.cfg.WMS.DefaultStyle]
	if defaultStyleCfg.FillColor == "" {
		defaultStyleCfg = conf.WMSStyle{
			FillColor:   "#3388ff",
			FillOpacity: 0.5,
			StrokeColor: "#3388ff",
			StrokeWidth: 2.0,
			PointRadius: 5.0,
		}
	}

	// Parse SLD if provided
	var sldDoc *sld.StyledLayerDescriptor
	if req.SLDBody != "" {
		sldDoc, err = sld.ParseString(req.SLDBody)
		if err != nil {
			h.writeGetMapException(w, req, ExceptionStyleNotDefined, fmt.Sprintf("Invalid SLD: %v", err))
			return
		}
	}
	if req.Format == FormatUTFGrid {
		h.renderUTFGrid(w, r, ws, req, sldDoc, maxFeatures, maxVertices, simplify, simplifyPixels)
		return
	}

	// Filter out empty layer names (WMS allows empty LAYERS for testing purposes)
	var renderLayers []groupRenderLayer
	role := workspaceRole(r, ws.ID)
	for index, name := range req.Layers {
		if name != "" {
			style := ""
			if index < len(req.Styles) {
				style = req.Styles[index]
			}
			expanded, expandErr := expandRenderLayer(ws, name, style, role, h.cfg.WMS.MaxGroupDepth)
			if expandErr != nil {
				h.writeGetMapException(w, req, ExceptionLayerNotDefined, expandErr.Error())
				return
			}
			renderLayers = append(renderLayers, expanded...)
		}
	}
	for _, layer := range renderLayers {
		if (layer.Composite != "" || layer.Opacity != 1) && !h.extensionEnabled(ws, "compositing") {
			h.writeGetMapException(w, req, ExceptionStyleNotDefined, "Layer group requires the disabled compositing extension")
			return
		}
	}

	// If no layers specified, return blank/background image (valid per WMS spec)
	if len(renderLayers) == 0 {
		body, err := encodeMapOutput(req.Format, mapRenderer, mapEncodeContext{Request: req, BaseURL: h.workspaceBaseURL(r, ws.Name), MaxBytes: h.cfg.WMS.MaxOutputBytes})
		if err != nil {
			h.writeError(w, err)
			return
		}
		if h.cfg.WMS.MaxOutputBytes > 0 && int64(len(body)) > h.cfg.WMS.MaxOutputBytes {
			h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "encoded map exceeds the configured output limit")
			return
		}
		if cacheEnabled {
			h.cache.SetTile(cacheKey, body, cacheTTL, cacheFill)
			cache.SetHTTPHeaders(w, r, ws.Settings.WMS.Public, cacheTTL, body)
		}
		setMapContentHeaders(w, req.Format)
		w.Header().Set("X-Cache", "MISS")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return
	}

	// Render each layer
	totalVertices := 0
	totalFeatures := 0
	for _, renderLayer := range renderLayers {
		layerName := renderLayer.Name
		// Find the layer and its service in the workspace. A layer the caller may
		// not read is treated as undefined so its existence is not disclosed.
		resource := ws.GetResource(layerName)
		if resource == nil || resource.Service == nil ||
			(resource.Layer != nil && !resource.Layer.VisibleToRole(workspaceRole(r, ws.ID))) ||
			(resource.Coverage != nil && !resource.Coverage.VisibleToRole(workspaceRole(r, ws.ID))) {
			h.writeGetMapException(w, req, ExceptionLayerNotDefined, fmt.Sprintf("Layer not found: %s", layerName))
			return
		}
		requestedStyle := renderLayer.Style
		if resource.Kind == workspace.ResourceCoverage {
			coverageRenderer := mapRenderer
			if renderLayer.Composite != "" || renderLayer.Opacity != 1 {
				coverageRenderer = h.newMapRenderer(transform, true, nil, ws)
			}
			if err := h.renderCoverageLayer(ctx, coverageRenderer, req, ws, resource, sldDoc, requestedStyle, transform.ScaleDenominator()); err != nil {
				h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, fmt.Sprintf("Coverage rendering failed: %v", err))
				return
			}
			if coverageRenderer != mapRenderer {
				mapRenderer.CompositeWithMode(coverageRenderer.Image(), renderLayer.Composite, renderLayer.Opacity)
			}
			continue
		}
		layer, service := resource.Layer, resource.Service

		// Check if service has a datasource
		if service.DataSource == nil {
			h.writeGetMapException(w, req, ExceptionLayerNotDefined, fmt.Sprintf("Service not available for layer: %s", layerName))
			return
		}

		// Get style for this layer
		style, styleErr := h.resolveWMSStyle(ws, resource, sldDoc, requestedStyle)
		if styleErr != nil {
			h.writeGetMapException(w, req, ExceptionStyleNotDefined, styleErr.Error())
			return
		}
		if style == nil {
			// Use default style
			style = sld.DefaultStyle(defaultStyleCfg)
		}
		if sld.StyleUsesAdvancedLabels(style) && !h.extensionEnabled(ws, "advanced-labels") {
			h.writeGetMapException(w, req, ExceptionStyleNotDefined, "Style requires the disabled advanced-labels extension")
			return
		}
		if sld.StyleUsesCompositing(style) && !h.extensionEnabled(ws, "compositing") {
			h.writeGetMapException(w, req, ExceptionStyleNotDefined, "Style requires the disabled compositing extension")
			return
		}
		if sld.StyleUsesZOrder(style) && !h.extensionEnabled(ws, "z-order") {
			h.writeGetMapException(w, req, ExceptionStyleNotDefined, "Style requires the disabled z-order extension")
			return
		}
		if sld.StyleUsesTransformation(style) && !h.extensionEnabled(ws, "rendering-transformations") {
			h.writeGetMapException(w, req, ExceptionStyleNotDefined, "Style requires the disabled rendering-transformations extension")
			return
		}
		if sld.StyleUsesRemoteGraphics(style) && !h.extensionEnabled(ws, "remote-graphics") {
			h.writeGetMapException(w, req, ExceptionStyleNotDefined, "Style requires the disabled remote-graphics extension")
			return
		}
		if sld.StyleUsesDynamicExpressions(style) && !h.extensionEnabled(ws, "dynamic-style") {
			h.writeGetMapException(w, req, ExceptionStyleNotDefined, "Style requires the disabled dynamic-style extension")
			return
		}
		layerRenderer := mapRenderer
		if sld.StyleUsesCompositing(style) || renderLayer.Composite != "" || renderLayer.Opacity != 1 {
			layerRenderer = h.newMapRenderer(transform, true, nil, ws)
		}

		// Query features using a stream where the datasource supports it.
		var featureStream datasource.RenderFeatureStream
		if layer.IsSQLView && layer.SQLViewConfig != nil {
			// SQL View layer
			sqlViewDS, ok := service.DataSource.(datasource.SQLViewDataSource)
			if !ok {
				h.writeGetMapException(w, req, ExceptionLayerNotDefined, "Data source does not support SQL views")
				return
			}
			dsConfig := convertWorkspaceSQLViewConfigForWMS(layer.SQLViewConfig)
			features, queryErr := h.queryFeaturesForMapSQLView(ctx, sqlViewDS, dsConfig, layer, req, maxFeatures+1, simplify, simplifyPixels)
			err = queryErr
			featureStream = datasource.NewSliceRenderStream(features)
		} else {
			params := mapQueryParams(req, maxFeatures+1, simplify, simplifyPixels)
			if err = applyDimensionFilters(req, layer, &params); err != nil {
				h.writeGetMapException(w, req, ExceptionInvalidDimensionValue, err.Error())
				return
			}
			if streaming, ok := service.DataSource.(datasource.RenderStreamingDataSource); ok {
				featureStream, err = streaming.QueryWKBStream(ctx, layer.SourceLayer, params)
			} else {
				features, queryErr := service.DataSource.QueryWKB(ctx, layer.SourceLayer, params)
				err = queryErr
				featureStream = datasource.NewSliceRenderStream(features)
			}
		}
		if err != nil {
			h.writeGetMapException(w, req, ExceptionInvalidParameterValue, fmt.Sprintf("Query failed: %v", err))
			return
		}
		if featureStream == nil {
			h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "data source returned no feature stream")
			return
		}
		processName := ""
		if style.Transformation != nil {
			processName = style.Transformation.Name
		}
		processFeatureLimit := maxFeatures
		if configured := h.cfg.WMS.MaxProcessFeatures; configured > 0 && configured < processFeatureLimit {
			processFeatureLimit = configured
		}
		if style.SortBy != "" || processName == "heatmap" || processName == "barnes" || processName == "pointstacker" || processName == "groupcandidateselection" {
			var buffered []datasource.RenderFeature
			for featureStream.Next() {
				buffered = append(buffered, featureStream.Feature())
				if len(buffered) > maxFeatures {
					_ = featureStream.Close()
					h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, fmt.Sprintf("render feature limit exceeded (%d)", maxFeatures))
					return
				}
			}
			streamErr := featureStream.Err()
			_ = featureStream.Close()
			if streamErr != nil {
				h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "feature stream failed")
				return
			}
			if style.SortBy != "" {
				sortRenderFeatures(buffered, style.SortBy, style.SortDescending)
			}
			if processName == "groupcandidateselection" {
				buffered = selectGroupCandidates(buffered, style.Transformation.WeightProperty)
			}
			if processName != "" && len(buffered) > processFeatureLimit {
				h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "rendering process feature limit exceeded")
				return
			}
			featureStream = datasource.NewSliceRenderStream(buffered)
		}
		if processName == "heatmap" || processName == "barnes" || processName == "pointstacker" {
			processStarted := time.Now()
			points, features, vertices := heatPointsFromStream(featureStream, style.Transformation.WeightProperty)
			totalFeatures += features
			totalVertices += vertices
			_ = featureStream.Close()
			if totalFeatures > processFeatureLimit || totalVertices > maxVertices {
				h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "render transformation limit exceeded")
				return
			}
			if processName == "pointstacker" {
				layerRenderer.Composite(renderer.RenderPointStacker(transform, points, style.Transformation.Radius, firstPointStyle(style)))
			} else {
				layerRenderer.Composite(renderer.RenderHeatmap(transform, points, style.Transformation.Radius))
			}
			if timeout := h.cfg.WMS.ProcessTimeoutMS; timeout > 0 && time.Since(processStarted) > time.Duration(timeout)*time.Millisecond {
				h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "rendering process timeout exceeded")
				return
			}
			if layerRenderer != mapRenderer {
				mode, opacity := effectiveGroupComposite(renderLayer, style)
				mapRenderer.CompositeWithMode(layerRenderer.Image(), mode, opacity)
			}
			continue
		}

		// Get scale denominator for rule matching
		scale := transform.ScaleDenominator()

		// Render features
		for featureStream.Next() {
			feat := featureStream.Feature()
			totalFeatures++
			if totalFeatures > maxFeatures {
				_ = featureStream.Close()
				h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, fmt.Sprintf("render feature limit exceeded (%d)", maxFeatures))
				return
			}
			if feat.Geometry == nil || len(feat.Geometry) == 0 {
				continue
			}

			// Parse geometry
			geom, err := renderer.ParseWKB(feat.Geometry)
			if err != nil {
				continue // Skip invalid geometries
			}
			totalVertices += geom.VertexCount()
			if totalVertices > maxVertices {
				_ = featureStream.Close()
				h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, fmt.Sprintf("render vertex limit exceeded (%d)", maxVertices))
				return
			}

			// Find matching rules
			rules := sld.FindMatchingRules(style, feat.Properties, scale)
			// Apply each matching rule in symbolizer order.
			for _, rule := range rules {
				if expressionErr := drawResolvedRule(layerRenderer, geom, feat.Properties, req.Environment, &rule); expressionErr != nil {
					_ = featureStream.Close()
					h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "style expression failed: "+expressionErr.Error())
					return
				}
			}
		}
		streamErr := featureStream.Err()
		_ = featureStream.Close()
		if streamErr != nil {
			h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "feature stream failed")
			return
		}
		if layerRenderer != mapRenderer {
			mode, opacity := effectiveGroupComposite(renderLayer, style)
			mapRenderer.CompositeWithMode(layerRenderer.Image(), mode, opacity)
		}
	}

	// Write response in requested format
	body, err := encodeMapOutput(req.Format, mapRenderer, mapEncodeContext{Request: req, BaseURL: h.workspaceBaseURL(r, ws.Name), MaxBytes: h.cfg.WMS.MaxOutputBytes})
	if err != nil {
		h.writeError(w, err)
		return
	}
	if h.cfg.WMS.MaxOutputBytes > 0 && int64(len(body)) > h.cfg.WMS.MaxOutputBytes {
		h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "encoded map exceeds the configured output limit")
		return
	}
	if cacheEnabled {
		h.cache.SetTile(cacheKey, body, cacheTTL, cacheFill)
		cache.SetHTTPHeaders(w, r, ws.Settings.WMS.Public, cacheTTL, body)
	}
	setMapContentHeaders(w, req.Format)
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func firstPointStyle(style *sld.Style) *sld.PointStyle {
	if style == nil {
		return nil
	}
	for _, rule := range style.Rules {
		for _, symbolizer := range rule.Symbolizers {
			if symbolizer.Point != nil {
				return symbolizer.Point
			}
		}
		if rule.PointStyle != nil {
			return rule.PointStyle
		}
	}
	return nil
}
func selectGroupCandidates(features []datasource.RenderFeature, property string) []datasource.RenderFeature {
	if property == "" {
		return features
	}
	seen := map[string]bool{}
	result := make([]datasource.RenderFeature, 0, len(features))
	for _, feature := range features {
		key := fmt.Sprint(feature.Properties[property])
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, feature)
	}
	return result
}

type groupRenderLayer struct {
	Name, Style, Composite string
	Opacity                float64
}

func expandRenderLayer(ws *workspace.Workspace, name, requestedStyle, role string, maxDepth int) ([]groupRenderLayer, error) {
	if maxDepth <= 0 {
		maxDepth = 8
	}
	var expand func(string, string, string, float64, map[string]bool, int) ([]groupRenderLayer, error)
	expand = func(resourceName, inheritedStyle, composite string, opacity float64, visiting map[string]bool, depth int) ([]groupRenderLayer, error) {
		if depth > maxDepth {
			return nil, fmt.Errorf("layer group nesting exceeds %d", maxDepth)
		}
		resource := ws.GetResource(resourceName)
		if resource == nil {
			return nil, fmt.Errorf("Layer not found: %s", resourceName)
		}
		if resource.Kind != workspace.ResourceGroup {
			return []groupRenderLayer{{Name: resourceName, Style: inheritedStyle, Composite: composite, Opacity: opacity}}, nil
		}
		group := resource.Group
		if !ws.GroupVisibleToRole(group, role) {
			return nil, fmt.Errorf("Layer not found: %s", resourceName)
		}
		if visiting[group.PublicID] {
			return nil, fmt.Errorf("layer group cycle includes %q", group.PublicID)
		}
		visiting[group.PublicID] = true
		defer delete(visiting, group.PublicID)
		groupStyle := inheritedStyle
		if groupStyle == "" {
			groupStyle = group.DefaultStyle
		}
		var result []groupRenderLayer
		for _, member := range group.Members {
			style := member.Style
			if groupStyle != "" {
				style = groupStyle
			}
			memberOpacity := member.EffectiveOpacity()
			memberOpacity *= opacity
			memberComposite := member.Composite
			if memberComposite == "" {
				memberComposite = composite
			}
			nested, err := expand(member.Resource, style, memberComposite, memberOpacity, visiting, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, nested...)
		}
		return result, nil
	}
	return expand(name, requestedStyle, "", 1, make(map[string]bool), 1)
}

func effectiveGroupComposite(layer groupRenderLayer, style *sld.Style) (string, float64) {
	mode, opacity := style.Composite, style.CompositeOpacity
	if layer.Composite != "" {
		mode = layer.Composite
	}
	if layer.Opacity != 1 {
		opacity *= layer.Opacity
	}
	if opacity == 0 && layer.Opacity == 1 {
		opacity = 1
	}
	return mode, opacity
}

func sortRenderFeatures(features []datasource.RenderFeature, property string, descending bool) {
	slices.SortStableFunc(features, func(a, b datasource.RenderFeature) int {
		left, right := fmt.Sprint(a.Properties[property]), fmt.Sprint(b.Properties[property])
		if leftNumber, leftErr := strconv.ParseFloat(left, 64); leftErr == nil {
			if rightNumber, rightErr := strconv.ParseFloat(right, 64); rightErr == nil {
				result := cmp.Compare(leftNumber, rightNumber)
				if descending {
					return -result
				}
				return result
			}
		}
		result := strings.Compare(left, right)
		if descending {
			return -result
		}
		return result
	})
}

func heatPointsFromStream(stream datasource.RenderFeatureStream, weightProperty string) ([]renderer.HeatPoint, int, int) {
	var points []renderer.HeatPoint
	features, vertices := 0, 0
	for stream.Next() {
		feature := stream.Feature()
		features++
		geometry, err := renderer.ParseWKB(feature.Geometry)
		if err != nil {
			continue
		}
		weight := 1.0
		if weightProperty != "" {
			if value, ok := feature.Properties[weightProperty]; ok {
				weight, _ = strconv.ParseFloat(fmt.Sprint(value), 64)
			}
		}
		var appendGeometry func(*renderer.Geometry)
		appendGeometry = func(value *renderer.Geometry) {
			if value.Type == renderer.WKBPoint && len(value.Coordinates) > 0 {
				points = append(points, renderer.HeatPoint{X: value.Coordinates[0][0], Y: value.Coordinates[0][1], Weight: weight})
				vertices++
			}
			for index := range value.Geometries {
				appendGeometry(&value.Geometries[index])
			}
		}
		appendGeometry(geometry)
	}
	return points, features, vertices
}

func drawResolvedRule(mapRenderer *renderer.MapRenderer, geom *renderer.Geometry, properties map[string]interface{}, environment map[string]string, rule *sld.ResolvedRule) error {
	if len(rule.Symbolizers) == 0 {
		mapRenderer.DrawGeometry(geom, sld.GetEffectiveStyle(rule, geom.TypeName()))
		return nil
	}
	for _, symbolizer := range rule.Symbolizers {
		resolved, err := sld.ResolveSymbolizerExpressions(symbolizer, properties, environment)
		if err != nil {
			return err
		}
		switch {
		case resolved.Point != nil:
			mapRenderer.DrawGeometry(geom, resolved.Point)
		case resolved.Line != nil:
			mapRenderer.DrawGeometry(geom, resolved.Line)
		case resolved.Polygon != nil:
			mapRenderer.DrawGeometry(geom, resolved.Polygon)
		case resolved.Text != nil:
			label := resolved.Text.Literal
			if resolved.Text.PropertyName != "" {
				if value, ok := properties[resolved.Text.PropertyName]; ok {
					label = fmt.Sprint(value)
				}
			}
			mapRenderer.DrawText(geom, resolved.Text, label)
		}
	}
	return nil
}

func (h *workspaceHandler) loadStyleFile(path string) (*sld.Style, error) {
	h.styleMu.RLock()
	cached, ok := h.styleCache[path]
	h.styleMu.RUnlock()
	if ok && time.Now().Before(cached.expires) {
		return cached.style, nil
	}
	doc, err := sld.ParseFile(path)
	if err != nil {
		return nil, err
	}
	style, err := doc.GetDefaultStyle()
	if err != nil {
		return nil, err
	}
	h.styleMu.Lock()
	if h.styleCache == nil {
		h.styleCache = make(map[string]cachedStyle)
	}
	h.styleCache[path] = cachedStyle{style: style, expires: time.Now().Add(time.Hour)}
	h.styleMu.Unlock()
	return style, nil
}

// Authorization must run even for cache hits and conditional requests.
func (h *workspaceHandler) authorizeMapResources(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, req *GetMapRequest) bool {
	role := workspaceRole(r, ws.ID)
	for _, name := range req.Layers {
		if name == "" {
			continue
		}
		resource := ws.GetResource(name)
		visible := false
		if resource != nil {
			switch resource.Kind {
			case workspace.ResourceFeature:
				visible = resource.Layer.VisibleToRole(role)
			case workspace.ResourceCoverage:
				visible = resource.Coverage.VisibleToRole(role)
			case workspace.ResourceGroup:
				visible = ws.GroupVisibleToRole(resource.Group, role)
			}
		}
		if !visible {
			h.writeGetMapException(w, req, ExceptionLayerNotDefined, "Layer not found: "+name)
			return false
		}
	}
	return true
}

func getMapCacheKey(ws *workspace.Workspace, req *GetMapRequest) string {
	styleHash := sha256.Sum256([]byte(req.SLDBody))
	return cache.TileKey(cache.TileParams{Workspace: ws.ID, Layers: strings.Join(req.Layers, ","), CRS: req.CRS,
		BBox: fmt.Sprintf("%.12g,%.12g,%.12g,%.12g", req.BBox.MinX, req.BBox.MinY, req.BBox.MaxX, req.BBox.MaxY), Width: req.Width, Height: req.Height,
		Format: req.Format, Styles: strings.Join(req.Styles, ",") + fmt.Sprintf(":%x", styleHash[:8]) + ":assets=" + ws.StyleAssetDigest, Transparent: fmt.Sprintf("%t", req.Transparent),
		BgColor: fmt.Sprintf("%02x%02x%02x%02x", req.BgColor.R, req.BgColor.G, req.BgColor.B, req.BgColor.A), Time: req.Time + "\x00" + req.Elevation + "\x00" + req.EnvironmentKey})
}

func (h *workspaceHandler) tileCachePolicy(ws *workspace.Workspace, req *GetMapRequest) (bool, time.Duration) {
	enabled, ttl := h.cache != nil, time.Duration(0)
	if h.cache == nil {
		return false, ttl
	}
	definition, ok := lookupGetMapFormat(req.Format)
	if !ok || !definition.Cacheable {
		return false, ttl
	}
	ttl = h.cache.TileTTL()
	for _, layerName := range req.Layers {
		if layerName == "" {
			continue
		}
		resource := ws.GetResource(layerName)
		if resource == nil || resource.Service == nil {
			return false, ttl
		}
		service := resource.Service
		serviceEnabled, serviceTTL := service.TileCachePolicy(ttl)
		if !serviceEnabled {
			enabled = false
		}
		if serviceTTL > 0 && serviceTTL < ttl {
			ttl = serviceTTL
		}
	}
	return enabled, ttl
}

func mapQueryParams(req *GetMapRequest, limit int, simplify bool, simplifyPixels float64) datasource.QueryParams {
	tolerance := 0.0
	if simplify {
		x := (req.BBox.MaxX - req.BBox.MinX) / float64(req.Width)
		y := (req.BBox.MaxY - req.BBox.MinY) / float64(req.Height)
		if y < x {
			x = y
		}
		tolerance = x * simplifyPixels
	}
	return datasource.QueryParams{
		BBox:              &datasource.BBox{MinX: req.BBox.MinX, MinY: req.BBox.MinY, MaxX: req.BBox.MaxX, MaxY: req.BBox.MaxY},
		BBoxSRID:          req.SRID,
		OutputSRID:        req.SRID,
		Limit:             limit,
		SimplifyTolerance: tolerance,
	}
}

func (h *workspaceHandler) wmsSimplification(ws *workspace.Workspace) (bool, float64) {
	enabled, tolerance := h.cfg.WMS.SimplifyEnabled, h.cfg.WMS.SimplifyPixelTolerance
	if tolerance <= 0 {
		tolerance = 0.5
	}
	if ws.Settings != nil {
		if ws.Settings.WMS.SimplifyEnabled != nil {
			enabled = *ws.Settings.WMS.SimplifyEnabled
		}
		if ws.Settings.WMS.SimplifyPixelTolerance > 0 {
			tolerance = ws.Settings.WMS.SimplifyPixelTolerance
		}
	}
	return enabled, tolerance
}

func (h *workspaceHandler) wmsLimits(ws *workspace.Workspace) (int, int, int, int, int) {
	w, hh, pixels := h.cfg.WMS.MaxWidth, h.cfg.WMS.MaxHeight, h.cfg.WMS.MaxPixels
	features, vertices := h.cfg.WMS.MaxRenderFeatures, h.cfg.WMS.MaxRenderVertices
	if features <= 0 {
		features = 50000
	}
	if vertices <= 0 {
		vertices = 5000000
	}
	if ws.Settings != nil {
		s := ws.Settings.WMS
		if s.MaxWidth > 0 {
			w = min(w, s.MaxWidth)
		}
		if s.MaxHeight > 0 {
			hh = min(hh, s.MaxHeight)
		}
		if s.MaxPixels > 0 {
			pixels = min(pixels, s.MaxPixels)
		}
		if s.MaxRenderFeatures > 0 {
			features = min(features, s.MaxRenderFeatures)
		}
		if s.MaxRenderVertices > 0 {
			vertices = min(vertices, s.MaxRenderVertices)
		}
	}
	return w, hh, pixels, features, vertices
}

// writeGetMapException writes an exception in the appropriate format based on the request.
func (h *workspaceHandler) writeGetMapException(w http.ResponseWriter, req *GetMapRequest, code, message string) {
	h.writeGetMapExceptionWithLocator(w, req, code, "", message)
}

func (h *workspaceHandler) writeGetMapExceptionWithLocator(w http.ResponseWriter, req *GetMapRequest, code, locator, message string) {
	definition, supportsImageExceptions := lookupGetMapFormat(req.Format)
	if !supportsImageExceptions || !definition.ImageExceptions {
		WriteExceptionWithLocator(w, code, locator, message)
		return
	}
	switch req.Exceptions {
	case ExceptionsINIMAGE:
		WriteExceptionINIMAGE(w, req.Format, req.Width, req.Height, code, message)
	case ExceptionsBLANK:
		WriteExceptionBLANK(w, req.Format, req.Width, req.Height, req.Transparent, req.BgColor)
	default:
		WriteExceptionWithLocator(w, code, locator, message)
	}
}

// queryFeaturesForMap queries features for rendering using QueryWKB.
func (h *workspaceHandler) queryFeaturesForMap(
	ctx context.Context,
	ds datasource.DataSource,
	layerName string,
	layerInfo *datasource.LayerInfo,
	req *GetMapRequest,
	limit int,
) ([]datasource.RenderFeature, error) {
	// Build query parameters
	return ds.QueryWKB(ctx, layerName, mapQueryParams(req, limit, h.cfg.WMS.SimplifyEnabled, h.cfg.WMS.SimplifyPixelTolerance))
}

// queryFeaturesForMapSQLView queries SQL View features for rendering using QuerySQLViewWKB.
func (h *workspaceHandler) queryFeaturesForMapSQLView(
	ctx context.Context,
	ds datasource.SQLViewDataSource,
	config *datasource.SQLViewConfig,
	layer *workspace.Layer,
	req *GetMapRequest,
	limit int,
	simplify bool,
	simplifyPixels float64,
) ([]datasource.RenderFeature, error) {
	// Build query parameters
	params := mapQueryParams(req, limit, simplify, simplifyPixels)
	if err := applyDimensionFilters(req, layer, &params); err != nil {
		return nil, err
	}

	return ds.QuerySQLViewWKB(ctx, config, params)
}

// convertWorkspaceSQLViewConfigForWMS converts workspace.SQLViewConfig to datasource.SQLViewConfig.
func convertWorkspaceSQLViewConfigForWMS(cfg *workspace.SQLViewConfig) *datasource.SQLViewConfig {
	if cfg == nil {
		return nil
	}
	dsCfg := &datasource.SQLViewConfig{
		SQL:            cfg.SQL,
		GeometryColumn: cfg.GeometryColumn,
		GeometryType:   cfg.GeometryType,
		SRID:           cfg.SRID,
		IDColumn:       cfg.IDColumn,
		ReadOnly:       cfg.ReadOnly,
	}
	if len(cfg.Properties) > 0 {
		dsCfg.Properties = make([]*datasource.SQLViewProperty, len(cfg.Properties))
		for i, prop := range cfg.Properties {
			dsCfg.Properties[i] = &datasource.SQLViewProperty{
				Name: prop.Name,
				Type: prop.Type,
			}
		}
	}
	return dsCfg
}
