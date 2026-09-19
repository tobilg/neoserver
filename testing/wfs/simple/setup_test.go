// Package simple contains Simple WFS conformance class tests.
// These tests cover the mandatory operations for a Simple WFS:
// - GetCapabilities
// - DescribeFeatureType
// - GetFeature (basic)
// - ListStoredQueries
// - DescribeStoredQueries
// - GetFeatureById stored query
package simple

import (
	"testing"

	"github.com/tobilg/neoserver/testing/wfs"
)

// getTestContext returns the shared test context.
func getTestContext(t *testing.T) *wfs.TestContext {
	t.Helper()
	return wfs.GetTestContext(t)
}

// skipIfNoFeatureTypes skips the test if no feature types are available.
func skipIfNoFeatureTypes(t *testing.T, ctx *wfs.TestContext) {
	t.Helper()
	wfs.SkipIfNoFeatureTypes(t, ctx)
}
