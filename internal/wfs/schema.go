package wfs

import (
	"encoding/xml"
	"net/http"
	"strings"

	"github.com/tobilg/neoserver/internal/datasource"
)

// XSDSchema represents an XML Schema Definition document.
type XSDSchema struct {
	XMLName            xml.Name         `xml:"xsd:schema"`
	NSXSD              string           `xml:"xmlns:xsd,attr"`
	NSGml              string           `xml:"xmlns:gml,attr"`
	NSTns              string           `xml:"xmlns:tns,attr"` // Target namespace with prefix
	TargetNamespace    string           `xml:"targetNamespace,attr"`
	ElementFormDefault string           `xml:"elementFormDefault,attr"`
	Imports            []XSDImport      `xml:"xsd:import"`
	Elements           []XSDElement     `xml:"xsd:element"`
	ComplexTypes       []XSDComplexType `xml:"xsd:complexType"`
}

// XSDImport represents an XSD import.
type XSDImport struct {
	Namespace      string `xml:"namespace,attr"`
	SchemaLocation string `xml:"schemaLocation,attr"`
}

// XSDElement represents an XSD element.
type XSDElement struct {
	Name              string `xml:"name,attr"`
	Type              string `xml:"type,attr,omitempty"`
	SubstitutionGroup string `xml:"substitutionGroup,attr,omitempty"`
	MinOccurs         string `xml:"minOccurs,attr,omitempty"`
	MaxOccurs         string `xml:"maxOccurs,attr,omitempty"`
	Nillable          string `xml:"nillable,attr,omitempty"`
}

// XSDComplexType represents an XSD complex type.
type XSDComplexType struct {
	Name           string             `xml:"name,attr"`
	ComplexContent *XSDComplexContent `xml:"xsd:complexContent,omitempty"`
}

// XSDComplexContent represents complex content in XSD.
type XSDComplexContent struct {
	Extension XSDExtension `xml:"xsd:extension"`
}

// XSDExtension represents an XSD extension.
type XSDExtension struct {
	Base     string       `xml:"base,attr"`
	Sequence *XSDSequence `xml:"xsd:sequence,omitempty"`
}

// XSDSequence represents an XSD sequence.
type XSDSequence struct {
	Elements []XSDElement `xml:"xsd:element"`
}

// GenerateSchema generates a GML application schema for the given layers.
func GenerateSchema(layers []*layerSchemaInfo, namespace, nsPrefix string) *XSDSchema {
	// Determine the effective namespace for the schema
	effectiveNS := namespace
	for _, layer := range layers {
		qn := ParseQName(layer.TypeName)
		if qn.Prefix != "" && qn.Namespace != "" && qn.Namespace != NSDefault {
			effectiveNS = qn.Namespace
			break // Use the first layer's namespace
		}
	}

	schema := &XSDSchema{
		NSXSD:              NSXsd,
		NSGml:              NSGml,
		NSTns:              effectiveNS,
		TargetNamespace:    effectiveNS,
		ElementFormDefault: "qualified",
		Imports: []XSDImport{
			{
				Namespace:      NSGml,
				SchemaLocation: GmlSchemaLocation,
			},
		},
	}

	for _, layer := range layers {
		qn := ParseQName(layer.TypeName)
		localPart := sanitizeXMLName(qn.LocalPart)

		// Generate element for this feature type
		typeName := localPart + "Type"
		elem := XSDElement{
			Name:              localPart,
			Type:              "tns:" + typeName,
			SubstitutionGroup: "gml:AbstractFeature",
		}
		schema.Elements = append(schema.Elements, elem)

		// Generate complex type
		complexType := generateComplexType(layer.Info, typeName)
		schema.ComplexTypes = append(schema.ComplexTypes, complexType)
	}

	return schema
}

type layerSchemaInfo struct {
	TypeName string
	Info     *datasource.LayerInfo
}

func generateComplexType(info *datasource.LayerInfo, typeName string) XSDComplexType {
	var elements []XSDElement
	propertyNames, _ := propertyXMLNames(info)

	// Add property elements
	for _, prop := range info.Properties {
		// Skip standard GML properties - they are inherited from gml:AbstractFeatureType
		if isStandardGMLProperty(prop.Name) {
			continue
		}

		elem := XSDElement{
			Name:      propertyNames[prop.Name],
			Type:      dbTypeToXSD(prop.Type),
			MinOccurs: "0",
			MaxOccurs: "1",
			Nillable:  "true",
		}
		elements = append(elements, elem)
	}

	// Add geometry element
	if info.GeometryColumn != "" {
		geomElem := XSDElement{
			Name:      propertyNames[info.GeometryColumn],
			Type:      geometryTypeToGML(info.GeometryType),
			MinOccurs: "0",
			MaxOccurs: "1",
		}
		elements = append(elements, geomElem)
	}

	return XSDComplexType{
		Name: typeName,
		ComplexContent: &XSDComplexContent{
			Extension: XSDExtension{
				Base: "gml:AbstractFeatureType",
				Sequence: &XSDSequence{
					Elements: elements,
				},
			},
		},
	}
}

// dbTypeToXSD maps database types to XSD types.
func dbTypeToXSD(dbType string) string {
	dbType = strings.ToLower(dbType)

	switch dbType {
	case "int2", "smallint":
		return "xsd:short"
	case "int4", "integer", "int", "int32":
		return "xsd:int"
	case "int8", "bigint", "int64":
		return "xsd:long"
	case "float4", "real", "float32":
		return "xsd:float"
	case "float8", "double precision", "double", "float64":
		return "xsd:double"
	case "numeric", "decimal":
		return "xsd:decimal"
	case "bool", "boolean":
		return "xsd:boolean"
	case "date":
		return "xsd:date"
	case "time", "timetz":
		return "xsd:time"
	case "timestamp", "timestamptz", "datetime":
		return "xsd:dateTime"
	case "uuid":
		return "xsd:string"
	case "json", "jsonb":
		return "xsd:anyType"
	case "bytea", "blob":
		return "xsd:base64Binary"
	default:
		return "xsd:string"
	}
}

// geometryTypeToGML maps geometry types to GML property types.
func geometryTypeToGML(geomType string) string {
	geomType = strings.ToUpper(geomType)

	switch geomType {
	case "POINT":
		return "gml:PointPropertyType"
	case "LINESTRING":
		return "gml:CurvePropertyType"
	case "POLYGON":
		return "gml:SurfacePropertyType"
	case "MULTIPOINT":
		return "gml:MultiPointPropertyType"
	case "MULTILINESTRING":
		return "gml:MultiCurvePropertyType"
	case "MULTIPOLYGON":
		return "gml:MultiSurfacePropertyType"
	case "GEOMETRYCOLLECTION":
		return "gml:MultiGeometryPropertyType"
	default:
		return "gml:GeometryPropertyType"
	}
}

// WriteSchema writes the XSD schema to the response.
func WriteSchema(w http.ResponseWriter, schema *XSDSchema) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	w.Write([]byte(xml.Header))

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(schema)
}
