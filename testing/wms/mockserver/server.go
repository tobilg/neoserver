package mockserver

import (
	"net/http"
	"net/http/httptest"
)

// Server represents a mock WMS server for testing.
type Server struct {
	httpServer *httptest.Server
	handler    *Handler
}

// New creates a new mock WMS server.
func New() *Server {
	// Create server first to get URL, then create handler with that URL
	server := &Server{}

	// Create a temporary handler
	mux := http.NewServeMux()

	// Create the test server
	server.httpServer = httptest.NewServer(mux)

	// Now create the handler with the actual URL
	server.handler = NewHandler(server.httpServer.URL + "?")

	// Update the mux to use our handler
	mux.Handle("/", server.handler)

	return server
}

// NewWithURL creates a mock WMS server with a custom base URL.
// Useful when you need to specify a particular URL format.
func NewWithURL(baseURL string) *Server {
	handler := NewHandler(baseURL)
	server := httptest.NewServer(handler)
	return &Server{
		httpServer: server,
		handler:    handler,
	}
}

// URL returns the base URL of the mock server.
func (s *Server) URL() string {
	return s.httpServer.URL
}

// Close shuts down the mock server.
func (s *Server) Close() {
	if s.httpServer != nil {
		s.httpServer.Close()
	}
}

// Handler returns the underlying HTTP handler.
// Useful for testing without starting a server.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// ServeHTTP allows Server to be used as an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// GetLayers returns the list of mock layers.
func (s *Server) GetLayers() []MockLayer {
	return DefaultMockLayers()
}

// GetLayerNames returns just the layer names.
func (s *Server) GetLayerNames() []string {
	layers := DefaultMockLayers()
	names := make([]string, len(layers))
	for i, layer := range layers {
		names[i] = layer.Name
	}
	return names
}

// StartMockServer is a convenience function that creates and returns a mock server.
// The caller is responsible for calling Close() when done.
func StartMockServer() *Server {
	return New()
}
