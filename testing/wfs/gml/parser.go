// Package gml provides GML 3.2 parsing utilities.
package gml

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// FeatureCollection represents a WFS feature collection response.
type FeatureCollection struct {
	XMLName        xml.Name `xml:"FeatureCollection"`
	TimeStamp      string   `xml:"timeStamp,attr"`
	NumberMatched  int      `xml:"numberMatched,attr"`
	NumberReturned int      `xml:"numberReturned,attr"`
	Next           string   `xml:"next,attr"`
	Previous       string   `xml:"previous,attr"`
	Members        []Member `xml:"member"`
}

// Member represents a feature collection member.
type Member struct {
	XMLName xml.Name
	Content []byte `xml:",innerxml"`
}

// Feature represents a generic GML feature.
type Feature struct {
	ID         string
	TypeName   string
	Properties map[string]string
	Geometry   string
}

// ParseFeatureCollection parses a GML feature collection.
func ParseFeatureCollection(data []byte) (*FeatureCollection, error) {
	var fc FeatureCollection
	if err := xml.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parsing feature collection: %w", err)
	}
	return &fc, nil
}

// CountFeatures returns the number of features in the collection.
func (fc *FeatureCollection) CountFeatures() int {
	return len(fc.Members)
}

// GetFeatureIDs extracts feature IDs from the collection.
func (fc *FeatureCollection) GetFeatureIDs() []string {
	var ids []string
	for _, m := range fc.Members {
		// Try to extract gml:id from the inner content
		id := extractGMLID(m.Content)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// extractGMLID extracts the gml:id attribute from XML content.
func extractGMLID(content []byte) string {
	s := string(content)

	// Look for gml:id="..." pattern
	idx := strings.Index(s, `gml:id="`)
	if idx == -1 {
		// Try without namespace prefix
		idx = strings.Index(s, `id="`)
		if idx == -1 {
			return ""
		}
		idx += 4
	} else {
		idx += 8
	}

	end := strings.Index(s[idx:], `"`)
	if end == -1 {
		return ""
	}

	return s[idx : idx+end]
}

// HasNext returns true if there's a next page.
func (fc *FeatureCollection) HasNext() bool {
	return fc.Next != ""
}

// HasPrevious returns true if there's a previous page.
func (fc *FeatureCollection) HasPrevious() bool {
	return fc.Previous != ""
}

// ValueCollection represents a GetPropertyValue response.
type ValueCollection struct {
	XMLName        xml.Name `xml:"ValueCollection"`
	TimeStamp      string   `xml:"timeStamp,attr"`
	NumberMatched  int      `xml:"numberMatched,attr"`
	NumberReturned int      `xml:"numberReturned,attr"`
	Members        []Value  `xml:"member>Value"`
}

// Value represents a property value.
type Value struct {
	Content string `xml:",chardata"`
}

// ParseValueCollection parses a GetPropertyValue response.
func ParseValueCollection(data []byte) (*ValueCollection, error) {
	var vc ValueCollection
	if err := xml.Unmarshal(data, &vc); err != nil {
		return nil, fmt.Errorf("parsing value collection: %w", err)
	}
	return &vc, nil
}

// GetValues returns all values as strings.
func (vc *ValueCollection) GetValues() []string {
	var values []string
	for _, v := range vc.Members {
		values = append(values, strings.TrimSpace(v.Content))
	}
	return values
}

// ListStoredQueriesResponse represents a ListStoredQueries response.
type ListStoredQueriesResponse struct {
	XMLName      xml.Name       `xml:"ListStoredQueriesResponse"`
	StoredQueries []StoredQuery `xml:"StoredQuery"`
}

// StoredQuery represents a stored query in the list.
type StoredQuery struct {
	ID    string   `xml:"id,attr"`
	Title []string `xml:"Title"`
}

// ParseListStoredQueries parses a ListStoredQueries response.
func ParseListStoredQueries(data []byte) (*ListStoredQueriesResponse, error) {
	var resp ListStoredQueriesResponse
	if err := xml.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing ListStoredQueries response: %w", err)
	}
	return &resp, nil
}

// GetQueryIDs returns all stored query IDs.
func (r *ListStoredQueriesResponse) GetQueryIDs() []string {
	var ids []string
	for _, q := range r.StoredQueries {
		ids = append(ids, q.ID)
	}
	return ids
}

// HasQuery returns true if the query ID exists.
func (r *ListStoredQueriesResponse) HasQuery(id string) bool {
	for _, q := range r.StoredQueries {
		if q.ID == id {
			return true
		}
	}
	return false
}

// DescribeStoredQueriesResponse represents a DescribeStoredQueries response.
type DescribeStoredQueriesResponse struct {
	XMLName          xml.Name                   `xml:"DescribeStoredQueriesResponse"`
	StoredQueryDescriptions []StoredQueryDescription `xml:"StoredQueryDescription"`
}

// StoredQueryDescription describes a stored query.
type StoredQueryDescription struct {
	ID        string               `xml:"id,attr"`
	Title     []string             `xml:"Title"`
	Abstract  []string             `xml:"Abstract"`
	Parameters []StoredQueryParameter `xml:"Parameter"`
}

// StoredQueryParameter describes a stored query parameter.
type StoredQueryParameter struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

// ParseDescribeStoredQueries parses a DescribeStoredQueries response.
func ParseDescribeStoredQueries(data []byte) (*DescribeStoredQueriesResponse, error) {
	var resp DescribeStoredQueriesResponse
	if err := xml.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing DescribeStoredQueries response: %w", err)
	}
	return &resp, nil
}

// ExtractNumberMatched extracts the numberMatched attribute from response body.
// This is useful when you just need the count without full parsing.
func ExtractNumberMatched(data []byte) (int, error) {
	s := string(data)
	idx := strings.Index(s, `numberMatched="`)
	if idx == -1 {
		return 0, fmt.Errorf("numberMatched not found")
	}
	idx += 15
	end := strings.Index(s[idx:], `"`)
	if end == -1 {
		return 0, fmt.Errorf("numberMatched end quote not found")
	}
	val := s[idx : idx+end]
	if val == "unknown" {
		return -1, nil // Unknown count
	}
	return strconv.Atoi(val)
}

// ExtractNumberReturned extracts the numberReturned attribute from response body.
func ExtractNumberReturned(data []byte) (int, error) {
	s := string(data)
	idx := strings.Index(s, `numberReturned="`)
	if idx == -1 {
		return 0, fmt.Errorf("numberReturned not found")
	}
	idx += 16
	end := strings.Index(s[idx:], `"`)
	if end == -1 {
		return 0, fmt.Errorf("numberReturned end quote not found")
	}
	return strconv.Atoi(s[idx : idx+end])
}
