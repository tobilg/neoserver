package wmts

import (
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/workspace"
)

// effectiveCapabilities is the single source of truth for what a workspace's
// WMTS endpoint advertises and accepts. WMTS publication deliberately reuses
// the tile formats and matrix sets configured for OGC API - Tiles.
type effectiveCapabilities struct {
	Operations     []string
	TileMatrixSets []string
	FeatureInfo    bool
}

func capabilitiesFor(ws *workspace.Workspace) effectiveCapabilities {
	capabilities := effectiveCapabilities{
		Operations:     []string{"GetCapabilities", "GetTile"},
		TileMatrixSets: append([]string(nil), ws.Settings.OGCTilesAPI.Settings.TileMatrixSets...),
		FeatureInfo:    ws.Settings.WMTS.FeatureInfoEnabled,
	}
	if capabilities.FeatureInfo {
		capabilities.Operations = append(capabilities.Operations, "GetFeatureInfo")
	}
	return capabilities
}

func tileFormatsForResource(ws *workspace.Workspace, resource *workspace.PublishedResource) []string {
	formats := make([]string, 0, 4)
	settings := ws.Settings.OGCTilesAPI.Settings
	if settings.MapTiles.Enabled {
		formats = appendUnique(formats, settings.MapTiles.Formats...)
	}
	if ws.Settings.WMTS.VectorTilesEnabled && resource.Kind == workspace.ResourceFeature && settings.VectorTiles.Enabled {
		formats = appendUnique(formats, settings.VectorTiles.Formats...)
	}
	return formats
}

func tileRequestType(ws *workspace.Workspace, resource *workspace.PublishedResource, format string) (string, bool) {
	if format == tiles.MediaTypeMVT {
		return "vector", ws.Settings.WMTS.VectorTilesEnabled &&
			resource.Kind == workspace.ResourceFeature &&
			ws.Settings.OGCTilesAPI.Settings.VectorTiles.Enabled &&
			contains(ws.Settings.OGCTilesAPI.Settings.VectorTiles.Formats, format)
	}
	return "map", ws.Settings.OGCTilesAPI.Settings.MapTiles.Enabled &&
		contains(ws.Settings.OGCTilesAPI.Settings.MapTiles.Formats, format)
}

func appendUnique(values []string, additions ...string) []string {
	for _, addition := range additions {
		if addition != "" && !contains(values, addition) {
			values = append(values, addition)
		}
	}
	return values
}
