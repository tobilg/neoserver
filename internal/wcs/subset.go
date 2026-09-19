package wcs

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/tobilg/neoserver/internal/datasource"
)

type axisSubset struct {
	axis      string
	low, high float64
	slice     bool
}

func parseSubsets(values []string, info *datasource.CoverageInfo) (datasource.CoverageWindow, *requestError) {
	window := datasource.CoverageWindow{Width: info.Width, Height: info.Height}
	seen := map[int]bool{}
	for _, raw := range values {
		sub, err := parseSubset(raw)
		if err != nil {
			return window, invalid("subset", err.Error())
		}
		axis := -1
		for i, label := range info.AxisLabels {
			if strings.EqualFold(sub.axis, label) {
				axis = i
			}
		}
		if axis < 0 {
			return window, invalidAxis("unknown subset axis " + sub.axis)
		}
		if seen[axis] {
			return window, invalidAxis("duplicate subset axis " + sub.axis)
		}
		seen[axis] = true
		origin, resolution, size := info.OriginX, info.ResolutionX, info.Width
		if axis == 1 {
			origin, resolution, size = info.OriginY, info.ResolutionY, info.Height
		}
		center := origin + resolution/2
		var first, last int
		if sub.slice {
			position := (sub.low - center) / resolution
			nearest := math.Round(position)
			tolerance := 1e-9 * math.Abs(resolution)
			if math.Abs((center+nearest*resolution)-sub.low) > tolerance {
				return window, invalidSubsetting("slice does not resolve to a grid coordinate")
			}
			first, last = int(nearest), int(nearest)
		} else {
			if sub.low > sub.high {
				return window, invalidSubsetting("trim lower bound exceeds upper bound")
			}
			a := (sub.low - center) / resolution
			b := (sub.high - center) / resolution
			if a > b {
				a, b = b, a
			}
			first, last = int(math.Ceil(a-1e-9)), int(math.Floor(b+1e-9))
		}
		if first < 0 || last >= size || first > last {
			return window, invalidSubsetting("subset is outside the coverage domain")
		}
		if axis == 0 {
			window.XOff, window.Width = first, last-first+1
		} else {
			window.YOff, window.Height = first, last-first+1
		}
	}
	return window, nil
}

func parseSubset(raw string) (axisSubset, error) {
	raw = strings.TrimSpace(raw)
	open, close := strings.IndexByte(raw, '('), strings.LastIndexByte(raw, ')')
	if open < 1 || close != len(raw)-1 {
		return axisSubset{}, fmt.Errorf("invalid subset syntax")
	}
	result := axisSubset{axis: strings.TrimSpace(raw[:open])}
	parts := strings.Split(raw[open+1:close], ",")
	if len(parts) < 1 || len(parts) > 2 {
		return result, fmt.Errorf("invalid subset bounds")
	}
	parse := func(value string) (float64, error) {
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		return strconv.ParseFloat(value, 64)
	}
	var err error
	result.low, err = parse(parts[0])
	if err != nil {
		return result, fmt.Errorf("invalid subset coordinate")
	}
	result.high, result.slice = result.low, len(parts) == 1
	if len(parts) == 2 {
		result.high, err = parse(parts[1])
		if err != nil {
			return result, fmt.Errorf("invalid subset coordinate")
		}
	}
	return result, nil
}
