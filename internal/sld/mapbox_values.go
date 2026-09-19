package sld

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// mapboxProperty keeps expression arrays separate from the one supported
// literal array (line-dasharray). Never flatten an unknown expression into a
// string that the renderer could silently replace with a default.
func mapboxProperty(name string, value any) (string, error) {
	if name == "line-dasharray" {
		items, ok := value.([]any)
		if !ok || len(items) == 0 {
			return "", fmt.Errorf("expected a nonempty numeric dash array")
		}
		parts := make([]string, len(items))
		positive := false
		for i, item := range items {
			n, ok := item.(float64)
			if !ok || !finiteNonnegative(n) {
				return "", fmt.Errorf("dash %d must be a finite non-negative number", i)
			}
			positive = positive || n > 0
			parts[i] = strconv.FormatFloat(n, 'g', -1, 64)
		}
		if !positive {
			return "", fmt.Errorf("dash array must contain a positive length")
		}
		return strings.Join(parts, " "), nil
	}
	result, err := mapboxScalar(value)
	if err != nil {
		return "", err
	}
	if _, expression := value.([]any); expression {
		if name == "raster-opacity" {
			return "", fmt.Errorf("raster opacity requires a literal number")
		}
		return result, nil
	}
	if name == "text-field" {
		if _, ok := value.(string); !ok {
			return "", fmt.Errorf("expected a string or get expression")
		}
		return result, nil
	}
	if strings.HasSuffix(name, "color") {
		color, ok := value.(string)
		if !ok || !validStyleColor(color) {
			return "", fmt.Errorf("expected #RGB, #RRGGBB, or a supported named color")
		}
		return result, nil
	}
	n, ok := value.(float64)
	if !ok || !finiteNonnegative(n) {
		return "", fmt.Errorf("expected a finite non-negative number or get expression")
	}
	if strings.HasSuffix(name, "opacity") && n > 1 {
		return "", fmt.Errorf("opacity must be between 0 and 1")
	}
	if name == "circle-radius" && math.IsInf(n*2, 0) {
		return "", fmt.Errorf("radius is too large")
	}
	return result, nil
}

func finiteNonnegative(value float64) bool {
	return value >= 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

func validStyleColor(value string) bool {
	value = strings.TrimSpace(value)
	if _, ok := namedColors[strings.ToLower(value)]; ok {
		return true
	}
	if !strings.HasPrefix(value, "#") || len(value) != 4 && len(value) != 7 {
		return false
	}
	_, err := strconv.ParseUint(value[1:], 16, 32)
	return err == nil
}
