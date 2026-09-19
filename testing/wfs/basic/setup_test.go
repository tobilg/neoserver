// Package basic contains Basic WFS conformance class tests.
// These tests cover additional operations for Basic WFS:
// - GetFeature with Query element
// - GetPropertyValue
// - CRS handling
package basic

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

// skipIfOperationNotSupported skips if the operation is not supported.
func skipIfOperationNotSupported(t *testing.T, ctx *wfs.TestContext, operation string) {
	t.Helper()
	wfs.SkipIfOperationNotSupported(t, ctx, operation)
}
