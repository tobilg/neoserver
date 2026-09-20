package wmts

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/workspace"
)

func resourceDimensions(resource *workspace.PublishedResource) []*workspace.Dimension {
	if resource.Layer != nil {
		return resource.Layer.Dimensions
	}
	if resource.Coverage != nil {
		return resource.Coverage.Dimensions
	}
	return nil
}

func writeDimensions(document *bytes.Buffer, resource *workspace.PublishedResource) {
	for _, dimension := range resourceDimensions(resource) {
		if dimension == nil || !supportedDimension(dimension.Name) {
			continue
		}
		document.WriteString(`<Dimension><ows:Identifier>`)
		xmlText(document, dimension.Name)
		document.WriteString(`</ows:Identifier>`)
		if dimension.Default != "" {
			document.WriteString(`<Default>`)
			xmlText(document, dimension.Default)
			document.WriteString(`</Default>`)
		}
		if dimension.Current {
			document.WriteString(`<Current>true</Current>`)
		}
		for _, value := range strings.Split(dimension.Extent, ",") {
			document.WriteString(`<Value>`)
			xmlText(document, strings.TrimSpace(value))
			document.WriteString(`</Value>`)
		}
		document.WriteString(`</Dimension>`)
	}
}

func supportedDimension(name string) bool {
	return strings.EqualFold(name, "time") || strings.EqualFold(name, "elevation")
}

func (h *handler) resolveDimensions(w http.ResponseWriter, resource *workspace.PublishedResource, request *tileRequest) bool {
	for _, dimension := range resourceDimensions(resource) {
		if dimension == nil || !supportedDimension(dimension.Name) {
			continue
		}
		target := &request.Time
		if strings.EqualFold(dimension.Name, "elevation") {
			target = &request.Elevation
		}
		value := strings.TrimSpace(*target)
		if value == "" || strings.EqualFold(value, "default") {
			value = dimension.Default
		}
		if value == "" {
			h.exception(w, http.StatusBadRequest, "MissingParameterValue", dimension.Name, "a dimension value is required")
			return false
		}
		resolved, err := resolveDimension(dimension, value, time.Now().UTC())
		if err != nil {
			h.exception(w, http.StatusBadRequest, "InvalidParameterValue", dimension.Name, err.Error())
			return false
		}
		*target = resolved // Freeze current/default before constructing cache identity.
	}
	return true
}

type dimensionRange struct{ first, last, step float64 }

func dimensionNumber(name, value string) (float64, error) {
	if strings.EqualFold(name, "time") {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return float64(parsed.Unix()) + float64(parsed.Nanosecond())/1e9, nil
			}
		}
		return 0, fmt.Errorf("invalid time value")
	}
	valueNumber, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(valueNumber) || math.IsInf(valueNumber, 0) {
		return 0, fmt.Errorf("invalid elevation value")
	}
	return valueNumber, nil
}

// Time grid resolutions are fixed ISO 8601 day/hour/minute/second durations.
// Calendar month/year intervals cannot be treated as a fixed number of seconds.
var durationPattern = regexp.MustCompile(`^P(?:(\d+(?:\.\d+)?)D)?(?:T(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

func dimensionStep(name, value string) (float64, error) {
	if !strings.EqualFold(name, "time") {
		return dimensionNumber(name, value)
	}
	parts := durationPattern.FindStringSubmatch(value)
	if parts == nil {
		return 0, fmt.Errorf("unsupported time resolution")
	}
	var seconds float64
	for i, factor := range []float64{86400, 3600, 60, 1} {
		if parts[i+1] != "" {
			number, _ := strconv.ParseFloat(parts[i+1], 64)
			seconds += number * factor
		}
	}
	return seconds, nil
}

func dimensionRanges(dimension *workspace.Dimension, now time.Time) ([]dimensionRange, error) {
	var ranges []dimensionRange
	for _, value := range strings.Split(dimension.Extent, ",") {
		parts := strings.Split(strings.TrimSpace(value), "/")
		if len(parts) > 3 {
			return nil, fmt.Errorf("invalid dimension extent")
		}
		first, err := dimensionNumber(dimension.Name, parts[0])
		if err != nil {
			return nil, err
		}
		item := dimensionRange{first: first, last: first}
		if len(parts) > 1 {
			if strings.EqualFold(parts[1], "current") && dimension.Current && strings.EqualFold(dimension.Name, "time") {
				item.last = float64(now.Unix())
			} else {
				item.last, err = dimensionNumber(dimension.Name, parts[1])
			}
			if err != nil || item.last < item.first {
				return nil, fmt.Errorf("invalid dimension extent")
			}
		}
		if len(parts) == 3 {
			item.step, err = dimensionStep(dimension.Name, parts[2])
			if err != nil || item.step <= 0 {
				return nil, fmt.Errorf("invalid dimension resolution")
			}
		}
		ranges = append(ranges, item)
	}
	return ranges, nil
}

func resolveDimension(dimension *workspace.Dimension, requested string, now time.Time) (string, error) {
	ranges, err := dimensionRanges(dimension, now)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(requested, "current") {
		if !dimension.Current || !strings.EqualFold(dimension.Name, "time") {
			return "", fmt.Errorf("current is unavailable for this dimension")
		}
		latest := math.Inf(-1)
		for _, item := range ranges {
			candidate := min(item.last, float64(now.Unix()))
			if item.step > 0 {
				candidate = item.first + math.Floor((candidate-item.first)/item.step)*item.step
			}
			if candidate >= item.first {
				latest = max(latest, candidate)
			}
		}
		if math.IsInf(latest, -1) {
			return "", fmt.Errorf("no current time is available")
		}
		seconds, fraction := math.Modf(latest)
		return time.Unix(int64(seconds), int64(math.Round(fraction*1e9))).UTC().Format(time.RFC3339Nano), nil
	}
	values := strings.Split(requested, ",")
	if len(values) > 1 && !dimension.MultipleValues {
		return "", fmt.Errorf("multiple dimension values are unavailable")
	}
	for index, value := range values {
		number, err := dimensionNumber(dimension.Name, strings.TrimSpace(value))
		if err != nil {
			return "", err
		}
		valid := false
		for _, item := range ranges {
			if number < item.first || number > item.last {
				continue
			}
			if item.step > 0 {
				position := (number - item.first) / item.step
				if math.Abs(position-math.Round(position)) > 1e-7 {
					continue
				}
			}
			valid = true
			break
		}
		if !valid {
			return "", fmt.Errorf("dimension value is outside the advertised extent")
		}
		values[index] = strings.TrimSpace(value)
	}
	return strings.Join(values, ","), nil
}
