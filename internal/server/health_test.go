package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func readinessTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func decodeReadiness(t *testing.T, recorder *httptest.ResponseRecorder) readinessResponse {
	t.Helper()
	var response readinessResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	return response
}

func TestReadinessHandlerHealthyAndDisabled(t *testing.T) {
	handler := ReadinessHandler(readinessTestLogger(), []readinessCheck{
		{name: "catalog", enabled: true, check: func(context.Context) error { return nil }},
		{name: "tile_cache", enabled: false},
	})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	response := decodeReadiness(t, recorder)
	if response.Status != "ready" || response.Checks["catalog"] != "ok" || response.Checks["tile_cache"] != "disabled" {
		t.Fatalf("response = %+v", response)
	}
}

func TestReadinessHandlerFailureDoesNotExposeDetails(t *testing.T) {
	handler := ReadinessHandler(readinessTestLogger(), []readinessCheck{
		{name: "catalog", enabled: true, check: func(context.Context) error { return errors.New("secret database path") }},
	})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("response exposed dependency error: %s", recorder.Body.String())
	}
	response := decodeReadiness(t, recorder)
	if response.Status != "not_ready" || response.Checks["catalog"] != "failed" {
		t.Fatalf("response = %+v", response)
	}
}

func TestReadinessHandlerTimeout(t *testing.T) {
	handler := readinessHandler(readinessTestLogger(), []readinessCheck{
		{name: "stuck", enabled: true, check: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }},
	}, 10*time.Millisecond)
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != http.StatusServiceUnavailable || decodeReadiness(t, recorder).Checks["stuck"] != "failed" {
		t.Fatalf("timeout response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestReadinessHandlerHeadHasNoBody(t *testing.T) {
	handler := ReadinessHandler(readinessTestLogger(), []readinessCheck{
		{name: "catalog", enabled: true, check: func(context.Context) error { return nil }},
	})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodHead, "/ready", nil))
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatalf("HEAD response = %d %q", recorder.Code, recorder.Body.String())
	}
}
