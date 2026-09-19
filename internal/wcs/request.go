package wcs

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/crs"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/rastergrid"
	"github.com/tobilg/neoserver/internal/workspace"
)

type rawSubset struct {
	axis      string
	low, high string
	slice     bool
}

type coveragePlan struct {
	query        datasource.CoverageQuery
	source       datasource.CoverageWindow
	output       datasource.CoverageWindow
	outputInfo   *datasource.CoverageInfo
	bands        []int
	time         string
	elevation    string
	resampling   string
	nativeCRS    string
	requiresGrid bool
}

func buildCoveragePlan(q url.Values, version string, info *datasource.CoverageInfo, coverage *workspace.Coverage, capabilities capabilityRegistry) (*coveragePlan, *requestError) {
	if info == nil || coverage == nil {
		return nil, &requestError{Code: "OperationProcessingFailed", Text: "coverage metadata is unavailable"}
	}
	plan := &coveragePlan{query: datasource.CoverageQuery{Version: version, CoverageID: coverage.PublicID}, outputInfo: cloneCoverageInfo(info), nativeCRS: info.CRS}
	plan.source = datasource.CoverageWindow{Width: info.Width, Height: info.Height}
	plan.output = datasource.CoverageWindow{Width: info.Width, Height: info.Height}

	spatialSubsets, dimensionSubsets, err := classifySubsets(q["SUBSET"], info, coverage, capabilities)
	if err != nil {
		return nil, err
	}
	plan.time, plan.elevation = dimensionSelection(dimensionSubsets, coverage)
	if capabilities.enabled(extMultidim) {
		for _, dimension := range coverage.Dimensions {
			if dimension == nil || dimension.Default == "" || hasRawSubset(dimensionSubsets, dimension.Name) || dimension.SourceAxis != "" && hasRawSubset(dimensionSubsets, dimension.SourceAxis) {
				continue
			}
			axis := dimension.SourceAxis
			if axis == "" {
				axis = dimension.Name
			}
			dimensionSubsets = append(dimensionSubsets, rawSubset{axis: axis, low: dimension.Default, high: dimension.Default, slice: true})
		}
	}
	if plan.time != "" || plan.elevation != "" {
		plan.requiresGrid = true
	}

	subsettingCRS := strings.TrimSpace(q.Get("SUBSETTINGCRS"))
	if subsettingCRS != "" {
		if !capabilities.enabled(extCRS) {
			return nil, extensionDisabled("subsettingCrs", extCRS)
		}
		if !allowedCRS(subsettingCRS, info.CRS, capabilities.subsettingCRS) {
			return nil, invalid("subsettingCrs", "unsupported subsetting CRS")
		}
		plan.query.SubsettingCRS = subsettingCRS
		plan.requiresGrid = !sameCRS(subsettingCRS, info.CRS)
	}
	if subsettingCRS == "" {
		subsettingCRS = info.CRS
	}

	if len(spatialSubsets) > 0 && sameCRS(subsettingCRS, info.CRS) {
		values := make([]string, len(spatialSubsets))
		for i, subset := range spatialSubsets {
			values[i] = subsetText(subset)
		}
		window, subsetErr := parseSubsets(values, info)
		if subsetErr != nil {
			return nil, subsetErr
		}
		plan.source = window
	} else if len(spatialSubsets) > 0 {
		full, transformErr := rastergrid.TransformBBox(info.Envelope, info.CRS, subsettingCRS)
		if transformErr != nil {
			return nil, invalid("subsettingCrs", transformErr.Error())
		}
		selected := full
		for _, subset := range spatialSubsets {
			axis := spatialAxis(subset.axis, info)
			low, parseErr := strconv.ParseFloat(unquote(subset.low), 64)
			if parseErr != nil {
				return nil, invalid("subset", "invalid spatial subset coordinate")
			}
			high := low
			if !subset.slice {
				high, parseErr = strconv.ParseFloat(unquote(subset.high), 64)
				if parseErr != nil || low > high {
					return nil, invalidSubsetting("invalid spatial trim")
				}
			}
			if axis == 0 {
				selected[0], selected[2] = low, high
			} else {
				selected[1], selected[3] = low, high
			}
		}
		if selected[0] >= selected[2] || selected[1] >= selected[3] {
			return nil, invalidSubsetting("slice in a non-native CRS does not define a two-dimensional grid")
		}
		window, ok, nativeErr := rastergrid.NativeWindow(info, datasource.CoverageRenderRequest{TargetCRS: subsettingCRS, BBox: selected, Width: info.Width, Height: info.Height})
		if nativeErr != nil {
			return nil, invalid("subset", nativeErr.Error())
		}
		if !ok {
			return nil, invalidSubsetting("subset is outside the coverage domain")
		}
		plan.source = window
		plan.requiresGrid = true
	}

	plan.bands, plan.query.RangeSubset, err = parseRangeSubset(q, info, coverage, capabilities)
	if err != nil {
		return nil, err
	}
	if len(plan.bands) != len(info.Bands) {
		plan.requiresGrid = true
	}

	nativeBBox := windowBBox(info, plan.source)
	outputCRS := strings.TrimSpace(q.Get("OUTPUTCRS"))
	if outputCRS != "" {
		if !capabilities.enabled(extCRS) {
			return nil, extensionDisabled("outputCrs", extCRS)
		}
		if !allowedCRS(outputCRS, info.CRS, capabilities.outputCRS) {
			return nil, invalid("outputCrs", "unsupported output CRS")
		}
		plan.query.OutputCRS = outputCRS
		if !sameCRS(outputCRS, info.CRS) {
			plan.requiresGrid = true
		}
	} else {
		outputCRS = info.CRS
	}
	outputBBox, transformErr := rastergrid.TransformBBox(nativeBBox, info.CRS, outputCRS)
	if transformErr != nil {
		return nil, invalid("outputCrs", transformErr.Error())
	}

	plan.output.Width, plan.output.Height = plan.source.Width, plan.source.Height
	plan.query.Scaling, err = parseScaling(q, info, &plan.output, capabilities)
	if err != nil {
		return nil, err
	}
	if plan.query.Scaling.Mode != datasource.CoverageScaleNone {
		plan.requiresGrid = true
	}

	requestedInterpolation := strings.TrimSpace(q.Get("INTERPOLATION"))
	if requestedInterpolation != "" {
		if !capabilities.enabled(extInterpolation) {
			return nil, extensionDisabled("interpolation", extInterpolation)
		}
		requestedInterpolation = canonicalInterpolation(requestedInterpolation)
		if !capabilities.supportsInterpolation(requestedInterpolation) {
			return nil, invalid("interpolation", "unsupported interpolation method")
		}
		plan.query.Interpolation = requestedInterpolation
		plan.requiresGrid = true
	}
	plan.resampling = coverage.Resampling
	if requestedInterpolation == "nearest-neighbor" {
		plan.resampling = "nearest"
	} else if requestedInterpolation == "linear" {
		plan.resampling = "bilinear"
	}
	if plan.resampling == "" {
		plan.resampling = "bilinear"
	}

	format := strings.ToLower(strings.TrimSpace(q.Get("FORMAT")))
	if format == "" {
		format = "image/tiff"
	}
	media := strings.ToLower(strings.TrimSpace(q.Get("MEDIATYPE")))
	if media != "" && media != "multipart/related" {
		return nil, invalid("mediaType", "MEDIATYPE must be multipart/related")
	}
	plan.query.Multipart = format == "multipart/related" || media == "multipart/related"
	if plan.query.Multipart {
		format = "multipart/related"
	}
	if !capabilities.supportsFormat(format) {
		return nil, invalid("format", "unsupported coverage format")
	}
	plan.query.Format = format

	plan.outputInfo = outputCoverageInfo(info, outputCRS, outputBBox, plan.output, plan.bands, coverage)
	plan.query.DomainSubsets = toDomainSubsets(append(spatialSubsets, dimensionSubsets...))
	plan.query.TargetGrid = &datasource.CoverageTargetGrid{
		CRS: plan.outputInfo.CRS, BBox: plan.outputInfo.Envelope, Width: plan.output.Width, Height: plan.output.Height,
		GridLowX: plan.output.GridLowX, GridLowY: plan.output.GridLowY, Bands: append([]int(nil), plan.bands...), Resampling: plan.resampling,
	}
	return plan, nil
}

func classifySubsets(values []string, info *datasource.CoverageInfo, coverage *workspace.Coverage, capabilities capabilityRegistry) ([]rawSubset, []rawSubset, *requestError) {
	var spatial, dimensions []rawSubset
	seen := map[string]bool{}
	for _, value := range values {
		subset, err := parseRawSubset(value)
		if err != nil {
			return nil, nil, invalid("subset", err.Error())
		}
		key := strings.ToLower(subset.axis)
		if seen[key] {
			return nil, nil, invalidAxis("duplicate subset axis " + subset.axis)
		}
		seen[key] = true
		if spatialAxis(subset.axis, info) >= 0 {
			spatial = append(spatial, subset)
			continue
		}
		dimension := dimensionForAxis(coverage, subset.axis)
		if dimension == nil {
			return nil, nil, invalidAxis("unknown subset axis " + subset.axis)
		}
		if !capabilities.enabled(extMultidim) {
			return nil, nil, extensionDisabled("subset", extMultidim)
		}
		if err := validateDimensionSubset(subset, dimension); err != nil {
			return nil, nil, err
		}
		if dimension.SourceAxis != "" {
			subset.axis = dimension.SourceAxis
		}
		dimensions = append(dimensions, subset)
	}
	return spatial, dimensions, nil
}

func parseRawSubset(raw string) (rawSubset, error) {
	raw = strings.TrimSpace(raw)
	open, close := strings.IndexByte(raw, '('), strings.LastIndexByte(raw, ')')
	if open < 1 || close != len(raw)-1 {
		return rawSubset{}, fmt.Errorf("invalid subset syntax")
	}
	parts := splitTopLevel(raw[open+1:close], ',')
	if len(parts) < 1 || len(parts) > 2 || strings.TrimSpace(parts[0]) == "" {
		return rawSubset{}, fmt.Errorf("invalid subset bounds")
	}
	result := rawSubset{axis: strings.TrimSpace(raw[:open]), low: strings.TrimSpace(parts[0]), slice: len(parts) == 1}
	result.high = result.low
	if len(parts) == 2 {
		result.high = strings.TrimSpace(parts[1])
		if result.high == "" {
			return rawSubset{}, fmt.Errorf("invalid subset bounds")
		}
	}
	return result, nil
}

func validateDimensionSubset(subset rawSubset, dimension *workspace.Dimension) *requestError {
	parse := func(value string) error {
		value = unquote(value)
		if strings.EqualFold(dimension.Name, "time") {
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
				if _, dateErr := time.Parse("2006-01-02", value); dateErr != nil {
					return fmt.Errorf("invalid time coordinate")
				}
			}
			return nil
		}
		if strings.EqualFold(dimension.Name, "elevation") {
			if _, err := strconv.ParseFloat(value, 64); err != nil {
				return fmt.Errorf("invalid elevation coordinate")
			}
		}
		return nil
	}
	if err := parse(subset.low); err != nil {
		return invalid("subset", err.Error())
	}
	if !subset.slice {
		if err := parse(subset.high); err != nil {
			return invalid("subset", err.Error())
		}
	}
	return nil
}

func dimensionSelection(subsets []rawSubset, coverage *workspace.Coverage) (string, string) {
	var timeValue, elevation string
	selected := map[string]bool{}
	for _, subset := range subsets {
		value := unquote(subset.low)
		if !subset.slice {
			value += "/" + unquote(subset.high)
		}
		selected[strings.ToLower(subset.axis)] = true
		dimension := dimensionForAxis(coverage, subset.axis)
		if dimension != nil {
			selected[strings.ToLower(dimension.Name)] = true
			selected[strings.ToLower(dimension.SourceAxis)] = true
		}
		if dimension != nil && strings.EqualFold(dimension.Name, "time") {
			timeValue = value
		} else if dimension != nil && strings.EqualFold(dimension.Name, "elevation") {
			elevation = value
		}
	}
	for _, dimension := range coverage.Dimensions {
		if dimension == nil || selected[strings.ToLower(dimension.Name)] || dimension.Default == "" {
			continue
		}
		if strings.EqualFold(dimension.Name, "time") {
			timeValue = dimension.Default
		} else if strings.EqualFold(dimension.Name, "elevation") {
			elevation = dimension.Default
		}
	}
	return timeValue, elevation
}

func parseRangeSubset(q url.Values, info *datasource.CoverageInfo, coverage *workspace.Coverage, capabilities capabilityRegistry) ([]int, []datasource.CoverageRangeSelection, *requestError) {
	all := make([]int, len(info.Bands))
	for i := range all {
		all[i] = i + 1
	}
	values := q["RANGESUBSET"]
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return all, nil, nil
	}
	if !capabilities.enabled(extRangeSubset) {
		return nil, nil, extensionDisabled("rangeSubset", extRangeSubset)
	}
	if len(values) != 1 {
		return nil, nil, invalid("rangeSubset", "RANGESUBSET may occur only once")
	}
	names := make([]string, len(info.Bands))
	for i, band := range info.Bands {
		names[i] = effectiveBand(coverage, band, i).Name
	}
	index := func(name string) int {
		for i, field := range names {
			if name == field {
				return i
			}
		}
		return -1
	}
	var bands []int
	var selections []datasource.CoverageRangeSelection
	seen := map[int]bool{}
	for _, token := range splitTopLevel(values[0], ',') {
		parts := strings.Split(strings.TrimSpace(token), ":")
		if len(parts) > 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, nil, invalid("rangeSubset", "invalid range component interval")
		}
		start := index(strings.TrimSpace(parts[0]))
		if start < 0 {
			return nil, nil, invalid("rangeSubset", "unknown range component "+strings.TrimSpace(parts[0]))
		}
		end := start
		selection := datasource.CoverageRangeSelection{Start: names[start]}
		if len(parts) == 2 {
			end = index(strings.TrimSpace(parts[1]))
			if end < start {
				return nil, nil, invalid("rangeSubset", "range component interval is reversed or unknown")
			}
			selection.End = names[end]
		}
		selections = append(selections, selection)
		for i := start; i <= end; i++ {
			if seen[i] {
				return nil, nil, invalid("rangeSubset", "duplicate range component "+names[i])
			}
			seen[i] = true
			bands = append(bands, i+1)
		}
	}
	return bands, selections, nil
}

func parseScaling(q url.Values, info *datasource.CoverageInfo, output *datasource.CoverageWindow, capabilities capabilityRegistry) (datasource.CoverageScaling, *requestError) {
	parameters := []struct {
		name string
		mode datasource.CoverageScaleMode
	}{{"SCALEFACTOR", datasource.CoverageScaleFactor}, {"SCALEAXES", datasource.CoverageScaleAxes}, {"SCALESIZE", datasource.CoverageScaleSize}, {"SCALEEXTENT", datasource.CoverageScaleExtent}}
	var selected *struct {
		name string
		mode datasource.CoverageScaleMode
	}
	for i := range parameters {
		if len(q[parameters[i].name]) > 0 && strings.TrimSpace(q.Get(parameters[i].name)) != "" {
			if selected != nil || len(q[parameters[i].name]) != 1 {
				return datasource.CoverageScaling{}, invalid("scaling", "exactly one scaling method may be supplied")
			}
			selected = &parameters[i]
		}
	}
	if selected == nil {
		return datasource.CoverageScaling{}, nil
	}
	if !capabilities.enabled(extScaling) {
		return datasource.CoverageScaling{}, extensionDisabled(strings.ToLower(selected.name), extScaling)
	}
	result := datasource.CoverageScaling{Mode: selected.mode}
	raw := strings.TrimSpace(q.Get(selected.name))
	if selected.mode == datasource.CoverageScaleFactor {
		factor, err := strconv.ParseFloat(raw, 64)
		if err != nil || factor <= 0 || math.IsInf(factor, 0) || math.IsNaN(factor) {
			return result, invalid("scaleFactor", "scale factor must be a positive finite number")
		}
		result.Factor = factor
		var ok bool
		if output.Width, ok = scaledDimension(output.Width, factor); !ok {
			return result, invalid("scaleFactor", "scaled width is too large")
		}
		if output.Height, ok = scaledDimension(output.Height, factor); !ok {
			return result, invalid("scaleFactor", "scaled height is too large")
		}
		return result, nil
	}
	tokens := splitTopLevel(raw, ',')
	seen := map[int]bool{}
	for _, token := range tokens {
		open, close := strings.IndexByte(token, '('), strings.LastIndexByte(token, ')')
		if open < 1 || close != len(token)-1 {
			return result, invalid(strings.ToLower(selected.name), "invalid scaling axis syntax")
		}
		name, value := strings.TrimSpace(token[:open]), strings.TrimSpace(token[open+1:close])
		axis := gridAxis(name, info)
		if axis < 0 || seen[axis] {
			return result, invalid(strings.ToLower(selected.name), "unknown or duplicate scaling axis "+name)
		}
		seen[axis] = true
		entry := datasource.CoverageScaleAxis{Axis: name}
		switch selected.mode {
		case datasource.CoverageScaleAxes:
			factor, err := strconv.ParseFloat(value, 64)
			if err != nil || factor <= 0 || math.IsInf(factor, 0) || math.IsNaN(factor) {
				return result, invalid("scaleAxes", "axis scale factor must be positive")
			}
			entry.Factor = factor
			if axis == 0 {
				scaled, ok := scaledDimension(output.Width, factor)
				if !ok {
					return result, invalid("scaleAxes", "scaled width is too large")
				}
				output.Width = scaled
			} else {
				scaled, ok := scaledDimension(output.Height, factor)
				if !ok {
					return result, invalid("scaleAxes", "scaled height is too large")
				}
				output.Height = scaled
			}
		case datasource.CoverageScaleSize:
			size, err := strconv.Atoi(value)
			if err != nil || size <= 0 {
				return result, invalid("scaleSize", "axis size must be positive")
			}
			entry.Size = size
			if axis == 0 {
				output.Width = size
			} else {
				output.Height = size
			}
		case datasource.CoverageScaleExtent:
			bounds := strings.Split(value, ":")
			if len(bounds) != 2 {
				return result, invalid("scaleExtent", "axis extent must contain low:high")
			}
			low, lowErr := strconv.Atoi(strings.TrimSpace(bounds[0]))
			high, highErr := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if lowErr != nil || highErr != nil || high < low {
				return result, invalid("scaleExtent", "axis extent is invalid")
			}
			entry.Low, entry.High = low, high
			if axis == 0 {
				output.GridLowX, output.Width = low, high-low+1
			} else {
				output.GridLowY, output.Height = low, high-low+1
			}
		}
		result.Axes = append(result.Axes, entry)
	}
	return result, nil
}

func scaledDimension(size int, factor float64) (int, bool) {
	value := math.Round(float64(size) * factor)
	if math.IsInf(value, 0) || math.IsNaN(value) || value > float64(math.MaxInt64) {
		return 0, false
	}
	return maxInt(1, int(value)), true
}

func outputCoverageInfo(source *datasource.CoverageInfo, outputCRS string, bbox [4]float64, window datasource.CoverageWindow, bands []int, coverage *workspace.Coverage) *datasource.CoverageInfo {
	result := cloneCoverageInfo(source)
	result.CRS = outputCRS
	if code, err := crs.Parse(outputCRS); err == nil {
		result.SRID = code
	}
	result.Width, result.Height = window.Width, window.Height
	result.OriginX, result.OriginY = bbox[0], bbox[3]
	result.ResolutionX = (bbox[2] - bbox[0]) / float64(window.Width)
	result.ResolutionY = -(bbox[3] - bbox[1]) / float64(window.Height)
	result.Envelope = bbox
	result.Bands = make([]datasource.CoverageBand, len(bands))
	for i, number := range bands {
		result.Bands[i] = effectiveBand(coverage, source.Bands[number-1], number-1)
		result.Bands[i].Band = i + 1
	}
	return result
}

func windowBBox(info *datasource.CoverageInfo, window datasource.CoverageWindow) [4]float64 {
	x1 := info.OriginX + float64(window.XOff)*info.ResolutionX
	x2 := info.OriginX + float64(window.XOff+window.Width)*info.ResolutionX
	y1 := info.OriginY + float64(window.YOff)*info.ResolutionY
	y2 := info.OriginY + float64(window.YOff+window.Height)*info.ResolutionY
	return [4]float64{math.Min(x1, x2), math.Min(y1, y2), math.Max(x1, x2), math.Max(y1, y2)}
}

func cloneCoverageInfo(info *datasource.CoverageInfo) *datasource.CoverageInfo {
	copy := *info
	copy.Bands = append([]datasource.CoverageBand(nil), info.Bands...)
	return &copy
}

func spatialAxis(name string, info *datasource.CoverageInfo) int {
	for i, label := range info.AxisLabels {
		if strings.EqualFold(name, label) {
			return i
		}
	}
	return -1
}

func gridAxis(name string, info *datasource.CoverageInfo) int {
	if strings.EqualFold(name, "i") || strings.EqualFold(name, info.AxisLabels[0]) {
		return 0
	}
	if strings.EqualFold(name, "j") || strings.EqualFold(name, info.AxisLabels[1]) {
		return 1
	}
	return -1
}

func dimensionForAxis(coverage *workspace.Coverage, name string) *workspace.Dimension {
	for _, dimension := range coverage.Dimensions {
		if dimension != nil && (strings.EqualFold(dimension.Name, name) || dimension.SourceAxis != "" && strings.EqualFold(dimension.SourceAxis, name)) {
			return dimension
		}
	}
	return nil
}

func hasRawSubset(values []rawSubset, axis string) bool {
	for _, value := range values {
		if strings.EqualFold(value.axis, axis) {
			return true
		}
	}
	return false
}

func sameCRS(a, b string) bool {
	ac, aerr := crs.Parse(a)
	bc, berr := crs.Parse(b)
	return aerr == nil && berr == nil && ac == bc
}

func allowedCRS(value, native string, allowed []string) bool {
	if sameCRS(value, native) {
		return true
	}
	for _, candidate := range allowed {
		if sameCRS(value, candidate) {
			return true
		}
	}
	return false
}

func extensionDisabled(locator, extension string) *requestError {
	return &requestError{Code: "OperationNotSupported", Locator: locator, Text: "WCS extension " + extension + " is not enabled"}
}

func subsetText(subset rawSubset) string {
	if subset.slice {
		return subset.axis + "(" + subset.low + ")"
	}
	return subset.axis + "(" + subset.low + "," + subset.high + ")"
}

func unquote(value string) string { return strings.Trim(strings.TrimSpace(value), `"'`) }

func splitTopLevel(value string, separator rune) []string {
	var result []string
	start, depth := 0, 0
	var quote rune
	for index, current := range value {
		if quote != 0 {
			if current == quote {
				quote = 0
			}
			continue
		}
		switch current {
		case '\'', '"':
			quote = current
		case '(':
			depth++
		case ')':
			depth--
		default:
			if current == separator && depth == 0 {
				result = append(result, strings.TrimSpace(value[start:index]))
				start = index + 1
			}
		}
	}
	result = append(result, strings.TrimSpace(value[start:]))
	return result
}

func toDomainSubsets(values []rawSubset) []datasource.CoverageDomainSubset {
	result := make([]datasource.CoverageDomainSubset, 0, len(values))
	for _, value := range values {
		result = append(result, datasource.CoverageDomainSubset{Axis: value.axis, Low: axisValue(value.low), High: axisValue(value.high), Slice: value.slice})
	}
	return result
}

func axisValue(value string) datasource.CoverageAxisValue {
	value = unquote(value)
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return datasource.CoverageAxisValue{Number: &number}
	}
	if instant, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return datasource.CoverageAxisValue{Time: &instant}
	}
	return datasource.CoverageAxisValue{Text: value}
}

func maxInt(a, b int) int {
	return slices.Max([]int{a, b})
}
