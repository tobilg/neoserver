package wfs

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================================
// getHTTPStatus Tests
// ============================================================================

func TestGetHTTPStatus(t *testing.T) {
	tests := []struct {
		code       string
		wantStatus int
	}{
		{ExceptionOperationNotSupported, http.StatusNotImplemented},
		{ExceptionMissingParameterValue, http.StatusBadRequest},
		{ExceptionInvalidParameterValue, http.StatusBadRequest},
		{ExceptionVersionNegotiationFailed, http.StatusBadRequest},
		{ExceptionOptionNotSupported, http.StatusBadRequest},
		{ExceptionOperationParsingFailed, http.StatusBadRequest},
		{ExceptionAuthorizationFailed, http.StatusUnauthorized},
		{ExceptionOperationProcessingFailed, http.StatusInternalServerError},
		{ExceptionNotFound, http.StatusNotFound},
		// ETS wfs20 GetFeatureWithLockTests asserts 403 for an expired lock,
		// LockFeatureTests asserts 400 when a lock cannot be granted.
		{ExceptionLockHasExpired, http.StatusForbidden},
		{ExceptionCannotLockAllFeatures, http.StatusBadRequest},
		{"UnknownCode", http.StatusBadRequest}, // default case
		{ExceptionNoApplicableCode, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := getHTTPStatus(tt.code)
			if got != tt.wantStatus {
				t.Errorf("getHTTPStatus(%q) = %d, want %d", tt.code, got, tt.wantStatus)
			}
		})
	}
}

// ============================================================================
// NewException Tests
// ============================================================================

func TestNewException(t *testing.T) {
	exc := NewException(ExceptionMissingParameterValue, "REQUEST", "Missing REQUEST parameter")

	if exc.ExceptionCode != ExceptionMissingParameterValue {
		t.Errorf("ExceptionCode = %q, want %q", exc.ExceptionCode, ExceptionMissingParameterValue)
	}
	if exc.Locator != "REQUEST" {
		t.Errorf("Locator = %q, want %q", exc.Locator, "REQUEST")
	}
	if len(exc.ExceptionText) != 1 || exc.ExceptionText[0] != "Missing REQUEST parameter" {
		t.Errorf("ExceptionText = %v, want [%q]", exc.ExceptionText, "Missing REQUEST parameter")
	}
}

// ============================================================================
// ExceptionFromError Tests
// ============================================================================

func TestExceptionFromError(t *testing.T) {
	t.Run("RequestError", func(t *testing.T) {
		reqErr := &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "BBOX",
			Message: "Invalid BBOX format",
		}
		exc := ExceptionFromError(reqErr)

		if exc.ExceptionCode != ExceptionInvalidParameterValue {
			t.Errorf("ExceptionCode = %q, want %q", exc.ExceptionCode, ExceptionInvalidParameterValue)
		}
		if exc.Locator != "BBOX" {
			t.Errorf("Locator = %q, want %q", exc.Locator, "BBOX")
		}
		if len(exc.ExceptionText) != 1 || exc.ExceptionText[0] != "Invalid BBOX format" {
			t.Errorf("ExceptionText = %v, want [%q]", exc.ExceptionText, "Invalid BBOX format")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		// Internal errors must be sanitized so DB details are not disclosed.
		err := errors.New("some database error")
		exc := ExceptionFromError(err)

		if exc.ExceptionCode != ExceptionNoApplicableCode {
			t.Errorf("ExceptionCode = %q, want %q", exc.ExceptionCode, ExceptionNoApplicableCode)
		}
		if exc.Locator != "" {
			t.Errorf("Locator = %q, want empty", exc.Locator)
		}
		if len(exc.ExceptionText) != 1 || exc.ExceptionText[0] != genericInternalMessage {
			t.Errorf("ExceptionText = %v, want [%q]", exc.ExceptionText, genericInternalMessage)
		}
	})
}

// ============================================================================
// WriteException Tests
// ============================================================================

func TestWriteException(t *testing.T) {
	t.Run("MissingParameterValue", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteException(w, ExceptionMissingParameterValue, "SERVICE", "Missing SERVICE parameter")

		if w.Code != http.StatusBadRequest {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
			t.Errorf("Content-Type = %q, want application/xml", ct)
		}
		body := w.Body.String()
		if !strings.Contains(body, "MissingParameterValue") {
			t.Errorf("Body should contain exception code")
		}
		if !strings.Contains(body, "SERVICE") {
			t.Errorf("Body should contain locator")
		}
		if !strings.Contains(body, "Missing SERVICE parameter") {
			t.Errorf("Body should contain exception text")
		}
	})

	t.Run("OperationNotSupported", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteException(w, ExceptionOperationNotSupported, "Transaction", "Operation not supported")

		if w.Code != http.StatusNotImplemented {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusNotImplemented)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		w := httptest.NewRecorder()
		WriteException(w, ExceptionNotFound, "", "Feature not found")

		if w.Code != http.StatusNotFound {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusNotFound)
		}
	})
}

// ============================================================================
// WriteExceptionFromError Tests
// ============================================================================

func TestWriteExceptionFromError(t *testing.T) {
	t.Run("RequestError", func(t *testing.T) {
		w := httptest.NewRecorder()
		reqErr := &RequestError{
			Code:    ExceptionInvalidParameterValue,
			Locator: "BBOX",
			Message: "Invalid BBOX format",
		}
		WriteExceptionFromError(w, reqErr)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
		}
		body := w.Body.String()
		if !strings.Contains(body, ExceptionInvalidParameterValue) {
			t.Errorf("Body should contain exception code")
		}
	})

	t.Run("GenericError", func(t *testing.T) {
		w := httptest.NewRecorder()
		// The raw error text must not appear in the client-facing response.
		err := errors.New("pq: column \"salary\" of relation \"employees\"")
		WriteExceptionFromError(w, err)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
		}
		body := w.Body.String()
		if !strings.Contains(body, ExceptionNoApplicableCode) {
			t.Errorf("Body should contain NoApplicableCode")
		}
		if !strings.Contains(body, genericInternalMessage) {
			t.Errorf("Body should contain the generic sanitized message")
		}
		if strings.Contains(body, "salary") || strings.Contains(body, "employees") {
			t.Errorf("Body must not leak raw error details, got: %s", body)
		}
	})
}

// ============================================================================
// WriteMultipleExceptions Tests
// ============================================================================

func TestWriteMultipleExceptions(t *testing.T) {
	w := httptest.NewRecorder()
	exceptions := []OWSException{
		NewException(ExceptionMissingParameterValue, "SERVICE", "Missing SERVICE"),
		NewException(ExceptionMissingParameterValue, "REQUEST", "Missing REQUEST"),
	}
	WriteMultipleExceptions(w, exceptions)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Missing SERVICE") {
		t.Errorf("Body should contain first exception")
	}
	if !strings.Contains(body, "Missing REQUEST") {
		t.Errorf("Body should contain second exception")
	}
}

// ============================================================================
// WriteExceptionFunc Tests
// ============================================================================

func TestWriteExceptionFunc(t *testing.T) {
	w := httptest.NewRecorder()
	WriteExceptionFunc(w, ExceptionAuthorizationFailed, "Access denied")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	body := w.Body.String()
	if !strings.Contains(body, ExceptionAuthorizationFailed) {
		t.Errorf("Body should contain exception code")
	}
	if !strings.Contains(body, "Access denied") {
		t.Errorf("Body should contain message")
	}
}
