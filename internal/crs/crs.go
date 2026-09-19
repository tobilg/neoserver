// Package crs provides unified CRS/SRS parsing utilities for geospatial services.
// It supports multiple CRS formats used in OGC standards (WMS, WFS, OGC API Features).
package crs

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultSRID is the default SRID (WGS84).
const DefaultSRID = 4326

// Parse extracts the SRID from a CRS/SRS string.
// Supports multiple formats:
//   - Simple: EPSG:4326
//   - URN: urn:ogc:def:crs:EPSG::4326 or urn:ogc:def:crs:EPSG:0:4326
//   - HTTP URI: http://www.opengis.net/def/crs/EPSG/0/4326
//   - CRS:84 (WGS84 lon/lat order) or OGC:CRS84
//
// Returns an error if the CRS format is not recognized or invalid.
func Parse(crs string) (int, error) {
	crs = strings.TrimSpace(crs)
	if crs == "" {
		return 0, fmt.Errorf("empty CRS string")
	}

	// Handle EPSG:XXXX format
	if strings.HasPrefix(strings.ToUpper(crs), "EPSG:") {
		sridStr := strings.TrimPrefix(strings.ToUpper(crs), "EPSG:")
		srid, err := strconv.Atoi(sridStr)
		if err != nil {
			return 0, fmt.Errorf("invalid EPSG code %s: %w", crs, err)
		}
		return srid, nil
	}

	// Handle CRS:84 (WGS84 with lon/lat order)
	if strings.EqualFold(crs, "CRS:84") || strings.EqualFold(crs, "OGC:CRS84") {
		return DefaultSRID, nil
	}

	// Handle URN format: urn:ogc:def:crs:EPSG::4326 or urn:ogc:def:crs:EPSG:0:4326
	if strings.HasPrefix(strings.ToLower(crs), "urn:") {
		parts := strings.Split(crs, ":")
		// Find EPSG in parts and get the number after it
		for i, p := range parts {
			if strings.EqualFold(p, "EPSG") {
				for j := i + 1; j < len(parts); j++ {
					if parts[j] != "" && parts[j] != "0" {
						srid, err := strconv.Atoi(parts[j])
						if err == nil {
							return srid, nil
						}
					}
				}
			}
		}
		return 0, fmt.Errorf("invalid URN CRS: %s", crs)
	}

	// Handle HTTP URI format: http://www.opengis.net/def/crs/EPSG/0/4326
	if strings.Contains(crs, "opengis.net") {
		parts := strings.Split(crs, "/")
		if len(parts) > 0 {
			// Last part should be the SRID
			srid, err := strconv.Atoi(parts[len(parts)-1])
			if err == nil {
				return srid, nil
			}
		}
		return 0, fmt.Errorf("invalid HTTP CRS URI: %s", crs)
	}

	// Try generic EPSG extraction for other formats containing "EPSG"
	if strings.Contains(strings.ToUpper(crs), "EPSG") {
		parts := strings.Split(crs, ":")
		for i, p := range parts {
			if strings.EqualFold(p, "EPSG") && i+1 < len(parts) {
				for j := i + 1; j < len(parts); j++ {
					if parts[j] != "" && parts[j] != "0" {
						srid, err := strconv.Atoi(parts[j])
						if err == nil {
							return srid, nil
						}
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("unsupported CRS: %s", crs)
}

// MustParse extracts the SRID from a CRS string, returning 0 on failure.
// This is useful when error handling is not needed.
func MustParse(crs string) int {
	srid, _ := Parse(crs)
	return srid
}

// ToURN converts an SRID to a URN format CRS string.
// Returns the standard OGC URN format: urn:ogc:def:crs:EPSG::XXXX
func ToURN(srid int) string {
	return fmt.Sprintf("urn:ogc:def:crs:EPSG::%d", srid)
}

// ToEPSG converts an SRID to a simple EPSG format CRS string.
// Returns the format: EPSG:XXXX
func ToEPSG(srid int) string {
	return fmt.Sprintf("EPSG:%d", srid)
}

// ToHTTPURI converts an SRID to an HTTP URI format CRS string.
// Returns the format: http://www.opengis.net/def/crs/EPSG/0/XXXX
func ToHTTPURI(srid int) string {
	return fmt.Sprintf("http://www.opengis.net/def/crs/EPSG/0/%d", srid)
}

// IsLatLonOrder returns true if the given CRS uses lat/lon (northing/easting) axis order.
// EPSG:4326 uses lat/lon order per the OGC standard.
// CRS:84 uses lon/lat order.
func IsLatLonOrder(crs string) bool {
	crs = strings.TrimSpace(crs)

	// CRS:84 explicitly uses lon/lat order
	if strings.EqualFold(crs, "CRS:84") || strings.EqualFold(crs, "OGC:CRS84") {
		return false
	}

	// EPSG:4326 uses lat/lon order in WMS 1.3.0
	srid, err := Parse(crs)
	if err != nil {
		return false
	}

	// Geographic CRS codes typically use lat/lon order
	// Common geographic SRIDs: 4326 (WGS84), 4269 (NAD83), 4267 (NAD27)
	switch srid {
	case 4326, 4269, 4267, 4258:
		return true
	default:
		return false
	}
}
