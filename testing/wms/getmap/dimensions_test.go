package getmap

import (
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
	"github.com/tobilg/neoserver/testing/wms/capabilities"
)

// TestDimensionValues implements the live TIME/ELEVATION request behavior in
// WMS 1.3.0 sections 7.2.4.6.9 and 7.3.3.
func TestDimensionValues(t *testing.T) {
	ctx := getTestContext(t)
	tested := 0
	for _, layer := range ctx.Layers {
		for _, dimensionName := range []string{"time", "elevation"} {
			dimension := layer.GetDimension(dimensionName)
			if dimension == nil {
				continue
			}
			value := firstDimensionValue(dimension)
			if value == "" {
				t.Errorf("layer %q advertises %s without a usable value", layer.Name, dimensionName)
				continue
			}
			tested++
			t.Run(layer.Name+"/"+dimensionName, func(t *testing.T) {
				params := ctx.BuildGetMapParams(layer)
				params.Set(strings.ToUpper(dimensionName), value)
				response, err := ctx.Client.GetMap(params)
				if err != nil {
					t.Fatalf("GetMap request failed: %v", err)
				}
				wms.AssertStatusCode(t, response, 200)
				wms.AssertNotException(t, response)
				wms.AssertIsImage(t, response, "")
			})
		}
	}
	if tested == 0 {
		t.Skip("no TIME or ELEVATION dimensions are advertised")
	}
}

func firstDimensionValue(dimension *capabilities.Dimension) string {
	value := strings.TrimSpace(dimension.Default)
	if value == "" {
		value = strings.TrimSpace(dimension.Value)
	}
	if before, _, ok := strings.Cut(value, ","); ok {
		value = before
	}
	if before, _, ok := strings.Cut(value, "/"); ok {
		value = before
	}
	return strings.TrimSpace(value)
}
