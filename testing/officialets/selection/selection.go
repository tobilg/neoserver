package selection

import (
	"path/filepath"
	"sort"
	"strings"
)

var allSuites = []string{"ogcapi-features10", "ogcapi-tiles10", "wcs20", "wfs20", "wms13", "wmts10"}

// All returns every stock official suite in stable matrix order.
func All() []string {
	return append([]string(nil), allSuites...)
}

// ForPaths selects official suites affected by repository-relative paths. The
// mapping is deliberately conservative: an unknown non-documentation path
// selects every suite.
func ForPaths(paths []string) []string {
	selected := map[string]bool{}
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" || documentationOnly(path) {
			continue
		}
		matched := true
		switch {
		case strings.HasPrefix(path, "internal/wms/") || strings.HasPrefix(path, "testing/wms/"):
			add(selected, "wms13")
		case strings.HasPrefix(path, "internal/wfs/") || strings.HasPrefix(path, "testing/wfs/"):
			add(selected, "wfs20")
		case strings.HasPrefix(path, "internal/wcs/") || strings.HasPrefix(path, "testing/wcs/") ||
			strings.HasPrefix(path, "internal/datasource/raster") || strings.HasPrefix(path, "internal/mosaiccatalog/"):
			add(selected, "wcs20")
		case strings.HasPrefix(path, "internal/wmts/"):
			add(selected, "wmts10")
		case strings.HasPrefix(path, "internal/ogc/") || strings.HasPrefix(path, "testing/ogcapi/"):
			add(selected, "ogcapi-features10")
		case strings.HasPrefix(path, "internal/tiles/") || strings.HasPrefix(path, "testing/ogcapitiles/"):
			add(selected, "ogcapi-tiles10", "wmts10")
		case strings.HasPrefix(path, "internal/renderer/") || strings.HasPrefix(path, "internal/sld/"):
			add(selected, "wms13", "wmts10", "ogcapi-tiles10")
		case strings.HasPrefix(path, "internal/filter/"):
			add(selected, "wfs20", "ogcapi-features10")
		case strings.HasPrefix(path, "testing/protocol/") || path == "scripts/conformance/run-protocol-integration.sh":
			// Protocol-only infrastructure does not alter stock ETS behavior.
		default:
			matched = false
		}
		if !matched {
			return All()
		}
	}
	result := make([]string, 0, len(selected))
	for suite := range selected {
		result = append(result, suite)
	}
	sort.Strings(result)
	return result
}

func documentationOnly(path string) bool {
	return strings.HasPrefix(path, "docs/") || strings.HasPrefix(path, "external-docs/") ||
		strings.HasSuffix(path, ".md") || path == "LICENSE"
}

func add(selected map[string]bool, suites ...string) {
	for _, suite := range suites {
		selected[suite] = true
	}
}
