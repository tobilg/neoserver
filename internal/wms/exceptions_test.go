package wms

import (
	"errors"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteException(t *testing.T) {
	tests := []struct {
		name           string
		code           string
		message        string
		expectedStatus int
	}{
		{
			name:           "LayerNotDefined",
			code:           ExceptionLayerNotDefined,
			message:        "Layer test not found",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "StyleNotDefined",
			code:           ExceptionStyleNotDefined,
			message:        "Style custom not found",
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "InvalidFormat",
			code:           ExceptionInvalidFormat,
			message:        "Format not supported",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "InvalidCRS",
			code:           ExceptionInvalidCRS,
			message:        "CRS not supported",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "MissingParameterValue",
			code:           ExceptionMissingParameterValue,
			message:        "LAYERS required",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "InvalidParameterValue",
			code:           ExceptionInvalidParameterValue,
			message:        "Invalid value",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "OperationNotSupported",
			code:           ExceptionOperationNotSupported,
			message:        "Operation not supported",
			expectedStatus: http.StatusNotImplemented,
		},
		{
			name:           "UnknownCode",
			code:           "UnknownError",
			message:        "Something went wrong",
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteException(w, tt.code, tt.message)

			if w.Code != tt.expectedStatus {
				t.Errorf("WriteException() status = %d, want %d", w.Code, tt.expectedStatus)
			}

			if ct := w.Header().Get("Content-Type"); ct != "text/xml" {
				t.Errorf("WriteException() Content-Type = %s, want text/xml", ct)
			}

			body := w.Body.String()
			if !strings.Contains(body, "<?xml") {
				t.Error("WriteException() response should contain XML declaration")
			}
			if !strings.Contains(body, "ServiceExceptionReport") {
				t.Error("WriteException() response should contain ServiceExceptionReport")
			}
			if !strings.Contains(body, tt.code) {
				t.Errorf("WriteException() response should contain code %s", tt.code)
			}
			if !strings.Contains(body, tt.message) {
				t.Errorf("WriteException() response should contain message %s", tt.message)
			}
		})
	}
}

func TestWriteMultipleExceptions(t *testing.T) {
	exceptions := []ServiceException{
		{Code: ExceptionMissingParameterValue, Text: "Missing LAYERS"},
		{Code: ExceptionMissingParameterValue, Text: "Missing CRS"},
		{Code: ExceptionInvalidFormat, Text: "Invalid format"},
	}

	w := httptest.NewRecorder()
	WriteMultipleExceptions(w, exceptions)

	if w.Code != http.StatusBadRequest {
		t.Errorf("WriteMultipleExceptions() status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	body := w.Body.String()
	for _, exc := range exceptions {
		if !strings.Contains(body, exc.Text) {
			t.Errorf("WriteMultipleExceptions() should contain message: %s", exc.Text)
		}
	}
}

func TestGetHTTPStatus(t *testing.T) {
	tests := []struct {
		code     string
		expected int
	}{
		{ExceptionLayerNotDefined, http.StatusNotFound},
		{ExceptionStyleNotDefined, http.StatusNotFound},
		{ExceptionInvalidFormat, http.StatusBadRequest},
		{ExceptionInvalidCRS, http.StatusBadRequest},
		{ExceptionInvalidPoint, http.StatusBadRequest},
		{ExceptionMissingParameterValue, http.StatusBadRequest},
		{ExceptionInvalidParameterValue, http.StatusBadRequest},
		{ExceptionInvalidDimensionValue, http.StatusBadRequest},
		{ExceptionMissingDimensionValue, http.StatusBadRequest},
		{ExceptionOperationNotSupported, http.StatusNotImplemented},
		{"AuthorizationFailed", http.StatusUnauthorized},
		{"UnknownCode", http.StatusInternalServerError},
		{"", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			result := getHTTPStatus(tt.code)
			if result != tt.expected {
				t.Errorf("getHTTPStatus(%s) = %d, want %d", tt.code, result, tt.expected)
			}
		})
	}
}

func TestNewException(t *testing.T) {
	exc := NewException(ExceptionLayerNotDefined, "Layer not found")

	if exc.Code != ExceptionLayerNotDefined {
		t.Errorf("NewException() Code = %s, want %s", exc.Code, ExceptionLayerNotDefined)
	}
	if exc.Text != "Layer not found" {
		t.Errorf("NewException() Text = %s, want 'Layer not found'", exc.Text)
	}
}

func TestExceptionFromError(t *testing.T) {
	t.Run("RequestError", func(t *testing.T) {
		reqErr := &RequestError{Code: ExceptionMissingParameterValue, Message: "Missing param"}
		exc := ExceptionFromError(reqErr)

		if exc.Code != ExceptionMissingParameterValue {
			t.Errorf("ExceptionFromError() Code = %s, want %s", exc.Code, ExceptionMissingParameterValue)
		}
		if exc.Text != "Missing param" {
			t.Errorf("ExceptionFromError() Text = %s, want 'Missing param'", exc.Text)
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		// Internal errors must be sanitized to avoid leaking internal details.
		err := errors.New("pq: relation \"secret_table\" does not exist")
		exc := ExceptionFromError(err)

		if exc.Code != "" {
			t.Errorf("ExceptionFromError() Code = %s, want empty string", exc.Code)
		}
		if exc.Text != genericInternalMessage {
			t.Errorf("ExceptionFromError() Text = %s, want %q", exc.Text, genericInternalMessage)
		}
	})
}

func TestWriteExceptionINIMAGE(t *testing.T) {
	w := httptest.NewRecorder()
	WriteExceptionINIMAGE(w, "image/png", 256, 256, ExceptionLayerNotDefined, "Layer not found")

	if w.Code != http.StatusOK {
		t.Errorf("WriteExceptionINIMAGE() status = %d, want %d", w.Code, http.StatusOK)
	}

	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("WriteExceptionINIMAGE() Content-Type = %s, want image/png", ct)
	}

	// Verify it's a valid PNG
	img, err := png.Decode(w.Body)
	if err != nil {
		t.Errorf("WriteExceptionINIMAGE() did not produce valid PNG: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() != 256 || bounds.Dy() != 256 {
		t.Errorf("WriteExceptionINIMAGE() image size = %dx%d, want 256x256", bounds.Dx(), bounds.Dy())
	}
}

func TestWriteExceptionBLANK(t *testing.T) {
	t.Run("Transparent", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteExceptionBLANK(w, "image/png", 128, 128, true, color.RGBA{})

		if w.Code != http.StatusOK {
			t.Errorf("WriteExceptionBLANK() status = %d, want %d", w.Code, http.StatusOK)
		}

		if ct := w.Header().Get("Content-Type"); ct != "image/png" {
			t.Errorf("WriteExceptionBLANK() Content-Type = %s, want image/png", ct)
		}

		img, err := png.Decode(w.Body)
		if err != nil {
			t.Errorf("WriteExceptionBLANK() did not produce valid PNG: %v", err)
		}

		bounds := img.Bounds()
		if bounds.Dx() != 128 || bounds.Dy() != 128 {
			t.Errorf("WriteExceptionBLANK() image size = %dx%d, want 128x128", bounds.Dx(), bounds.Dy())
		}
	})

	t.Run("OpaqueWithColor", func(t *testing.T) {
		w := httptest.NewRecorder()
		bgColor := color.RGBA{R: 255, G: 0, B: 0, A: 255}
		WriteExceptionBLANK(w, "image/png", 64, 64, false, bgColor)

		if w.Code != http.StatusOK {
			t.Errorf("WriteExceptionBLANK() status = %d, want %d", w.Code, http.StatusOK)
		}

		img, err := png.Decode(w.Body)
		if err != nil {
			t.Errorf("WriteExceptionBLANK() did not produce valid PNG: %v", err)
		}

		bounds := img.Bounds()
		if bounds.Dx() != 64 || bounds.Dy() != 64 {
			t.Errorf("WriteExceptionBLANK() image size = %dx%d, want 64x64", bounds.Dx(), bounds.Dy())
		}
	})
}

func TestCreateExceptionImage(t *testing.T) {
	img := createExceptionImage(200, 100, "Test error message")

	bounds := img.Bounds()
	if bounds.Dx() != 200 || bounds.Dy() != 100 {
		t.Errorf("createExceptionImage() size = %dx%d, want 200x100", bounds.Dx(), bounds.Dy())
	}
}

func TestServiceExceptionReport(t *testing.T) {
	report := ServiceExceptionReport{
		Version: Version130,
		ServiceExceptions: []ServiceException{
			{Code: ExceptionLayerNotDefined, Text: "test"},
		},
	}

	if report.Version != Version130 {
		t.Errorf("ServiceExceptionReport Version = %s, want %s", report.Version, Version130)
	}
	if len(report.ServiceExceptions) != 1 {
		t.Errorf("ServiceExceptionReport should have 1 exception, got %d", len(report.ServiceExceptions))
	}
}
