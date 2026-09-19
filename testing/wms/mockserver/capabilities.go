package mockserver

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// CapabilitiesConfig holds configuration for generating capabilities.
type CapabilitiesConfig struct {
	BaseURL        string
	Title          string
	Abstract       string
	OnlineResource string
	Layers         []MockLayer
}

// DefaultCapabilitiesConfig returns default configuration.
func DefaultCapabilitiesConfig(baseURL string) CapabilitiesConfig {
	return CapabilitiesConfig{
		BaseURL:        baseURL,
		Title:          "Mock WMS 1.3.0 Server",
		Abstract:       "A mock WMS server for conformance testing",
		OnlineResource: baseURL + "?",
		Layers:         DefaultMockLayers(),
	}
}

// GenerateCapabilities generates WMS 1.3.0 capabilities XML.
func GenerateCapabilities(cfg CapabilitiesConfig) ([]byte, error) {
	caps := buildCapabilities(cfg)
	return xml.MarshalIndent(caps, "", "  ")
}

// XML structure types for WMS capabilities

type wmsCapabilities struct {
	XMLName        xml.Name   `xml:"WMS_Capabilities"`
	Version        string     `xml:"version,attr"`
	XMLNS          string     `xml:"xmlns,attr"`
	XMLNSXLINK     string     `xml:"xmlns:xlink,attr"`
	XMLNSXSI       string     `xml:"xmlns:xsi,attr"`
	SchemaLocation string     `xml:"xsi:schemaLocation,attr"`
	Service        wmsService `xml:"Service"`
	Capability     wmsCapability
}

type wmsService struct {
	Name           string         `xml:"Name"`
	Title          string         `xml:"Title"`
	Abstract       string         `xml:"Abstract,omitempty"`
	KeywordList    *wmsKeywords   `xml:"KeywordList,omitempty"`
	OnlineResource wmsOnlineRes   `xml:"OnlineResource"`
	ContactInfo    *wmsContact    `xml:"ContactInformation,omitempty"`
	Fees           string         `xml:"Fees,omitempty"`
	AccessConstr   string         `xml:"AccessConstraints,omitempty"`
	LayerLimit     int            `xml:"LayerLimit,omitempty"`
	MaxWidth       int            `xml:"MaxWidth,omitempty"`
	MaxHeight      int            `xml:"MaxHeight,omitempty"`
}

type wmsKeywords struct {
	Keywords []string `xml:"Keyword"`
}

type wmsOnlineRes struct {
	Type string `xml:"xlink:type,attr"`
	Href string `xml:"xlink:href,attr"`
}

type wmsContact struct {
	ContactPerson string `xml:"ContactPersonPrimary>ContactPerson,omitempty"`
	ContactOrg    string `xml:"ContactPersonPrimary>ContactOrganization,omitempty"`
	ContactEmail  string `xml:"ContactElectronicMailAddress,omitempty"`
}

type wmsCapability struct {
	XMLName   xml.Name   `xml:"Capability"`
	Request   wmsRequest `xml:"Request"`
	Exception wmsException
	Layer     wmsLayer
}

type wmsRequest struct {
	GetCapabilities wmsOperation `xml:"GetCapabilities"`
	GetMap          wmsOperation `xml:"GetMap"`
	GetFeatureInfo  wmsOperation `xml:"GetFeatureInfo"`
}

type wmsOperation struct {
	Format []string       `xml:"Format"`
	DCPType wmsDCPType    `xml:"DCPType"`
}

type wmsDCPType struct {
	HTTP wmsHTTP `xml:"HTTP"`
}

type wmsHTTP struct {
	Get  wmsGet `xml:"Get"`
	Post *wmsPost `xml:"Post,omitempty"`
}

type wmsGet struct {
	OnlineResource wmsOnlineRes `xml:"OnlineResource"`
}

type wmsPost struct {
	OnlineResource wmsOnlineRes `xml:"OnlineResource"`
}

type wmsException struct {
	XMLName xml.Name `xml:"Exception"`
	Format  []string `xml:"Format"`
}

type wmsLayer struct {
	Queryable   int    `xml:"queryable,attr,omitempty"`
	Opaque      int    `xml:"opaque,attr,omitempty"`
	Cascaded    int    `xml:"cascaded,attr,omitempty"`
	NoSubsets   int    `xml:"noSubsets,attr,omitempty"`
	FixedWidth  int    `xml:"fixedWidth,attr,omitempty"`
	FixedHeight int    `xml:"fixedHeight,attr,omitempty"`
	Name        string `xml:"Name,omitempty"`
	Title       string `xml:"Title"`
	Abstract    string `xml:"Abstract,omitempty"`
	CRS         []string `xml:"CRS,omitempty"`
	EXGeoBBox   *wmsEXGeoBBox `xml:"EX_GeographicBoundingBox,omitempty"`
	BoundingBox []wmsBoundingBox `xml:"BoundingBox,omitempty"`
	Dimension   []wmsDimension `xml:"Dimension,omitempty"`
	Attribution *wmsAttribution `xml:"Attribution,omitempty"`
	MinScale    float64 `xml:"MinScaleDenominator,omitempty"`
	MaxScale    float64 `xml:"MaxScaleDenominator,omitempty"`
	Style       []wmsStyle `xml:"Style,omitempty"`
	Layer       []wmsLayer `xml:"Layer,omitempty"`
}

type wmsEXGeoBBox struct {
	WestBound float64 `xml:"westBoundLongitude"`
	EastBound float64 `xml:"eastBoundLongitude"`
	SouthBound float64 `xml:"southBoundLatitude"`
	NorthBound float64 `xml:"northBoundLatitude"`
}

type wmsBoundingBox struct {
	CRS  string  `xml:"CRS,attr"`
	MinX float64 `xml:"minx,attr"`
	MinY float64 `xml:"miny,attr"`
	MaxX float64 `xml:"maxx,attr"`
	MaxY float64 `xml:"maxy,attr"`
}

type wmsDimension struct {
	Name    string `xml:"name,attr"`
	Units   string `xml:"units,attr"`
	Default string `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}

type wmsAttribution struct {
	Title          string       `xml:"Title,omitempty"`
	OnlineResource *wmsOnlineRes `xml:"OnlineResource,omitempty"`
}

type wmsStyle struct {
	Name     string `xml:"Name"`
	Title    string `xml:"Title"`
	Abstract string `xml:"Abstract,omitempty"`
	LegendURL *wmsLegendURL `xml:"LegendURL,omitempty"`
}

type wmsLegendURL struct {
	Width          int          `xml:"width,attr"`
	Height         int          `xml:"height,attr"`
	Format         string       `xml:"Format"`
	OnlineResource wmsOnlineRes `xml:"OnlineResource"`
}

func buildCapabilities(cfg CapabilitiesConfig) *wmsCapabilities {
	caps := &wmsCapabilities{
		Version:        "1.3.0",
		XMLNS:          "http://www.opengis.net/wms",
		XMLNSXLINK:     "http://www.w3.org/1999/xlink",
		XMLNSXSI:       "http://www.w3.org/2001/XMLSchema-instance",
		SchemaLocation: "http://www.opengis.net/wms http://schemas.opengis.net/wms/1.3.0/capabilities_1_3_0.xsd",
		Service: wmsService{
			Name:     "WMS",
			Title:    cfg.Title,
			Abstract: cfg.Abstract,
			OnlineResource: wmsOnlineRes{
				Type: "simple",
				Href: cfg.OnlineResource,
			},
			KeywordList: &wmsKeywords{
				Keywords: []string{"WMS", "Mock", "Test"},
			},
			Fees:         "NONE",
			AccessConstr: "NONE",
			LayerLimit:   16,
			MaxWidth:     4096,
			MaxHeight:    4096,
		},
		Capability: wmsCapability{
			Request: wmsRequest{
				GetCapabilities: wmsOperation{
					Format: []string{"text/xml"},
					DCPType: wmsDCPType{
						HTTP: wmsHTTP{
							Get: wmsGet{
								OnlineResource: wmsOnlineRes{Type: "simple", Href: cfg.OnlineResource},
							},
						},
					},
				},
				GetMap: wmsOperation{
					Format: []string{
						"image/png",
						"image/jpeg",
						"image/gif",
						"image/png; mode=8bit",
					},
					DCPType: wmsDCPType{
						HTTP: wmsHTTP{
							Get: wmsGet{
								OnlineResource: wmsOnlineRes{Type: "simple", Href: cfg.OnlineResource},
							},
						},
					},
				},
				GetFeatureInfo: wmsOperation{
					Format: []string{
						"text/xml",
						"application/vnd.ogc.gml",
						"text/plain",
						"text/html",
						"application/json",
					},
					DCPType: wmsDCPType{
						HTTP: wmsHTTP{
							Get: wmsGet{
								OnlineResource: wmsOnlineRes{Type: "simple", Href: cfg.OnlineResource},
							},
						},
					},
				},
			},
			Exception: wmsException{
				Format: []string{"XML", "INIMAGE", "BLANK"},
			},
			Layer: buildRootLayer(cfg),
		},
	}

	return caps
}

func buildRootLayer(cfg CapabilitiesConfig) wmsLayer {
	rootLayer := wmsLayer{
		Title:    cfg.Title,
		Abstract: cfg.Abstract,
		CRS: []string{
			"CRS:84",
			"EPSG:4326",
			"EPSG:3857",
		},
		EXGeoBBox: &wmsEXGeoBBox{
			WestBound:  -180,
			EastBound:  180,
			SouthBound: -90,
			NorthBound: 90,
		},
	}

	// Add child layers
	for _, mockLayer := range cfg.Layers {
		childLayer := buildLayer(mockLayer, cfg.OnlineResource)
		rootLayer.Layer = append(rootLayer.Layer, childLayer)
	}

	return rootLayer
}

func buildLayer(mock MockLayer, baseURL string) wmsLayer {
	layer := wmsLayer{
		Name:     mock.Name,
		Title:    mock.Title,
		Abstract: mock.Abstract,
		CRS:      mock.CRS,
	}

	if mock.Queryable {
		layer.Queryable = 1
	}
	if mock.Opaque {
		layer.Opaque = 1
	}
	if mock.Cascaded > 0 {
		layer.Cascaded = mock.Cascaded
	}
	if mock.NoSubsets {
		layer.NoSubsets = 1
	}
	if mock.FixedWidth > 0 {
		layer.FixedWidth = mock.FixedWidth
	}
	if mock.FixedHeight > 0 {
		layer.FixedHeight = mock.FixedHeight
	}
	if mock.MinScale > 0 {
		layer.MinScale = mock.MinScale
	}
	if mock.MaxScale > 0 {
		layer.MaxScale = mock.MaxScale
	}

	// Add geographic bounding box
	if bbox, ok := mock.BBox["CRS:84"]; ok {
		layer.EXGeoBBox = &wmsEXGeoBBox{
			WestBound:  bbox.MinX,
			EastBound:  bbox.MaxX,
			SouthBound: bbox.MinY,
			NorthBound: bbox.MaxY,
		}
	}

	// Add CRS-specific bounding boxes
	for crs, bbox := range mock.BBox {
		layer.BoundingBox = append(layer.BoundingBox, wmsBoundingBox{
			CRS:  crs,
			MinX: bbox.MinX,
			MinY: bbox.MinY,
			MaxX: bbox.MaxX,
			MaxY: bbox.MaxY,
		})
	}

	// Add dimensions
	for _, dim := range mock.Dimensions {
		layer.Dimension = append(layer.Dimension, wmsDimension{
			Name:    dim.Name,
			Units:   dim.Units,
			Default: dim.Default,
			Value:   dim.Values,
		})
	}

	// Add styles
	for _, style := range mock.Styles {
		wmsS := wmsStyle{
			Name:     style.Name,
			Title:    style.Title,
			Abstract: style.Abstract,
		}
		// Add legend URL
		legendURL := fmt.Sprintf("%sSERVICE=WMS&VERSION=1.3.0&REQUEST=GetLegendGraphic&LAYER=%s&STYLE=%s&FORMAT=image/png",
			baseURL, mock.Name, style.Name)
		wmsS.LegendURL = &wmsLegendURL{
			Width:  20,
			Height: 20,
			Format: "image/png",
			OnlineResource: wmsOnlineRes{
				Type: "simple",
				Href: legendURL,
			},
		}
		layer.Style = append(layer.Style, wmsS)
	}

	// Add attribution
	if mock.Attribution != "" {
		layer.Attribution = &wmsAttribution{
			Title: mock.Attribution,
		}
	}

	return layer
}

// GenerateExceptionXML generates a WMS ServiceExceptionReport.
func GenerateExceptionXML(code, message string) []byte {
	// Escape XML special characters in message
	message = escapeXML(message)

	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<ServiceExceptionReport version="1.3.0"
  xmlns="http://www.opengis.net/ogc"
  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
  xsi:schemaLocation="http://www.opengis.net/ogc http://schemas.opengis.net/wms/1.3.0/exceptions_1_3_0.xsd">
  <ServiceException code="%s">%s</ServiceException>
</ServiceExceptionReport>`, code, message)
	return []byte(xml)
}

// GenerateFeatureInfoXML generates a mock GetFeatureInfo response.
func GenerateFeatureInfoXML(layerName string, features []map[string]interface{}) []byte {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString("\n<FeatureInfoResponse>\n")

	for i, feature := range features {
		sb.WriteString(fmt.Sprintf("  <Feature id=\"%d\">\n", i+1))
		sb.WriteString(fmt.Sprintf("    <Layer>%s</Layer>\n", escapeXML(layerName)))
		for key, value := range feature {
			sb.WriteString(fmt.Sprintf("    <%s>%v</%s>\n", key, value, key))
		}
		sb.WriteString("  </Feature>\n")
	}

	sb.WriteString("</FeatureInfoResponse>")
	return []byte(sb.String())
}

// GenerateFeatureInfoGML generates a mock GetFeatureInfo GML response.
func GenerateFeatureInfoGML(layerName string, features []map[string]interface{}) []byte {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString("\n<wfs:FeatureCollection ")
	sb.WriteString(`xmlns:wfs="http://www.opengis.net/wfs" `)
	sb.WriteString(`xmlns:gml="http://www.opengis.net/gml" `)
	sb.WriteString(`xmlns:mock="http://mock.example.com">`)
	sb.WriteString("\n")

	for i, feature := range features {
		sb.WriteString("  <gml:featureMember>\n")
		sb.WriteString(fmt.Sprintf("    <mock:%s gml:id=\"%s.%d\">\n", layerName, layerName, i+1))
		for key, value := range feature {
			sb.WriteString(fmt.Sprintf("      <mock:%s>%v</mock:%s>\n", key, value, key))
		}
		sb.WriteString(fmt.Sprintf("    </mock:%s>\n", layerName))
		sb.WriteString("  </gml:featureMember>\n")
	}

	sb.WriteString("</wfs:FeatureCollection>")
	return []byte(sb.String())
}

// GenerateFeatureInfoJSON generates a mock GetFeatureInfo JSON response.
func GenerateFeatureInfoJSON(layerName string, features []map[string]interface{}) []byte {
	var sb strings.Builder
	sb.WriteString(`{"type":"FeatureCollection","features":[`)

	for i, feature := range features {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf(`{"type":"Feature","id":%d,"properties":{`, i+1))
		first := true
		for key, value := range feature {
			if !first {
				sb.WriteString(",")
			}
			first = false
			sb.WriteString(fmt.Sprintf(`"%s":`, key))
			switch v := value.(type) {
			case string:
				sb.WriteString(fmt.Sprintf(`"%s"`, v))
			default:
				sb.WriteString(fmt.Sprintf(`%v`, v))
			}
		}
		sb.WriteString(`}}`)
	}

	sb.WriteString("]}")
	return []byte(sb.String())
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
