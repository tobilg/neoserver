package wms

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"net/http"

	"github.com/fogleman/gg"
)

// ServiceException represents a WMS service exception.
type ServiceException struct {
	XMLName xml.Name `xml:"ServiceException"`
	Code    string   `xml:"code,attr,omitempty"`
	Locator string   `xml:"locator,attr,omitempty"`
	Text    string   `xml:",chardata"`
}

// ServiceExceptionReport is the root element for WMS exception responses.
type ServiceExceptionReport struct {
	XMLName           xml.Name           `xml:"ServiceExceptionReport"`
	Version           string             `xml:"version,attr"`
	Xmlns             string             `xml:"xmlns,attr,omitempty"`
	Xsi               string             `xml:"xmlns:xsi,attr,omitempty"`
	SchemaLocation    string             `xml:"xsi:schemaLocation,attr,omitempty"`
	ServiceExceptions []ServiceException `xml:"ServiceException"`
}

// Exception codes as defined in WMS 1.3.0 specification
const (
	ExceptionInvalidFormat             = "InvalidFormat"
	ExceptionInvalidCRS                = "InvalidCRS"
	ExceptionLayerNotDefined           = "LayerNotDefined"
	ExceptionStyleNotDefined           = "StyleNotDefined"
	ExceptionLayerNotQueryable         = "LayerNotQueryable"
	ExceptionInvalidPoint              = "InvalidPoint"
	ExceptionCurrentUpdateSequence     = "CurrentUpdateSequence"
	ExceptionInvalidUpdateSequence     = "InvalidUpdateSequence"
	ExceptionMissingDimensionValue     = "MissingDimensionValue"
	ExceptionInvalidDimensionValue     = "InvalidDimensionValue"
	ExceptionOperationNotSupported     = "OperationNotSupported"
	ExceptionMissingParameterValue     = "MissingParameterValue"
	ExceptionInvalidParameterValue     = "InvalidParameterValue"
	ExceptionOperationProcessingFailed = "OperationProcessingFailed"
	ExceptionServerBusy                = "ServerBusy"
)

// WriteException writes a WMS service exception response.
func WriteException(w http.ResponseWriter, code, message string) {
	WriteExceptionWithLocator(w, code, "", message)
}

func WriteExceptionWithLocator(w http.ResponseWriter, code, locator, message string) {
	report := ServiceExceptionReport{
		Version:        Version130,
		Xmlns:          "http://www.opengis.net/ogc",
		Xsi:            "http://www.w3.org/2001/XMLSchema-instance",
		SchemaLocation: "http://www.opengis.net/ogc http://schemas.opengis.net/wms/1.3.0/exceptions_1_3_0.xsd",
		ServiceExceptions: []ServiceException{
			{Code: code, Locator: locator, Text: message},
		},
	}

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(getHTTPStatus(code))

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")

	// Write XML declaration
	w.Write([]byte(xml.Header))
	enc.Encode(report)
}

// WriteMultipleExceptions writes multiple WMS service exceptions.
func WriteMultipleExceptions(w http.ResponseWriter, exceptions []ServiceException) {
	report := ServiceExceptionReport{
		Version:           Version130,
		Xmlns:             "http://www.opengis.net/ogc",
		Xsi:               "http://www.w3.org/2001/XMLSchema-instance",
		SchemaLocation:    "http://www.opengis.net/ogc http://schemas.opengis.net/wms/1.3.0/exceptions_1_3_0.xsd",
		ServiceExceptions: exceptions,
	}

	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusBadRequest)

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")

	w.Write([]byte(xml.Header))
	enc.Encode(report)
}

// getHTTPStatus returns the appropriate HTTP status code for an exception code.
func getHTTPStatus(code string) int {
	switch code {
	case ExceptionLayerNotDefined, ExceptionStyleNotDefined:
		return http.StatusNotFound
	case ExceptionInvalidFormat, ExceptionInvalidCRS, ExceptionInvalidPoint,
		ExceptionMissingParameterValue, ExceptionInvalidParameterValue,
		ExceptionInvalidDimensionValue, ExceptionMissingDimensionValue:
		return http.StatusBadRequest
	case ExceptionOperationNotSupported:
		return http.StatusNotImplemented
	case ExceptionServerBusy:
		return http.StatusServiceUnavailable
	case "AuthorizationFailed":
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// NewException creates a new ServiceException.
func NewException(code, message string) ServiceException {
	return ServiceException{
		Code: code,
		Text: message,
	}
}

// genericInternalMessage is returned to clients for internal errors so that raw
// error strings (which may contain SQL fragments, schema names, or file paths) are
// never disclosed. The underlying error is logged server-side instead.
const genericInternalMessage = "internal server error"

// ExceptionFromError converts an error to a ServiceException. Typed *RequestError
// values carry a safe, client-facing message; any other (internal) error is reduced
// to a generic message to avoid leaking internal details.
func ExceptionFromError(err error) ServiceException {
	if reqErr, ok := err.(*RequestError); ok {
		return ServiceException{
			Code: reqErr.Code,
			Text: reqErr.Message,
		}
	}
	return ServiceException{
		Code: "",
		Text: genericInternalMessage,
	}
}

// WriteExceptionINIMAGE writes a WMS exception as an image with the error text drawn on it.
// The Content-Type matches the requested format (e.g., image/png).
func WriteExceptionINIMAGE(w http.ResponseWriter, format string, width, height int, code, message string) {
	img := createExceptionImage(width, height, code+": "+message)
	body, err := encodeExceptionImage(format, img, width, height)
	if err != nil {
		WriteException(w, code, message)
		return
	}
	w.Header().Set("Content-Type", format)
	w.WriteHeader(http.StatusOK) // INIMAGE returns 200 OK with error in image
	_, _ = w.Write(body)
}

// WriteExceptionBLANK writes a blank image as a WMS exception response.
// If transparent is true and format supports transparency, returns transparent image.
// Otherwise returns an opaque image with the specified background color.
func WriteExceptionBLANK(w http.ResponseWriter, format string, width, height int, transparent bool, bgColor color.RGBA) {
	var img image.Image
	if transparent {
		// Create transparent image
		rgba := image.NewRGBA(image.Rect(0, 0, width, height))
		// Already transparent (zero-initialized)
		img = rgba
	} else {
		// Create opaque image with background color
		dc := gg.NewContext(width, height)
		dc.SetRGBA(float64(bgColor.R)/255, float64(bgColor.G)/255, float64(bgColor.B)/255, 1.0)
		dc.Clear()
		img = dc.Image()
	}
	body, err := encodeExceptionImage(format, img, width, height)
	if err != nil {
		WriteException(w, ExceptionOperationProcessingFailed, "failed to encode exception image")
		return
	}
	w.Header().Set("Content-Type", format)
	w.WriteHeader(http.StatusOK) // BLANK returns 200 OK with blank image
	_, _ = w.Write(body)
}

func encodeExceptionImage(format string, img image.Image, width, height int) ([]byte, error) {
	if format != FormatSVG {
		return encodeImage(format, img)
	}
	pngBody, err := encodeImage(FormatPNG, img)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d"><image width="%d" height="%d" href="data:image/png;base64,%s"/></svg>`, width, height, width, height, width, height, base64.StdEncoding.EncodeToString(pngBody))
	return out.Bytes(), nil
}

// createExceptionImage creates an image with the error message drawn on it.
func createExceptionImage(width, height int, message string) image.Image {
	dc := gg.NewContext(width, height)

	// White background
	dc.SetRGB(1, 1, 1)
	dc.Clear()

	// Draw red error text
	dc.SetRGB(1, 0, 0)

	// Draw the error message wrapped to fit the image
	dc.DrawStringWrapped(message, float64(width)/2, float64(height)/2, 0.5, 0.5, float64(width)-10, 1.5, gg.AlignCenter)

	return dc.Image()
}
