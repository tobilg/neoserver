package wfs

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGMLAxisOrderAcrossWritersAndGeometryTypes(t *testing.T) {
	for _, srid := range []int{4326, 3857, 3035} {
		for _, writer := range []struct {
			name  string
			write func(http.ResponseWriter, map[string]interface{}, int)
		}{{"collection", writeGMLGeometry}, {"single", writeGMLGeometryNoIndent}} {
			for _, shape := range []struct{ kind, coordinates, tag string }{{"Point", `[7,51]`, "pos"}, {"LineString", `[[7,51],[8,52]]`, "posList"}, {"Polygon", `[[[7,51],[8,52],[7,51]]]`, "posList"}, {"MultiPoint", `[[7,51],[8,52]]`, "pos"}, {"MultiLineString", `[[[7,51],[8,52]]]`, "posList"}, {"MultiPolygon", `[[[[7,51],[8,52],[7,51]]]]`, "posList"}} {
				t.Run(fmt.Sprintf("%d/%s/%s", srid, writer.name, shape.kind), func(t *testing.T) {
					feature, err := decodeFeatureJSON([]byte(fmt.Sprintf(`{"type":%q,"coordinates":%s}`, shape.kind, shape.coordinates)))
					if err != nil {
						t.Fatal(err)
					}
					w := httptest.NewRecorder()
					writer.write(w, feature, srid)
					prefix := "51 7"
					if srid == 3857 {
						prefix = "7 51"
					}
					if !strings.Contains(w.Body.String(), "<gml:"+shape.tag+">"+prefix) {
						t.Fatal(w.Body.String())
					}
					// Repeating serialization cannot swap the source in place.
					repeat := httptest.NewRecorder()
					writer.write(repeat, feature, srid)
					if repeat.Body.String() != w.Body.String() {
						t.Fatal("source coordinates mutated")
					}
				})
			}
		}
	}
}
