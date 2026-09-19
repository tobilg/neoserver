// Package wms provides WMS 1.3.0 conformance testing utilities.
package wms

// WMS version constants.
const (
	Version130 = "1.3.0"
	Version111 = "1.1.1"
)

// Image format MIME types.
const (
	FormatPNG  = "image/png"
	FormatJPEG = "image/jpeg"
	FormatGIF  = "image/gif"
)

// GetFeatureInfo format MIME types.
const (
	InfoFormatXML  = "text/xml"
	InfoFormatJSON = "application/json"
	InfoFormatHTML = "text/html"
	InfoFormatText = "text/plain"
)

// Capabilities format MIME types.
const (
	CapabilitiesFormatXML = "text/xml"
)

// Exception format values.
const (
	ExceptionXML     = "XML"
	ExceptionINIMAGE = "INIMAGE"
	ExceptionBLANK   = "BLANK"
)

// WMS exception codes (matching OGC WMS 1.3.0 spec and internal/wms/exceptions.go).
const (
	ExCodeInvalidFormat           = "InvalidFormat"
	ExCodeInvalidCRS              = "InvalidCRS"
	ExCodeLayerNotDefined         = "LayerNotDefined"
	ExCodeStyleNotDefined         = "StyleNotDefined"
	ExCodeLayerNotQueryable       = "LayerNotQueryable"
	ExCodeInvalidPoint            = "InvalidPoint"
	ExCodeCurrentUpdateSequence   = "CurrentUpdateSequence"
	ExCodeInvalidUpdateSequence   = "InvalidUpdateSequence"
	ExCodeMissingDimensionValue   = "MissingDimensionValue"
	ExCodeInvalidDimensionValue   = "InvalidDimensionValue"
	ExCodeOperationNotSupported   = "OperationNotSupported"
	ExCodeMissingParameterValue   = "MissingParameterValue"
	ExCodeInvalidParameterValue   = "InvalidParameterValue"
)

// CRS values commonly used in WMS.
const (
	CRSCRS84    = "CRS:84"
	CRSEPSG4326 = "EPSG:4326"
	CRSEPSG3857 = "EPSG:3857"
)

// WMS request types.
const (
	RequestGetCapabilities   = "GetCapabilities"
	RequestGetMap            = "GetMap"
	RequestGetFeatureInfo    = "GetFeatureInfo"
	RequestGetLegendGraphic  = "GetLegendGraphic"
)

// Service type.
const ServiceWMS = "WMS"

// Default values.
const (
	DefaultWidth     = 256
	DefaultHeight    = 256
	DefaultTimeout   = 30 // seconds
	DefaultMaxLayers = 10 // for exhaustive testing
)

// XML namespaces used in WMS responses.
const (
	WMSNamespace = "http://www.opengis.net/wms"
	OGCNamespace = "http://www.opengis.net/ogc"
	XLinkNamespace = "http://www.w3.org/1999/xlink"
)
