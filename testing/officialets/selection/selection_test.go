package selection

import (
	"reflect"
	"testing"
)

func TestForPaths(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  []string
	}{
		{"documentation", []string{"docs/conformance.md", "README.md"}, []string{}},
		{"shared WMS legend renderer", []string{"internal/wms/resource_legend.go"}, []string{"wms13", "wmts10"}},
		{"WMS tests", []string{"testing/wms/map_test.go"}, []string{"wms13"}},
		{"filter", []string{"internal/filter/compiler.go"}, []string{"ogcapi-features10", "wfs20"}},
		{"tiles", []string{"internal/tiles/map.go"}, []string{"ogcapi-tiles10", "wmts10"}},
		{"renderer", []string{"internal/renderer/render.go"}, []string{"ogcapi-tiles10", "wms13", "wmts10"}},
		{"wcs derived follows stock", []string{"testing/wcs/wcs20_test.go"}, []string{"wcs20"}},
		{"protocol only", []string{"testing/protocol/profiles.go"}, []string{}},
		{"shared defaults all", []string{"internal/workspace/registry.go"}, All()},
		{"workflow defaults all", []string{".github/workflows/conformance.yml"}, All()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ForPaths(test.paths); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ForPaths(%v)=%v, want %v", test.paths, got, test.want)
			}
		})
	}
}
