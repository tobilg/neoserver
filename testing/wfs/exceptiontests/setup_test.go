// Package exceptiontests contains WFS exception handling tests.
// These tests verify proper exception handling for various error conditions.
package exceptiontests

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
