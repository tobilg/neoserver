package wms

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

// applyDimensionFilters turns advertised layer dimensions into actual source
// predicates. Legacy metadata-only dimensions remain valid, but a client may
// not request one until source_property has been configured.
func applyDimensionFilters(req *GetMapRequest, layer *workspace.Layer, params *datasource.QueryParams) error {
	if req == nil || layer == nil || params == nil {
		return nil
	}
	for _, dimension := range layer.Dimensions {
		if dimension == nil {
			continue
		}
		var requested string
		switch strings.ToLower(dimension.Name) {
		case "time":
			requested = req.Time
		case "elevation":
			requested = req.Elevation
		default:
			continue
		}
		explicit := requested != ""
		if !explicit {
			requested = dimension.Default
		}
		if requested == "" {
			continue
		}
		if dimension.SourceProperty == "" {
			if explicit {
				return fmt.Errorf("dimension %q is advertised but has no source_property", dimension.Name)
			}
			continue
		}
		predicate, err := dimensionPredicate(dimension, requested)
		if err != nil {
			return err
		}
		if params.Filter == "" {
			params.Filter = predicate
		} else {
			params.Filter = "(" + params.Filter + ") AND (" + predicate + ")"
		}
	}
	return nil
}

func dimensionPredicate(dimension *workspace.Dimension, requested string) (string, error) {
	values := strings.Split(requested, ",")
	if len(values) > 1 && !dimension.MultipleValues {
		return "", fmt.Errorf("dimension %q does not allow multiple values", dimension.Name)
	}
	predicates := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return "", fmt.Errorf("dimension %q contains an empty value", dimension.Name)
		}
		if strings.EqualFold(value, "current") {
			if !dimension.Current || !strings.EqualFold(dimension.Name, "time") {
				return "", fmt.Errorf("dimension %q does not support current", dimension.Name)
			}
			value = "NOW()"
		}
		parts := strings.Split(value, "/")
		if len(parts) > 3 {
			return "", fmt.Errorf("dimension request intervals must contain a start, end, and optional resolution")
		}
		if len(parts) >= 2 {
			start, err := dimensionLiteral(dimension.Name, strings.TrimSpace(parts[0]))
			if err != nil {
				return "", err
			}
			end, err := dimensionLiteral(dimension.Name, strings.TrimSpace(parts[1]))
			if err != nil {
				return "", err
			}
			if dimension.EndProperty != "" {
				predicates = append(predicates, fmt.Sprintf("(%s <= %s AND %s >= %s)", dimension.SourceProperty, end, dimension.EndProperty, start))
			} else {
				predicates = append(predicates, fmt.Sprintf("%s BETWEEN %s AND %s", dimension.SourceProperty, start, end))
			}
			continue
		}
		literal, err := dimensionLiteral(dimension.Name, value)
		if err != nil {
			return "", err
		}
		if dimension.EndProperty != "" {
			predicates = append(predicates, fmt.Sprintf("(%s <= %s AND %s >= %s)", dimension.SourceProperty, literal, dimension.EndProperty, literal))
		} else {
			predicates = append(predicates, fmt.Sprintf("%s = %s", dimension.SourceProperty, literal))
		}
	}
	return "(" + strings.Join(predicates, " OR ") + ")", nil
}

func dimensionLiteral(name, value string) (string, error) {
	if strings.EqualFold(value, "NOW()") {
		return "NOW()", nil
	}
	if strings.EqualFold(name, "time") {
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			if _, dateErr := time.Parse("2006-01-02", value); dateErr != nil {
				return "", fmt.Errorf("invalid TIME value %q", value)
			}
		}
		return value, nil
	}
	if strings.EqualFold(name, "elevation") {
		number, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return "", fmt.Errorf("invalid ELEVATION value %q", value)
		}
		return strconv.FormatFloat(number, 'g', -1, 64), nil
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'", nil
}
