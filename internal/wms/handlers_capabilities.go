package wms

import (
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

func (h *workspaceHandler) handleGetCapabilities(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	// Try to get from cache first. The advertised layer set is filtered by the
	// caller's per-layer read access, so the cache key must include the role.
	role := workspaceRole(r, ws.ID)
	cacheKey := cache.WMSCapabilitiesKey(ws.ID) + "|role=" + role
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

	// Build the capabilities response
	baseURL := h.workspaceBaseURL(r, ws.Name)

	// Use workspace WMS settings for title and abstract
	title := ws.Name
	abstract := ws.Description
	if ws.Settings != nil && ws.Settings.WMS.Title != "" {
		title = ws.Settings.WMS.Title
	}
	if ws.Settings != nil && ws.Settings.WMS.Abstract != "" {
		abstract = ws.Settings.WMS.Abstract
	}

	// OnlineResource URLs must end with ? to be valid URL prefixes per WMS 1.3.0
	onlineResourceURL := baseURL + "?"

	// Build the XML in a buffer so we can cache it
	var buf strings.Builder
	fmt.Fprintf(&buf, `<?xml version="1.0" encoding="UTF-8"?>
<WMS_Capabilities version="1.3.0" xmlns="http://www.opengis.net/wms" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://www.opengis.net/wms http://schemas.opengis.net/wms/1.3.0/capabilities_1_3_0.xsd">
  <Service>
    <Name>WMS</Name>
    <Title>%s</Title>
    <Abstract>%s</Abstract>
    <OnlineResource xmlns:xlink="http://www.w3.org/1999/xlink" xlink:type="simple" xlink:href="%s"/>
    __NEOSERVER_WMS_SERVICE_LIMITS__
  </Service>
  <Capability>
    <Request>
      <GetCapabilities>
        <Format>text/xml</Format>
        <DCPType><HTTP><Get><OnlineResource xmlns:xlink="http://www.w3.org/1999/xlink" xlink:type="simple" xlink:href="%s"/></Get></HTTP></DCPType>
      </GetCapabilities>
      <GetMap>
        __NEOSERVER_WMS_OUTPUT_FORMATS__
        <DCPType><HTTP><Get><OnlineResource xmlns:xlink="http://www.w3.org/1999/xlink" xlink:type="simple" xlink:href="%s"/></Get></HTTP></DCPType>
      </GetMap>
      <GetFeatureInfo>
        <Format>text/xml</Format>
        <Format>text/html</Format>
        <Format>text/plain</Format>
        <Format>application/json</Format>
        <DCPType><HTTP><Get><OnlineResource xmlns:xlink="http://www.w3.org/1999/xlink" xlink:type="simple" xlink:href="%s"/></Get></HTTP></DCPType>
      </GetFeatureInfo>
	  <DescribeLayer>
		<Format>text/xml</Format>
		<DCPType><HTTP><Get><OnlineResource xmlns:xlink="http://www.w3.org/1999/xlink" xlink:type="simple" xlink:href="%s"/></Get></HTTP></DCPType>
	  </DescribeLayer>
    </Request>
    <Exception>
      <Format>XML</Format>
      <Format>INIMAGE</Format>
      <Format>BLANK</Format>
    </Exception>
    <Layer>
      <Title>%s</Title>
      <Abstract>%s</Abstract>
      <CRS>EPSG:4326</CRS>
      <CRS>CRS:84</CRS>
      <EX_GeographicBoundingBox>
        <westBoundLongitude>-180</westBoundLongitude>
        <eastBoundLongitude>180</eastBoundLongitude>
        <southBoundLatitude>-90</southBoundLatitude>
        <northBoundLatitude>90</northBoundLatitude>
      </EX_GeographicBoundingBox>
      <BoundingBox CRS="CRS:84" minx="-180" miny="-90" maxx="180" maxy="90"/>
      <BoundingBox CRS="EPSG:4326" minx="-90" miny="-180" maxx="90" maxy="180"/>
	`, xmlEscape(title), xmlEscape(abstract), xmlEscape(onlineResourceURL), xmlEscape(onlineResourceURL), xmlEscape(onlineResourceURL), xmlEscape(onlineResourceURL), xmlEscape(onlineResourceURL), xmlEscape(ws.Name), xmlEscape(ws.Description))

	// Add layers (only those the caller may read)
	for _, layer := range ws.VisibleLayers(role) {
		fmt.Fprintf(&buf, `      <Layer queryable="1">
        <Name>%s</Name>
        <Title>%s</Title>
        <Abstract>%s</Abstract>
        <CRS>EPSG:4326</CRS>
        <CRS>CRS:84</CRS>
        <EX_GeographicBoundingBox>
          <westBoundLongitude>-180</westBoundLongitude>
          <eastBoundLongitude>180</eastBoundLongitude>
          <southBoundLatitude>-90</southBoundLatitude>
          <northBoundLatitude>90</northBoundLatitude>
        </EX_GeographicBoundingBox>
        <BoundingBox CRS="CRS:84" minx="-180" miny="-90" maxx="180" maxy="90"/>
        <BoundingBox CRS="EPSG:4326" minx="-90" miny="-180" maxx="90" maxy="180"/>
`, xmlEscape(layer.PublicID), xmlEscape(layer.Title), xmlEscape(layer.Description))

		// Add dimensions (time, elevation, etc.) per WMS 1.3.0 spec
		for _, dim := range layer.Dimensions {
			attrs := fmt.Sprintf(`name="%s" units="%s"`, xmlEscape(dim.Name), xmlEscape(dim.Units))
			if dim.Default != "" {
				attrs += fmt.Sprintf(` default="%s"`, xmlEscape(dim.Default))
			}
			if dim.MultipleValues {
				attrs += ` multipleValues="true"`
			}
			if dim.NearestValue {
				attrs += ` nearestValue="true"`
			}
			if dim.Current {
				attrs += ` current="true"`
			}
			fmt.Fprintf(&buf, "        <Dimension %s>%s</Dimension>\n", attrs, xmlEscape(dim.Extent))
		}
		writeCapabilityStyles(&buf, ws, layer.DefaultStyle, layer.Styles)

		fmt.Fprintf(&buf, `      </Layer>
`)
	}

	// Add portrayed coverages without changing the existing feature-layer
	// entries. Feature identifiers retain precedence for legacy collisions.
	for _, coverage := range ws.VisibleCoverages(role) {
		resource := ws.GetResource(coverage.PublicID)
		if resource == nil || resource.Kind != workspace.ResourceCoverage || resource.Service == nil || resource.Service.CoverageSource == nil {
			continue
		}
		info, err := resource.Service.CoverageSource.GetCoverageInfo(r.Context(), coverage.SourceCoverage)
		if err != nil {
			h.logger.Warn("omit unavailable coverage from WMS capabilities", "coverage", coverage.PublicID, "error", err)
			continue
		}
		geo := geographicCoverageBounds(info)
		native := wms13CoverageBounds(info)
		fmt.Fprintf(&buf, `      <Layer queryable="1">
        <Name>%s</Name>
        <Title>%s</Title>
        <Abstract>%s</Abstract>
        <CRS>EPSG:%d</CRS>
        <CRS>EPSG:4326</CRS>
        <CRS>CRS:84</CRS>
        <CRS>EPSG:3857</CRS>
        <EX_GeographicBoundingBox>
          <westBoundLongitude>%.12g</westBoundLongitude>
          <eastBoundLongitude>%.12g</eastBoundLongitude>
          <southBoundLatitude>%.12g</southBoundLatitude>
          <northBoundLatitude>%.12g</northBoundLatitude>
        </EX_GeographicBoundingBox>
        <BoundingBox CRS="EPSG:%d" minx="%.12g" miny="%.12g" maxx="%.12g" maxy="%.12g"/>
`, xmlEscape(coverage.PublicID), xmlEscape(coverage.Title), xmlEscape(coverage.Description), info.SRID, geo[0], geo[2], geo[1], geo[3], info.SRID, native[0], native[1], native[2], native[3])
		writeCapabilityStyles(&buf, ws, coverage.DefaultStyle, coverage.Styles)
		for _, dim := range coverage.Dimensions {
			attrs := fmt.Sprintf(`name="%s" units="%s"`, xmlEscape(dim.Name), xmlEscape(dim.Units))
			if dim.Default != "" {
				attrs += fmt.Sprintf(` default="%s"`, xmlEscape(dim.Default))
			}
			if dim.MultipleValues {
				attrs += ` multipleValues="true"`
			}
			if dim.NearestValue {
				attrs += ` nearestValue="true"`
			}
			if dim.Current {
				attrs += ` current="true"`
			}
			fmt.Fprintf(&buf, "        <Dimension %s>%s</Dimension>\n", attrs, xmlEscape(dim.Extent))
		}
		buf.WriteString("      </Layer>\n")
	}
	for _, resource := range ws.VisibleResources(role) {
		if resource.Kind != workspace.ResourceGroup || resource.Group == nil {
			continue
		}
		group := resource.Group
		minX, minY, maxX, maxY := -180.0, -90.0, 180.0, 90.0
		if group.NativeExtent != nil && group.NativeExtent.SRID == 4326 {
			minX, minY, maxX, maxY = group.NativeExtent.MinX, group.NativeExtent.MinY, group.NativeExtent.MaxX, group.NativeExtent.MaxY
		}
		fmt.Fprintf(&buf, `      <Layer queryable="1">
        <Name>%s</Name><Title>%s</Title><Abstract>%s</Abstract>
        <CRS>CRS:84</CRS><CRS>EPSG:4326</CRS><CRS>EPSG:3857</CRS>
        <EX_GeographicBoundingBox><westBoundLongitude>%.12g</westBoundLongitude><eastBoundLongitude>%.12g</eastBoundLongitude><southBoundLatitude>%.12g</southBoundLatitude><northBoundLatitude>%.12g</northBoundLatitude></EX_GeographicBoundingBox>
        <BoundingBox CRS="CRS:84" minx="%.12g" miny="%.12g" maxx="%.12g" maxy="%.12g"/>
`, xmlEscape(group.PublicID), xmlEscape(group.Title), xmlEscape(group.Description), minX, maxX, minY, maxY, minX, minY, maxX, maxY)
		writeCapabilityStyles(&buf, ws, group.DefaultStyle, group.Styles)
		buf.WriteString("      </Layer>\n")
	}

	fmt.Fprintf(&buf, `    </Layer>
  </Capability>
</WMS_Capabilities>`)

	// Cache the response
	formatValues := make([]string, 0, len(getMapFormats))
	for _, outputFormat := range getMapFormats {
		formatValues = append(formatValues, fmt.Sprintf("<Format>%s</Format>", xmlEscape(outputFormat)))
	}
	capabilities := strings.Replace(buf.String(), "__NEOSERVER_WMS_OUTPUT_FORMATS__", strings.Join(formatValues, "\n\t\t"), 1)
	maxWidth, maxHeight, _, _, _ := h.wmsLimits(ws)
	serviceLimits := ""
	if maxWidth > 0 {
		serviceLimits += fmt.Sprintf("<MaxWidth>%d</MaxWidth>\n    ", maxWidth)
	}
	if maxHeight > 0 {
		serviceLimits += fmt.Sprintf("<MaxHeight>%d</MaxHeight>", maxHeight)
	}
	response := []byte(strings.Replace(capabilities, "__NEOSERVER_WMS_SERVICE_LIMITS__", serviceLimits, 1))
	if h.cache != nil {
		h.cache.SetCapabilities(cacheKey, response, cacheFill)
	}

	// Write the response
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(http.StatusOK)
	w.Write(response)
}

// wms13CoverageBounds returns native coverage bounds in the axis order defined
// by the advertised CRS. EPSG:4326 is latitude/longitude in WMS 1.3.0, while
// CoverageInfo envelopes use the internal longitude/latitude order.
func wms13CoverageBounds(info *datasource.CoverageInfo) [4]float64 {
	if info == nil {
		return [4]float64{-90, -180, 90, 180}
	}
	if info.SRID == 4326 {
		return [4]float64{info.Envelope[1], info.Envelope[0], info.Envelope[3], info.Envelope[2]}
	}
	return info.Envelope
}

func writeCapabilityStyles(buf *strings.Builder, ws *workspace.Workspace, defaultName string, alternates []string) {
	if defaultName != "" {
		if style := ws.GetStyle(defaultName); style != nil {
			fmt.Fprintf(buf, "        <Style><Name>%s</Name><Title>%s</Title></Style>\n", xmlEscape(style.Name), xmlEscape(style.Title))
		}
	}
	for _, name := range alternates {
		if style := ws.GetStyle(name); style != nil {
			fmt.Fprintf(buf, "        <Style><Name>%s</Name><Title>%s</Title></Style>\n", xmlEscape(style.Name), xmlEscape(style.Title))
		}
	}
}

func geographicCoverageBounds(info *datasource.CoverageInfo) [4]float64 {
	if info == nil {
		return [4]float64{-180, -90, 180, 90}
	}
	if info.SRID == 4326 {
		return info.Envelope
	}
	src, err := godal.NewSpatialRefFromEPSG(info.SRID)
	if err != nil {
		return [4]float64{-180, -90, 180, 90}
	}
	defer src.Close()
	dst, err := godal.NewSpatialRefFromEPSG(4326)
	if err != nil {
		return [4]float64{-180, -90, 180, 90}
	}
	defer dst.Close()
	tr, err := godal.NewTransform(src, dst)
	if err != nil {
		return [4]float64{-180, -90, 180, 90}
	}
	defer tr.Close()
	x := []float64{info.Envelope[0], info.Envelope[2], info.Envelope[2], info.Envelope[0]}
	y := []float64{info.Envelope[1], info.Envelope[1], info.Envelope[3], info.Envelope[3]}
	ok := make([]bool, 4)
	if tr.TransformEx(x, y, nil, ok) != nil {
		return [4]float64{-180, -90, 180, 90}
	}
	result := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for i := range x {
		if !ok[i] {
			return [4]float64{-180, -90, 180, 90}
		}
		result[0] = math.Min(result[0], x[i])
		result[1] = math.Min(result[1], y[i])
		result[2] = math.Max(result[2], x[i])
		result[3] = math.Max(result[3], y[i])
	}
	return result
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
