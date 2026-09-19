package mockserver

import (
	"fmt"
	"strings"
)

// BuildCapabilities generates a WFS 2.0 capabilities document.
func BuildCapabilities(baseURL string, featureTypes []MockFeatureType) string {
	var sb strings.Builder

	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<wfs:WFS_Capabilities version="2.0.0"
  xmlns:wfs="http://www.opengis.net/wfs/2.0"
  xmlns:ows="http://www.opengis.net/ows/1.1"
  xmlns:fes="http://www.opengis.net/fes/2.0"
  xmlns:gml="http://www.opengis.net/gml/3.2"
  xmlns:xlink="http://www.w3.org/1999/xlink"
  xmlns:mock="http://mock.test/wfs"
  updateSequence="1">

  <ows:ServiceIdentification>
    <ows:Title>Mock WFS 2.0 Server</ows:Title>
    <ows:Abstract>Mock WFS server for testing</ows:Abstract>
    <ows:Keywords>
      <ows:Keyword>WFS</ows:Keyword>
      <ows:Keyword>Mock</ows:Keyword>
      <ows:Keyword>Testing</ows:Keyword>
    </ows:Keywords>
    <ows:ServiceType>WFS</ows:ServiceType>
    <ows:ServiceTypeVersion>2.0.0</ows:ServiceTypeVersion>
    <ows:ServiceTypeVersion>2.0.2</ows:ServiceTypeVersion>
    <ows:Fees>NONE</ows:Fees>
    <ows:AccessConstraints>NONE</ows:AccessConstraints>
  </ows:ServiceIdentification>

  <ows:ServiceProvider>
    <ows:ProviderName>Mock Provider</ows:ProviderName>
    <ows:ProviderSite xlink:type="simple" xlink:href="http://mock.test/"/>
    <ows:ServiceContact>
      <ows:IndividualName>Mock Contact</ows:IndividualName>
      <ows:PositionName>Developer</ows:PositionName>
      <ows:ContactInfo>
        <ows:Phone>
          <ows:Voice>+1-555-555-5555</ows:Voice>
        </ows:Phone>
        <ows:Address>
          <ows:City>Mock City</ows:City>
          <ows:Country>USA</ows:Country>
          <ows:ElectronicMailAddress>mock@test.com</ows:ElectronicMailAddress>
        </ows:Address>
      </ows:ContactInfo>
      <ows:Role>Developer</ows:Role>
    </ows:ServiceContact>
  </ows:ServiceProvider>

  <ows:OperationsMetadata>
`)

	// Add operations
	operations := []struct {
		name   string
		params []struct{ name, values string }
	}{
		{"GetCapabilities", []struct{ name, values string }{
			{"AcceptVersions", "2.0.0,2.0.2"},
			{"AcceptFormats", "text/xml,application/xml"},
		}},
		{"DescribeFeatureType", []struct{ name, values string }{
			{"outputFormat", "application/gml+xml; version=3.2,text/xml; subtype=gml/3.2.1"},
		}},
		{"GetFeature", []struct{ name, values string }{
			{"outputFormat", "application/gml+xml; version=3.2,text/xml; subtype=gml/3.2.1,application/json"},
			{"resultType", "results,hits"},
		}},
		{"GetPropertyValue", nil},
		{"ListStoredQueries", nil},
		{"DescribeStoredQueries", nil},
	}

	for _, op := range operations {
		sb.WriteString(fmt.Sprintf(`    <ows:Operation name="%s">
      <ows:DCP>
        <ows:HTTP>
          <ows:Get xlink:type="simple" xlink:href="%s?"/>
          <ows:Post xlink:type="simple" xlink:href="%s"/>
        </ows:HTTP>
      </ows:DCP>
`, op.name, baseURL, baseURL))
		for _, p := range op.params {
			sb.WriteString(fmt.Sprintf(`      <ows:Parameter name="%s">
        <ows:AllowedValues>
`, p.name))
			for _, v := range strings.Split(p.values, ",") {
				sb.WriteString(fmt.Sprintf(`          <ows:Value>%s</ows:Value>
`, v))
			}
			sb.WriteString(`        </ows:AllowedValues>
      </ows:Parameter>
`)
		}
		sb.WriteString(`    </ows:Operation>
`)
	}

	// Global parameters
	sb.WriteString(`    <ows:Parameter name="version">
      <ows:AllowedValues>
        <ows:Value>2.0.0</ows:Value>
        <ows:Value>2.0.2</ows:Value>
      </ows:AllowedValues>
    </ows:Parameter>
    <ows:Constraint name="ImplementsSimpleWFS">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsBasicWFS">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsTransactionalWFS">
      <ows:NoValues/>
      <ows:DefaultValue>FALSE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsLockingWFS">
      <ows:NoValues/>
      <ows:DefaultValue>FALSE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="KVPEncoding">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="XMLEncoding">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
  </ows:OperationsMetadata>

  <wfs:FeatureTypeList>
`)

	// Add feature types
	for _, ft := range featureTypes {
		sb.WriteString(fmt.Sprintf(`    <wfs:FeatureType>
      <wfs:Name>%s</wfs:Name>
      <wfs:Title>%s</wfs:Title>
      <wfs:Abstract>%s</wfs:Abstract>
      <wfs:DefaultCRS>%s</wfs:DefaultCRS>
`, ft.Name, ft.Title, ft.Abstract, ft.DefaultCRS))

		for _, crs := range ft.CRS {
			if crs != ft.DefaultCRS {
				sb.WriteString(fmt.Sprintf(`      <wfs:OtherCRS>%s</wfs:OtherCRS>
`, crs))
			}
		}

		sb.WriteString(fmt.Sprintf(`      <ows:WGS84BoundingBox>
        <ows:LowerCorner>%.4f %.4f</ows:LowerCorner>
        <ows:UpperCorner>%.4f %.4f</ows:UpperCorner>
      </ows:WGS84BoundingBox>
    </wfs:FeatureType>
`, ft.BBox[0], ft.BBox[1], ft.BBox[2], ft.BBox[3]))
	}

	sb.WriteString(`  </wfs:FeatureTypeList>

  <fes:Filter_Capabilities>
    <fes:Conformance>
      <fes:Constraint name="ImplementsQuery">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsAdHocQuery">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsFunctions">
        <ows:NoValues/>
        <ows:DefaultValue>FALSE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsMinStandardFilter">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsStandardFilter">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsMinSpatialFilter">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsSpatialFilter">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsResourceId">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
    </fes:Conformance>
    <fes:Id_Capabilities>
      <fes:ResourceIdentifier name="fes:ResourceId"/>
    </fes:Id_Capabilities>
    <fes:Scalar_Capabilities>
      <fes:LogicalOperators/>
      <fes:ComparisonOperators>
        <fes:ComparisonOperator name="PropertyIsEqualTo"/>
        <fes:ComparisonOperator name="PropertyIsNotEqualTo"/>
        <fes:ComparisonOperator name="PropertyIsLessThan"/>
        <fes:ComparisonOperator name="PropertyIsGreaterThan"/>
        <fes:ComparisonOperator name="PropertyIsLessThanOrEqualTo"/>
        <fes:ComparisonOperator name="PropertyIsGreaterThanOrEqualTo"/>
        <fes:ComparisonOperator name="PropertyIsLike"/>
        <fes:ComparisonOperator name="PropertyIsNull"/>
        <fes:ComparisonOperator name="PropertyIsNil"/>
        <fes:ComparisonOperator name="PropertyIsBetween"/>
      </fes:ComparisonOperators>
    </fes:Scalar_Capabilities>
    <fes:Spatial_Capabilities>
      <fes:GeometryOperands>
        <fes:GeometryOperand name="gml:Point"/>
        <fes:GeometryOperand name="gml:MultiPoint"/>
        <fes:GeometryOperand name="gml:LineString"/>
        <fes:GeometryOperand name="gml:MultiLineString"/>
        <fes:GeometryOperand name="gml:Polygon"/>
        <fes:GeometryOperand name="gml:MultiPolygon"/>
        <fes:GeometryOperand name="gml:Envelope"/>
      </fes:GeometryOperands>
      <fes:SpatialOperators>
        <fes:SpatialOperator name="BBOX"/>
        <fes:SpatialOperator name="Equals"/>
        <fes:SpatialOperator name="Disjoint"/>
        <fes:SpatialOperator name="Intersects"/>
        <fes:SpatialOperator name="Touches"/>
        <fes:SpatialOperator name="Crosses"/>
        <fes:SpatialOperator name="Within"/>
        <fes:SpatialOperator name="Contains"/>
        <fes:SpatialOperator name="Overlaps"/>
        <fes:SpatialOperator name="DWithin"/>
        <fes:SpatialOperator name="Beyond"/>
      </fes:SpatialOperators>
    </fes:Spatial_Capabilities>
  </fes:Filter_Capabilities>
</wfs:WFS_Capabilities>
`)

	return sb.String()
}

// BuildStoredQueriesList generates a ListStoredQueries response.
func BuildStoredQueriesList() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<wfs:ListStoredQueriesResponse
  xmlns:wfs="http://www.opengis.net/wfs/2.0">
  <wfs:StoredQuery id="http://www.opengis.net/def/query/OGC-WFS/0/GetFeatureById">
    <wfs:Title>GetFeatureById</wfs:Title>
  </wfs:StoredQuery>
</wfs:ListStoredQueriesResponse>
`
}

// BuildStoredQueryDescription generates a DescribeStoredQueries response for GetFeatureById.
func BuildStoredQueryDescription() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<wfs:DescribeStoredQueriesResponse
  xmlns:wfs="http://www.opengis.net/wfs/2.0"
  xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <wfs:StoredQueryDescription id="http://www.opengis.net/def/query/OGC-WFS/0/GetFeatureById">
    <wfs:Title>GetFeatureById</wfs:Title>
    <wfs:Abstract>Returns a feature based on its identifier.</wfs:Abstract>
    <wfs:Parameter name="id" type="xs:string"/>
    <wfs:QueryExpressionText isPrivate="true"
      returnFeatureTypes=""
      language="urn:ogc:def:queryLanguage:OGC-WFS::WFS_QueryExpression"/>
  </wfs:StoredQueryDescription>
</wfs:DescribeStoredQueriesResponse>
`
}
