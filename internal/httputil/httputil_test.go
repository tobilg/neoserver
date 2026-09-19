package httputil

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		value          any
		wantStatus     int
		wantType       string
		wantBodySubstr string
	}{
		{
			name:           "simple object",
			status:         http.StatusOK,
			value:          map[string]string{"message": "hello"},
			wantStatus:     http.StatusOK,
			wantType:       "application/json",
			wantBodySubstr: `"message":"hello"`,
		},
		{
			name:           "error status",
			status:         http.StatusBadRequest,
			value:          map[string]int{"code": 400},
			wantStatus:     http.StatusBadRequest,
			wantType:       "application/json",
			wantBodySubstr: `"code":400`,
		},
		{
			name:           "html chars not escaped",
			status:         http.StatusOK,
			value:          map[string]string{"url": "http://example.com/path?a=1&b=2"},
			wantStatus:     http.StatusOK,
			wantType:       "application/json",
			wantBodySubstr: `&b=2`, // Should NOT be escaped to \u0026
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteJSON(w, tt.status, tt.value)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			gotType := w.Header().Get("Content-Type")
			if gotType != tt.wantType {
				t.Errorf("Content-Type = %q, want %q", gotType, tt.wantType)
			}

			body := w.Body.String()
			if !strings.Contains(body, tt.wantBodySubstr) {
				t.Errorf("body = %q, want to contain %q", body, tt.wantBodySubstr)
			}
		})
	}
}

func TestWriteGeoJSON(t *testing.T) {
	w := httptest.NewRecorder()
	value := map[string]string{"type": "FeatureCollection"}
	WriteGeoJSON(w, http.StatusOK, value)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	gotType := w.Header().Get("Content-Type")
	wantType := "application/geo+json"
	if gotType != wantType {
		t.Errorf("Content-Type = %q, want %q", gotType, wantType)
	}

	body := w.Body.String()
	if !strings.Contains(body, `"type":"FeatureCollection"`) {
		t.Errorf("body = %q, want to contain FeatureCollection", body)
	}
}

func TestReadJSON(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name:    "valid JSON",
			body:    `{"name": "test", "value": 42}`,
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			body:    `{invalid}`,
			wantErr: true,
		},
		{
			name:    "empty body",
			body:    ``,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(tt.body))
			var result map[string]any
			err := ReadJSON(r, &result)

			if (err != nil) != tt.wantErr {
				t.Errorf("ReadJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
