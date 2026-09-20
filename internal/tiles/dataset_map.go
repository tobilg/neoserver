package tiles

import (
	"net/http"
	"strconv"

	"github.com/tobilg/neoserver/internal/workspace"
)

// Map tiles cover the complete configured grid, including empty tiles outside
// the data extent. Advertise the same matrix range enforced by the renderer.
func (h *workspaceHandler) mapTileMatrixLimits(tms *TileMatrixSetDefinition) []TileMatrixSetLimits {
	var limits []TileMatrixSetLimits
	for _, matrix := range tms.TileMatrices {
		zoom, err := strconv.Atoi(matrix.ID)
		if err != nil || zoom < h.cfg.Tiles.MinZoom || zoom > h.cfg.Tiles.MaxZoom {
			continue
		}
		limits = append(limits, TileMatrixSetLimits{
			TileMatrixID: matrix.ID,
			MaxTileRow:   matrix.MatrixHeight - 1,
			MaxTileCol:   matrix.MatrixWidth - 1,
		})
	}
	return limits
}

// datasetMapResource operates on the request's immutable workspace snapshot.
// Every discovery surface uses this same access check; rendering repeats it
// before entering the shared cache, whose identity does not include a role.
func datasetMapResource(ws *workspace.Workspace, role string) *workspace.PublishedResource {
	if ws == nil || ws.Settings == nil {
		return nil
	}
	settings := ws.Settings.OGCTilesAPI
	if !settings.Enabled || !settings.Settings.MapTiles.Enabled || settings.Settings.DatasetMapLayerGroupID == "" {
		return nil
	}
	hasFormat := false
	for _, format := range settings.Settings.MapTiles.Formats {
		if format == MediaTypePNG || format == MediaTypeJPEG || format == MediaTypeWEBP {
			hasFormat = true
		}
	}
	if !hasFormat {
		return nil
	}
	hasGrid := false
	for _, id := range settings.Settings.TileMatrixSets {
		if _, err := GetTileMatrixSetDefinition(id); err == nil {
			hasGrid = true
			break
		}
	}
	if !hasGrid {
		return nil
	}
	for _, group := range ws.Groups {
		if group.ID == settings.Settings.DatasetMapLayerGroupID && len(group.Members) > 0 && ws.GroupVisibleToRole(group, role) {
			return &workspace.PublishedResource{Kind: workspace.ResourceGroup, Group: group}
		}
	}
	return nil
}

func (h *workspaceHandler) mapResource(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, collectionID string) *workspace.PublishedResource {
	var resource *workspace.PublishedResource
	if collectionID == "" {
		if r.URL.Query().Has("collections") {
			writeErr(w, http.StatusBadRequest, "InvalidParameter", "collections selection is not supported for the workspace map")
			return nil
		}
		resource = datasetMapResource(ws, workspaceRole(r, ws.ID))
	} else {
		resource = ws.GetResource(collectionID)
		if !resourceVisible(ws, resource, workspaceRole(r, ws.ID)) {
			resource = nil
		}
	}
	if resource == nil {
		writeErr(w, http.StatusNotFound, "NotFound", "map resource not found")
	}
	return resource
}

func mapTilesURL(base, collectionID string) string {
	if collectionID == "" {
		return base + "/map/tiles"
	}
	return base + "/collections/" + urlPathEscape(collectionID) + "/map/tiles"
}
