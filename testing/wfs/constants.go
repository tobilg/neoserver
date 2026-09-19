// Package wfs provides WFS 2.0 test utilities.
package wfs

import "time"

// WFS versions
const (
	Version200 = "2.0.0"
	Version202 = "2.0.2"
)

// Service identifier
const (
	ServiceWFS = "WFS"
)

// Request types
const (
	RequestGetCapabilities      = "GetCapabilities"
	RequestDescribeFeatureType  = "DescribeFeatureType"
	RequestGetFeature           = "GetFeature"
	RequestGetPropertyValue     = "GetPropertyValue"
	RequestListStoredQueries    = "ListStoredQueries"
	RequestDescribeStoredQueries = "DescribeStoredQueries"
)

// Output formats
const (
	FormatGML32   = "application/gml+xml; version=3.2"
	FormatGML     = "application/gml+xml"
	FormatGeoJSON = "application/json"
	FormatXML     = "text/xml"
)

// Result types
const (
	ResultTypeResults = "results"
	ResultTypeHits    = "hits"
)

// Exception codes (OWS 1.1)
const (
	ExCodeOperationNotSupported     = "OperationNotSupported"
	ExCodeMissingParameterValue     = "MissingParameterValue"
	ExCodeInvalidParameterValue     = "InvalidParameterValue"
	ExCodeVersionNegotiationFailed  = "VersionNegotiationFailed"
	ExCodeInvalidUpdateSequence     = "InvalidUpdateSequence"
	ExCodeOptionNotSupported        = "OptionNotSupported"
	ExCodeNoApplicableCode          = "NoApplicableCode"
	ExCodeOperationParsingFailed    = "OperationParsingFailed"
	ExCodeOperationProcessingFailed = "OperationProcessingFailed"
	ExCodeNotFound                  = "NotFound"

	// Aliases for test convenience
	ExceptionOperationNotSupported    = ExCodeOperationNotSupported
	ExceptionMissingParameterValue    = ExCodeMissingParameterValue
	ExceptionInvalidParameterValue    = ExCodeInvalidParameterValue
	ExceptionVersionNegotiationFailed = ExCodeVersionNegotiationFailed
	ExceptionOptionNotSupported       = ExCodeOptionNotSupported
	ExceptionNoApplicableCode         = ExCodeNoApplicableCode
)

// CRS values
const (
	CRSURN4326  = "urn:ogc:def:crs:EPSG::4326"
	CRSURN3857  = "urn:ogc:def:crs:EPSG::3857"
	CRSHTTP4326 = "http://www.opengis.net/def/crs/EPSG/0/4326"
	CRSCRS84    = "CRS:84"
)

// Stored query IDs
// Note: Both URN and HTTP URI forms are valid per OGC standards
const (
	StoredQueryGetFeatureById    = "http://www.opengis.net/def/query/OGC-WFS/0/GetFeatureById"
	StoredQueryGetFeatureByIdURN = "urn:ogc:def:query:OGC-WFS::GetFeatureById"
)

// XML namespaces
const (
	NamespaceWFS  = "http://www.opengis.net/wfs/2.0"
	NamespaceOWS  = "http://www.opengis.net/ows/1.1"
	NamespaceFES  = "http://www.opengis.net/fes/2.0"
	NamespaceGML  = "http://www.opengis.net/gml/3.2"
	NamespaceXLink = "http://www.w3.org/1999/xlink"
	NamespaceXSI  = "http://www.w3.org/2001/XMLSchema-instance"
)

// Default values
const (
	DefaultCount     = 10
	DefaultTimeout   = 30 * time.Second
	DefaultMaxTypes  = 10
)
