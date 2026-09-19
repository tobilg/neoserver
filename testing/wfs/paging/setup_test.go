// Package paging contains response paging tests.
// These tests cover WFS 2.0 paging capabilities:
// - count parameter
// - startIndex parameter
// - resultType (results/hits)
// - next/previous links
package paging

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
