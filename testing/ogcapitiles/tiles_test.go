// Package ogcapitiles exercises the public OGC API - Tiles contract against a
// running deterministic demo workspace.
package ogcapitiles

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

type collection struct {
	ID string
}

type link struct {
	Href string `json:"href"`
	Rel  string `json:"rel"`
	Type string `json:"type"`
}

type tileset struct {
	TileMatrixSetID string `json:"tileMatrixSetId"`
	Links           []link `json:"links"`
}

type tilesetList struct {
	TileSets []tileset `json:"tilesets"`
}

func TestTilesMetadataAndFormats(t *testing.T) {
	endpoint := strings.TrimRight(os.Getenv("OGC_TILES_TEST_URL"), "/")
	if endpoint == "" {
		t.Skip("OGC_TILES_TEST_URL is not set")
	}

	var conformance struct {
		ConformsTo []string
	}
	getJSON(t, endpoint+"/conformance", &conformance)
	for _, required := range []string{
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/core",
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/tileset",
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/tilesets-list",
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/geodata-tilesets",
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/mvt",
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/png",
		"http://www.opengis.net/spec/ogcapi-tiles-1/1.0/conf/jpeg",
	} {
		if !contains(conformance.ConformsTo, required) {
			t.Fatalf("conformance declaration missing %s: %v", required, conformance.ConformsTo)
		}
	}

	var collections struct {
		Collections []collection
	}
	getJSON(t, endpoint+"/collections", &collections)
	if len(collections.Collections) == 0 {
		t.Fatal("no tile collections were published")
	}

	collectionID := "places"
	if !hasCollection(collections.Collections, collectionID) {
		collectionID = collections.Collections[0].ID
	}
	escaped := url.PathEscape(collectionID)
	t.Run("VectorTileset", func(t *testing.T) {
		var metadata tilesetList
		getJSON(t, endpoint+"/collections/"+escaped+"/tiles", &metadata)
		template := tileItemTemplate(t, metadata, "WebMercatorQuad", "application/vnd.mapbox-vector-tile")
		response := get(t, expandTileTemplate(template, "0", "0", "0"))
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Content-Type"), "mapbox-vector-tile") {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("vector tile status=%d content-type=%q body=%s", response.StatusCode, response.Header.Get("Content-Type"), body)
		}
	})

	t.Run("RasterMapTileAndTileJSON", func(t *testing.T) {
		var tileJSON struct {
			Tiles []string
		}
		getJSON(t, endpoint+"/collections/"+escaped+"/tilejson.json", &tileJSON)
		if len(tileJSON.Tiles) == 0 {
			t.Fatal("TileJSON contains no tile template")
		}
		for _, placeholder := range []string{"{z}", "{y}", "{x}"} {
			if !strings.Contains(tileJSON.Tiles[0], placeholder) {
				t.Fatalf("TileJSON template %q is missing %s", tileJSON.Tiles[0], placeholder)
			}
		}

		var metadata tilesetList
		getJSON(t, endpoint+"/collections/"+escaped+"/map/tiles", &metadata)
		template := tileItemTemplate(t, metadata, "WebMercatorQuad", "image/png")
		response := get(t, expandTileTemplate(template, "0", "0", "0"))
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "image/png") {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("map tile status=%d content-type=%q body=%s", response.StatusCode, response.Header.Get("Content-Type"), body)
		}
	})
}

func tileItemTemplate(t *testing.T, metadata tilesetList, matrixSet, mediaType string) string {
	t.Helper()
	for _, tileset := range metadata.TileSets {
		if tileset.TileMatrixSetID != matrixSet {
			continue
		}
		for _, link := range tileset.Links {
			if link.Rel != "item" || link.Type != mediaType {
				continue
			}
			for _, placeholder := range []string{"{tileMatrix}", "{tileRow}", "{tileCol}"} {
				if !strings.Contains(link.Href, placeholder) {
					t.Fatalf("item template %q is missing %s", link.Href, placeholder)
				}
			}
			return link.Href
		}
	}
	t.Fatalf("no %s item template found for %s: %+v", mediaType, matrixSet, metadata.TileSets)
	return ""
}

func expandTileTemplate(template, matrix, row, column string) string {
	return strings.NewReplacer(
		"{tileMatrix}", matrix,
		"{tileRow}", row,
		"{tileCol}", column,
	).Replace(template)
}

func getJSON(t *testing.T, target string, destination any) {
	t.Helper()
	response := get(t, target)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET %s: status=%d body=%s", target, response.StatusCode, body)
	}
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatalf("decode %s: %v", target, err)
	}
}

func get(t *testing.T, target string) *http.Response {
	t.Helper()
	response, err := http.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	return response
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasCollection(collections []collection, target string) bool {
	for _, collection := range collections {
		if collection.ID == target {
			return true
		}
	}
	return false
}
