// Package filter contains filter conformance tests.
// These tests cover FES 2.0 filter capabilities:
// - Comparison operators (PropertyIsEqualTo, Less/Greater, Like, Null)
// - ResourceId filter
// - Spatial operators (BBOX, Intersects, Within, DWithin)
// - Logical operators (AND, OR, NOT)
package filter

import (
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
	"github.com/tobilg/neoserver/testing/wfs/capabilities"
)

// getTestContext returns the shared test context.
func getTestContext(t *testing.T) *wfs.TestContext {
	t.Helper()
	return wfs.GetTestContext(t)
}

// filterFeatureType selects the fixture deliberately instead of depending on
// capability document order. The conformance fixture guarantees name and geom
// properties on BasicPolygons; the in-process test server uses Streams.
func filterFeatureType(t *testing.T, ctx *wfs.TestContext) *capabilities.FeatureType {
	t.Helper()
	for _, preferred := range []string{"BasicPolygons", "Streams"} {
		for _, name := range ctx.GetFeatureTypeNames() {
			local := name
			if index := strings.LastIndexAny(local, ":."); index >= 0 {
				local = local[index+1:]
			}
			if strings.EqualFold(local, preferred) {
				if featureType := ctx.GetFeatureType(name); featureType != nil {
					return featureType
				}
			}
		}
	}
	if featureType := ctx.GetFirstFeatureType(); featureType != nil {
		return featureType
	}
	t.Fatal("filter fixture exposes no feature types")
	return nil
}

// skipIfNoFeatureTypes skips the test if no feature types are available.
func skipIfNoFeatureTypes(t *testing.T, ctx *wfs.TestContext) {
	t.Helper()
	wfs.SkipIfNoFeatureTypes(t, ctx)
}

// skipIfComparisonNotSupported skips if comparison operator is not supported.
func skipIfComparisonNotSupported(t *testing.T, ctx *wfs.TestContext, operator string) {
	t.Helper()
	wfs.SkipIfComparisonOperatorNotSupported(t, ctx, operator)
}

// skipIfSpatialNotSupported skips if spatial operator is not supported.
func skipIfSpatialNotSupported(t *testing.T, ctx *wfs.TestContext, operator string) {
	t.Helper()
	wfs.SkipIfSpatialOperatorNotSupported(t, ctx, operator)
}
