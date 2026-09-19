package ogcapi

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// AssertStatusCode checks that the response has the expected status code.
func AssertStatusCode(t *testing.T, resp *Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Errorf("Expected status code %d, got %d", expected, resp.StatusCode)
	}
}

// AssertContentType checks that the response has the expected content type.
func AssertContentType(t *testing.T, resp *Response, expected string) {
	t.Helper()
	contentType := resp.Headers.Get("Content-Type")
	if !strings.HasPrefix(contentType, expected) {
		t.Errorf("Expected content type starting with %q, got %q", expected, contentType)
	}
}

// AssertJSONProperty checks that a JSON property exists and has the expected value.
func AssertJSONProperty(t *testing.T, json map[string]any, key string, expected any) {
	t.Helper()
	value, ok := json[key]
	if !ok {
		t.Errorf("Expected JSON property %q to exist", key)
		return
	}
	if value != expected {
		t.Errorf("Expected JSON property %q to be %v, got %v", key, expected, value)
	}
}

// AssertJSONPropertyExists checks that a JSON property exists.
func AssertJSONPropertyExists(t *testing.T, json map[string]any, key string) {
	t.Helper()
	if _, ok := json[key]; !ok {
		t.Errorf("Expected JSON property %q to exist", key)
	}
}

// AssertJSONPropertyIsArray checks that a JSON property exists and is an array.
func AssertJSONPropertyIsArray(t *testing.T, json map[string]any, key string) []any {
	t.Helper()
	value, ok := json[key]
	if !ok {
		t.Errorf("Expected JSON property %q to exist", key)
		return nil
	}
	arr, ok := value.([]any)
	if !ok {
		t.Errorf("Expected JSON property %q to be an array, got %T", key, value)
		return nil
	}
	return arr
}

// AssertJSONPropertyIsString checks that a JSON property exists and is a string.
func AssertJSONPropertyIsString(t *testing.T, json map[string]any, key string) string {
	t.Helper()
	value, ok := json[key]
	if !ok {
		t.Errorf("Expected JSON property %q to exist", key)
		return ""
	}
	str, ok := value.(string)
	if !ok {
		t.Errorf("Expected JSON property %q to be a string, got %T", key, value)
		return ""
	}
	return str
}

// ParseLinks extracts links from a JSON response.
func ParseLinks(json map[string]any) []Link {
	linksData, ok := json["links"].([]any)
	if !ok {
		return nil
	}

	var links []Link
	for _, l := range linksData {
		linkMap, ok := l.(map[string]any)
		if !ok {
			continue
		}
		links = append(links, Link{
			Href:  getString(linkMap, "href"),
			Rel:   getString(linkMap, "rel"),
			Type:  getString(linkMap, "type"),
			Title: getString(linkMap, "title"),
		})
	}
	return links
}

// FindLinkByRel finds a link by its relation type.
func FindLinkByRel(links []Link, rel string) *Link {
	for i := range links {
		if links[i].Rel == rel {
			return &links[i]
		}
	}
	return nil
}

// FindLinksByRel finds all links with a specific relation type.
func FindLinksByRel(links []Link, rel string) []Link {
	var result []Link
	for _, l := range links {
		if l.Rel == rel {
			result = append(result, l)
		}
	}
	return result
}

// AssertLinkExists checks that a link with the given relation exists.
func AssertLinkExists(t *testing.T, links []Link, rel string) {
	t.Helper()
	if FindLinkByRel(links, rel) == nil {
		t.Errorf("Expected link with rel=%q to exist", rel)
	}
}

// AssertLinkExistsWithType checks that a link with the given relation and type exists.
func AssertLinkExistsWithType(t *testing.T, links []Link, rel, mediaType string) {
	t.Helper()
	for _, l := range links {
		if l.Rel == rel && strings.HasPrefix(l.Type, mediaType) {
			return
		}
	}
	t.Errorf("Expected link with rel=%q and type starting with %q to exist", rel, mediaType)
}

// AssertLinksHaveRelAndType checks that all links have rel and type properties.
func AssertLinksHaveRelAndType(t *testing.T, links []Link) {
	t.Helper()
	for i, l := range links {
		if l.Rel == "" {
			t.Errorf("Link %d is missing 'rel' property", i)
		}
		if l.Type == "" {
			t.Errorf("Link %d (rel=%q) is missing 'type' property", i, l.Rel)
		}
	}
}

// AssertSelfLink checks that a self link exists.
func AssertSelfLink(t *testing.T, links []Link) {
	t.Helper()
	AssertLinkExists(t, links, RelSelf)
}

// AssertAlternateLinks checks that alternate links exist for the given media types.
func AssertAlternateLinks(t *testing.T, links []Link, mediaTypes []string) {
	t.Helper()
	alternateLinks := FindLinksByRel(links, RelAlternate)
	for _, mt := range mediaTypes {
		found := false
		for _, l := range alternateLinks {
			if strings.HasPrefix(l.Type, mt) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected alternate link with type starting with %q", mt)
		}
	}
}

// AssertTypeIsFeatureCollection checks that the type property is FeatureCollection.
func AssertTypeIsFeatureCollection(t *testing.T, json map[string]any) {
	t.Helper()
	typeVal := AssertJSONPropertyIsString(t, json, "type")
	if typeVal != "FeatureCollection" {
		t.Errorf("Expected type to be 'FeatureCollection', got %q", typeVal)
	}
}

// AssertFeaturesProperty checks that the features property exists and is an array.
func AssertFeaturesProperty(t *testing.T, json map[string]any) []any {
	t.Helper()
	return AssertJSONPropertyIsArray(t, json, "features")
}

// AssertNumberReturned checks that numberReturned matches the features array length.
func AssertNumberReturned(t *testing.T, json map[string]any) {
	t.Helper()
	features := AssertFeaturesProperty(t, json)
	if features == nil {
		return
	}

	if nr, ok := json["numberReturned"]; ok {
		if nrFloat, ok := nr.(float64); ok {
			if int(nrFloat) != len(features) {
				t.Errorf("numberReturned (%d) does not match features array length (%d)",
					int(nrFloat), len(features))
			}
		}
	}
}

// AssertTimestamp checks that a timestamp property is valid and within bounds.
func AssertTimestamp(t *testing.T, json map[string]any, before, after time.Time) {
	t.Helper()
	if ts, ok := json["timeStamp"]; ok {
		tsStr, ok := ts.(string)
		if !ok {
			t.Errorf("timeStamp is not a string: %T", ts)
			return
		}

		timestamp, err := time.Parse(time.RFC3339, tsStr)
		if err != nil {
			t.Errorf("timeStamp is not valid RFC3339: %v", err)
			return
		}

		if timestamp.Before(before) || timestamp.After(after) {
			t.Errorf("timeStamp %v is outside expected range [%v, %v]",
				timestamp, before, after)
		}
	}
}

// AssertCRS84Header checks that the Content-Crs header indicates CRS84.
func AssertCRS84Header(t *testing.T, resp *Response) {
	t.Helper()
	crsHeader := resp.Headers.Get("Content-Crs")
	if crsHeader == "" {
		return // Optional header
	}

	// Remove angle brackets if present
	crsHeader = strings.TrimPrefix(crsHeader, "<")
	crsHeader = strings.TrimSuffix(crsHeader, ">")

	if crsHeader != CRS84 && crsHeader != CRS84h {
		t.Errorf("Expected Content-Crs to be CRS84 or CRS84h, got %q", crsHeader)
	}
}

// AssertDefaultCRS checks that CRS84 is the first/default CRS in the list.
func AssertDefaultCRS(t *testing.T, crsList []string) {
	t.Helper()
	if len(crsList) == 0 {
		return
	}
	if crsList[0] != CRS84 && crsList[0] != CRS84h {
		t.Errorf("Expected default CRS (first in list) to be CRS84 or CRS84h, got %q", crsList[0])
	}
}

// AssertCRS84InList checks that CRS84 is in the CRS list.
func AssertCRS84InList(t *testing.T, crsList []string) {
	t.Helper()
	for _, crs := range crsList {
		if crs == CRS84 || crs == CRS84h {
			return
		}
	}
	t.Error("Expected CRS84 or CRS84h to be in the CRS list")
}

// BBox represents a bounding box for spatial testing.
type BBox struct {
	MinX, MinY, MaxX, MaxY float64
}

// String returns the bbox as a query parameter string.
func (b BBox) String() string {
	return fmt.Sprintf("%f,%f,%f,%f", b.MinX, b.MinY, b.MaxX, b.MaxY)
}

// Contains checks if a point is within the bounding box.
func (b BBox) Contains(x, y float64) bool {
	return x >= b.MinX && x <= b.MaxX && y >= b.MinY && y <= b.MaxY
}

// Intersects checks if another bbox intersects with this one.
func (b BBox) Intersects(other BBox) bool {
	return !(other.MaxX < b.MinX || other.MinX > b.MaxX ||
		other.MaxY < b.MinY || other.MinY > b.MaxY)
}

// ParseBBox parses a spatial extent into a BBox.
func ParseBBox(extent *SpatialExtent) *BBox {
	if extent == nil || len(extent.BBox) == 0 || len(extent.BBox[0]) < 4 {
		return nil
	}
	bbox := extent.BBox[0]
	return &BBox{
		MinX: bbox[0],
		MinY: bbox[1],
		MaxX: bbox[2],
		MaxY: bbox[3],
	}
}

// AssertCoordinateInCRS84 checks that coordinates are valid for CRS84.
func AssertCoordinateInCRS84(t *testing.T, lon, lat float64, featureID string) {
	t.Helper()
	if lon < CRS84MinLon || lon > CRS84MaxLon {
		t.Errorf("Feature %s: longitude %f is outside CRS84 range [%f, %f]",
			featureID, lon, CRS84MinLon, CRS84MaxLon)
	}
	if lat < CRS84MinLat || lat > CRS84MaxLat {
		t.Errorf("Feature %s: latitude %f is outside CRS84 range [%f, %f]",
			featureID, lat, CRS84MinLat, CRS84MaxLat)
	}
}
