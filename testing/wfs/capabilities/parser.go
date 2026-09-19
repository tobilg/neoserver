// Package capabilities provides WFS capabilities XML parsing utilities.
package capabilities

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// Capabilities represents a parsed WFS capabilities document.
type Capabilities struct {
	XMLName               xml.Name              `xml:"WFS_Capabilities"`
	Version               string                `xml:"version,attr"`
	UpdateSequence        string                `xml:"updateSequence,attr"`
	ServiceIdentification ServiceIdentification `xml:"ServiceIdentification"`
	ServiceProvider       ServiceProvider       `xml:"ServiceProvider"`
	OperationsMetadata    OperationsMetadata    `xml:"OperationsMetadata"`
	FeatureTypeList       FeatureTypeList       `xml:"FeatureTypeList"`
	FilterCapabilities    *FilterCapabilities   `xml:"Filter_Capabilities"`
}

// ServiceIdentification contains service identification metadata.
type ServiceIdentification struct {
	Title           string   `xml:"Title"`
	Abstract        string   `xml:"Abstract"`
	Keywords        []string `xml:"Keywords>Keyword"`
	ServiceType     string   `xml:"ServiceType"`
	ServiceTypeVersion []string `xml:"ServiceTypeVersion"`
	Fees            string   `xml:"Fees"`
	AccessConstraints string `xml:"AccessConstraints"`
}

// ServiceProvider contains service provider metadata.
type ServiceProvider struct {
	ProviderName string       `xml:"ProviderName"`
	ProviderSite OnlineResource `xml:"ProviderSite"`
	ServiceContact ServiceContact `xml:"ServiceContact"`
}

// OnlineResource represents an xlink reference.
type OnlineResource struct {
	Type string `xml:"type,attr"`
	Href string `xml:"href,attr"`
}

// ServiceContact contains contact information.
type ServiceContact struct {
	IndividualName string `xml:"IndividualName"`
	PositionName   string `xml:"PositionName"`
	ContactInfo    ContactInfo `xml:"ContactInfo"`
	Role           string `xml:"Role"`
}

// ContactInfo contains detailed contact information.
type ContactInfo struct {
	Phone   Phone   `xml:"Phone"`
	Address Address `xml:"Address"`
}

// Phone contains phone numbers.
type Phone struct {
	Voice     string `xml:"Voice"`
	Facsimile string `xml:"Facsimile"`
}

// Address contains address information.
type Address struct {
	DeliveryPoint         string `xml:"DeliveryPoint"`
	City                  string `xml:"City"`
	AdministrativeArea    string `xml:"AdministrativeArea"`
	PostalCode            string `xml:"PostalCode"`
	Country               string `xml:"Country"`
	ElectronicMailAddress string `xml:"ElectronicMailAddress"`
}

// OperationsMetadata contains supported operations.
type OperationsMetadata struct {
	Operations []Operation `xml:"Operation"`
	Parameters []Parameter `xml:"Parameter"`
	Constraints []Constraint `xml:"Constraint"`
}

// Operation represents a WFS operation.
type Operation struct {
	Name       string      `xml:"name,attr"`
	DCP        DCP         `xml:"DCP"`
	Parameters []Parameter `xml:"Parameter"`
	Constraints []Constraint `xml:"Constraint"`
}

// DCP contains distributed computing platform info.
type DCP struct {
	HTTP HTTP `xml:"HTTP"`
}

// HTTP contains HTTP endpoint information.
type HTTP struct {
	Get  RequestMethod `xml:"Get"`
	Post RequestMethod `xml:"Post"`
}

// RequestMethod contains endpoint URL.
type RequestMethod struct {
	Href string `xml:"href,attr"`
}

// Parameter represents an operation parameter.
type Parameter struct {
	Name          string   `xml:"name,attr"`
	AllowedValues []string `xml:"AllowedValues>Value"`
	DefaultValue  string   `xml:"DefaultValue"`
}

// Constraint represents an operation constraint.
type Constraint struct {
	Name          string   `xml:"name,attr"`
	DefaultValue  string   `xml:"DefaultValue"`
	AllowedValues []string `xml:"AllowedValues>Value"`
}

// FeatureTypeList contains the list of feature types.
type FeatureTypeList struct {
	FeatureTypes []FeatureType `xml:"FeatureType"`
}

// FeatureType represents a feature type.
type FeatureType struct {
	Name            string   `xml:"Name"`
	Title           string   `xml:"Title"`
	Abstract        string   `xml:"Abstract"`
	Keywords        []string `xml:"Keywords>Keyword"`
	DefaultCRS      string   `xml:"DefaultCRS"`
	OtherCRS        []string `xml:"OtherCRS"`
	WGS84BoundingBox *WGS84BoundingBox `xml:"WGS84BoundingBox"`
	MetadataURL     []MetadataURL `xml:"MetadataURL"`
}

// WGS84BoundingBox represents the geographic extent.
type WGS84BoundingBox struct {
	LowerCorner string `xml:"LowerCorner"`
	UpperCorner string `xml:"UpperCorner"`
}

// MetadataURL contains a metadata reference.
type MetadataURL struct {
	Type string `xml:"type,attr"`
	Href string `xml:"href,attr"`
}

// FilterCapabilities contains filter capabilities.
type FilterCapabilities struct {
	Conformance        Conformance        `xml:"Conformance"`
	IdCapabilities     IdCapabilities     `xml:"Id_Capabilities"`
	ScalarCapabilities ScalarCapabilities `xml:"Scalar_Capabilities"`
	SpatialCapabilities SpatialCapabilities `xml:"Spatial_Capabilities"`
}

// Conformance contains conformance class declarations.
type Conformance struct {
	Constraints []Constraint `xml:"Constraint"`
}

// IdCapabilities contains ID filtering capabilities.
type IdCapabilities struct {
	ResourceIdentifier struct{} `xml:"ResourceIdentifier"`
}

// ScalarCapabilities contains scalar comparison capabilities.
type ScalarCapabilities struct {
	LogicalOperators    struct{} `xml:"LogicalOperators"`
	ComparisonOperators ComparisonOperators `xml:"ComparisonOperators"`
}

// ComparisonOperators lists supported comparison operators.
type ComparisonOperators struct {
	Operators []ComparisonOperator `xml:"ComparisonOperator"`
}

// ComparisonOperator represents a comparison operator.
type ComparisonOperator struct {
	Name string `xml:"name,attr"`
}

// SpatialCapabilities contains spatial filter capabilities.
type SpatialCapabilities struct {
	GeometryOperands GeometryOperands `xml:"GeometryOperands"`
	SpatialOperators SpatialOperators `xml:"SpatialOperators"`
}

// GeometryOperands lists supported geometry types.
type GeometryOperands struct {
	Operands []GeometryOperand `xml:"GeometryOperand"`
}

// GeometryOperand represents a geometry type.
type GeometryOperand struct {
	Name string `xml:"name,attr"`
}

// SpatialOperators lists supported spatial operators.
type SpatialOperators struct {
	Operators []SpatialOperator `xml:"SpatialOperator"`
}

// SpatialOperator represents a spatial operator.
type SpatialOperator struct {
	Name string `xml:"name,attr"`
}

// Parse parses a WFS capabilities XML document.
func Parse(data []byte) (*Capabilities, error) {
	var caps Capabilities
	if err := xml.Unmarshal(data, &caps); err != nil {
		return nil, fmt.Errorf("parsing capabilities XML: %w", err)
	}
	return &caps, nil
}

// GetFeatureTypes returns all feature types.
func (c *Capabilities) GetFeatureTypes() []FeatureType {
	return c.FeatureTypeList.FeatureTypes
}

// GetFeatureType returns a feature type by name (case-insensitive).
func (c *Capabilities) GetFeatureType(name string) *FeatureType {
	for i := range c.FeatureTypeList.FeatureTypes {
		ft := &c.FeatureTypeList.FeatureTypes[i]
		if strings.EqualFold(ft.Name, name) {
			return ft
		}
	}
	return nil
}

// GetFeatureTypeNames returns all feature type names.
func (c *Capabilities) GetFeatureTypeNames() []string {
	var names []string
	for _, ft := range c.FeatureTypeList.FeatureTypes {
		names = append(names, ft.Name)
	}
	return names
}

// GetOperation returns an operation by name.
func (c *Capabilities) GetOperation(name string) *Operation {
	for i := range c.OperationsMetadata.Operations {
		op := &c.OperationsMetadata.Operations[i]
		if strings.EqualFold(op.Name, name) {
			return op
		}
	}
	return nil
}

// SupportsOperation returns true if the operation is supported.
func (c *Capabilities) SupportsOperation(name string) bool {
	return c.GetOperation(name) != nil
}

// GetOperationURL returns the GET URL for an operation.
func (c *Capabilities) GetOperationURL(name string) string {
	op := c.GetOperation(name)
	if op == nil {
		return ""
	}
	return op.DCP.HTTP.Get.Href
}

// SupportsComparisonOperator returns true if the operator is supported.
func (c *Capabilities) SupportsComparisonOperator(name string) bool {
	if c.FilterCapabilities == nil {
		return false
	}
	for _, op := range c.FilterCapabilities.ScalarCapabilities.ComparisonOperators.Operators {
		if strings.EqualFold(op.Name, name) {
			return true
		}
	}
	return false
}

// SupportsSpatialOperator returns true if the operator is supported.
func (c *Capabilities) SupportsSpatialOperator(name string) bool {
	if c.FilterCapabilities == nil {
		return false
	}
	for _, op := range c.FilterCapabilities.SpatialCapabilities.SpatialOperators.Operators {
		if strings.EqualFold(op.Name, name) {
			return true
		}
	}
	return false
}

// GetCRSList returns the CRS list for the feature type.
func (ft *FeatureType) GetCRSList() []string {
	var crs []string
	if ft.DefaultCRS != "" {
		crs = append(crs, ft.DefaultCRS)
	}
	crs = append(crs, ft.OtherCRS...)
	return crs
}

// GetDefaultCRS returns the default CRS for the feature type.
func (ft *FeatureType) GetDefaultCRS() string {
	return ft.DefaultCRS
}

// SupportsCRS returns true if the feature type supports the given CRS.
func (ft *FeatureType) SupportsCRS(crs string) bool {
	for _, c := range ft.GetCRSList() {
		if strings.EqualFold(c, crs) || strings.Contains(c, crs) {
			return true
		}
	}
	return false
}
