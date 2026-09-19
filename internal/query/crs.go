package query

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	CRS84URI = "http://www.opengis.net/def/crs/OGC/1.3/CRS84"
)

// ParseCRS parses a CRS by reference URI or common shorthands into an SRID.
// We treat CRS84 as EPSG:4326 SRID for transformations, but keep the CRS84 URI for response metadata.
func ParseCRS(v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, fmt.Errorf("empty crs")
	}
	if v == CRS84URI || strings.EqualFold(v, "CRS84") {
		return 4326, nil
	}
	if strings.HasPrefix(strings.ToUpper(v), "EPSG:") {
		n, err := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(v), "EPSG:"))
		if err != nil {
			return 0, fmt.Errorf("invalid EPSG code: %q", v)
		}
		return n, nil
	}
	// OGC CRS by reference for EPSG (most common)
	// http://www.opengis.net/def/crs/EPSG/0/4326
	if strings.Contains(v, "/def/crs/EPSG/0/") {
		parts := strings.Split(strings.TrimRight(v, "/"), "/")
		last := parts[len(parts)-1]
		n, err := strconv.Atoi(last)
		if err != nil {
			return 0, fmt.Errorf("invalid EPSG crs uri: %q", v)
		}
		return n, nil
	}
	// Accept raw integer SRID
	if n, err := strconv.Atoi(v); err == nil {
		return n, nil
	}
	return 0, fmt.Errorf("unsupported crs: %q", v)
}

func CRSURIFromSRID(srid int) string {
	if srid == 4326 {
		// For GeoJSON axis order expectations, CRS84 is usually the best default.
		return CRS84URI
	}
	return fmt.Sprintf("http://www.opengis.net/def/crs/EPSG/0/%d", srid)
}


