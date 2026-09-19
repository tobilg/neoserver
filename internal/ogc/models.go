package ogc

import "encoding/json"

type Link struct {
	Href  string `json:"href"`
	Rel   string `json:"rel"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
}

type LandingPage struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Links       []Link `json:"links"`
}

type Conformance struct {
	ConformsTo []string `json:"conformsTo"`
}

type Collections struct {
	Collections []Collection `json:"collections"`
	Links       []Link       `json:"links"`
}

type Collection struct {
	ID          string            `json:"id"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Extent      *CollectionExtent `json:"extent,omitempty"`
	CRS         []string          `json:"crs,omitempty"`
	StorageCRS  string            `json:"storageCrs,omitempty"`
	Links       []Link            `json:"links"`
}

type CollectionExtent struct {
	Spatial  *SpatialExtent  `json:"spatial,omitempty"`
	Temporal *TemporalExtent `json:"temporal,omitempty"`
}

type SpatialExtent struct {
	BBox [][]float64 `json:"bbox"`
	CRS  string      `json:"crs,omitempty"`
}

type TemporalExtent struct {
	Interval [][]any `json:"interval"`
	TRS      string  `json:"trs,omitempty"`
}

type Error struct {
	Code   int    `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

type FeatureCollection struct {
	Type           string    `json:"type"`
	Features       []jsonRaw `json:"features"`
	NumberMatched  *int      `json:"numberMatched,omitempty"`
	NumberReturned int       `json:"numberReturned"`
	TimeStamp      string    `json:"timeStamp"`
	Links          []Link    `json:"links,omitempty"`
}

// Queryables is the JSON Schema representation defined by OGC API - Features Part 3.
// Properties intentionally remains raw JSON Schema so datasource-specific formats can
// be represented without coupling them to the OpenAPI schema model.
type Queryables struct {
	Schema               string                    `json:"$schema"`
	ID                   string                    `json:"$id"`
	Type                 string                    `json:"type"`
	Title                string                    `json:"title,omitempty"`
	Description          string                    `json:"description,omitempty"`
	Properties           map[string]map[string]any `json:"properties"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}

// jsonRaw is a local alias to avoid importing encoding/json in this file.
type jsonRaw = json.RawMessage
