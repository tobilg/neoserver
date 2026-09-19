package wfs

import (
	"encoding/xml"
	"net/http"
)

// OWS Exception codes as defined in OWS 1.1 and WFS 2.0 specifications
const (
	ExceptionOperationNotSupported       = "OperationNotSupported"
	ExceptionMissingParameterValue       = "MissingParameterValue"
	ExceptionInvalidParameterValue       = "InvalidParameterValue"
	ExceptionVersionNegotiationFailed    = "VersionNegotiationFailed"
	ExceptionInvalidUpdateSequence       = "InvalidUpdateSequence"
	ExceptionOptionNotSupported          = "OptionNotSupported"
	ExceptionNoApplicableCode            = "NoApplicableCode"
	ExceptionOperationParsingFailed      = "OperationParsingFailed"
	ExceptionOperationProcessingFailed   = "OperationProcessingFailed"
	ExceptionCannotLockAllFeatures       = "CannotLockAllFeatures"
	ExceptionDuplicateStoredQueryIdValue = "DuplicateStoredQueryIdValue"
	ExceptionLockHasExpired              = "LockHasExpired"
	ExceptionResponseCacheExpired        = "ResponseCacheExpired"
	// WFS 2.0 Transaction exception codes
	ExceptionInvalidValue = "InvalidValue" // Invalid property value in Transaction
	// Additional codes for authorization
	ExceptionAuthorizationFailed = "AuthorizationFailed"
	// Resource not found - returns HTTP 404 per WFS 2.0 spec for GetFeatureById with unknown ID
	ExceptionNotFound = "NotFound"
)

// OWSExceptionReport is the root element for OWS exception responses (OGC 06-121r9).
type OWSExceptionReport struct {
	XMLName        xml.Name       `xml:"ows:ExceptionReport"`
	Version        string         `xml:"version,attr"`
	XMLLang        string         `xml:"xml:lang,attr,omitempty"`
	NSOWS          string         `xml:"xmlns:ows,attr"`
	NSXsi          string         `xml:"xmlns:xsi,attr"`
	SchemaLocation string         `xml:"xsi:schemaLocation,attr"`
	Exceptions     []OWSException `xml:"ows:Exception"`
}

// OWSException represents a single OWS exception.
type OWSException struct {
	ExceptionCode string   `xml:"exceptionCode,attr"`
	Locator       string   `xml:"locator,attr,omitempty"`
	ExceptionText []string `xml:"ows:ExceptionText,omitempty"`
}

// WriteException writes an OWS exception response.
func WriteException(w http.ResponseWriter, code, locator, message string) {
	writeException(w, getHTTPStatus(code), code, locator, message)
}

func writeException(w http.ResponseWriter, status int, code, locator, message string) {
	report := OWSExceptionReport{
		Version:        Version200,
		XMLLang:        "en",
		NSOWS:          NSOws,
		NSXsi:          NSXsi,
		SchemaLocation: NSOws + " http://schemas.opengis.net/ows/1.1.0/owsExceptionReport.xsd",
		Exceptions: []OWSException{
			{
				ExceptionCode: code,
				Locator:       locator,
				ExceptionText: []string{message},
			},
		},
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(status)

	// Write XML declaration
	w.Write([]byte(xml.Header))

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(report)
}

// genericInternalMessage is returned to clients for internal errors so that raw
// error strings (which may contain SQL fragments, schema names, or file paths) are
// never disclosed. The underlying error is logged server-side instead.
const genericInternalMessage = "internal server error"

// WriteExceptionFromError writes an OWS exception from a RequestError. Internal
// (non-RequestError) errors are reduced to a generic message.
func WriteExceptionFromError(w http.ResponseWriter, err error) {
	if reqErr, ok := err.(*RequestError); ok {
		WriteException(w, reqErr.Code, reqErr.Locator, reqErr.Message)
		return
	}
	WriteException(w, ExceptionNoApplicableCode, "", genericInternalMessage)
}

// WriteMultipleExceptions writes multiple OWS exceptions.
func WriteMultipleExceptions(w http.ResponseWriter, exceptions []OWSException) {
	report := OWSExceptionReport{
		Version:        Version200,
		XMLLang:        "en",
		NSOWS:          NSOws,
		NSXsi:          NSXsi,
		SchemaLocation: NSOws + " http://schemas.opengis.net/ows/1.1.0/owsExceptionReport.xsd",
		Exceptions:     exceptions,
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)

	w.Write([]byte(xml.Header))

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(report)
}

// getHTTPStatus returns the appropriate HTTP status code for an exception code.
func getHTTPStatus(code string) int {
	switch code {
	case ExceptionOperationNotSupported:
		return http.StatusNotImplemented
	case ExceptionMissingParameterValue, ExceptionInvalidParameterValue,
		ExceptionVersionNegotiationFailed, ExceptionOptionNotSupported,
		ExceptionOperationParsingFailed, ExceptionInvalidValue:
		return http.StatusBadRequest
	case ExceptionAuthorizationFailed:
		return http.StatusUnauthorized
	case ExceptionOperationProcessingFailed:
		return http.StatusInternalServerError
	case ExceptionNotFound:
		return http.StatusNotFound
	// ETS wfs20 LockFeatureTests asserts 400 for CannotLockAllFeatures (the
	// default) and GetFeatureWithLockTests asserts 403 for LockHasExpired.
	case ExceptionLockHasExpired:
		return http.StatusForbidden
	default:
		return http.StatusBadRequest
	}
}

// NewException creates a new OWSException.
func NewException(code, locator, message string) OWSException {
	return OWSException{
		ExceptionCode: code,
		Locator:       locator,
		ExceptionText: []string{message},
	}
}

// ExceptionFromError converts an error to an OWSException.
func ExceptionFromError(err error) OWSException {
	if reqErr, ok := err.(*RequestError); ok {
		return OWSException{
			ExceptionCode: reqErr.Code,
			Locator:       reqErr.Locator,
			ExceptionText: []string{reqErr.Message},
		}
	}
	return OWSException{
		ExceptionCode: ExceptionNoApplicableCode,
		ExceptionText: []string{genericInternalMessage},
	}
}

// WriteExceptionFunc is the exception writer function for auth middleware.
func WriteExceptionFunc(w http.ResponseWriter, code, message string) {
	WriteException(w, code, "", message)
}
