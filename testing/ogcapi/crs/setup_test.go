// Package crs contains OGC API Features Part 2 (CRS) conformance tests.
// These tests validate CRS discovery and query parameter support.
//
// By default, tests run against a mock server (no external dependencies).
// To test against a real server, set OGC_TEST_URL environment variable.
package crs

import (
	"log"
	"os"
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
	"github.com/tobilg/neoserver/testing/ogcapi/testserver"
)

// testCtx is the shared test context for all CRS conformance tests.
var testCtx *ogcapi.TestContext

// mockServer holds the mock server instance (nil if using real server).
var mockServer *testserver.MockServer

// TestMain sets up the test context before running tests.
func TestMain(m *testing.M) {
	cfg := ogcapi.LoadConfig()

	// If no URL configured, use mock server
	if os.Getenv("OGC_TEST_URL") == "" {
		mockServer = testserver.NewMock()
		cfg.BaseURL = mockServer.URL()
	}

	testCtx = ogcapi.NewTestContext(cfg)

	// Run prerequisite discovery
	if err := testCtx.DiscoverConformance(); err != nil {
		log.Printf("Warning: Failed to discover conformance: %v", err)
	}

	if err := testCtx.DiscoverCollections(); err != nil {
		log.Printf("Warning: Failed to discover collections: %v", err)
	}

	code := m.Run()

	// Cleanup
	if mockServer != nil {
		mockServer.Close()
	}

	os.Exit(code)
}

// getTestContext returns the shared test context.
func getTestContext(t *testing.T) *ogcapi.TestContext {
	t.Helper()
	if testCtx == nil {
		t.Fatal("Test context not initialized")
	}
	return testCtx
}

// requireCollections skips the test if no collections are available.
func requireCollections(t *testing.T) {
	t.Helper()
	if len(testCtx.Collections) == 0 {
		t.Skip("No collections available for testing")
	}
}

// requireCRSConformance skips the test if CRS conformance is not declared.
func requireCRSConformance(t *testing.T) {
	t.Helper()
	if !testCtx.HasConformanceClass(ogcapi.ConformanceCRS) {
		t.Skip("Server does not declare CRS conformance class")
	}
}
