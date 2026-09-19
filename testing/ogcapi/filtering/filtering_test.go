package filtering

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
)

const (
	queryablesClass = "http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/queryables"
	filterClass     = "http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/filter"
	featuresFilter  = "http://www.opengis.net/spec/ogcapi-features-3/1.0/conf/features-filter"
	cqlTextClass    = "http://www.opengis.net/spec/cql2/1.0/conf/cql2-text"
	basicCQLClass   = "http://www.opengis.net/spec/cql2/1.0/conf/basic-cql2"
	basicSpatial    = "http://www.opengis.net/spec/cql2/1.0/conf/basic-spatial-functions"
)

func firstCollection(t *testing.T) ogcapi.Collection {
	t.Helper()
	if len(testCtx.Collections) == 0 {
		t.Skip("No collections available")
	}
	return testCtx.Collections[0]
}

func TestFiltering_AdvertisedClasses(t *testing.T) {
	for _, class := range []string{queryablesClass, filterClass, featuresFilter, cqlTextClass, basicCQLClass, basicSpatial} {
		if !testCtx.HasConformanceClass(class) {
			t.Errorf("missing conformance class %s", class)
		}
	}
}

func TestQueryables_JSONSchemaAndCollectionLink(t *testing.T) {
	collection := firstCollection(t)
	resp, err := testCtx.Client.Get(fmt.Sprintf("/collections/%s/queryables", collection.ID), "application/schema+json")
	if err != nil {
		t.Fatal(err)
	}
	ogcapi.AssertStatusCode(t, resp, 200)
	if !strings.HasPrefix(resp.Headers.Get("Content-Type"), "application/schema+json") {
		t.Fatalf("Content-Type=%q", resp.Headers.Get("Content-Type"))
	}
	if resp.JSON["$schema"] != "https://json-schema.org/draft/2020-12/schema" || resp.JSON["type"] != "object" {
		t.Fatalf("invalid queryables schema: %s", resp.Body)
	}
	if additional, ok := resp.JSON["additionalProperties"].(bool); !ok || additional {
		t.Fatalf("additionalProperties must be false: %v", resp.JSON["additionalProperties"])
	}
	collectionResp, err := testCtx.Client.GetJSON(fmt.Sprintf("/collections/%s", collection.ID))
	if err != nil {
		t.Fatal(err)
	}
	links, _ := collectionResp.JSON["links"].([]any)
	found := false
	for _, raw := range links {
		link, _ := raw.(map[string]any)
		if link["rel"] == "http://www.opengis.net/def/rel/ogc/1.0/queryables" && link["type"] == "application/schema+json" {
			found = true
		}
	}
	if !found {
		t.Fatal("collection has no Queryables link")
	}
}

func TestFiltering_CQL2TextDefaultAndSelection(t *testing.T) {
	collection := firstCollection(t)
	queryables, err := testCtx.Client.Get(fmt.Sprintf("/collections/%s/queryables", collection.ID), "application/schema+json")
	if err != nil {
		t.Fatal(err)
	}
	properties, _ := queryables.JSON["properties"].(map[string]any)
	var property string
	for name, raw := range properties {
		schema, _ := raw.(map[string]any)
		if _, geometry := schema["format"].(string); !geometry || !strings.HasPrefix(fmt.Sprint(schema["format"]), "geometry-") {
			property = name
			break
		}
	}
	if property == "" {
		t.Skip("collection exposes no scalar queryable")
	}
	filter := fmt.Sprintf(`"%s" IS NOT NULL`, strings.ReplaceAll(property, `"`, `""`))
	for _, explicit := range []bool{false, true} {
		params := url.Values{"filter": {filter}}
		if explicit {
			params.Set("filter-lang", "cql2-text")
		}
		resp, err := testCtx.Client.GetWithParams(fmt.Sprintf("/collections/%s/items", collection.ID), params, ogcapi.GeoJSONMediaType)
		if err != nil {
			t.Fatal(err)
		}
		ogcapi.AssertStatusCode(t, resp, 200)
	}
	params := url.Values{"filter": {filter}, "filter-lang": {"cql2-json"}}
	resp, err := testCtx.Client.GetWithParams(fmt.Sprintf("/collections/%s/items", collection.ID), params, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatal(err)
	}
	ogcapi.AssertStatusCode(t, resp, 400)
}

func TestFiltering_BasicSpatialSIntersectsBBox(t *testing.T) {
	collection := firstCollection(t)
	queryables, err := testCtx.Client.Get(fmt.Sprintf("/collections/%s/queryables", collection.ID), "application/schema+json")
	if err != nil {
		t.Fatal(err)
	}
	properties, _ := queryables.JSON["properties"].(map[string]any)
	var geometry string
	for name, raw := range properties {
		schema, _ := raw.(map[string]any)
		if strings.HasPrefix(fmt.Sprint(schema["format"]), "geometry-") {
			geometry = name
			break
		}
	}
	if geometry == "" {
		t.Skip("collection exposes no geometry queryable")
	}
	filter := fmt.Sprintf(`S_INTERSECTS("%s",BBOX(-180,-90,180,90))`, strings.ReplaceAll(geometry, `"`, `""`))
	params := url.Values{"filter": {filter}, "filter-lang": {"cql2-text"}}
	resp, err := testCtx.Client.GetWithParams(fmt.Sprintf("/collections/%s/items", collection.ID), params, ogcapi.GeoJSONMediaType)
	if err != nil {
		t.Fatal(err)
	}
	ogcapi.AssertStatusCode(t, resp, 200)
}
