package wfs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tobilg/neoserver/internal/crs"
	"github.com/tobilg/neoserver/internal/query"
)

// QName represents a qualified name with namespace prefix and local part.
type QName struct {
	Prefix    string
	LocalPart string
	Namespace string
}

// ParseQName parses a qualified name string like "cite:Bridges" into prefix and local part.
func ParseQName(name string) QName {
	if idx := strings.Index(name, ":"); idx >= 0 {
		prefix := name[:idx]
		localPart := name[idx+1:]
		return QName{
			Prefix:    prefix,
			LocalPart: localPart,
			Namespace: GetNamespaceForPrefix(prefix),
		}
	}
	return QName{
		Prefix:    "",
		LocalPart: name,
		Namespace: NSDefault,
	}
}

// GetNamespaceForPrefix returns the namespace URI for a known prefix.
func GetNamespaceForPrefix(prefix string) string {
	switch strings.ToLower(prefix) {
	case "cite":
		return NSCite
	case "gml":
		return NSGml
	case "wfs":
		return NSWfs
	case "fes":
		return NSFes
	case "ows":
		return NSOws
	case "xlink":
		return NSXlink
	default:
		return NSDefault
	}
}

// GetPrefixesFromCollections extracts unique namespace prefixes from collection IDs.
func GetPrefixesFromCollections(collectionIDs []string) map[string]string {
	prefixes := make(map[string]string)
	for _, id := range collectionIDs {
		qn := ParseQName(id)
		if qn.Prefix != "" {
			prefixes[qn.Prefix] = qn.Namespace
		}
	}
	return prefixes
}

// parseTypeNames parses a list of type names.
// WFS 2.0 allows both comma-separated and space-separated type names.
// For spatial joins, multiple type names are space-separated in the typeNames attribute.
// Handles both namespace prefixes and bare names.
func parseTypeNames(s string) []string {
	// First replace commas with spaces, then split by whitespace
	// This handles both "type1,type2" and "type1 type2" formats
	s = strings.ReplaceAll(s, ",", " ")
	parts := strings.Fields(s) // Fields splits by any whitespace and handles multiple spaces
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			// Keep the full name for lookup
			result = append(result, p)
		}
	}
	return result
}

// parseNamespacesParam parses the NAMESPACES query parameter.
// Format: xmlns(prefix1,uri1),xmlns(prefix2,uri2),...
// Example: xmlns(ns42,http://cite.opengeospatial.org/gmlsf),xmlns(wfs,http://www.opengis.net/wfs/2.0)
// Returns a map from prefix to our known namespace prefix (e.g., ns42 -> cite).
func parseNamespacesParam(namespaces string) map[string]string {
	nsMap := make(map[string]string)
	if namespaces == "" {
		return nsMap
	}

	// Split by ),xmlns( to get individual declarations
	// First, normalize the string
	namespaces = strings.TrimSpace(namespaces)

	// Find all xmlns(...) declarations
	start := 0
	for {
		idx := strings.Index(namespaces[start:], "xmlns(")
		if idx == -1 {
			break
		}
		start += idx + 6 // len("xmlns(")

		// Find the closing )
		end := strings.Index(namespaces[start:], ")")
		if end == -1 {
			break
		}

		content := namespaces[start : start+end]
		start += end + 1

		// Split by comma to get prefix and URI
		parts := strings.SplitN(content, ",", 2)
		if len(parts) != 2 {
			continue
		}

		prefix := strings.TrimSpace(parts[0])
		uri := strings.TrimSpace(parts[1])

		// Map URI to known prefix
		switch uri {
		case NSCite:
			nsMap[prefix] = "cite"
		case NSDefault:
			nsMap[prefix] = "app"
		case NSWfs:
			nsMap[prefix] = "wfs"
		case NSGml:
			nsMap[prefix] = "gml"
		}
	}

	return nsMap
}

// ParseSRSName parses an SRS name and returns the SRID.
// Supports multiple formats:
// - URN: urn:ogc:def:crs:EPSG::4326
// - HTTP: http://www.opengis.net/def/crs/EPSG/0/4326
// - Simple: EPSG:4326
// Delegates to the unified crs.Parse function.
func ParseSRSName(srs string) (int, error) {
	return crs.Parse(srs)
}

// SRSNameFromSRID converts an SRID to a URN format SRS name.
// Delegates to the unified crs.ToURN function.
func SRSNameFromSRID(srid int) string {
	return crs.ToURN(srid)
}

// parseBBox parses a WFS BBOX parameter.
// Format: minx,miny,maxx,maxy[,srsName] (comma-separated, per WFS 2.0 spec)
// Also accepts space-separated format for compatibility: minx miny maxx maxy [srsName]
func parseBBox(bboxStr string) (*query.BBox, int, error) {
	// First try comma-separated (standard WFS 2.0 KVP encoding)
	parts := strings.Split(bboxStr, ",")
	if len(parts) < 4 {
		// Try space-separated for compatibility (common in capabilities LowerCorner/UpperCorner)
		parts = strings.Fields(bboxStr)
	}
	if len(parts) < 4 || len(parts) > 5 {
		return nil, 0, fmt.Errorf("BBOX must have 4 or 5 values (comma or space separated)")
	}

	values := make([]float64, 4)
	for i := 0; i < 4; i++ {
		v, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid BBOX value %s: %w", parts[i], err)
		}
		values[i] = v
	}

	bbox := &query.BBox{
		MinX: values[0],
		MinY: values[1],
		MaxX: values[2],
		MaxY: values[3],
	}

	// Validate BBOX
	if bbox.MinX > bbox.MaxX || bbox.MinY > bbox.MaxY {
		return nil, 0, fmt.Errorf("invalid BBOX: min values must be less than max values")
	}

	// Parse optional SRS
	srid := 4326 // Default to WGS84
	if len(parts) == 5 {
		srsName := strings.TrimSpace(parts[4])
		var err error
		var swap bool
		srid, swap, err = inputGeometryCRS(srsName, srid)
		if err != nil {
			return nil, 0, err
		}
		if swap {
			bbox.MinX, bbox.MinY = bbox.MinY, bbox.MinX
			bbox.MaxX, bbox.MaxY = bbox.MaxY, bbox.MaxX
		}
	}

	return bbox, srid, nil
}

// parseSortBy parses a WFS SORTBY parameter.
// Format: property1 A,property2 D (A=ascending, D=descending)
func parseSortBy(sortByStr string) ([]SortField, error) {
	parts := strings.Split(sortByStr, ",")
	result := make([]SortField, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// Split on space to separate property from order
		tokens := strings.Fields(p)
		if len(tokens) == 0 {
			continue
		}

		field := SortField{
			Name: tokens[0],
			Desc: false,
		}

		if len(tokens) > 1 {
			order := strings.ToUpper(tokens[1])
			if order == "D" || order == "DESC" {
				field.Desc = true
			} else if order != "A" && order != "ASC" {
				return nil, fmt.Errorf("invalid sort order: %s (expected A or D)", tokens[1])
			}
		}

		result = append(result, field)
	}

	return result, nil
}
