// Package testserver provides an in-memory OGC API Features server for testing.
// It allows running conformance tests directly against the handlers without
// needing a database or external server.
package testserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/ogc"
	"github.com/tobilg/neoserver/internal/workspace"
)

// TestServer wraps an httptest.Server with OGC API Features handlers.
type TestServer struct {
	Server    *httptest.Server
	Config    conf.Config
	Workspace *workspace.Workspace
}

// Option configures the test server.
type Option func(*TestServer)

// WithConfig sets the configuration for the test server.
func WithConfig(cfg conf.Config) Option {
	return func(ts *TestServer) {
		ts.Config = cfg
	}
}

// New creates a new test server with mock data.
// The server is started automatically and should be closed with Close().
func New(opts ...Option) *TestServer {
	ts := &TestServer{
		Config: defaultConfig(),
	}

	for _, opt := range opts {
		opt(ts)
	}

	// Create mock workspace with mock data source
	mockDS := NewMockDataSource("test-service")

	service := &workspace.Service{
		ID:         "test-service",
		Name:       "test",
		Type:       "postgis",
		Enabled:    true,
		Layers:     make(map[string]*workspace.Layer),
		DataSource: mockDS,
	}

	// Add layers from mock data source
	for _, layerName := range mockDS.GetLayers() {
		service.Layers[layerName] = &workspace.Layer{
			ID:          layerName,
			SourceLayer: layerName,
			PublicID:    layerName,
			Title:       mockDS.GetLayerTitle(layerName),
			Description: mockDS.GetLayerDescription(layerName),
			Enabled:     true,
			CRSDefault:  4326,
		}
	}

	ws := &workspace.Workspace{
		ID:          "test-workspace",
		Name:        "test",
		Description: "Test workspace for OGC API conformance testing",
		Services:    map[string]*workspace.Service{"test-service": service},
	}
	ts.Workspace = ws

	// Create router with workspace middleware and OGC handlers
	r := chi.NewRouter()

	// Workspace middleware that injects our test workspace
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := workspace.WithWorkspace(req.Context(), ws)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})

	// Register workspace-aware OGC routes
	ogc.RegisterWorkspaceRoutes(r, ogc.WorkspaceDependencies{
		Config:   ts.Config,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Registry: nil, // Not needed for single workspace test
	})

	ts.Server = httptest.NewServer(r)
	return ts
}

// URL returns the base URL of the test server.
func (ts *TestServer) URL() string {
	return ts.Server.URL
}

// Close shuts down the test server.
func (ts *TestServer) Close() {
	ts.Server.Close()
}

// defaultConfig returns a minimal configuration for testing.
func defaultConfig() conf.Config {
	return conf.Config{
		Server: conf.Server{
			UrlBase: "", // Will be set dynamically
		},
		Metadata: conf.Metadata{
			Title:       "OGC API Features Test Server",
			Description: "Test server for OGC API Features conformance testing",
		},
		Paging: conf.Paging{
			LimitDefault: 10,
			LimitMax:     1000,
		},
	}
}
