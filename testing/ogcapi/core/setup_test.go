// Package core contains OGC API Features Part 1 (Core) conformance tests.
// These tests implement the Abstract Test Suite from the OGC API - Features 1.0 specification.
//
// By default, tests run against a mock server (no external dependencies).
// To test against a real server, set OGC_TEST_URL environment variable.
package core

import (
	"log"
	"os"
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
	"github.com/tobilg/neoserver/testing/ogcapi/testserver"
)

// testCtx is the shared test context for all core conformance tests.
var testCtx *ogcapi.TestContext

// mockServer holds the mock server instance (nil if using real server).
var mockServer *testserver.MockServer

// TestMain sets up the test context before running tests.
// It discovers collections and conformance classes which are required
// by downstream tests.
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
