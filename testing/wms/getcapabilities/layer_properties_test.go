package getcapabilities

import (
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestLayerProperties_NamedLayersExist tests that named layers exist in capabilities.
func TestLayerProperties_NamedLayersExist(t *testing.T) {
	ctx := getTestContext(t)

	if !ctx.HasLayers() {
		t.Error("No named layers in capabilities")
	}

	t.Logf("Found %d named layers", ctx.LayerCount())
}

// TestLayerProperties_BBoxCRSAdvertised tests that BBox CRS matches advertised CRS.
// Reference: WMS 1.3.0 section 7.2.4.6.6
func TestLayerProperties_BBoxCRSAdvertised(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			// Each bounding box should reference an advertised CRS
			for _, bbox := range layer.BoundingBox {
				if bbox.CRS == "" {
					continue
				}

				// Check if CRS is in layer's CRS list
				found := false
				for _, crs := range layer.CRS {
					if strings.EqualFold(crs, bbox.CRS) {
						found = true
						break
					}
				}

				if !found {
					t.Errorf("Layer %q has BBox with CRS %q not in advertised CRS list", layer.Name, bbox.CRS)
				}
			}
		})
	}
}

// TestLayerProperties_BBoxPresent tests that each named layer has a bounding box.
func TestLayerProperties_BBoxPresent(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			// Layer should have either EX_GeographicBoundingBox or BoundingBox
			if layer.EXGeographicBoundingBox == nil && len(layer.BoundingBox) == 0 {
				t.Errorf("Layer %q has no bounding box", layer.Name)
			}
		})
	}
}

// TestLayerProperties_CRSPresent tests that each named layer has at least one CRS.
func TestLayerProperties_CRSPresent(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			if len(layer.CRS) == 0 {
				t.Errorf("Layer %q has no CRS advertised", layer.Name)
			}
		})
	}
}

// TestLayerProperties_EXGeoBBoxPresent tests that each named layer has EX_GeographicBoundingBox.
func TestLayerProperties_EXGeoBBoxPresent(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			if layer.EXGeographicBoundingBox == nil {
				t.Logf("Warning: Layer %q has no EX_GeographicBoundingBox", layer.Name)
			}
		})
	}
}

// TestLayerProperties_EXGeoBBoxCoordinates tests that EX_GeographicBoundingBox has valid coordinates.
// Reference: WMS 1.3.0 section 7.2.4.6.5
func TestLayerProperties_EXGeoBBoxCoordinates(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			if layer.EXGeographicBoundingBox == nil {
				t.Skip("No EX_GeographicBoundingBox")
			}

			bb := layer.EXGeographicBoundingBox

			// Coordinates should be within valid ranges
			if bb.WestBoundLongitude < -180 || bb.WestBoundLongitude > 180 {
				t.Errorf("WestBoundLongitude %f out of range [-180, 180]", bb.WestBoundLongitude)
			}
			if bb.EastBoundLongitude < -180 || bb.EastBoundLongitude > 180 {
				t.Errorf("EastBoundLongitude %f out of range [-180, 180]", bb.EastBoundLongitude)
			}
			if bb.SouthBoundLatitude < -90 || bb.SouthBoundLatitude > 90 {
				t.Errorf("SouthBoundLatitude %f out of range [-90, 90]", bb.SouthBoundLatitude)
			}
			if bb.NorthBoundLatitude < -90 || bb.NorthBoundLatitude > 90 {
				t.Errorf("NorthBoundLatitude %f out of range [-90, 90]", bb.NorthBoundLatitude)
			}

			// South should be less than or equal to North
			if bb.SouthBoundLatitude > bb.NorthBoundLatitude {
				t.Errorf("SouthBoundLatitude %f > NorthBoundLatitude %f", bb.SouthBoundLatitude, bb.NorthBoundLatitude)
			}
		})
	}
}

// TestLayerProperties_StyleUnique tests that style names are unique within a layer.
func TestLayerProperties_StyleUnique(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			styleNames := make(map[string]bool)
			for _, style := range layer.Style {
				if style.Name == "" {
					continue
				}
				if styleNames[style.Name] {
					t.Errorf("Duplicate style name %q in layer %q", style.Name, layer.Name)
				}
				styleNames[style.Name] = true
			}
		})
	}
}

// TestLayerProperties_StyleHasTitle tests that each style has a title.
func TestLayerProperties_StyleHasTitle(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			for _, style := range layer.Style {
				if style.Title == "" {
					t.Logf("Warning: Style %q in layer %q has no title", style.Name, layer.Name)
				}
			}
		})
	}
}

// TestLayerProperties_QueryableLayers tests that queryable layers are properly marked.
func TestLayerProperties_QueryableLayers(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	queryableLayers := ctx.GetQueryableLayers()
	t.Logf("Found %d queryable layers out of %d total", len(queryableLayers), ctx.LayerCount())

	for _, layer := range queryableLayers {
		wms.AssertLayerQueryable(t, layer)
	}
}

// TestLayerProperties_LayerTitle tests that each named layer has a title.
func TestLayerProperties_LayerTitle(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			if layer.Title == "" {
				t.Errorf("Layer %q has no title", layer.Name)
			}
		})
	}
}

// TestLayerProperties_CRSForAllLayers tests that all layers have usable CRS.
func TestLayerProperties_CRSForAllLayers(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			crs := layer.GetFirstCRS()
			if crs == "" {
				t.Errorf("Layer %q has no usable CRS", layer.Name)
			}
		})
	}
}

// TestLayerProperties_BBoxDistinctCRS tests that BoundingBox elements have distinct CRS.
func TestLayerProperties_BBoxDistinctCRS(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			crsSet := make(map[string]bool)
			for _, bbox := range layer.BoundingBox {
				if bbox.CRS == "" {
					continue
				}
				normalized := strings.ToUpper(bbox.CRS)
				if crsSet[normalized] {
					t.Errorf("Layer %q has multiple BoundingBox elements with CRS %q", layer.Name, bbox.CRS)
				}
				crsSet[normalized] = true
			}
		})
	}
}

// TestLayerProperties_ScaleDenominators tests that scale denominators are valid if present.
func TestLayerProperties_ScaleDenominators(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			if layer.MinScaleDenominator > 0 && layer.MaxScaleDenominator > 0 {
				if layer.MinScaleDenominator > layer.MaxScaleDenominator {
					t.Errorf("Layer %q has MinScaleDenominator %f > MaxScaleDenominator %f",
						layer.Name, layer.MinScaleDenominator, layer.MaxScaleDenominator)
				}
			}
		})
	}
}
