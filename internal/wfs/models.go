// Package wfs implements an OGC WFS 2.0.0 (Web Feature Service) server.
package wfs

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/tobilg/neoserver/internal/query"
)

// SortField represents a field to sort by.
type SortField struct {
	Name string
	Desc bool
}

// XMLGetFeature represents a WFS 2.0 GetFeature XML request.
type XMLGetFeature struct {
	XMLName      xml.Name           `xml:"GetFeature"`
	Service      string             `xml:"service,attr"`
	Version      string             `xml:"version,attr"`
	OutputFormat string             `xml:"outputFormat,attr"`
	Count        string             `xml:"count,attr"`
	StartIndex   string             `xml:"startIndex,attr"`
	ResultType   string             `xml:"resultType,attr"`
	Queries      []XMLQuery         `xml:"Query"`
	StoredQuery  *XMLStoredQueryRef `xml:"StoredQuery"`
}

// XMLGetFeatureWithLock represents a WFS 2.0 GetFeatureWithLock XML request.
type XMLGetFeatureWithLock struct {
	XMLName      xml.Name           `xml:"GetFeatureWithLock"`
	Service      string             `xml:"service,attr"`
	Version      string             `xml:"version,attr"`
	OutputFormat string             `xml:"outputFormat,attr"`
	Count        string             `xml:"count,attr"`
	StartIndex   string             `xml:"startIndex,attr"`
	ResultType   string             `xml:"resultType,attr"`
	Expiry       string             `xml:"expiry,attr"`
	LockAction   string             `xml:"lockAction,attr"`
	Queries      []XMLQuery         `xml:"Query"`
	StoredQuery  *XMLStoredQueryRef `xml:"StoredQuery"`
}

// XMLQuery represents a Query element in a GetFeature request.
type XMLQuery struct {
	TypeNames     string   `xml:"typeNames,attr"`
	SrsName       string   `xml:"srsName,attr"`
	PropertyNames []string `xml:"PropertyName"`
	FilterRaw     string   `xml:",innerxml"` // Capture raw inner XML to extract Filter
}

// XMLStoredQueryRef represents a StoredQuery element in a GetFeature request.
type XMLStoredQueryRef struct {
	ID         string                    `xml:"id,attr"`
	Parameters []XMLStoredQueryParameter `xml:"Parameter"`
}

// XMLStoredQueryParameter represents a Parameter in a StoredQuery invocation.
type XMLStoredQueryParameter struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

// XMLFilter represents a filter element in a Query.
type XMLFilter struct {
	XMLName xml.Name
	Content string `xml:",innerxml"`
}

// XMLDescribeFeatureType represents a WFS 2.0 DescribeFeatureType XML request.
type XMLDescribeFeatureType struct {
	XMLName      xml.Name `xml:"DescribeFeatureType"`
	Service      string   `xml:"service,attr"`
	Version      string   `xml:"version,attr"`
	OutputFormat string   `xml:"outputFormat,attr"`
	TypeNames    []string `xml:"TypeName"`
}

// XMLDescribeStoredQueries represents a WFS 2.0 DescribeStoredQueries XML request.
type XMLDescribeStoredQueries struct {
	XMLName        xml.Name `xml:"DescribeStoredQueries"`
	Service        string   `xml:"service,attr"`
	Version        string   `xml:"version,attr"`
	StoredQueryIds []string `xml:"http://www.opengis.net/wfs/2.0 StoredQueryId"`
}

// XMLListStoredQueries represents a WFS 2.0 ListStoredQueries XML request.
type XMLListStoredQueries struct {
	XMLName xml.Name `xml:"ListStoredQueries"`
	Service string   `xml:"service,attr"`
	Version string   `xml:"version,attr"`
}

// XMLGetPropertyValue represents a WFS 2.0 GetPropertyValue XML request.
type XMLGetPropertyValue struct {
	XMLName        xml.Name   `xml:"GetPropertyValue"`
	Service        string     `xml:"service,attr"`
	Version        string     `xml:"version,attr"`
	ValueReference string     `xml:"valueReference,attr"`
	Count          string     `xml:"count,attr"`
	StartIndex     string     `xml:"startIndex,attr"`
	ResultType     string     `xml:"resultType,attr"`
	Queries        []XMLQuery `xml:"Query"`
}

// WFS version
const (
	Version200 = "2.0.0"
	Version202 = "2.0.2"
)

// XML Namespaces
const (
	NSWfs     = "http://www.opengis.net/wfs/2.0"
	NSGml     = "http://www.opengis.net/gml/3.2"
	NSFes     = "http://www.opengis.net/fes/2.0"
	NSOws     = "http://www.opengis.net/ows/1.1"
	NSXlink   = "http://www.w3.org/1999/xlink"
	NSXsi     = "http://www.w3.org/2001/XMLSchema-instance"
	NSXsd     = "http://www.w3.org/2001/XMLSchema"
	NSGmlSF   = "http://www.opengis.net/gmlsf/2.0"
	NSDefault = "http://neoserver/app"
)

// Schema locations
const (
	WfsSchemaLocation = "http://www.opengis.net/wfs/2.0 http://schemas.opengis.net/wfs/2.0/wfs.xsd"
	GmlSchemaLocation = "http://schemas.opengis.net/gml/3.2.1/gml.xsd"
)

// Request types
const (
	RequestGetCapabilities       = "GetCapabilities"
	RequestDescribeFeatureType   = "DescribeFeatureType"
	RequestGetFeature            = "GetFeature"
	RequestGetPropertyValue      = "GetPropertyValue"
	RequestListStoredQueries     = "ListStoredQueries"
	RequestDescribeStoredQueries = "DescribeStoredQueries"
)

// Output formats
const (
	FormatGML32      = "application/gml+xml; version=3.2"
	FormatGML        = "application/gml+xml"
	FormatGeoJSON    = "application/json"
	FormatXML        = "text/xml"
	FormatXSD        = "application/xml"
	FormatXMLSubtype = "application/xml; subtype=gml/3.2"
)

// Result types for GetFeature
const (
	ResultTypeResults = "results"
	ResultTypeHits    = "hits"
)

// Resolve values
const (
	ResolveNone  = "none"
	ResolveLocal = "local"
)

// Stored query IDs - WFS 2.0 supports both URN and HTTP URI formats
const (
	StoredQueryGetFeatureByIdURN  = "urn:ogc:def:query:OGC-WFS::GetFeatureById"
	StoredQueryGetFeatureByIdHTTP = "http://www.opengis.net/def/query/OGC-WFS/0/GetFeatureById"
	// Keep backward compatibility alias
	StoredQueryGetFeatureById = StoredQueryGetFeatureByIdURN
)

// IsGetFeatureByIdQuery returns true if the given ID matches the GetFeatureById stored query.
func IsGetFeatureByIdQuery(id string) bool {
	return id == StoredQueryGetFeatureByIdURN || id == StoredQueryGetFeatureByIdHTTP
}

// Default SRS formats
const (
	DefaultSRSURN    = "urn:ogc:def:crs:EPSG::4326"
	DefaultSRSHTTP   = "http://www.opengis.net/def/crs/EPSG/0/4326"
	DefaultSRSSimple = "EPSG:4326"
)

// CITE namespace for OGC conformance testing
const (
	NSCite = "http://cite.opengeospatial.org/gmlsf"
)

// NormalizeQuery returns a copy of the query values with all parameter names uppercased.
// WFS 2.0 requires parameter names to be case-insensitive.
func NormalizeQuery(r *http.Request) url.Values {
	q := make(url.Values)
	for k, v := range r.URL.Query() {
		q[strings.ToUpper(k)] = v
	}
	return q
}

// RequestError represents a WFS request error.
type RequestError struct {
	Code    string
	Locator string
	Message string
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// GetFeatureRequest represents a parsed GetFeature request.
type GetFeatureRequest struct {
	Version           string
	TypeNames         []string
	SrsName           string
	SRID              int
	BBox              *query.BBox
	BBoxSRID          int
	Filter            string   // CQL2 text filter or empty for FES XML
	FESFilter         string   // FES XML filter
	ResourceID        []string // Feature IDs to filter by (RESOURCEID parameter)
	Count             int
	StartIndex        int
	OutputFormat      string
	ResultType        string
	SortBy            []SortField
	PropertyName      []string
	Resolve           string
	ResolveDepth      string
	StoredQueryID     string            // Stored query ID (e.g., urn:ogc:def:query:OGC-WFS::GetFeatureById)
	StoredQueryParams map[string]string // Stored query parameters (uppercase keys)
}

// GetPropertyValueRequest represents a parsed GetPropertyValue request.
type GetPropertyValueRequest struct {
	ResourceID     []string
	Version        string
	TypeNames      []string
	ValueReference string
	SrsName        string
	SRID           int
	BBox           *query.BBox
	BBoxSRID       int
	Filter         string
	FESFilter      string
	Count          int
	StartIndex     int
	ResultType     string
	SortBy         []SortField
	Resolve        string
	ResolveDepth   string
}

// DescribeFeatureTypeRequest represents a parsed DescribeFeatureType request.
type DescribeFeatureTypeRequest struct {
	Version      string
	TypeNames    []string
	OutputFormat string
}

// DescribeStoredQueriesRequest represents a parsed DescribeStoredQueries request.
type DescribeStoredQueriesRequest struct {
	Version        string
	StoredQueryIds []string
}
