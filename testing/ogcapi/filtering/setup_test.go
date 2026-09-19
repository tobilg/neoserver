// Package filtering contains OGC API - Features Part 3 and CQL2 conformance tests.
package filtering

import (
	"log"
	"os"
	"testing"

	"github.com/tobilg/neoserver/testing/ogcapi"
	"github.com/tobilg/neoserver/testing/ogcapi/testserver"
)

var testCtx *ogcapi.TestContext
var mockServer *testserver.MockServer

func TestMain(m *testing.M) {
	cfg := ogcapi.LoadConfig()
	if os.Getenv("OGC_TEST_URL") == "" {
		mockServer = testserver.NewMock()
		cfg.BaseURL = mockServer.URL()
	}
	testCtx = ogcapi.NewTestContext(cfg)
	if err := testCtx.DiscoverConformance(); err != nil {
		log.Printf("Warning: Failed to discover conformance: %v", err)
	}
	if err := testCtx.DiscoverCollections(); err != nil {
		log.Printf("Warning: Failed to discover collections: %v", err)
	}
	code := m.Run()
	if mockServer != nil {
		mockServer.Close()
	}
	os.Exit(code)
}
