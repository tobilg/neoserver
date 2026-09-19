package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransportSecurityRejectsUntrustedForwardedProto(t *testing.T) {
	middleware, err := TransportSecurity(TransportConfig{RequireHTTPS: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	request.RemoteAddr = "203.0.113.10:1234"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("got %d", response.Code)
	}
}

func TestTransportSecurityAcceptsTrustedProxy(t *testing.T) {
	middleware, err := TransportSecurity(TransportConfig{RequireHTTPS: true, TrustedProxyCIDRs: []string{"192.0.2.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsSecureTransport(r.Context()) {
			t.Error("transport was not marked secure")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	request.RemoteAddr = "192.0.2.2:1234"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-For", "198.51.100.7")
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("got %d", response.Code)
	}
	if request.RemoteAddr != "198.51.100.7" {
		t.Fatalf("unexpected client address %q", request.RemoteAddr)
	}
}

func TestTransportSecurityAllowsDirectLoopbackDevelopment(t *testing.T) {
	middleware, err := TransportSecurity(TransportConfig{RequireHTTPS: true})
	if err != nil {
		t.Fatal(err)
	}
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("got %d", response.Code)
	}
}
