package tiles

import (
	"slices"
	"testing"

	claim "github.com/tobilg/neoserver/internal/conformance"
	"github.com/tobilg/neoserver/internal/store"
)

func TestEffectiveConformanceClassesTracksEnabledFormats(t *testing.T) {
	tests := []struct {
		name     string
		settings store.OGCTilesAPISettings
		want     []string
	}{
		{
			name:     "default vector and map formats",
			settings: store.DefaultOGCTilesAPISettings(),
			want: claim.URIs(
				claim.TilesCore, claim.TilesTileSet, claim.TilesTileSetsList, claim.TilesGeoData,
				claim.TilesMVT, claim.TilesPNG, claim.TilesJPEG,
			),
		},
		{
			name: "png and webp map tiles",
			settings: store.OGCTilesAPISettings{Settings: store.OGCTilesAPIInnerSettings{
				MapTiles: store.OGCTilesAPIMapSettings{Enabled: true, Formats: []string{MediaTypePNG, MediaTypeWEBP}},
			}},
			want: claim.URIs(claim.TilesCore, claim.TilesTileSet, claim.TilesTileSetsList, claim.TilesGeoData, claim.TilesPNG),
		},
		{
			name: "vector only",
			settings: store.OGCTilesAPISettings{Settings: store.OGCTilesAPIInnerSettings{
				VectorTiles: store.OGCTilesAPIVectorSettings{Enabled: true, Formats: []string{MediaTypeMVT}},
			}},
			want: claim.URIs(claim.TilesCore, claim.TilesTileSet, claim.TilesTileSetsList, claim.TilesGeoData, claim.TilesMVT),
		},
		{
			name:     "metadata only",
			settings: store.OGCTilesAPISettings{},
			want:     claim.URIs(claim.TilesCore),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveConformanceClasses(tt.settings)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("classes = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEffectiveConformanceClassesNeverAdvertisesWebPClass(t *testing.T) {
	settings := store.OGCTilesAPISettings{Settings: store.OGCTilesAPIInnerSettings{
		MapTiles: store.OGCTilesAPIMapSettings{Enabled: true, Formats: []string{MediaTypeWEBP}},
	}}
	for _, uri := range effectiveConformanceClasses(settings) {
		if slices.Contains([]string{
			"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/webp",
			"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/web-p",
		}, uri) {
			t.Fatalf("invented WebP requirement class was advertised: %s", uri)
		}
	}
}
