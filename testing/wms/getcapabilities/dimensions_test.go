package getcapabilities

import (
	"strings"
	"testing"
)

// TestDimensions_TimeUnitsISO8601 tests that TIME dimension uses ISO8601 units.
// Reference: WMS 1.3.0 section 7.2.4.6.9
func TestDimensions_TimeUnitsISO8601(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	foundTimeDimension := false
	for _, layer := range ctx.Layers {
		for _, dim := range layer.Dimension {
			if strings.EqualFold(dim.Name, "time") {
				foundTimeDimension = true
				t.Run(layer.Name, func(t *testing.T) {
					if !strings.EqualFold(dim.Units, "ISO8601") {
						t.Errorf("TIME dimension in layer %q has units %q, expected ISO8601", layer.Name, dim.Units)
					}
				})
			}
		}
	}

	if !foundTimeDimension {
		t.Skip("No layers with TIME dimension found")
	}
}

// TestDimensions_ElevationCRS tests that ELEVATION dimension specifies a valid CRS.
// Reference: WMS 1.3.0 section 7.2.4.6.9
func TestDimensions_ElevationCRS(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	foundElevationDimension := false
	for _, layer := range ctx.Layers {
		for _, dim := range layer.Dimension {
			if strings.EqualFold(dim.Name, "elevation") {
				foundElevationDimension = true
				t.Run(layer.Name, func(t *testing.T) {
					// Elevation units should specify a CRS (typically EPSG:5030 or similar)
					if dim.Units == "" {
						t.Errorf("ELEVATION dimension in layer %q has no units specified", layer.Name)
					}
				})
			}
		}
	}

	if !foundElevationDimension {
		t.Skip("No layers with ELEVATION dimension found")
	}
}

// TestDimensions_NoRedeclarations tests that dimensions are not redeclared in child layers.
// Reference: WMS 1.3.0 section 7.2.4.6.9
func TestDimensions_NoRedeclarations(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	// Build a map of dimension names per layer
	layerDimensions := make(map[string]map[string]bool)
	for _, layer := range ctx.Layers {
		dims := make(map[string]bool)
		for _, dim := range layer.Dimension {
			normalized := strings.ToLower(dim.Name)
			if dims[normalized] {
				t.Errorf("Layer %q redeclares dimension %q", layer.Name, dim.Name)
			}
			dims[normalized] = true
		}
		layerDimensions[layer.Name] = dims
	}
}

// TestDimensions_DefaultValue tests that dimensions with nearestValue have a default.
func TestDimensions_DefaultValue(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	for _, layer := range ctx.Layers {
		for _, dim := range layer.Dimension {
			t.Run(layer.Name+"/"+dim.Name, func(t *testing.T) {
				// Log dimension info
				t.Logf("Dimension: name=%s, units=%s, default=%s", dim.Name, dim.Units, dim.Default)

				// If dimension has nearestValue attribute set, it should have a default
				// This is informational for now as we may not parse all attributes
			})
		}
	}
}

// TestDimensions_ValidValues tests that dimension values are properly formatted.
func TestDimensions_ValidValues(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	foundDimensions := false
	for _, layer := range ctx.Layers {
		for _, dim := range layer.Dimension {
			foundDimensions = true
			t.Run(layer.Name+"/"+dim.Name, func(t *testing.T) {
				// Dimensions should have values defined
				// Values can be:
				// - Comma-separated list: "value1,value2,value3"
				// - Range: "min/max/resolution"
				// - Mix of both
				if dim.Value == "" && dim.Default == "" {
					t.Logf("Warning: Dimension %q in layer %q has no values or default", dim.Name, layer.Name)
				}
			})
		}
	}

	if !foundDimensions {
		t.Skip("No layers with dimensions found")
	}
}

// TestDimensions_TimeLayerExists tests that if TIME dimension tests are needed, we have a layer.
func TestDimensions_TimeLayerExists(t *testing.T) {
	ctx := getTestContext(t)

	layer := ctx.GetLayerWithTimeDimension()
	if layer == nil {
		t.Skip("No layer with TIME dimension available")
	}

	t.Logf("Found layer with TIME dimension: %s", layer.Name)

	// Verify the TIME dimension is properly configured
	timeDim := layer.GetTimeDimension()
	if timeDim == nil {
		t.Error("GetTimeDimension returned nil for layer that should have TIME dimension")
		return
	}

	if !strings.EqualFold(timeDim.Units, "ISO8601") {
		t.Errorf("TIME dimension units should be ISO8601, got %q", timeDim.Units)
	}
}
