package getmap

import (
	"os"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
	"github.com/tobilg/neoserver/testing/wms/mockserver"
)

var testCtx *wms.TestContext
var mockServer *mockserver.Server

func TestMain(m *testing.M) {
	cfg := wms.LoadConfig()

	// If no URL specified, use mock server
	if cfg.BaseURL == "" {
		mockServer = mockserver.New()
		cfg.BaseURL = mockServer.URL()
		defer mockServer.Close()
	}

	testCtx = wms.NewTestContext(cfg)

	// Discover capabilities before running tests
	if err := testCtx.DiscoverCapabilities(); err != nil {
		os.Stderr.WriteString("Failed to discover capabilities: " + err.Error() + "\n")
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func getTestContext(t *testing.T) *wms.TestContext {
	t.Helper()
	if testCtx == nil {
		t.Fatal("Test context not initialized")
	}
	return testCtx
}

func skipIfNoLayers(t *testing.T) {
	t.Helper()
	if !testCtx.HasLayers() {
		t.Skip("No named layers available in capabilities")
	}
}

func skipIfNoFormats(t *testing.T) {
	t.Helper()
	if len(testCtx.GetMapFormats()) == 0 {
		t.Skip("No GetMap formats available in capabilities")
	}
}
