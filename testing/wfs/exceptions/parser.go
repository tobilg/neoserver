// Package exceptions provides OWS 1.1 exception report parsing utilities.
package exceptions

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// ExceptionReport represents an OWS 1.1 exception report.
type ExceptionReport struct {
	XMLName    xml.Name    `xml:"ExceptionReport"`
	Version    string      `xml:"version,attr"`
	Exceptions []Exception `xml:"Exception"`
}

// Exception represents a single exception.
type Exception struct {
	ExceptionCode string   `xml:"exceptionCode,attr"`
	Locator       string   `xml:"locator,attr"`
	ExceptionText []string `xml:"ExceptionText"`
}

// Parse parses an OWS exception report XML document.
func Parse(data []byte) (*ExceptionReport, error) {
	var report ExceptionReport
	if err := xml.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parsing exception XML: %w", err)
	}
	return &report, nil
}

// IsException checks if the data looks like an exception report.
func IsException(data []byte) bool {
	return bytes.Contains(data, []byte("ExceptionReport")) ||
		bytes.Contains(data, []byte("exceptionCode"))
}

// GetCode returns the first exception code.
func (r *ExceptionReport) GetCode() string {
	if len(r.Exceptions) > 0 {
		return r.Exceptions[0].ExceptionCode
	}
	return ""
}

// GetMessage returns the first exception message.
func (r *ExceptionReport) GetMessage() string {
	if len(r.Exceptions) > 0 && len(r.Exceptions[0].ExceptionText) > 0 {
		return r.Exceptions[0].ExceptionText[0]
	}
	return ""
}

// GetLocator returns the first exception locator.
func (r *ExceptionReport) GetLocator() string {
	if len(r.Exceptions) > 0 {
		return r.Exceptions[0].Locator
	}
	return ""
}

// HasCode returns true if any exception has the given code.
func (r *ExceptionReport) HasCode(code string) bool {
	for _, ex := range r.Exceptions {
		if ex.ExceptionCode == code {
			return true
		}
	}
	return false
}

// GetCodes returns all exception codes.
func (r *ExceptionReport) GetCodes() []string {
	var codes []string
	for _, ex := range r.Exceptions {
		codes = append(codes, ex.ExceptionCode)
	}
	return codes
}

// Count returns the number of exceptions.
func (r *ExceptionReport) Count() int {
	return len(r.Exceptions)
}
