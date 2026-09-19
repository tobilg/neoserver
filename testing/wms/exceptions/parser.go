// Package exceptions provides WMS exception XML parsing utilities.
package exceptions

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// ServiceExceptionReport represents a WMS exception response.
type ServiceExceptionReport struct {
	XMLName    xml.Name           `xml:"ServiceExceptionReport"`
	Version    string             `xml:"version,attr"`
	Exceptions []ServiceException `xml:"ServiceException"`
}

// ServiceException represents a single WMS exception.
type ServiceException struct {
	Code    string `xml:"code,attr"`
	Locator string `xml:"locator,attr"`
	Message string `xml:",chardata"`
}

// Parse parses a WMS ServiceExceptionReport XML document.
func Parse(data []byte) (*ServiceExceptionReport, error) {
	var report ServiceExceptionReport
	if err := xml.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parsing exception XML: %w", err)
	}
	return &report, nil
}

// IsException checks if the given data is a ServiceExceptionReport.
func IsException(data []byte) bool {
	return strings.Contains(string(data), "ServiceExceptionReport") ||
		strings.Contains(string(data), "ServiceException")
}

// GetCode returns the exception code of the first exception.
func (r *ServiceExceptionReport) GetCode() string {
	if len(r.Exceptions) > 0 {
		return r.Exceptions[0].Code
	}
	return ""
}

// GetMessage returns the message of the first exception.
func (r *ServiceExceptionReport) GetMessage() string {
	if len(r.Exceptions) > 0 {
		return strings.TrimSpace(r.Exceptions[0].Message)
	}
	return ""
}

// HasCode checks if any exception has the given code.
func (r *ServiceExceptionReport) HasCode(code string) bool {
	for _, ex := range r.Exceptions {
		if strings.EqualFold(ex.Code, code) {
			return true
		}
	}
	return false
}

// GetCodes returns all exception codes.
func (r *ServiceExceptionReport) GetCodes() []string {
	codes := make([]string, len(r.Exceptions))
	for i, ex := range r.Exceptions {
		codes[i] = ex.Code
	}
	return codes
}

// Count returns the number of exceptions.
func (r *ServiceExceptionReport) Count() int {
	return len(r.Exceptions)
}
