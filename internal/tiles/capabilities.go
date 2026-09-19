package tiles

import (
	"slices"

	claim "github.com/tobilg/neoserver/internal/conformance"
	"github.com/tobilg/neoserver/internal/store"
)

// effectiveConformanceClasses derives the public declaration from the same
// workspace format switches enforced by the tile handlers.
func effectiveConformanceClasses(settings store.OGCTilesAPISettings) []string {
	keys := []string{claim.TilesCore}
	vector := settings.Settings.VectorTiles.Enabled && slices.Contains(settings.Settings.VectorTiles.Formats, MediaTypeMVT)
	mapTiles := settings.Settings.MapTiles.Enabled && len(settings.Settings.MapTiles.Formats) > 0
	if vector || mapTiles {
		keys = append(keys, claim.TilesTileSet, claim.TilesTileSetsList, claim.TilesGeoData)
	}
	if vector {
		keys = append(keys, claim.TilesMVT)
	}
	if mapTiles && slices.Contains(settings.Settings.MapTiles.Formats, MediaTypePNG) {
		keys = append(keys, claim.TilesPNG)
	}
	if mapTiles && slices.Contains(settings.Settings.MapTiles.Formats, MediaTypeJPEG) {
		keys = append(keys, claim.TilesJPEG)
	}
	return claim.URIs(keys...)
}
