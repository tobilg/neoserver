package wfs

import (
	"net/url"
	"sync"
	"testing"

	"github.com/tobilg/neoserver/testing/wfs/capabilities"
	"github.com/tobilg/neoserver/testing/wfs/mockserver"
)

// TestContext holds shared state for WFS tests.
type TestContext struct {
	Config       Config
	Client       *Client
	Capabilities *capabilities.Capabilities
	MockServer   *mockserver.Server

	mu sync.RWMutex
}

var (
	globalContext *TestContext
	contextOnce   sync.Once
)

// GetTestContext returns the shared test context.
// It initializes the context once on first call.
func GetTestContext(t *testing.T) *TestContext {
	t.Helper()

	contextOnce.Do(func() {
		cfg := LoadConfig()
		globalContext = &TestContext{
			Config: cfg,
		}

		// If no external URL is configured, start a mock server
		if cfg.BaseURL == "" {
			globalContext.MockServer = mockserver.New()
			cfg.BaseURL = globalContext.MockServer.Server.URL
			globalContext.Config = cfg
		}

		globalContext.Client = NewClient(cfg.BaseURL)
		globalContext.Client.Timeout = cfg.Timeout

		// Fetch capabilities
		resp, err := globalContext.Client.GetCapabilities(nil)
		if err != nil {
			t.Fatalf("Failed to get capabilities: %v", err)
		}

		caps, err := capabilities.Parse(resp.Body)
		if err != nil {
			t.Fatalf("Failed to parse capabilities: %v", err)
		}
		globalContext.Capabilities = caps
	})

	if globalContext == nil {
		t.Fatal("Test context not initialized")
	}

	return globalContext
}

// NewIsolatedContext creates a new isolated test context with its own mock server.
// Use this when you need a fresh server state for a test.
func NewIsolatedContext(t *testing.T) *TestContext {
	t.Helper()

	mockSrv := mockserver.New()
	cfg := Config{
		BaseURL: mockSrv.Server.URL,
		Timeout: LoadConfig().Timeout,
	}

	client := NewClient(cfg.BaseURL)
	client.Timeout = cfg.Timeout

	resp, err := client.GetCapabilities(nil)
	if err != nil {
		t.Fatalf("Failed to get capabilities: %v", err)
	}

	caps, err := capabilities.Parse(resp.Body)
	if err != nil {
		t.Fatalf("Failed to parse capabilities: %v", err)
	}

	ctx := &TestContext{
		Config:       cfg,
		Client:       client,
		Capabilities: caps,
		MockServer:   mockSrv,
	}

	t.Cleanup(func() {
		mockSrv.Close()
	})

	return ctx
}

// GetFirstFeatureType returns the first feature type from capabilities.
func (ctx *TestContext) GetFirstFeatureType() *capabilities.FeatureType {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	types := ctx.Capabilities.GetFeatureTypes()
	if len(types) == 0 {
		return nil
	}
	return &types[0]
}

// GetFeatureType returns a feature type by name.
func (ctx *TestContext) GetFeatureType(name string) *capabilities.FeatureType {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	return ctx.Capabilities.GetFeatureType(name)
}

// GetFeatureTypeNames returns all feature type names.
func (ctx *TestContext) GetFeatureTypeNames() []string {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	return ctx.Capabilities.GetFeatureTypeNames()
}

// HasFeatureTypes returns true if there are feature types available.
func (ctx *TestContext) HasFeatureTypes() bool {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	return len(ctx.Capabilities.GetFeatureTypes()) > 0
}

// SupportsOperation returns true if the operation is supported.
func (ctx *TestContext) SupportsOperation(name string) bool {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	return ctx.Capabilities.SupportsOperation(name)
}

// SupportsSpatialOperator returns true if the spatial operator is supported.
func (ctx *TestContext) SupportsSpatialOperator(name string) bool {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	return ctx.Capabilities.SupportsSpatialOperator(name)
}

// SupportsComparisonOperator returns true if the comparison operator is supported.
func (ctx *TestContext) SupportsComparisonOperator(name string) bool {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()

	return ctx.Capabilities.SupportsComparisonOperator(name)
}

// GetFeature performs a GetFeature request with the given parameters.
func (ctx *TestContext) GetFeature(t *testing.T, params url.Values) *Response {
	t.Helper()

	resp, err := ctx.Client.GetFeature(params)
	if err != nil {
		t.Fatalf("GetFeature failed: %v", err)
	}
	return resp
}

// DescribeFeatureType performs a DescribeFeatureType request.
func (ctx *TestContext) DescribeFeatureType(t *testing.T, typeName string) *Response {
	t.Helper()

	params := url.Values{
		"TYPENAMES": {typeName},
	}
	resp, err := ctx.Client.DescribeFeatureType(params)
	if err != nil {
		t.Fatalf("DescribeFeatureType failed: %v", err)
	}
	return resp
}

// SkipIfNoFeatureTypes skips the test if no feature types are available.
func SkipIfNoFeatureTypes(t *testing.T, ctx *TestContext) {
	t.Helper()
	if !ctx.HasFeatureTypes() {
		t.Skip("No feature types available")
	}
}

// SkipIfOperationNotSupported skips if the operation is not supported.
func SkipIfOperationNotSupported(t *testing.T, ctx *TestContext, operation string) {
	t.Helper()
	if !ctx.SupportsOperation(operation) {
		t.Skipf("Operation %s not supported", operation)
	}
}

// SkipIfSpatialOperatorNotSupported skips if the spatial operator is not supported.
func SkipIfSpatialOperatorNotSupported(t *testing.T, ctx *TestContext, operator string) {
	t.Helper()
	if !ctx.SupportsSpatialOperator(operator) {
		t.Skipf("Spatial operator %s not supported", operator)
	}
}

// SkipIfComparisonOperatorNotSupported skips if the comparison operator is not supported.
func SkipIfComparisonOperatorNotSupported(t *testing.T, ctx *TestContext, operator string) {
	t.Helper()
	if !ctx.SupportsComparisonOperator(operator) {
		t.Skipf("Comparison operator %s not supported", operator)
	}
}
