package wms

import (
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

func TestApplyDimensionFilters(t *testing.T) {
	layer := &workspace.Layer{Dimensions: []*workspace.Dimension{{Name: "time", SourceProperty: "start_time", EndProperty: "end_time", Default: "2026-01-01T00:00:00Z"}, {Name: "elevation", SourceProperty: "z", MultipleValues: true}}}
	params := datasource.QueryParams{}
	err := applyDimensionFilters(&GetMapRequest{Elevation: "10/20"}, layer, &params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(params.Filter, `start_time`) || !strings.Contains(params.Filter, `z BETWEEN 10 AND 20`) {
		t.Fatalf("filter=%s", params.Filter)
	}
}
