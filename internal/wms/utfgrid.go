package wms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/sld"
	"github.com/tobilg/neoserver/internal/workspace"
)

const utfGridResolution = 4

type utfGridHitBuffer struct {
	width, height int
	transform     *renderer.Transform
	cells         []int
}

func newUTFGridHitBuffer(bbox query.BBox, mapWidth, mapHeight int) *utfGridHitBuffer {
	width := (mapWidth + utfGridResolution - 1) / utfGridResolution
	height := (mapHeight + utfGridResolution - 1) / utfGridResolution
	return &utfGridHitBuffer{
		width: width, height: height,
		transform: renderer.NewTransform(bbox, width, height),
		cells:     make([]int, width*height),
	}
}

func (g *utfGridHitBuffer) set(x, y, value int) {
	if x >= 0 && x < g.width && y >= 0 && y < g.height {
		g.cells[y*g.width+x] = value
	}
}

func (g *utfGridHitBuffer) paint(geometry *renderer.Geometry, value int) {
	if geometry == nil {
		return
	}
	switch geometry.Type {
	case renderer.WKBPoint:
		for _, coordinate := range geometry.Coordinates {
			if len(coordinate) < 2 {
				continue
			}
			x, y := g.transform.ToPixel(coordinate[0], coordinate[1])
			g.set(int(math.Floor(x)), int(math.Floor(y)), value)
		}
	case renderer.WKBLineString:
		g.paintLine(geometry.Coordinates, value)
	case renderer.WKBPolygon:
		g.paintPolygon(geometry.Rings, value)
	default:
		for index := range geometry.Geometries {
			g.paint(&geometry.Geometries[index], value)
		}
	}
}

func (g *utfGridHitBuffer) paintLine(coordinates [][]float64, value int) {
	for index := 1; index < len(coordinates); index++ {
		if len(coordinates[index-1]) < 2 || len(coordinates[index]) < 2 {
			continue
		}
		x0, y0 := g.transform.ToPixel(coordinates[index-1][0], coordinates[index-1][1])
		x1, y1 := g.transform.ToPixel(coordinates[index][0], coordinates[index][1])
		clipped, ok := clipUTFGridLine(x0, y0, x1, y1, float64(g.width), float64(g.height))
		if !ok {
			continue
		}
		g.paintSegment(int(math.Floor(clipped[0])), int(math.Floor(clipped[1])), int(math.Floor(clipped[2])), int(math.Floor(clipped[3])), value)
	}
}

func clipUTFGridLine(x0, y0, x1, y1, width, height float64) ([4]float64, bool) {
	dx, dy := x1-x0, y1-y0
	t0, t1 := 0.0, 1.0
	maxX, maxY := math.Nextafter(width, 0), math.Nextafter(height, 0)
	for _, edge := range [][2]float64{{-dx, x0}, {dx, maxX - x0}, {-dy, y0}, {dy, maxY - y0}} {
		p, q := edge[0], edge[1]
		if p == 0 {
			if q < 0 {
				return [4]float64{}, false
			}
			continue
		}
		ratio := q / p
		if p < 0 {
			if ratio > t1 {
				return [4]float64{}, false
			}
			t0 = max(t0, ratio)
		} else {
			if ratio < t0 {
				return [4]float64{}, false
			}
			t1 = min(t1, ratio)
		}
	}
	return [4]float64{x0 + t0*dx, y0 + t0*dy, x0 + t1*dx, y0 + t1*dy}, true
}

func (g *utfGridHitBuffer) paintSegment(x0, y0, x1, y1, value int) {
	dx := absInt(x1 - x0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	dy := -absInt(y1 - y0)
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		g.set(x0, y0, value)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func (g *utfGridHitBuffer) paintPolygon(rings [][][]float64, value int) {
	if len(rings) == 0 || len(rings[0]) == 0 {
		return
	}
	pixelRings := make([][][2]float64, 0, len(rings))
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, ring := range rings {
		pixels := make([][2]float64, 0, len(ring))
		for _, coordinate := range ring {
			if len(coordinate) < 2 {
				continue
			}
			x, y := g.transform.ToPixel(coordinate[0], coordinate[1])
			pixels = append(pixels, [2]float64{x, y})
			minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
		}
		pixelRings = append(pixelRings, pixels)
		g.paintPixelRing(pixels, value)
	}
	startX, startY := max(0, int(math.Floor(minX))), max(0, int(math.Floor(minY)))
	endX, endY := min(g.width-1, int(math.Ceil(maxX))), min(g.height-1, int(math.Ceil(maxY)))
	for y := startY; y <= endY; y++ {
		for x := startX; x <= endX; x++ {
			pointX, pointY := float64(x)+0.5, float64(y)+0.5
			if !pointInUTFGridRing(pointX, pointY, pixelRings[0]) {
				continue
			}
			insideHole := false
			for _, hole := range pixelRings[1:] {
				if pointInUTFGridRing(pointX, pointY, hole) {
					insideHole = true
					break
				}
			}
			if !insideHole {
				g.set(x, y, value)
			}
		}
	}
}

func (g *utfGridHitBuffer) paintPixelRing(ring [][2]float64, value int) {
	for index := 1; index < len(ring); index++ {
		clipped, ok := clipUTFGridLine(ring[index-1][0], ring[index-1][1], ring[index][0], ring[index][1], float64(g.width), float64(g.height))
		if ok {
			g.paintSegment(int(math.Floor(clipped[0])), int(math.Floor(clipped[1])), int(math.Floor(clipped[2])), int(math.Floor(clipped[3])), value)
		}
	}
}

func pointInUTFGridRing(x, y float64, ring [][2]float64) bool {
	if len(ring) == 0 {
		return false
	}
	inside := false
	for current, previous := 0, len(ring)-1; current < len(ring); previous, current = current, current+1 {
		a, b := ring[current], ring[previous]
		if (a[1] > y) != (b[1] > y) && x < (b[0]-a[0])*(y-a[1])/(b[1]-a[1])+a[0] {
			inside = !inside
		}
	}
	return inside
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

type utfGridDocument struct {
	Grid []string                  `json:"grid"`
	Keys []string                  `json:"keys"`
	Data map[string]map[string]any `json:"data"`
}

func utfGridRune(index int) rune {
	value := index + 32
	if value >= 34 {
		value++
	}
	if value >= 92 {
		value++
	}
	return rune(value)
}

func (g *utfGridHitBuffer) document(keys []string, data map[string]map[string]any) utfGridDocument {
	rows := make([]string, g.height)
	for y := 0; y < g.height; y++ {
		row := make([]rune, g.width)
		for x := 0; x < g.width; x++ {
			row[x] = utfGridRune(g.cells[y*g.width+x])
		}
		rows[y] = string(row)
	}
	return utfGridDocument{Grid: rows, Keys: keys, Data: data}
}

func (h *workspaceHandler) renderUTFGrid(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, req *GetMapRequest, sldDoc *sld.StyledLayerDescriptor, maxFeatures, maxVertices int, simplify bool, simplifyPixels float64) {
	if len(req.Layers) != 1 || req.Layers[0] == "" {
		h.writeGetMapExceptionWithLocator(w, req, ExceptionInvalidParameterValue, "LAYERS", "UTFGrid requires exactly one vector layer")
		return
	}
	resource := ws.GetResource(req.Layers[0])
	if resource == nil || resource.Kind != workspace.ResourceFeature || resource.Layer == nil || resource.Service == nil || resource.Service.DataSource == nil || !resource.Layer.VisibleToRole(workspaceRole(r, ws.ID)) {
		h.writeGetMapExceptionWithLocator(w, req, ExceptionLayerNotDefined, "LAYERS", "UTFGrid requires one visible vector layer")
		return
	}
	requestedStyle := ""
	if len(req.Styles) > 0 {
		requestedStyle = req.Styles[0]
	}
	style, err := h.resolveWMSStyle(ws, resource, sldDoc, requestedStyle)
	if err != nil {
		h.writeGetMapException(w, req, ExceptionStyleNotDefined, err.Error())
		return
	}
	if style != nil && style.Transformation != nil {
		h.writeGetMapExceptionWithLocator(w, req, ExceptionInvalidParameterValue, "FORMAT", "UTFGrid does not support rendering transformations")
		return
	}
	layer := resource.Layer
	params := mapQueryParams(req, maxFeatures+1, simplify, simplifyPixels)
	if err := applyDimensionFilters(req, layer, &params); err != nil {
		h.writeGetMapException(w, req, ExceptionInvalidDimensionValue, err.Error())
		return
	}
	stream, err := h.utfGridFeatureStream(r.Context(), resource, params)
	if err != nil {
		h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "UTFGrid query failed")
		return
	}
	defer stream.Close()
	grid := newUTFGridHitBuffer(req.BBox, req.Width, req.Height)
	keys := []string{""}
	data := make(map[string]map[string]any)
	indexes := make(map[string]int)
	featureCount, vertexCount := 0, 0
	for stream.Next() {
		feature := stream.Feature()
		featureCount++
		if featureCount > maxFeatures {
			h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, fmt.Sprintf("render feature limit exceeded (%d)", maxFeatures))
			return
		}
		geometry, parseErr := renderer.ParseWKB(feature.Geometry)
		if parseErr != nil {
			continue
		}
		vertexCount += geometry.VertexCount()
		if vertexCount > maxVertices {
			h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, fmt.Sprintf("render vertex limit exceeded (%d)", maxVertices))
			return
		}
		id := datasource.StableRenderFeatureID(feature.ID, feature.Geometry, feature.Properties)
		id = layer.PublicID + "." + id
		index, exists := indexes[id]
		if !exists {
			index = len(keys)
			indexes[id] = index
			keys = append(keys, id)
			data[id] = feature.Properties
		}
		grid.paint(geometry, index)
	}
	if err := stream.Err(); err != nil {
		h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "UTFGrid feature stream failed")
		return
	}
	body, err := json.Marshal(grid.document(keys, data))
	if err != nil {
		h.writeError(w, err)
		return
	}
	if h.cfg.WMS.MaxOutputBytes > 0 && int64(len(body)) > h.cfg.WMS.MaxOutputBytes {
		h.writeGetMapException(w, req, ExceptionOperationProcessingFailed, "encoded map exceeds the configured output limit")
		return
	}
	setMapContentHeaders(w, req.Format)
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *workspaceHandler) utfGridFeatureStream(ctx context.Context, resource *workspace.PublishedResource, params datasource.QueryParams) (datasource.RenderFeatureStream, error) {
	layer, service := resource.Layer, resource.Service
	if layer.IsSQLView && layer.SQLViewConfig != nil {
		sqlView, ok := service.DataSource.(datasource.SQLViewDataSource)
		if !ok {
			return nil, errors.New("data source does not support SQL views")
		}
		features, err := sqlView.QuerySQLViewWKB(ctx, convertWorkspaceSQLViewConfigForWMS(layer.SQLViewConfig), params)
		if err != nil {
			return nil, err
		}
		return datasource.NewSliceRenderStream(features), nil
	}
	if streaming, ok := service.DataSource.(datasource.RenderStreamingDataSource); ok {
		return streaming.QueryWKBStream(ctx, layer.SourceLayer, params)
	}
	features, err := service.DataSource.QueryWKB(ctx, layer.SourceLayer, params)
	if err != nil {
		return nil, err
	}
	return datasource.NewSliceRenderStream(features), nil
}
