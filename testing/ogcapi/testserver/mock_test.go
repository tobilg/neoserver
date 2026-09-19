package testserver_test

import (
	"os"
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
	"github.com/tobilg/neoserver/testing/ogcapi/testserver"
)

// TestWithMockServer demonstrates running conformance tests against the mock server.
// This tests the test framework itself without requiring a database.
func TestWithMockServer(t *testing.T) {
	// Start mock server
	mock := testserver.NewMock()
	defer mock.Close()

	// Create test context pointing to mock server
	cfg := ogcapi.LoadConfig()
	cfg.BaseURL = mock.URL()
	ctx := ogcapi.NewTestContext(cfg)

	// Discover collections and conformance
	if err := ctx.DiscoverConformance(); err != nil {
		t.Fatalf("Failed to discover conformance: %v", err)
	}

	if err := ctx.DiscoverCollections(); err != nil {
		t.Fatalf("Failed to discover collections: %v", err)
	}

	// Run basic validation tests
	t.Run("LandingPage", func(t *testing.T) {
		resp, err := ctx.Client.Get("/", ogcapi.JSONMediaType)
		if err != nil {
			t.Fatalf("Failed to get landing page: %v", err)
		}
		ogcapi.AssertStatusCode(t, resp, 200)
		ogcapi.AssertJSONPropertyExists(t, resp.JSON, "title")
		ogcapi.AssertJSONPropertyExists(t, resp.JSON, "links")
	})

	t.Run("Conformance", func(t *testing.T) {
		resp, err := ctx.Client.Get("/conformance", ogcapi.JSONMediaType)
		if err != nil {
			t.Fatalf("Failed to get conformance: %v", err)
		}
		ogcapi.AssertStatusCode(t, resp, 200)
		ogcapi.AssertJSONPropertyExists(t, resp.JSON, "conformsTo")
	})

	t.Run("Collections", func(t *testing.T) {
		resp, err := ctx.Client.Get("/collections", ogcapi.JSONMediaType)
		if err != nil {
			t.Fatalf("Failed to get collections: %v", err)
		}
		ogcapi.AssertStatusCode(t, resp, 200)
		ogcapi.AssertJSONPropertyExists(t, resp.JSON, "collections")
	})

	t.Run("Features", func(t *testing.T) {
		for _, col := range ctx.Collections {
			t.Run(col.ID, func(t *testing.T) {
				resp, err := ctx.Client.Get("/collections/"+col.ID+"/items", ogcapi.GeoJSONMediaType)
				if err != nil {
					t.Fatalf("Failed to get features: %v", err)
				}
				ogcapi.AssertStatusCode(t, resp, 200)
				ogcapi.AssertTypeIsFeatureCollection(t, resp.JSON)
			})
		}
	})
}

// TestWithRealServer runs conformance tests against a real running server.
// Set OGC_INTEGRATION_TEST=true and OGC_TEST_URL to enable.
func TestWithRealServer(t *testing.T) {
	if os.Getenv("OGC_INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test. Set OGC_INTEGRATION_TEST=true to run.")
	}

	cfg := ogcapi.LoadConfig()
	if cfg.BaseURL == "" || cfg.BaseURL == "http://localhost:9000/ogc" {
		// Try to check if server is running
		ctx := ogcapi.NewTestContext(cfg)
		if _, err := ctx.Client.Get("/", ogcapi.JSONMediaType); err != nil {
			t.Skip("Server not running. Start with 'make up' or set OGC_TEST_URL")
		}
	}

	ctx := ogcapi.NewTestContext(cfg)

	if err := ctx.DiscoverConformance(); err != nil {
		t.Fatalf("Failed to discover conformance: %v", err)
	}

	if err := ctx.DiscoverCollections(); err != nil {
		t.Fatalf("Failed to discover collections: %v", err)
	}

	t.Logf("Testing against %s with %d collections", cfg.BaseURL, len(ctx.Collections))

	// Run conformance checks
	t.Run("CoreConformance", func(t *testing.T) {
		if !ctx.HasConformanceClass(ogcapi.ConformanceCore) {
			t.Error("Server does not declare Core conformance")
		}
		if !ctx.HasConformanceClass(ogcapi.ConformanceGeoJSON) {
			t.Error("Server does not declare GeoJSON conformance")
		}
	})

	t.Run("AllCollectionsAccessible", func(t *testing.T) {
		for _, col := range ctx.Collections {
			t.Run(col.ID, func(t *testing.T) {
				resp, err := ctx.Client.Get("/collections/"+col.ID+"/items", ogcapi.GeoJSONMediaType)
				if err != nil {
					t.Fatalf("Failed to get features: %v", err)
				}
				ogcapi.AssertStatusCode(t, resp, 200)
			})
		}
	})
}
