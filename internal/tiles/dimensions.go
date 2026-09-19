package tiles

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/workspace"
)

func tileDimensionFilter(layer *workspace.Layer, timeValue, elevationValue string) (string, error) {
	if layer == nil {
		return "", nil
	}
	var predicates []string
	for _, dimension := range layer.Dimensions {
		if dimension == nil {
			continue
		}
		value := ""
		switch strings.ToLower(dimension.Name) {
		case "time":
			value = timeValue
		case "elevation":
			value = elevationValue
		default:
			continue
		}
		if value == "" {
			value = dimension.Default
		}
		if value == "" {
			continue
		}
		if dimension.SourceProperty == "" {
			return "", fmt.Errorf("dimension %q has no source_property", dimension.Name)
		}
		literal := func(raw string) (string, error) {
			raw = strings.TrimSpace(raw)
			if strings.EqualFold(raw, "current") && dimension.Current {
				return "NOW()", nil
			}
			if strings.EqualFold(dimension.Name, "time") {
				if _, err := time.Parse(time.RFC3339Nano, raw); err != nil {
					if _, err = time.Parse("2006-01-02", raw); err != nil {
						return "", fmt.Errorf("invalid TIME")
					}
				}
				return raw, nil
			}
			number, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return "", fmt.Errorf("invalid ELEVATION")
			}
			return strconv.FormatFloat(number, 'g', -1, 64), nil
		}
		values := strings.Split(value, ",")
		if len(values) > 1 && !dimension.MultipleValues {
			return "", fmt.Errorf("dimension %q does not allow multiple values", dimension.Name)
		}
		var valuePredicates []string
		for _, selected := range values {
			parts := strings.Split(strings.TrimSpace(selected), "/")
			if len(parts) > 3 {
				return "", fmt.Errorf("invalid dimension interval")
			}
			start, err := literal(parts[0])
			if err != nil {
				return "", err
			}
			if len(parts) >= 2 {
				end, err := literal(parts[1])
				if err != nil {
					return "", err
				}
				if dimension.EndProperty != "" {
					valuePredicates = append(valuePredicates, fmt.Sprintf("(%s <= %s AND %s >= %s)", dimension.SourceProperty, end, dimension.EndProperty, start))
				} else {
					valuePredicates = append(valuePredicates, fmt.Sprintf("%s BETWEEN %s AND %s", dimension.SourceProperty, start, end))
				}
			} else if dimension.EndProperty != "" {
				valuePredicates = append(valuePredicates, fmt.Sprintf("(%s <= %s AND %s >= %s)", dimension.SourceProperty, start, dimension.EndProperty, start))
			} else {
				valuePredicates = append(valuePredicates, fmt.Sprintf("%s = %s", dimension.SourceProperty, start))
			}
		}
		predicates = append(predicates, "("+strings.Join(valuePredicates, " OR ")+")")
	}
	return strings.Join(predicates, " AND "), nil
}
