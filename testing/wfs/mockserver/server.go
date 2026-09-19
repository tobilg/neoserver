package mockserver

import (
	"net/http/httptest"
)

// Server is a mock WFS 2.0 server for testing.
type Server struct {
	*httptest.Server
	handler *Handler
}

// New creates a new mock WFS server.
func New() *Server {
	s := &Server{}
	s.Server = httptest.NewServer(nil)
	s.handler = NewHandler(s.Server.URL)
	s.Server.Config.Handler = s.handler
	return s
}

// NewTLS creates a new mock WFS server with TLS.
func NewTLS() *Server {
	s := &Server{}
	s.Server = httptest.NewTLSServer(nil)
	s.handler = NewHandler(s.Server.URL)
	s.Server.Config.Handler = s.handler
	return s
}

// GetURL returns the base URL of the mock server.
func (s *Server) GetURL() string {
	return s.Server.URL
}

// Close shuts down the mock server.
func (s *Server) Close() {
	s.Server.Close()
}

// FeatureTypes returns the mock feature types.
func (s *Server) FeatureTypes() []MockFeatureType {
	return s.handler.featureTypes
}

// Features returns the mock features.
func (s *Server) Features() []MockFeature {
	return s.handler.features
}

// SetFeatureTypes sets custom feature types.
func (s *Server) SetFeatureTypes(types []MockFeatureType) {
	s.handler.featureTypes = types
}

// SetFeatures sets custom features.
func (s *Server) SetFeatures(features []MockFeature) {
	s.handler.features = features
}

// AddFeature adds a feature to the mock server.
func (s *Server) AddFeature(f MockFeature) {
	s.handler.features = append(s.handler.features, f)
}

// ClearFeatures removes all features from the mock server.
func (s *Server) ClearFeatures() {
	s.handler.features = nil
}
