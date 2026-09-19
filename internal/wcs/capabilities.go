package wcs

import (
	"slices"
	"strings"

	"github.com/airbusgeo/godal"
	claim "github.com/tobilg/neoserver/internal/conformance"
	"github.com/tobilg/neoserver/internal/store"
)

const (
	extXMLPost       = "xml-post"
	extRangeSubset   = "range-subsetting"
	extScaling       = "scaling"
	extCRS           = "crs"
	extInterpolation = "interpolation"
	extMultidim      = "multidimensional"
)

var defaultWCSFormats = []string{"image/tiff", "application/gml+xml", "multipart/related"}

type capabilityRegistry struct {
	version       string
	extensions    map[string]bool
	formats       []string
	interpolation []string
	subsettingCRS []string
	outputCRS     []string
}

func newCapabilityRegistry(version string, settings store.WCSSettings) capabilityRegistry {
	result := capabilityRegistry{version: version, extensions: map[string]bool{}}
	for _, extension := range settings.Extensions {
		result.extensions[extension] = true
	}
	result.formats = append([]string(nil), settings.OutputFormats...)
	if len(result.formats) == 0 {
		result.formats = append([]string(nil), defaultWCSFormats...)
	}
	result.formats = slices.DeleteFunc(result.formats, func(format string) bool {
		switch format {
		case "application/netcdf":
			_, ok := godal.RasterDriver(godal.DriverName("netCDF"))
			return !ok
		case "image/jp2":
			_, ok := godal.RasterDriver(godal.DriverName("JP2OpenJPEG"))
			return !ok
		default:
			return false
		}
	})
	result.interpolation = append([]string(nil), settings.InterpolationMethods...)
	if len(result.interpolation) == 0 {
		result.interpolation = []string{"nearest-neighbor", "linear"}
	}
	result.subsettingCRS = append([]string(nil), settings.AllowedSubsettingCRS...)
	result.outputCRS = append([]string(nil), settings.AllowedOutputCRS...)
	return result
}

func (c capabilityRegistry) enabled(extension string) bool { return c.extensions[extension] }

func (c capabilityRegistry) supportsFormat(format string) bool {
	return slices.Contains(c.formats, strings.ToLower(strings.TrimSpace(format)))
}

func (c capabilityRegistry) supportsInterpolation(method string) bool {
	method = canonicalInterpolation(method)
	return slices.Contains(c.interpolation, method)
}

func (c capabilityRegistry) profiles() []string {
	keys := []string{claim.WCS21Core, claim.WCS21KVP}
	if c.version == "2.0.1" {
		keys = []string{claim.WCS20Core, claim.WCS20KVP}
	}
	if c.enabled(extXMLPost) {
		keys = append(keys, claim.WCSXMLPost)
	}
	if c.enabled(extRangeSubset) {
		keys = append(keys, claim.WCSRangeSubsetting)
	}
	if c.enabled(extScaling) {
		keys = append(keys, claim.WCSScaling)
	}
	if c.enabled(extCRS) {
		keys = append(keys, claim.WCSCRS, claim.WCSCRSGrid)
	}
	if c.enabled(extInterpolation) {
		interpolation, nearest, linear := claim.WCSInterpolation11, claim.WCSNearest11, claim.WCSLinear11
		if c.version == "2.0.1" {
			interpolation, nearest, linear = claim.WCSInterpolation10, claim.WCSNearest10, claim.WCSLinear10
		}
		keys = append(keys, interpolation)
		if slices.Contains(c.interpolation, "nearest-neighbor") {
			keys = append(keys, nearest)
		}
		if slices.Contains(c.interpolation, "linear") {
			keys = append(keys, linear)
		}
	}
	return claim.URIs(keys...)
}

func canonicalInterpolation(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case value == "nearest", value == "near", value == "nearest-neighbor", strings.HasSuffix(value, "/nearest-neighbor"):
		return "nearest-neighbor"
	case value == "linear", value == "bilinear", strings.HasSuffix(value, "/linear"):
		return "linear"
	default:
		return value
	}
}
