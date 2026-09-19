package filter

import (
	"net/url"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
	"github.com/tobilg/neoserver/testing/wfs/gml"
)

// TestFilter_BBOX tests BBOX spatial filter via KVP.
func TestFilter_BBOX(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfSpatialNotSupported(t, ctx, "BBOX")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Use a large bounding box that should include some features
	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"BBOX":      {"-180,-90,180,90"},
		"COUNT":     {"10"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_BBOX_WithCRS tests BBOX with CRS parameter.
func TestFilter_BBOX_WithCRS(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfSpatialNotSupported(t, ctx, "BBOX")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// BBOX with explicit CRS
	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"BBOX":      {"-180,-90,180,90,urn:ogc:def:crs:EPSG::4326"},
		"COUNT":     {"10"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_BBOX_SmallArea tests BBOX with a smaller area.
func TestFilter_BBOX_SmallArea(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfSpatialNotSupported(t, ctx, "BBOX")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Get bounding box from capabilities if available
	bbox := ft.WGS84BoundingBox
	var bboxParam string
	if bbox != nil && bbox.LowerCorner != "" {
		// Use the capabilities bbox
		bboxParam = bbox.LowerCorner + " " + bbox.UpperCorner
	} else {
		// Use a default small area (US area)
		bboxParam = "-100,35,-90,45"
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"BBOX":      {bboxParam},
		"COUNT":     {"10"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_BBOX_OutsideData tests BBOX outside any data.
func TestFilter_BBOX_OutsideData(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfSpatialNotSupported(t, ctx, "BBOX")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// BBOX in the middle of the Pacific Ocean
	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"BBOX":      {"170,0,175,5"},
		"COUNT":     {"10"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)

	// Should return empty or few results
	fc, err := gml.ParseFeatureCollection(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	// We just verify it doesn't error; may or may not have data
	t.Logf("Found %d features in remote bbox", fc.NumberReturned)
}

// TestFilter_BBOX_InvalidFormat tests invalid BBOX format.
func TestFilter_BBOX_InvalidFormat(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfSpatialNotSupported(t, ctx, "BBOX")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	params := url.Values{
		"SERVICE":   {"WFS"},
		"REQUEST":   {"GetFeature"},
		"VERSION":   {"2.0.0"},
		"TYPENAMES": {ft.Name},
		"BBOX":      {"invalid,bbox,format"},
	}

	resp, err := ctx.Client.Get(params)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertIsException(t, resp)
	wfs.AssertExceptionCode(t, resp, wfs.ExceptionInvalidParameterValue)
}

// TestFilter_Intersects tests Intersects spatial filter.
func TestFilter_Intersects(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfSpatialNotSupported(t, ctx, "Intersects")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Build Intersects filter with a point
	filter := NewFilterBuilder().
		Start().
		IntersectsPoint("geom", -90.0, 40.0, "urn:ogc:def:crs:EPSG::4326").
		End()

	requestBody := buildGetFeaturePost(ft.Name, filter)
	resp, err := ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_Within tests Within spatial filter.
func TestFilter_Within(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfSpatialNotSupported(t, ctx, "Within")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Build Within filter with a polygon
	coords := [][2]float64{
		{-100, 30},
		{-80, 30},
		{-80, 50},
		{-100, 50},
		{-100, 30},
	}
	filter := NewFilterBuilder().
		Start().
		WithinPolygon("geom", coords, "urn:ogc:def:crs:EPSG::4326").
		End()

	requestBody := buildGetFeaturePost(ft.Name, filter)
	resp, err := ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}

// TestFilter_DWithin tests DWithin (distance within) spatial filter.
func TestFilter_DWithin(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoFeatureTypes(t, ctx)
	skipIfSpatialNotSupported(t, ctx, "DWithin")

	ft := filterFeatureType(t, ctx)
	if ft == nil {
		t.Skip("No feature types available")
	}

	// Build DWithin filter
	filter := NewFilterBuilder().
		Start().
		DWithin("geom", -90.0, 40.0, 100000, "m", "urn:ogc:def:crs:EPSG::4326").
		End()

	requestBody := buildGetFeaturePost(ft.Name, filter)
	resp, err := ctx.Client.GetFeaturePost([]byte(requestBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	wfs.AssertStatusCode(t, resp, 200)
	wfs.AssertNotException(t, resp)
}
