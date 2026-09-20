package wmts

import (
	"bytes"
	"fmt"
	"math"
	"strconv"

	"github.com/tobilg/neoserver/internal/datasource/rastergrid"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/workspace"
)

type matrixLimits struct{ minRow, maxRow, minCol, maxCol int }

// Both advertisement and request validation use this calculation. Unknown or
// stale envelopes keep the full matrix available, as before limits were enabled.
func resourceMatrixLimits(resource *workspace.PublishedResource, matrixSet string, matrix tiles.TileMatrix) (matrixLimits, bool) {
	var extent *store.SpatialExtent
	switch {
	case resource.Layer != nil:
		extent = resource.Layer.NativeExtent
	case resource.Coverage != nil:
		extent = resource.Coverage.NativeExtent
	case resource.Group != nil:
		extent = resource.Group.NativeExtent
	}
	if extent == nil || extent.Stale || extent.SRID <= 0 || len(matrix.PointOfOrigin) != 2 {
		return matrixLimits{}, false
	}
	bbox := [4]float64{extent.MinX, extent.MinY, extent.MaxX, extent.MaxY}
	if extent.SRID == 4326 && tiles.GetTMSSRID(matrixSet) == 3857 {
		bbox[1], bbox[3] = max(-85.0511287798066, bbox[1]), min(85.0511287798066, bbox[3])
	}
	// Points and horizontal/vertical line envelopes are valid bounds too.
	if bbox[0] == bbox[2] {
		bbox[2] = math.Nextafter(bbox[2], math.Inf(1))
	}
	if bbox[1] == bbox[3] {
		bbox[3] = math.Nextafter(bbox[3], math.Inf(1))
	}
	bbox, err := rastergrid.TransformBBox(bbox, fmt.Sprintf("EPSG:%d", extent.SRID), fmt.Sprintf("EPSG:%d", tiles.GetTMSSRID(matrixSet)))
	if err != nil {
		return matrixLimits{}, false
	}
	w, h := matrix.CellSize*float64(matrix.TileWidth), matrix.CellSize*float64(matrix.TileHeight)
	if w <= 0 || h <= 0 {
		return matrixLimits{}, false
	}
	col := func(x float64) int { return int(math.Floor((x - matrix.PointOfOrigin[0]) / w)) }
	row := func(y float64) int { return int(math.Floor((matrix.PointOfOrigin[1] - y) / h)) }
	limits := matrixLimits{max(0, row(bbox[3])), min(matrix.MatrixHeight-1, row(bbox[1])), max(0, col(bbox[0])), min(matrix.MatrixWidth-1, col(bbox[2]))}
	return limits, limits.minRow <= limits.maxRow && limits.minCol <= limits.maxCol
}

func writeMatrixLimits(document *bytes.Buffer, resource *workspace.PublishedResource, matrixSet string, minZoom, maxZoom int) {
	definition, err := tiles.GetTileMatrixSetDefinition(matrixSet)
	if err != nil {
		return
	}
	var content bytes.Buffer
	for _, matrix := range definition.TileMatrices {
		zoom, err := strconv.Atoi(matrix.ID)
		if err != nil || zoom < minZoom || zoom > maxZoom {
			continue
		}
		limits, ok := resourceMatrixLimits(resource, matrixSet, matrix)
		if !ok {
			continue
		}
		content.WriteString(`<TileMatrixLimits><TileMatrix>`)
		xmlText(&content, matrix.ID)
		fmt.Fprintf(&content, `</TileMatrix><MinTileRow>%d</MinTileRow><MaxTileRow>%d</MaxTileRow><MinTileCol>%d</MinTileCol><MaxTileCol>%d</MaxTileCol></TileMatrixLimits>`, limits.minRow, limits.maxRow, limits.minCol, limits.maxCol)
	}
	if content.Len() > 0 {
		document.WriteString(`<TileMatrixSetLimits>`)
		document.Write(content.Bytes())
		document.WriteString(`</TileMatrixSetLimits>`)
	}
}
