package tilejobs

import (
	"math"
	"testing"

	"github.com/tobilg/neoserver/internal/tiles"
)

func TestBuildChunksUsesMatrixDimensions(t *testing.T) {
	webChunks, webTotal, err := buildChunks(tiles.TMSWebMercatorQuad, 0, 1, [4]float64{-tiles.WebMercatorOriginY, -tiles.WebMercatorOriginY, tiles.WebMercatorOriginY, tiles.WebMercatorOriginY})
	if err != nil {
		t.Fatal(err)
	}
	if webTotal != 5 || len(webChunks) != 2 {
		t.Fatalf("WebMercator chunks=%d total=%d", len(webChunks), webTotal)
	}
	crs84Chunks, crs84Total, err := buildChunks(tiles.TMSWorldCRS84Quad, 0, 0, [4]float64{-180, -90, 180, 90})
	if err != nil {
		t.Fatal(err)
	}
	if crs84Total != 2 || len(crs84Chunks) != 1 || crs84Chunks[0].MaxCol != 1 {
		t.Fatalf("CRS84 chunks=%+v total=%d", crs84Chunks, crs84Total)
	}
}

func TestTransformBoundsRoundTrip(t *testing.T) {
	original := [4]float64{-12, -45, 33, 70}
	mercator, err := transformBounds(original, 4326, 3857)
	if err != nil {
		t.Fatal(err)
	}
	result, err := transformBounds(mercator, 3857, 4326)
	if err != nil {
		t.Fatal(err)
	}
	for index := range original {
		if math.Abs(original[index]-result[index]) > 1e-9 {
			t.Fatalf("coordinate %d: got %.12f want %.12f", index, result[index], original[index])
		}
	}
}

func TestTransformBoundsFromProjectedCRS(t *testing.T) {
	result, err := transformBounds([4]float64{500000, 5700000, 510000, 5710000}, 32632, 4326)
	if err != nil {
		t.Fatal(err)
	}
	if result[0] < 8 || result[2] > 10 || result[1] < 50 || result[3] > 53 || result[0] >= result[2] || result[1] >= result[3] {
		t.Fatalf("unexpected UTM envelope: %v", result)
	}
}

func TestParseSRID(t *testing.T) {
	for input, expected := range map[string]int{"EPSG:4326": 4326, "http://www.opengis.net/def/crs/EPSG/0/3857": 3857, "CRS84": 4326, "unknown": 0} {
		if got := parseSRID(input); got != expected {
			t.Fatalf("parseSRID(%q)=%d want %d", input, got, expected)
		}
	}
}
