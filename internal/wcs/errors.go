package wcs

import (
	"encoding/xml"
	"net/http"
)

type requestError struct {
	Code    string
	Locator string
	Text    string
	Status  int
}

func (e *requestError) Error() string { return e.Text }

type exceptionReport struct {
	XMLName xml.Name  `xml:"http://www.opengis.net/ows/2.0 ExceptionReport"`
	Version string    `xml:"version,attr"`
	Error   exception `xml:"Exception"`
}
type exception struct {
	Code    string `xml:"exceptionCode,attr"`
	Locator string `xml:"locator,attr,omitempty"`
	Text    string `xml:"ExceptionText"`
}

func writeException(w http.ResponseWriter, err *requestError) {
	status := err.Status
	if status == 0 {
		status = http.StatusBadRequest
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_ = xml.NewEncoder(w).Encode(exceptionReport{Version: "2.0.0", Error: exception{Code: err.Code, Locator: err.Locator, Text: err.Text}})
}

func missing(name string) *requestError {
	return &requestError{Code: "MissingParameterValue", Locator: name, Text: name + " parameter is required"}
}
func invalid(name, text string) *requestError {
	return &requestError{Code: "InvalidParameterValue", Locator: name, Text: text}
}
func invalidAxis(text string) *requestError {
	return &requestError{Code: "InvalidAxisLabel", Locator: "subset", Text: text, Status: http.StatusNotFound}
}
func invalidSubsetting(text string) *requestError {
	return &requestError{Code: "InvalidSubsetting", Locator: "subset", Text: text, Status: http.StatusNotFound}
}
func noCoverage() *requestError {
	return &requestError{Code: "NoSuchCoverage", Locator: "coverageId", Text: "coverage not found", Status: http.StatusNotFound}
}
