package filter

import (
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
)

// TestFilter_And tests AND logical operator.
func TestFilter_And(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Build AND filter
	filter := `<fes:Filter xmlns:fes="http://www.opengis.net/fes/2.0">
  <fes:And>
    <fes:PropertyIsEqualTo>
      <fes:ValueReference>name</fes:ValueReference>
      <fes:Literal>Test</fes:Literal>
    </fes:PropertyIsEqualTo>
    <fes:PropertyIsNotEqualTo>
      <fes:ValueReference>name</fes:ValueReference>
      <fes:Literal>Other</fes:Literal>
    </fes:PropertyIsNotEqualTo>
  </fes:And>
</fes:Filter>
`

	requestBody := buildGetFeaturePost(ft.Name, filter)
	resp, err := ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_Or tests OR logical operator.
func TestFilter_Or(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Build OR filter
	filter := `<fes:Filter xmlns:fes="http://www.opengis.net/fes/2.0">
  <fes:Or>
    <fes:PropertyIsEqualTo>
      <fes:ValueReference>name</fes:ValueReference>
      <fes:Literal>Test1</fes:Literal>
    </fes:PropertyIsEqualTo>
    <fes:PropertyIsEqualTo>
      <fes:ValueReference>name</fes:ValueReference>
      <fes:Literal>Test2</fes:Literal>
    </fes:PropertyIsEqualTo>
  </fes:Or>
</fes:Filter>
`

	requestBody := buildGetFeaturePost(ft.Name, filter)
	resp, err := ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_Not tests NOT logical operator.
func TestFilter_Not(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Build NOT filter
	filter := `<fes:Filter xmlns:fes="http://www.opengis.net/fes/2.0">
  <fes:Not>
    <fes:PropertyIsEqualTo>
      <fes:ValueReference>name</fes:ValueReference>
      <fes:Literal>Excluded</fes:Literal>
    </fes:PropertyIsEqualTo>
  </fes:Not>
</fes:Filter>
`

	requestBody := buildGetFeaturePost(ft.Name, filter)
	resp, err := ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_NestedLogical tests nested logical operators.
func TestFilter_NestedLogical(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Build nested AND/OR filter
	filter := `<fes:Filter xmlns:fes="http://www.opengis.net/fes/2.0">
  <fes:And>
    <fes:Or>
      <fes:PropertyIsEqualTo>
      <fes:ValueReference>name</fes:ValueReference>
        <fes:Literal>Test1</fes:Literal>
      </fes:PropertyIsEqualTo>
      <fes:PropertyIsEqualTo>
      <fes:ValueReference>name</fes:ValueReference>
        <fes:Literal>Test2</fes:Literal>
      </fes:PropertyIsEqualTo>
    </fes:Or>
    <fes:Not>
      <fes:PropertyIsNull>
      <fes:ValueReference>name</fes:ValueReference>
      </fes:PropertyIsNull>
    </fes:Not>
  </fes:And>
</fes:Filter>
`

	requestBody := buildGetFeaturePost(ft.Name, filter)
	resp, err := ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_AndWithSpatial tests AND with spatial filter.
func TestFilter_AndWithSpatial(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfSpatialNotSupported(t, ctx, "BBOX")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Build AND filter with BBOX
	filter := `<fes:Filter xmlns:fes="http://www.opengis.net/fes/2.0" xmlns:gml="http://www.opengis.net/gml/3.2">
  <fes:And>
    <fes:BBOX>
      <fes:ValueReference>geom</fes:ValueReference>
      <gml:Envelope srsName="urn:ogc:def:crs:EPSG::4326">
        <gml:lowerCorner>-180 -90</gml:lowerCorner>
        <gml:upperCorner>180 90</gml:upperCorner>
      </gml:Envelope>
    </fes:BBOX>
    <fes:PropertyIsNotEqualTo>
      <fes:ValueReference>name</fes:ValueReference>
      <fes:Literal>Excluded</fes:Literal>
    </fes:PropertyIsNotEqualTo>
  </fes:And>
</fes:Filter>
`

	requestBody := buildGetFeaturePost(ft.Name, filter)
	resp, err := ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}
