// Package capabilities provides WMS capabilities XML parsing utilities.
package capabilities

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// BoolInt is a custom type that can unmarshal both "0"/"1" and "true"/"false"
// as boolean values. WMS 1.3.0 spec allows both formats for attributes like queryable.
type BoolInt bool

// UnmarshalXMLAttr implements xml.UnmarshalerAttr for BoolInt.
func (b *BoolInt) UnmarshalXMLAttr(attr xml.Attr) error {
	switch strings.ToLower(attr.Value) {
	case "1", "true":
		*b = true
	case "0", "false", "":
		*b = false
	default:
		*b = false
	}
	return nil
}

// Capabilities represents a parsed WMS capabilities document.
type Capabilities struct {
	XMLName        xml.Name   `xml:"WMS_Capabilities"`
	Version        string     `xml:"version,attr"`
	UpdateSequence string     `xml:"updateSequence,attr"`
	Service        Service    `xml:"Service"`
	Capability     Capability `xml:"Capability"`
}

// Service contains WMS service metadata.
type Service struct {
	Name               string             `xml:"Name"`
	Title              string             `xml:"Title"`
	Abstract           string             `xml:"Abstract"`
	Keywords           []string           `xml:"KeywordList>Keyword"`
	OnlineResource     OnlineResource     `xml:"OnlineResource"`
	ContactInformation ContactInformation `xml:"ContactInformation"`
	Fees               string             `xml:"Fees"`
	AccessConstraints  string             `xml:"AccessConstraints"`
	LayerLimit         int                `xml:"LayerLimit"`
	MaxWidth           int                `xml:"MaxWidth"`
	MaxHeight          int                `xml:"MaxHeight"`
}

// OnlineResource represents an xlink reference.
type OnlineResource struct {
	Type string `xml:"type,attr"`
	Href string `xml:"href,attr"`
}

// ContactInformation contains contact metadata.
type ContactInformation struct {
	ContactPersonPrimary         ContactPersonPrimary `xml:"ContactPersonPrimary"`
	ContactPosition              string               `xml:"ContactPosition"`
	ContactAddress               ContactAddress       `xml:"ContactAddress"`
	ContactVoiceTelephone        string               `xml:"ContactVoiceTelephone"`
	ContactElectronicMailAddress string               `xml:"ContactElectronicMailAddress"`
}

// ContactPersonPrimary contains primary contact info.
type ContactPersonPrimary struct {
	ContactPerson       string `xml:"ContactPerson"`
	ContactOrganization string `xml:"ContactOrganization"`
}

// ContactAddress contains address information.
type ContactAddress struct {
	AddressType     string `xml:"AddressType"`
	Address         string `xml:"Address"`
	City            string `xml:"City"`
	StateOrProvince string `xml:"StateOrProvince"`
	PostCode        string `xml:"PostCode"`
	Country         string `xml:"Country"`
}

// Capability contains WMS capability information.
type Capability struct {
	Request   *Request  `xml:"Request"`
	Exception Exception `xml:"Exception"`
	Layer     *Layer    `xml:"Layer"`
}

// Request contains supported WMS operations.
type Request struct {
	GetCapabilities *Operation `xml:"GetCapabilities"`
	GetMap          *Operation `xml:"GetMap"`
	GetFeatureInfo  *Operation `xml:"GetFeatureInfo"`
}

// Operation represents a WMS operation with its formats and URLs.
type Operation struct {
	Format  []string `xml:"Format"`
	DCPType DCPType  `xml:"DCPType"`
}

// DCPType contains HTTP endpoint information.
type DCPType struct {
	HTTP HTTP `xml:"HTTP"`
}

// HTTP contains Get and Post endpoints.
type HTTP struct {
	Get  Endpoint `xml:"Get"`
	Post Endpoint `xml:"Post"`
}

// Endpoint contains the OnlineResource for an HTTP method.
type Endpoint struct {
	OnlineResource OnlineResource `xml:"OnlineResource"`
}

// Exception contains supported exception formats.
type Exception struct {
	Format []string `xml:"Format"`
}

// Layer represents a WMS layer (can be nested).
type Layer struct {
	Queryable               BoolInt                  `xml:"queryable,attr"`
	Opaque                  BoolInt                  `xml:"opaque,attr"`
	Cascaded                BoolInt                  `xml:"cascaded,attr"`
	Name                    string                   `xml:"Name"`
	Title                   string                   `xml:"Title"`
	Abstract                string                   `xml:"Abstract"`
	Keywords                []string                 `xml:"KeywordList>Keyword"`
	CRS                     []string                 `xml:"CRS"`
	EXGeographicBoundingBox *EXGeographicBoundingBox `xml:"EX_GeographicBoundingBox"`
	BoundingBox             []BoundingBox            `xml:"BoundingBox"`
	Dimension               []Dimension              `xml:"Dimension"`
	MetadataURL             []MetadataURL            `xml:"MetadataURL"`
	AuthorityURL            []AuthorityURL           `xml:"AuthorityURL"`
	Identifier              []Identifier             `xml:"Identifier"`
	Style                   []Style                  `xml:"Style"`
	MinScaleDenominator     float64                  `xml:"MinScaleDenominator"`
	MaxScaleDenominator     float64                  `xml:"MaxScaleDenominator"`
	Layer                   []*Layer                 `xml:"Layer"`
}

// EXGeographicBoundingBox represents the geographic extent in WGS84.
type EXGeographicBoundingBox struct {
	WestBoundLongitude float64 `xml:"westBoundLongitude"`
	EastBoundLongitude float64 `xml:"eastBoundLongitude"`
	SouthBoundLatitude float64 `xml:"southBoundLatitude"`
	NorthBoundLatitude float64 `xml:"northBoundLatitude"`
}

// BoundingBox represents a bounding box in a specific CRS.
type BoundingBox struct {
	CRS  string  `xml:"CRS,attr"`
	MinX float64 `xml:"minx,attr"`
	MinY float64 `xml:"miny,attr"`
	MaxX float64 `xml:"maxx,attr"`
	MaxY float64 `xml:"maxy,attr"`
}

// Dimension represents a layer dimension (e.g., TIME, ELEVATION).
type Dimension struct {
	Name         string  `xml:"name,attr"`
	Units        string  `xml:"units,attr"`
	Default      string  `xml:"default,attr"`
	NearestValue BoolInt `xml:"nearestValue,attr"`
	Current      BoolInt `xml:"current,attr"`
	Value        string  `xml:",chardata"`
}

// MetadataURL contains a metadata reference.
type MetadataURL struct {
	Type           string         `xml:"type,attr"`
	Format         string         `xml:"Format"`
	OnlineResource OnlineResource `xml:"OnlineResource"`
}

// AuthorityURL identifies an authority used by layer identifiers.
type AuthorityURL struct {
	Name           string         `xml:"name,attr"`
	OnlineResource OnlineResource `xml:"OnlineResource"`
}

// Identifier is a layer identifier qualified by an advertised authority.
type Identifier struct {
	Authority string `xml:"authority,attr"`
	Value     string `xml:",chardata"`
}

// Style represents a layer style.
type Style struct {
	Name      string     `xml:"Name"`
	Title     string     `xml:"Title"`
	Abstract  string     `xml:"Abstract"`
	LegendURL *LegendURL `xml:"LegendURL"`
}

// LegendURL contains legend graphic information.
type LegendURL struct {
	Width          int            `xml:"width,attr"`
	Height         int            `xml:"height,attr"`
	Format         string         `xml:"Format"`
	OnlineResource OnlineResource `xml:"OnlineResource"`
}

// Parse parses a WMS capabilities XML document.
func Parse(data []byte) (*Capabilities, error) {
	var caps Capabilities
	if err := xml.Unmarshal(data, &caps); err != nil {
		return nil, fmt.Errorf("parsing capabilities XML: %w", err)
	}
	return &caps, nil
}

// GetNamedLayers returns all named layers (layers with a Name element).
func (c *Capabilities) GetNamedLayers() []*Layer {
	var layers []*Layer
	if c.Capability.Layer != nil {
		collectNamedLayers(c.Capability.Layer, &layers)
	}
	return layers
}

// collectNamedLayers recursively collects all named layers.
func collectNamedLayers(layer *Layer, result *[]*Layer) {
	if layer.Name != "" {
		*result = append(*result, layer)
	}
	for _, child := range layer.Layer {
		collectNamedLayers(child, result)
	}
}

// GetLayerByName finds a layer by name (case-insensitive).
func (c *Capabilities) GetLayerByName(name string) *Layer {
	if c.Capability.Layer != nil {
		return findLayerByName(c.Capability.Layer, name)
	}
	return nil
}

// findLayerByName recursively finds a layer by name.
func findLayerByName(layer *Layer, name string) *Layer {
	if strings.EqualFold(layer.Name, name) {
		return layer
	}
	for _, child := range layer.Layer {
		if found := findLayerByName(child, name); found != nil {
			return found
		}
	}
	return nil
}

// GetQueryableLayers returns all queryable named layers.
func (c *Capabilities) GetQueryableLayers() []*Layer {
	var layers []*Layer
	for _, layer := range c.GetNamedLayers() {
		if layer.Queryable {
			layers = append(layers, layer)
		}
	}
	return layers
}

// SupportsGetFeatureInfo returns true if GetFeatureInfo is supported.
func (c *Capabilities) SupportsGetFeatureInfo() bool {
	if c.Capability.Request != nil && c.Capability.Request.GetFeatureInfo != nil {
		return len(c.Capability.Request.GetFeatureInfo.Format) > 0
	}
	return false
}

// GetMapFormats returns supported GetMap formats.
func (c *Capabilities) GetMapFormats() []string {
	if c.Capability.Request != nil && c.Capability.Request.GetMap != nil {
		return c.Capability.Request.GetMap.Format
	}
	return nil
}

// GetFeatureInfoFormats returns supported GetFeatureInfo formats.
func (c *Capabilities) GetFeatureInfoFormats() []string {
	if c.Capability.Request != nil && c.Capability.Request.GetFeatureInfo != nil {
		return c.Capability.Request.GetFeatureInfo.Format
	}
	return nil
}

// GetExceptionFormats returns supported exception formats.
func (c *Capabilities) GetExceptionFormats() []string {
	return c.Capability.Exception.Format
}

// IsQueryable returns true if the layer is queryable.
func (l *Layer) IsQueryable() bool {
	return bool(l.Queryable)
}

// IsOpaque returns true if the layer is opaque.
func (l *Layer) IsOpaque() bool {
	return bool(l.Opaque)
}

// GetCRSList returns the CRS list for the layer (including inherited).
func (l *Layer) GetCRSList() []string {
	return l.CRS
}

// GetFirstCRS returns the first CRS for the layer.
func (l *Layer) GetFirstCRS() string {
	if len(l.CRS) > 0 {
		return l.CRS[0]
	}
	return ""
}

// GetBoundingBox returns the bounding box for the given CRS.
func (l *Layer) GetBoundingBox(crs string) *BoundingBox {
	for _, bb := range l.BoundingBox {
		if strings.EqualFold(bb.CRS, crs) {
			return &bb
		}
	}
	return nil
}

// GetFirstStyle returns the first style for the layer.
func (l *Layer) GetFirstStyle() *Style {
	if len(l.Style) > 0 {
		return &l.Style[0]
	}
	return nil
}

// GetDimension returns the dimension with the given name.
func (l *Layer) GetDimension(name string) *Dimension {
	for _, dim := range l.Dimension {
		if strings.EqualFold(dim.Name, name) {
			return &dim
		}
	}
	return nil
}

// HasTimeDimension returns true if the layer has a TIME dimension.
func (l *Layer) HasTimeDimension() bool {
	return l.GetDimension("time") != nil
}

// GetTimeDimension returns the TIME dimension if present.
func (l *Layer) GetTimeDimension() *Dimension {
	return l.GetDimension("time")
}

// GetStyles returns the layer's styles.
func (l *Layer) GetStyles() []Style {
	return l.Style
}

// BoundingBoxes returns the layer's bounding boxes.
func (l *Layer) BoundingBoxes() []BoundingBox {
	return l.BoundingBox
}

// Dimensions returns the layer's dimensions.
func (l *Layer) Dimensions() []Dimension {
	return l.Dimension
}

// GetCapabilitiesFormats returns supported GetCapabilities formats.
func (c *Capabilities) GetCapabilitiesFormats() []string {
	if c.Capability.Request != nil && c.Capability.Request.GetCapabilities != nil {
		return c.Capability.Request.GetCapabilities.Format
	}
	return nil
}

// GetOnlineResource returns the OnlineResource href for the operation.
func (o *Operation) GetOnlineResource() string {
	if o == nil {
		return ""
	}
	return o.DCPType.HTTP.Get.OnlineResource.Href
}
