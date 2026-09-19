package mgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestGetCacheStats_Disabled(t *testing.T) {
	mockStore := newMockStore()
	h := newTestHandler(t, mockStore)
	h.cache = nil // Disable cache

	r := httptest.NewRequest("GET", "/cache/stats", nil)
	w := httptest.NewRecorder()

	h.getCacheStats(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if enabled, ok := result["enabled"].(bool); !ok || enabled {
		t.Error("expected enabled=false")
	}
	if msg, ok := result["message"].(string); !ok || msg != "Caching is not enabled" {
		t.Errorf("expected message about caching disabled, got %v", result["message"])
	}
}

func TestGetCacheStats_Enabled(t *testing.T) {
	mockStore := newMockStore()
	h := newTestHandler(t, mockStore)

	// Add some cache entries to generate stats
	h.cache.SetCapabilities("test:caps", []byte("caps"))

	r := httptest.NewRequest("GET", "/cache/stats", nil)
	w := httptest.NewRecorder()

	h.getCacheStats(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if enabled, ok := result["enabled"].(bool); !ok || !enabled {
		t.Error("expected enabled=true")
	}
	if _, ok := result["capabilities"]; !ok {
		t.Error("expected capabilities field")
	}
	if _, ok := result["collections"]; !ok {
		t.Error("expected collections field")
	}
	if _, ok := result["features"]; !ok {
		t.Error("expected features field")
	}
	if _, ok := result["tiles"]; !ok {
		t.Error("expected tiles field")
	}
	if profile, ok := result["profile"].(string); !ok || profile != "balanced" {
		t.Errorf("expected balanced cache profile, got %v", result["profile"])
	}
	if _, ok := result["total_max_size_bytes"]; !ok {
		t.Error("expected total_max_size_bytes field")
	}
	if _, ok := result["total_invalidation_keys"]; !ok {
		t.Error("expected total_invalidation_keys field")
	}
}

func TestClearAllCaches_Disabled(t *testing.T) {
	mockStore := newMockStore()
	h := newTestHandler(t, mockStore)
	h.cache = nil // Disable cache

	r := httptest.NewRequest("POST", "/cache/clear", nil)
	w := httptest.NewRecorder()

	h.clearAllCaches(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestClearAllCaches_Enabled(t *testing.T) {
	mockStore := newMockStore()
	h := newTestHandler(t, mockStore)

	// Add some cache entries
	h.cache.SetCapabilities("key1", []byte("value1"))
	h.cache.SetCollections("key2", []byte("value2"))

	r := httptest.NewRequest("POST", "/cache/clear", nil)
	w := httptest.NewRecorder()

	h.clearAllCaches(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if msg, ok := result["message"].(string); !ok || msg != "All caches cleared successfully" {
		t.Errorf("expected success message, got %v", result["message"])
	}
}

func TestClearCacheByType_Disabled(t *testing.T) {
	mockStore := newMockStore()
	h := newTestHandler(t, mockStore)
	h.cache = nil

	r := httptest.NewRequest("POST", "/cache/clear/capabilities", nil)
	w := httptest.NewRecorder()

	// Set up chi context with URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("cacheType", "capabilities")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.clearCacheByType(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestClearCacheByType_ValidTypes(t *testing.T) {
	validTypes := []string{"capabilities", "collections", "features", "tiles"}

	for _, cacheType := range validTypes {
		t.Run(cacheType, func(t *testing.T) {
			mockStore := newMockStore()
			h := newTestHandler(t, mockStore)

			r := httptest.NewRequest("POST", "/cache/clear/"+cacheType, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("cacheType", cacheType)
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

			h.clearCacheByType(w, r)

			if w.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", w.Code)
			}

			var result map[string]any
			if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if result["type"] != cacheType {
				t.Errorf("expected type=%s, got %v", cacheType, result["type"])
			}
		})
	}
}

func TestClearCacheByType_InvalidType(t *testing.T) {
	mockStore := newMockStore()
	h := newTestHandler(t, mockStore)

	r := httptest.NewRequest("POST", "/cache/clear/invalid", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("cacheType", "invalid")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.clearCacheByType(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var result errorResponse
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Detail != "Valid types: capabilities, collections, features, tiles, counts" {
		t.Errorf("expected detail about valid types, got %s", result.Detail)
	}
}

func TestClearWorkspaceCache_Disabled(t *testing.T) {
	mockStore := newMockStore()
	h := newTestHandler(t, mockStore)
	h.cache = nil

	r := httptest.NewRequest("POST", "/workspaces/test/cache/clear", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspace", "test")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.clearWorkspaceCache(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestClearWorkspaceCache_NotFound(t *testing.T) {
	mockStore := newMockStore()
	h := newTestHandler(t, mockStore)

	r := httptest.NewRequest("POST", "/workspaces/nonexistent/cache/clear", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspace", "nonexistent")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	h.clearWorkspaceCache(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}
