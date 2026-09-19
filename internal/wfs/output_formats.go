package wfs

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/tobilg/neoserver/internal/gdalcap"
)

const (
	geoPackageMediaType = "application/geopackage+sqlite3"
	shapeZipFormat      = "shape-zip"
	shapeZipMediaType   = "application/zip"
)

func isCSVOutput(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "csv" || value == "text/csv"
}

func isGMLOutput(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case strings.ToLower(FormatGML32), strings.ToLower(FormatGML), strings.ToLower(FormatXML),
		strings.ToLower(FormatXSD), strings.ToLower(FormatXMLSubtype):
		return true
	default:
		return strings.HasPrefix(value, "application/gml+xml;") ||
			(strings.HasPrefix(value, "application/xml;") && strings.Contains(value, "subtype=gml/3.2"))
	}
}

func isJSONOutput(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "json" || value == FormatGeoJSON || value == "application/geo+json"
}

func isSupportedGetFeatureOutput(value string) bool {
	return isGMLOutput(value) || isJSONOutput(value) || isCSVOutput(value) || isBinaryExportOutput(value)
}

func isSupportedDescribeFeatureTypeOutput(value string) bool {
	return isGMLOutput(value)
}

func isGeoPackageOutput(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "gpkg" || value == "geopackage" || value == geoPackageMediaType || value == "application/x-sqlite3"
}

func isShapeZipOutput(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), shapeZipFormat)
}

func isBinaryExportOutput(value string) bool {
	return isGeoPackageOutput(value) || isShapeZipOutput(value)
}

func availableWFSOutputFormats() []string {
	formats := []string{"application/gml+xml; version=3.2", "application/json", "text/csv"}
	capabilities := gdalcap.Get()
	if capabilities.GeoPackage {
		formats = append(formats, geoPackageMediaType)
	}
	if capabilities.Shapefile {
		formats = append(formats, shapeZipFormat)
	}
	return formats
}

type limitedBuffer struct {
	bytes.Buffer
	maximum int64
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	if b.maximum > 0 && int64(b.Len()+len(value)) > b.maximum {
		return 0, errors.New("WFS output exceeds the configured byte limit")
	}
	return b.Buffer.Write(value)
}

func (h *workspaceHandler) writeCSV(w http.ResponseWriter, features [][]byte, layerName string) error {
	properties := make(map[string]bool)
	decoded := make([]map[string]any, 0, len(features))
	for _, raw := range features {
		var feature map[string]any
		if err := json.Unmarshal(raw, &feature); err != nil {
			return errors.New("feature cannot be encoded as CSV")
		}
		if values, ok := feature["properties"].(map[string]any); ok {
			for name := range values {
				properties[name] = true
			}
		}
		decoded = append(decoded, feature)
	}
	headings := make([]string, 0, len(properties)+2)
	headings = append(headings, "id")
	for name := range properties {
		headings = append(headings, name)
	}
	sort.Strings(headings[1:])
	headings = append(headings, "geometry")
	buffer := &limitedBuffer{maximum: h.cfg.WFS.MaxOutputBytes}
	encoder := csv.NewWriter(buffer)
	if err := encoder.Write(headings); err != nil {
		return err
	}
	for _, feature := range decoded {
		row := make([]string, len(headings))
		row[0] = scalarCSV(feature["id"])
		values, _ := feature["properties"].(map[string]any)
		for i, heading := range headings[1 : len(headings)-1] {
			row[i+1] = scalarCSV(values[heading])
		}
		if geometry := feature["geometry"]; geometry != nil {
			encoded, _ := json.Marshal(geometry)
			row[len(row)-1] = string(encoded)
		}
		if err := encoder.Write(row); err != nil {
			return err
		}
	}
	encoder.Flush()
	if err := encoder.Error(); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, safeOutputName(layerName)))
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(buffer.Bytes())
	return err
}

func scalarCSV(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64, bool:
		return fmt.Sprint(typed)
	default:
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
}

func safeOutputName(value string) string {
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, value)
	if value == "" {
		return "features"
	}
	return value
}
