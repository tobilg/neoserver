package wfs

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/workspace"
)

func (h *workspaceHandler) handleGetCapabilities(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	q := NormalizeQuery(r)
	responseVersion := "2.0.0"
	if q.Get("VERSION") == "2.0.2" {
		responseVersion = "2.0.2"
	}

	// Handle version negotiation per WFS 2.0 spec (ISO 19142:7.2.3)
	// AcceptVersions parameter contains a comma-separated list of acceptable versions
	if acceptVersions := q.Get("ACCEPTVERSIONS"); acceptVersions != "" {
		versions := strings.Split(acceptVersions, ",")
		supported := false
		for _, v := range versions {
			v = strings.TrimSpace(v)
			if v == "2.0.0" || v == "2.0.2" {
				supported = true
				responseVersion = v
				break
			}
		}
		if !supported {
			WriteException(w, ExceptionVersionNegotiationFailed,
				"AcceptVersions", "Server supports WFS 2.0.0 and 2.0.2")
			return
		}
	}

	// Try to get from cache first. The advertised feature types are filtered by
	// the caller's per-layer read access, so the cache key must include the role.
	role := workspaceRole(r, ws.ID)
	cacheKey := cache.WFSCapabilitiesKey(ws.ID) + "|role=" + role + "|version=" + responseVersion
	cacheFill := h.cache.BeginFill(cache.CacheTypeCapabilities, cacheKey)
	if h.cache != nil {
		if cached, ok := h.cache.GetCapabilities(cacheKey); ok {
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			w.Write(cached)
			return
		}
	}

	baseURL := h.workspaceBaseURL(r, ws.Name)

	// Use workspace WFS settings for title and abstract
	title := ws.Name
	abstract := ws.Description
	if ws.Settings != nil && ws.Settings.WFS.Title != "" {
		title = ws.Settings.WFS.Title
	}
	if ws.Settings != nil && ws.Settings.WFS.Abstract != "" {
		abstract = ws.Settings.WFS.Abstract
	}

	// Build the XML in a buffer so we can cache it
	var buf strings.Builder
	namespaceDeclarations := applicationNamespaceDeclarations(h.cfg.WFS.AppNamespace, h.cfg.WFS.AppNamespacePrefix)
	fmt.Fprintf(&buf, `<?xml version="1.0" encoding="UTF-8"?>
<wfs:WFS_Capabilities version="__NEOSERVER_WFS_VERSION__" xmlns:wfs="http://www.opengis.net/wfs/2.0" xmlns:ows="http://www.opengis.net/ows/1.1" xmlns:fes="http://www.opengis.net/fes/2.0" xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" %s xsi:schemaLocation="http://www.opengis.net/wfs/2.0 http://schemas.opengis.net/wfs/2.0/wfs.xsd http://www.opengis.net/fes/2.0 http://schemas.opengis.net/filter/2.0/filterAll.xsd http://www.opengis.net/gml/3.2 http://schemas.opengis.net/gml/3.2.1/gml.xsd">
  <ows:ServiceIdentification>
    <ows:Title>%s</ows:Title>
    <ows:Abstract>%s</ows:Abstract>
    <ows:ServiceType>WFS</ows:ServiceType>
    <ows:ServiceTypeVersion>__NEOSERVER_WFS_VERSION__</ows:ServiceTypeVersion>
  </ows:ServiceIdentification>
  <ows:ServiceProvider>
    <ows:ProviderName>neoserver</ows:ProviderName>
    <ows:ServiceContact>
      <ows:ContactInfo>
        <ows:Address>
          <ows:ElectronicMailAddress>info@example.com</ows:ElectronicMailAddress>
        </ows:Address>
      </ows:ContactInfo>
    </ows:ServiceContact>
  </ows:ServiceProvider>
  <ows:OperationsMetadata>
    <ows:Operation name="GetCapabilities">
      <ows:DCP><ows:HTTP><ows:Get xlink:href="%s"/><ows:Post xlink:href="%s"/></ows:HTTP></ows:DCP>
    </ows:Operation>
    <ows:Operation name="DescribeFeatureType">
      <ows:DCP><ows:HTTP><ows:Get xlink:href="%s"/><ows:Post xlink:href="%s"/></ows:HTTP></ows:DCP>
    </ows:Operation>
    <ows:Operation name="GetFeature">
      <ows:DCP><ows:HTTP><ows:Get xlink:href="%s"/><ows:Post xlink:href="%s"/></ows:HTTP></ows:DCP>
      <ows:Parameter name="resolve">
        <ows:AllowedValues><ows:Value>local</ows:Value></ows:AllowedValues>
      </ows:Parameter>
	  <ows:Parameter name="outputFormat">
		<ows:AllowedValues>
		  __NEOSERVER_WFS_OUTPUT_FORMATS__
		</ows:AllowedValues>
	  </ows:Parameter>
    </ows:Operation>
    <ows:Operation name="GetPropertyValue">
      <ows:DCP><ows:HTTP><ows:Get xlink:href="%s"/><ows:Post xlink:href="%s"/></ows:HTTP></ows:DCP>
      <ows:Parameter name="resolve">
        <ows:AllowedValues><ows:Value>local</ows:Value></ows:AllowedValues>
      </ows:Parameter>
    </ows:Operation>
    <ows:Operation name="ListStoredQueries">
      <ows:DCP><ows:HTTP><ows:Get xlink:href="%s"/><ows:Post xlink:href="%s"/></ows:HTTP></ows:DCP>
    </ows:Operation>
    <ows:Operation name="DescribeStoredQueries">
      <ows:DCP><ows:HTTP><ows:Get xlink:href="%s"/><ows:Post xlink:href="%s"/></ows:HTTP></ows:DCP>
    </ows:Operation>
    <ows:Operation name="Transaction">
      <ows:DCP><ows:HTTP><ows:Post xlink:href="%s"/></ows:HTTP></ows:DCP>
    </ows:Operation>
    <ows:Operation name="CreateStoredQuery">
      <ows:DCP><ows:HTTP><ows:Post xlink:href="%s"/></ows:HTTP></ows:DCP>
      <ows:Parameter name="language">
        <ows:AllowedValues>
          <ows:Value>urn:ogc:def:queryLanguage:OGC-WFS::WFSQueryExpression</ows:Value>
          <ows:Value>urn:ogc:def:queryLanguage:OGC-WFS::WFS_QueryExpression</ows:Value>
        </ows:AllowedValues>
      </ows:Parameter>
    </ows:Operation>
    <ows:Operation name="DropStoredQuery">
      <ows:DCP><ows:HTTP><ows:Get xlink:href="%s"/><ows:Post xlink:href="%s"/></ows:HTTP></ows:DCP>
    </ows:Operation>
    <ows:Operation name="LockFeature">
      <ows:DCP><ows:HTTP><ows:Get xlink:href="%s"/><ows:Post xlink:href="%s"/></ows:HTTP></ows:DCP>
    </ows:Operation>
    <ows:Operation name="GetFeatureWithLock">
      <ows:DCP><ows:HTTP><ows:Get xlink:href="%s"/><ows:Post xlink:href="%s"/></ows:HTTP></ows:DCP>
      <ows:Parameter name="resolve">
        <ows:AllowedValues><ows:Value>local</ows:Value></ows:AllowedValues>
      </ows:Parameter>
    </ows:Operation>
    <ows:Constraint name="ImplementsBasicWFS">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsTransactionalWFS">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsLockingWFS">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="KVPEncoding">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="XMLEncoding">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="SOAPEncoding">
      <ows:NoValues/>
      <ows:DefaultValue>FALSE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsInheritance">
      <ows:NoValues/>
      <ows:DefaultValue>FALSE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsRemoteResolve">
      <ows:NoValues/>
      <ows:DefaultValue>FALSE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsResultPaging">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsStandardJoins">
      <ows:NoValues/>
      <ows:DefaultValue>FALSE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsSpatialJoins">
      <ows:NoValues/>
      <ows:DefaultValue>FALSE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsTemporalJoins">
      <ows:NoValues/>
      <ows:DefaultValue>FALSE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ImplementsFeatureVersioning">
      <ows:NoValues/>
      <ows:DefaultValue>FALSE</ows:DefaultValue>
    </ows:Constraint>
    <ows:Constraint name="ManageStoredQueries">
      <ows:NoValues/>
      <ows:DefaultValue>TRUE</ows:DefaultValue>
    </ows:Constraint>
  </ows:OperationsMetadata>
  <wfs:FeatureTypeList>
`, namespaceDeclarations, escapeXML(title), escapeXML(abstract), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL), escapeXML(baseURL))

	// Add feature types from workspace layers (only those the caller may read)
	for _, layer := range ws.VisibleLayers(role) {
		fmt.Fprintf(&buf, `    <wfs:FeatureType>
      <wfs:Name>%s</wfs:Name>
      <wfs:Title>%s</wfs:Title>
      <wfs:Abstract>%s</wfs:Abstract>
      <wfs:DefaultCRS>%s</wfs:DefaultCRS>
      <wfs:OtherCRS>urn:ogc:def:crs:EPSG::3857</wfs:OtherCRS>
    </wfs:FeatureType>
`, escapeXML(publishedFeatureTypeName(layer.PublicID, h.cfg.WFS.AppNamespacePrefix)), escapeXML(layer.Title), escapeXML(layer.Description), escapeXML(h.cfg.WFS.DefaultSRS))
	}

	fmt.Fprintf(&buf, `  </wfs:FeatureTypeList>
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
      <fes:Constraint name="ImplementsResourceId">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
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
      <fes:Constraint name="ImplementsMinTemporalFilter">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsTemporalFilter">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsVersionNav">
        <ows:NoValues/>
        <ows:DefaultValue>FALSE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsSorting">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsExtendedOperators">
        <ows:NoValues/>
        <ows:DefaultValue>FALSE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsMinimumXPath">
        <ows:NoValues/>
        <ows:DefaultValue>TRUE</ows:DefaultValue>
      </fes:Constraint>
      <fes:Constraint name="ImplementsSchemaElementFunc">
        <ows:NoValues/>
        <ows:DefaultValue>FALSE</ows:DefaultValue>
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
        <fes:GeometryOperand name="gml:Envelope"/>
        <fes:GeometryOperand name="gml:Point"/>
        <fes:GeometryOperand name="gml:MultiPoint"/>
        <fes:GeometryOperand name="gml:LineString"/>
        <fes:GeometryOperand name="gml:MultiLineString"/>
        <fes:GeometryOperand name="gml:Polygon"/>
        <fes:GeometryOperand name="gml:MultiPolygon"/>
        <fes:GeometryOperand name="gml:MultiGeometry"/>
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
      </fes:SpatialOperators>
    </fes:Spatial_Capabilities>
    <fes:Temporal_Capabilities>
      <fes:TemporalOperands>
        <fes:TemporalOperand name="gml:TimeInstant"/>
        <fes:TemporalOperand name="gml:TimePeriod"/>
      </fes:TemporalOperands>
      <fes:TemporalOperators>
        <fes:TemporalOperator name="After"/>
        <fes:TemporalOperator name="Before"/>
        <fes:TemporalOperator name="During"/>
        <fes:TemporalOperator name="Begins"/>
        <fes:TemporalOperator name="BegunBy"/>
        <fes:TemporalOperator name="TContains"/>
        <fes:TemporalOperator name="TEquals"/>
        <fes:TemporalOperator name="TOverlaps"/>
        <fes:TemporalOperator name="Meets"/>
        <fes:TemporalOperator name="OverlappedBy"/>
        <fes:TemporalOperator name="MetBy"/>
        <fes:TemporalOperator name="Ends"/>
        <fes:TemporalOperator name="EndedBy"/>
      </fes:TemporalOperators>
    </fes:Temporal_Capabilities>
  </fes:Filter_Capabilities>
</wfs:WFS_Capabilities>`)

	// Cache the response
	formatValues := make([]string, 0, len(availableWFSOutputFormats()))
	for _, outputFormat := range availableWFSOutputFormats() {
		formatValues = append(formatValues, fmt.Sprintf("<ows:Value>%s</ows:Value>", escapeXML(outputFormat)))
	}
	response := []byte(strings.Replace(strings.ReplaceAll(buf.String(), "__NEOSERVER_WFS_VERSION__", responseVersion), "__NEOSERVER_WFS_OUTPUT_FORMATS__", strings.Join(formatValues, "\n\t\t  "), 1))
	if h.cache != nil {
		h.cache.SetCapabilities(cacheKey, response, cacheFill)
	}

	// Write the response
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(http.StatusOK)
	w.Write(response)
}
