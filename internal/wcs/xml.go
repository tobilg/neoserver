package wcs

import (
	"bytes"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

func capabilitiesXML(version, endpoint, title, abstract string, coverages []*workspace.Coverage, coverageInfo map[string]*datasource.CoverageInfo, capabilities capabilityRegistry) []byte {
	ns := "http://www.opengis.net/wcs/2.1"
	if version == "2.0.1" {
		ns = "http://www.opengis.net/wcs/2.0"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?><wcs:Capabilities xmlns:wcs="%s" xmlns:ows="http://www.opengis.net/ows/2.0" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:rsub="http://www.opengis.net/wcs/range-subsetting/1.0" xmlns:scal="http://www.opengis.net/wcs/scaling/1.0" xmlns:wcscrs="http://www.opengis.net/wcs/crs/1.0" xmlns:int="http://www.opengis.net/wcs/interpolation/1.0" version="%s"><ows:ServiceIdentification><ows:Title>%s</ows:Title><ows:Abstract>%s</ows:Abstract><ows:ServiceType>OGC WCS</ows:ServiceType><ows:ServiceTypeVersion>%s</ows:ServiceTypeVersion>`, ns, version, escape(title), escape(abstract), version)
	for _, profile := range capabilities.profiles() {
		fmt.Fprintf(&b, `<ows:Profile>%s</ows:Profile>`, profile)
	}
	b.WriteString(`</ows:ServiceIdentification><ows:OperationsMetadata>`)
	for _, operation := range []string{"GetCapabilities", "DescribeCoverage", "GetCoverage"} {
		fmt.Fprintf(&b, `<ows:Operation name="%s"><ows:DCP><ows:HTTP><ows:Get xlink:href="%s"/>`, operation, escape(endpoint))
		if capabilities.enabled(extXMLPost) {
			fmt.Fprintf(&b, `<ows:Post xlink:href="%s"><ows:Constraint name="PostEncoding"><ows:AllowedValues><ows:Value>XML</ows:Value></ows:AllowedValues></ows:Constraint></ows:Post>`, escape(endpoint))
		}
		b.WriteString(`</ows:HTTP></ows:DCP></ows:Operation>`)
	}
	if capabilities.enabled(extXMLPost) {
		b.WriteString(`<ows:Constraint name="PostEncoding"><ows:AllowedValues><ows:Value>XML</ows:Value></ows:AllowedValues></ows:Constraint>`)
	}
	b.WriteString(`</ows:OperationsMetadata><wcs:ServiceMetadata>`)
	for _, format := range capabilities.formats {
		fmt.Fprintf(&b, `<wcs:formatSupported>%s</wcs:formatSupported>`, escape(format))
	}
	interpolationEnabled := capabilities.enabled(extInterpolation)
	crsEnabled := capabilities.enabled(extCRS)
	if interpolationEnabled || crsEnabled {
		b.WriteString(`<wcs:Extension>`)
	}
	if interpolationEnabled {
		b.WriteString(`<int:InterpolationMetadata>`)
		for _, method := range capabilities.interpolation {
			fmt.Fprintf(&b, `<int:InterpolationSupported>%s</int:InterpolationSupported>`, interpolationURI(method))
		}
		b.WriteString(`</int:InterpolationMetadata>`)
	}
	if crsEnabled {
		b.WriteString(`<wcscrs:CrsMetadata>`)
		for _, value := range unionStrings(capabilities.subsettingCRS, capabilities.outputCRS) {
			fmt.Fprintf(&b, `<wcscrs:crsSupported>%s</wcscrs:crsSupported>`, escape(value))
		}
		b.WriteString(`</wcscrs:CrsMetadata>`)
	}
	if interpolationEnabled || crsEnabled {
		b.WriteString(`</wcs:Extension>`)
	}
	b.WriteString(`</wcs:ServiceMetadata><wcs:Contents>`)
	for _, coverage := range coverages {
		subtype := "GeneralGridCoverage"
		if version == "2.0.1" {
			subtype = wcs20CoverageSubtype(coverage)
		}
		fmt.Fprintf(&b, `<wcs:CoverageSummary><wcs:CoverageId>%s</wcs:CoverageId><wcs:CoverageSubtype>%s</wcs:CoverageSubtype>`, escape(coverage.PublicID), subtype)
		if info := coverageInfo[coverage.PublicID]; info != nil && info.CRS != "" {
			fmt.Fprintf(&b, `<ows:BoundingBox crs="%s" dimensions="2"><ows:LowerCorner>%s %s</ows:LowerCorner><ows:UpperCorner>%s %s</ows:UpperCorner></ows:BoundingBox>`, escape(info.CRS), number(info.Envelope[0]), number(info.Envelope[1]), number(info.Envelope[2]), number(info.Envelope[3]))
		}
		b.WriteString(`</wcs:CoverageSummary>`)
	}
	b.WriteString(`</wcs:Contents></wcs:Capabilities>`)
	return []byte(b.String())
}

func interpolationURI(method string) string {
	return "http://www.opengis.net/def/interpolation/OGC/1/" + canonicalInterpolation(method)
}

func unionStrings(groups ...[]string) []string {
	seen := map[string]bool{}
	var result []string
	for _, group := range groups {
		for _, value := range group {
			if value != "" && !seen[value] {
				seen[value] = true
				result = append(result, value)
			}
		}
	}
	return result
}

func coverageDescriptionXML(version string, coverage *workspace.Coverage, info *datasource.CoverageInfo, descriptor *datasource.CoverageDescriptor) []byte {
	if version == "2.0.1" {
		return coverageDescription20(coverage, info)
	}
	if descriptor == nil {
		descriptor = datasource.DescriptorFromInfo(coverage.SourceCoverage, info)
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><wcs:CoverageDescriptions xmlns:wcs="http://www.opengis.net/wcs/2.1/gml" xmlns:cis="http://www.opengis.net/cis/1.1/gml" xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:swe="http://www.opengis.net/swe/2.0"><wcs:CoverageDescription gml:id="` + escape(coverage.PublicID) + `">`)
	b.WriteString(descriptorEnvelope11(info, descriptor))
	b.WriteString(descriptorDomainSet11(info, descriptor))
	b.WriteString(rangeType11(coverage, info))
	fmt.Fprintf(&b, `<wcs:ServiceParameters><wcs:CoverageSubtype>GeneralGridCoverage</wcs:CoverageSubtype><wcs:nativeFormat>%s</wcs:nativeFormat></wcs:ServiceParameters>`, escape(descriptorNativeFormat(descriptor)))
	b.WriteString(`</wcs:CoverageDescription></wcs:CoverageDescriptions>`)
	return []byte(b.String())
}

func descriptorNativeFormat(descriptor *datasource.CoverageDescriptor) string {
	if descriptor != nil {
		for _, format := range descriptor.Formats {
			if format == "application/netcdf" && len(descriptor.Axes) > 2 {
				return format
			}
		}
	}
	return "image/tiff"
}

func descriptorEnvelope11(info *datasource.CoverageInfo, descriptor *datasource.CoverageDescriptor) string {
	if descriptor == nil || len(descriptor.Axes) <= 2 {
		return envelope11(info, datasource.CoverageWindow{Width: info.Width, Height: info.Height})
	}
	labels := make([]string, 0, len(descriptor.Axes))
	var b strings.Builder
	for _, axis := range descriptor.Axes {
		labels = append(labels, axis.Label)
	}
	fmt.Fprintf(&b, `<cis:envelope srsName="%s" axisLabels="%s" srsDimension="%d">`, escape(descriptorCRS(descriptor)), escape(strings.Join(labels, " ")), len(labels))
	for _, axis := range descriptor.Axes {
		low, high := descriptorAxisBounds(info, axis)
		fmt.Fprintf(&b, `<cis:axisExtent axisLabel="%s" uomLabel="%s" lowerBound="%s" upperBound="%s"/>`, escape(axis.Label), escape(axisUOM(axis)), escape(low), escape(high))
	}
	b.WriteString(`</cis:envelope>`)
	return b.String()
}

func descriptorDomainSet11(info *datasource.CoverageInfo, descriptor *datasource.CoverageDescriptor) string {
	if descriptor == nil || len(descriptor.Axes) <= 2 {
		return domainSet11(info, datasource.CoverageWindow{Width: info.Width, Height: info.Height})
	}
	labels := make([]string, 0, len(descriptor.Axes))
	indexes := make([]string, 0, len(descriptor.Axes))
	for index, axis := range descriptor.Axes {
		labels = append(labels, axis.Label)
		indexes = append(indexes, indexAxisLabel(index))
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<cis:domainSet><cis:generalGrid srsName="%s" axisLabels="%s">`, escape(descriptorCRS(descriptor)), escape(strings.Join(labels, " ")))
	for _, axis := range descriptor.Axes {
		low, high := descriptorAxisBounds(info, axis)
		if axis.Regular {
			fmt.Fprintf(&b, `<cis:regularAxis axisLabel="%s" uomLabel="%s" lowerBound="%s" upperBound="%s" resolution="%s"/>`, escape(axis.Label), escape(axisUOM(axis)), escape(low), escape(high), escape(axisResolution(axis)))
			continue
		}
		fmt.Fprintf(&b, `<cis:irregularAxis axisLabel="%s" uomLabel="%s">`, escape(axis.Label), escape(axisUOM(axis)))
		for _, coordinate := range axis.Coordinates {
			fmt.Fprintf(&b, `<cis:C>%s</cis:C>`, escape(coverageAxisValue(coordinate)))
		}
		b.WriteString(`</cis:irregularAxis>`)
	}
	fmt.Fprintf(&b, `<cis:gridLimits srsName="http://www.opengis.net/def/crs/OGC/0/Index%dD" axisLabels="%s">`, len(descriptor.Axes), strings.Join(indexes, " "))
	for index, axis := range descriptor.Axes {
		fmt.Fprintf(&b, `<cis:indexAxis axisLabel="%s" lowerBound="%d" upperBound="%d"/>`, indexes[index], axis.GridLow, axis.GridHigh)
	}
	b.WriteString(`</cis:gridLimits></cis:generalGrid></cis:domainSet>`)
	return b.String()
}

func descriptorCRS(descriptor *datasource.CoverageDescriptor) string {
	if descriptor == nil || descriptor.NativeCRS == "" {
		return "http://www.opengis.net/def/crs/OGC/0/Index2D"
	}
	components := []string{descriptor.NativeCRS}
	for _, axis := range descriptor.Axes {
		switch {
		case axis.CRS != "" && axis.CRS != descriptor.NativeCRS:
			components = append(components, axis.CRS)
		case axis.Kind == datasource.CoverageAxisTime:
			components = append(components, "http://www.opengis.net/def/crs/OGC/0/AnsiDate")
		}
	}
	components = unionStrings(components)
	if len(components) == 1 {
		return components[0]
	}
	var b strings.Builder
	b.WriteString("http://www.opengis.net/def/crs-compound")
	for index, component := range components {
		if index == 0 {
			b.WriteByte('?')
		} else {
			b.WriteByte('&')
		}
		fmt.Fprintf(&b, "%d=%s", index+1, component)
	}
	return b.String()
}

func descriptorAxisBounds(info *datasource.CoverageInfo, axis datasource.CoverageAxis) (string, string) {
	switch axis.Kind {
	case datasource.CoverageAxisSpatialX:
		return number(info.Envelope[0]), number(info.Envelope[2])
	case datasource.CoverageAxisSpatialY:
		return number(info.Envelope[1]), number(info.Envelope[3])
	}
	if len(axis.Coordinates) > 0 {
		return coverageAxisValue(axis.Coordinates[0]), coverageAxisValue(axis.Coordinates[len(axis.Coordinates)-1])
	}
	low := axis.Origin
	high := addAxisSteps(axis.Origin, axis.Resolution, axis.GridHigh-axis.GridLow, axis.Unit)
	lowText, highText := coverageAxisValue(low), coverageAxisValue(high)
	if low.Number != nil && high.Number != nil && *low.Number > *high.Number {
		return highText, lowText
	}
	if low.Time != nil && high.Time != nil && low.Time.After(*high.Time) {
		return highText, lowText
	}
	return lowText, highText
}

func addAxisSteps(origin, resolution datasource.CoverageAxisValue, steps int64, unit string) datasource.CoverageAxisValue {
	if origin.Number != nil && resolution.Number != nil {
		value := *origin.Number + float64(steps)**resolution.Number
		return datasource.CoverageAxisValue{Number: &value}
	}
	if origin.Time != nil && resolution.Number != nil {
		seconds := cfUnitSeconds(unit)
		value := origin.Time.Add(time.Duration(float64(steps) * *resolution.Number * seconds * float64(time.Second)))
		return datasource.CoverageAxisValue{Time: &value}
	}
	return origin
}

func cfUnitSeconds(unit string) float64 {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(unit, " since ", 2)[0]))
	switch base {
	case "day", "days", "d":
		return 86400
	case "hour", "hours", "h":
		return 3600
	case "minute", "minutes", "min":
		return 60
	default:
		return 1
	}
}

func coverageAxisValue(value datasource.CoverageAxisValue) string {
	if value.Number != nil {
		return number(*value.Number)
	}
	if value.Time != nil {
		return value.Time.UTC().Format(time.RFC3339Nano)
	}
	return value.Text
}

func axisResolution(axis datasource.CoverageAxis) string {
	if axis.Resolution.Number == nil {
		return defaultString(axis.Resolution.Text, "1")
	}
	value := math.Abs(*axis.Resolution.Number)
	if axis.Kind != datasource.CoverageAxisTime {
		return number(value)
	}
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(axis.Unit, " since ", 2)[0])) {
	case "day", "days", "d":
		return "P" + number(value) + "D"
	case "hour", "hours", "h":
		return "PT" + number(value) + "H"
	case "minute", "minutes", "min":
		return "PT" + number(value) + "M"
	default:
		return "PT" + number(value) + "S"
	}
}

func axisUOM(axis datasource.CoverageAxis) string {
	if axis.Kind == datasource.CoverageAxisTime {
		return "ISO8601"
	}
	unit := strings.TrimSpace(axis.Unit)
	if unit == "" {
		return "1"
	}
	var b strings.Builder
	for index, rn := range unit {
		valid := rn == '_' || rn == '-' || rn == '.' || rn >= 'A' && rn <= 'Z' || rn >= 'a' && rn <= 'z' || index > 0 && rn >= '0' && rn <= '9'
		if valid {
			b.WriteRune(rn)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 || b.String()[0] >= '0' && b.String()[0] <= '9' {
		return "u_" + b.String()
	}
	return b.String()
}

func indexAxisLabel(index int) string {
	if index < 26 {
		return string(rune('i' + index))
	}
	return "i" + strconv.Itoa(index)
}

func coverageDescription20(coverage *workspace.Coverage, info *datasource.CoverageInfo) []byte {
	subtype := wcs20CoverageSubtype(coverage)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><wcs:CoverageDescriptions xmlns:wcs="http://www.opengis.net/wcs/2.0" xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:gmlcov="http://www.opengis.net/gmlcov/1.0" xmlns:swe="http://www.opengis.net/swe/2.0"><wcs:CoverageDescription gml:id="` + escape(coverage.PublicID) + `">`)
	fmt.Fprintf(&b, `<gml:boundedBy><gml:Envelope srsName="%s" axisLabels="%s %s" srsDimension="2"><gml:lowerCorner>%s %s</gml:lowerCorner><gml:upperCorner>%s %s</gml:upperCorner></gml:Envelope></gml:boundedBy><wcs:CoverageId>%s</wcs:CoverageId>`, escape(info.CRS), escape(info.AxisLabels[0]), escape(info.AxisLabels[1]), number(info.Envelope[0]), number(info.Envelope[1]), number(info.Envelope[2]), number(info.Envelope[3]), escape(coverage.PublicID))
	b.WriteString(domainSet20(info, datasource.CoverageWindow{Width: info.Width, Height: info.Height}, coverage.PublicID, subtype))
	b.WriteString(rangeType20(coverage, info))
	fmt.Fprintf(&b, `<wcs:ServiceParameters><wcs:CoverageSubtype>%s</wcs:CoverageSubtype><wcs:nativeFormat>image/tiff</wcs:nativeFormat></wcs:ServiceParameters></wcs:CoverageDescription></wcs:CoverageDescriptions>`, subtype)
	return []byte(b.String())
}

func gmlCoverage(version string, coverage *workspace.Coverage, info *datasource.CoverageInfo, window datasource.CoverageWindow, raster *datasource.CoverageRaster, tiffCID string) []byte {
	if version == "2.0.1" {
		return gmlCoverage20(coverage, info, window, raster, tiffCID)
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><cis:GeneralGridCoverage xmlns:cis="http://www.opengis.net/cis/1.1/gml" xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:swe="http://www.opengis.net/swe/2.0" gml:id="` + escape(coverage.PublicID) + `">`)
	b.WriteString(envelope11(info, window))
	b.WriteString(domainSet11(info, window))
	b.WriteString(rangeType11(coverage, info))
	b.WriteString(rangeSet11(raster, tiffCID))
	b.WriteString(`</cis:GeneralGridCoverage>`)
	return []byte(b.String())
}

func gmlCoverage20(coverage *workspace.Coverage, info *datasource.CoverageInfo, window datasource.CoverageWindow, raster *datasource.CoverageRaster, tiffCID string) []byte {
	subtype := wcs20CoverageSubtype(coverage)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><gmlcov:` + subtype + ` xmlns:gmlcov="http://www.opengis.net/gmlcov/1.0" xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:swe="http://www.opengis.net/swe/2.0" xmlns:xlink="http://www.w3.org/1999/xlink" gml:id="` + escape(coverage.PublicID) + `">`)
	b.WriteString(domainSet20(info, window, coverage.PublicID, subtype))
	b.WriteString(rangeSet(raster, tiffCID))
	b.WriteString(rangeType20(coverage, info))
	b.WriteString(`</gmlcov:` + subtype + `>`)
	return []byte(b.String())
}

func wcs20CoverageSubtype(coverage *workspace.Coverage) string {
	if coverage != nil && coverage.WCS20CoverageSubtype == store.WCS20CoverageSubtypeGrid {
		return store.WCS20CoverageSubtypeGrid
	}
	return store.WCS20CoverageSubtypeRectifiedGrid
}

func domainSet20(info *datasource.CoverageInfo, w datasource.CoverageWindow, idPrefix, subtype string) string {
	if subtype == store.WCS20CoverageSubtypeGrid {
		return fmt.Sprintf(`<gml:domainSet><gml:Grid dimension="2" gml:id="%s_grid"><gml:limits><gml:GridEnvelope><gml:low>%d %d</gml:low><gml:high>%d %d</gml:high></gml:GridEnvelope></gml:limits><gml:axisLabels>%s %s</gml:axisLabels></gml:Grid></gml:domainSet>`, escape(idPrefix), w.GridLowX, w.GridLowY, w.GridLowX+w.Width-1, w.GridLowY+w.Height-1, escape(info.AxisLabels[0]), escape(info.AxisLabels[1]))
	}
	ox := info.OriginX + (float64(w.XOff)+.5)*info.ResolutionX
	oy := info.OriginY + (float64(w.YOff)+.5)*info.ResolutionY
	return fmt.Sprintf(`<gml:domainSet><gml:RectifiedGrid dimension="2" gml:id="%s_grid"><gml:limits><gml:GridEnvelope><gml:low>%d %d</gml:low><gml:high>%d %d</gml:high></gml:GridEnvelope></gml:limits><gml:axisLabels>%s %s</gml:axisLabels><gml:origin><gml:Point gml:id="%s_origin" srsName="%s"><gml:pos>%s %s</gml:pos></gml:Point></gml:origin><gml:offsetVector srsName="%s">%s 0</gml:offsetVector><gml:offsetVector srsName="%s">0 %s</gml:offsetVector></gml:RectifiedGrid></gml:domainSet>`, escape(idPrefix), w.GridLowX, w.GridLowY, w.GridLowX+w.Width-1, w.GridLowY+w.Height-1, escape(info.AxisLabels[0]), escape(info.AxisLabels[1]), escape(idPrefix), escape(info.CRS), number(ox), number(oy), escape(info.CRS), number(info.ResolutionX), escape(info.CRS), number(info.ResolutionY))
}

func domainSet11(info *datasource.CoverageInfo, w datasource.CoverageWindow) string {
	type axis struct {
		label, index       string
		off, size          int
		origin, resolution float64
	}
	axes := []axis{{info.AxisLabels[0], "i", w.XOff, w.Width, info.OriginX, info.ResolutionX}, {info.AxisLabels[1], "j", w.YOff, w.Height, info.OriginY, info.ResolutionY}}
	active := axes[:0]
	for _, a := range axes {
		if a.size > 1 {
			active = append(active, a)
		}
	}
	if len(active) == 0 {
		active = append(active, axes[0])
	}
	labels := make([]string, len(active))
	indexes := make([]string, len(active))
	for i, a := range active {
		labels[i] = a.label
		indexes[i] = a.index
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<cis:domainSet><cis:generalGrid srsName="%s" axisLabels="%s">`, escape(info.CRS), escape(strings.Join(labels, " ")))
	for _, a := range active {
		low := a.origin + (float64(a.off)+.5)*a.resolution
		high := a.origin + (float64(a.off+a.size)-.5)*a.resolution
		lo, hi := minmaxFloat(low, high)
		fmt.Fprintf(&b, `<cis:regularAxis axisLabel="%s" uomLabel="1" lowerBound="%s" upperBound="%s" resolution="%s"/>`, escape(a.label), number(lo), number(hi), number(absFloat(a.resolution)))
	}
	fmt.Fprintf(&b, `<cis:gridLimits srsName="http://www.opengis.net/def/crs/OGC/0/Index%dD" axisLabels="%s">`, len(active), strings.Join(indexes, " "))
	for _, a := range active {
		low := w.GridLowX
		if a.index == "j" {
			low = w.GridLowY
		}
		fmt.Fprintf(&b, `<cis:indexAxis axisLabel="%s" lowerBound="%d" upperBound="%d"/>`, a.index, low, low+a.size-1)
	}
	b.WriteString(`</cis:gridLimits></cis:generalGrid></cis:domainSet>`)
	return b.String()
}

func envelope11(info *datasource.CoverageInfo, w datasource.CoverageWindow) string {
	x1 := info.OriginX + float64(w.XOff)*info.ResolutionX
	x2 := info.OriginX + float64(w.XOff+w.Width)*info.ResolutionX
	y1 := info.OriginY + float64(w.YOff)*info.ResolutionY
	y2 := info.OriginY + float64(w.YOff+w.Height)*info.ResolutionY
	minX, maxX := minmaxFloat(x1, x2)
	minY, maxY := minmaxFloat(y1, y2)
	return fmt.Sprintf(`<cis:envelope srsName="%s" axisLabels="%s %s" srsDimension="2"><cis:axisExtent axisLabel="%s" uomLabel="1" lowerBound="%s" upperBound="%s"/><cis:axisExtent axisLabel="%s" uomLabel="1" lowerBound="%s" upperBound="%s"/></cis:envelope>`, escape(info.CRS), escape(info.AxisLabels[0]), escape(info.AxisLabels[1]), escape(info.AxisLabels[0]), number(minX), number(maxX), escape(info.AxisLabels[1]), number(minY), number(maxY))
}

func rangeSet(raster *datasource.CoverageRaster, cid string) string {
	if cid != "" {
		return `<gml:rangeSet><gml:File><gml:rangeParameters/><gml:fileReference>cid:` + escape(cid) + `</gml:fileReference><gml:fileStructure>Record Interleaved</gml:fileStructure></gml:File></gml:rangeSet>`
	}
	var b strings.Builder
	b.WriteString(`<gml:rangeSet><gml:DataBlock><gml:rangeParameters/><gml:tupleList>`)
	if raster != nil {
		for i := 0; i < raster.Width*raster.Height; i++ {
			if i > 0 {
				b.WriteByte(' ')
			}
			for band := range raster.Bands {
				if band > 0 {
					b.WriteByte(',')
				}
				b.WriteString(number(raster.Bands[band][i]))
			}
		}
	}
	b.WriteString(`</gml:tupleList></gml:DataBlock></gml:rangeSet>`)
	return b.String()
}

func rangeSet11(raster *datasource.CoverageRaster, cid string) string {
	return strings.ReplaceAll(strings.ReplaceAll(rangeSet(raster, cid), "<gml:", "<cis:"), "</gml:", "</cis:")
}

func rangeType20(coverage *workspace.Coverage, info *datasource.CoverageInfo) string {
	var b strings.Builder
	b.WriteString(`<gmlcov:rangeType><swe:DataRecord>`)
	for i, band := range info.Bands {
		field := effectiveBand(coverage, band, i)
		fmt.Fprintf(&b, `<swe:field name="%s"><swe:Quantity definition="%s">`, escape(field.Name), escape(field.Definition))
		if len(field.NilValues) > 0 {
			b.WriteString(`<swe:nilValues><swe:NilValues>`)
			for _, nilValue := range field.NilValues {
				fmt.Fprintf(&b, `<swe:nilValue reason="unknown">%s</swe:nilValue>`, escape(nilValue))
			}
			b.WriteString(`</swe:NilValues></swe:nilValues>`)
		}
		fmt.Fprintf(&b, `<swe:uom code="%s"/>`, escape(defaultString(field.UOM, "1")))
		b.WriteString(`</swe:Quantity></swe:field>`)
	}
	b.WriteString(`</swe:DataRecord></gmlcov:rangeType>`)
	return b.String()
}

func rangeType11(coverage *workspace.Coverage, info *datasource.CoverageInfo) string {
	var b strings.Builder
	b.WriteString(`<cis:rangeType><swe:DataRecord>`)
	for i, band := range info.Bands {
		field := effectiveBand(coverage, band, i)
		fmt.Fprintf(&b, `<swe:field name="%s"><swe:Quantity definition="%s">`, escape(field.Name), escape(field.Definition))
		if len(field.NilValues) > 0 {
			b.WriteString(`<swe:nilValues><swe:NilValues>`)
			for _, nilValue := range field.NilValues {
				fmt.Fprintf(&b, `<swe:nilValue reason="unknown">%s</swe:nilValue>`, escape(nilValue))
			}
			b.WriteString(`</swe:NilValues></swe:nilValues>`)
		}
		fmt.Fprintf(&b, `<swe:uom code="%s"/>`, escape(defaultString(field.UOM, "1")))
		b.WriteString(`</swe:Quantity></swe:field>`)
	}
	b.WriteString(`</swe:DataRecord></cis:rangeType>`)
	return b.String()
}

func effectiveBand(c *workspace.Coverage, band datasource.CoverageBand, index int) datasource.CoverageBand {
	if index >= len(c.RangeFields) {
		return band
	}
	o := c.RangeFields[index]
	if o.Band != 0 && o.Band != band.Band {
		return band
	}
	if o.Name != "" {
		band.Name = o.Name
	}
	if o.Description != "" {
		band.Description = o.Description
	}
	if o.Definition != "" {
		band.Definition = o.Definition
	}
	if o.UOM != "" {
		band.UOM = o.UOM
	}
	if o.NilValues != nil {
		band.NilValues = o.NilValues
	}
	return band
}
func escape(s string) string  { return html.EscapeString(s) }
func number(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
func defaultString(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
func minmaxFloat(a, b float64) (float64, float64) {
	if a < b {
		return a, b
	}
	return b, a
}
func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
func joinXML(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
