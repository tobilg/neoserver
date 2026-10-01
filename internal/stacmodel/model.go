// Package stacmodel validates STAC documents against pinned, offline schemas.
// Documents retain unknown fields and extension declarations on round trips.
package stacmodel

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Document map[string]any

func Decode(raw []byte) (Document, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON object")
	}
	if doc == nil {
		return nil, fmt.Errorf("expected a JSON object")
	}
	return doc, nil
}
func (d Document) String(key string) string { s, _ := d[key].(string); return s }
func (d Document) Object(key string) Document {
	if v, ok := d[key].(Document); ok {
		return v
	}
	v, _ := d[key].(map[string]any)
	return Document(v)
}
func (d Document) Clone() Document { raw, _ := json.Marshal(d); v, _ := Decode(raw); return v }

// Only schemas for runtime Item, Collection, and query geometry validation are
// embedded. The generated Catalog is validated by the conformance test runner.
//
//go:embed schemas/*.json
var schemaFiles embed.FS
var schemasOnce sync.Once
var compiled map[string]*jsonschema.Schema
var schemaErr error

type offlineLoader struct{}

func (offlineLoader) Load(uri string) (any, error) {
	return nil, fmt.Errorf("schema is not vendored: %s", uri)
}

func loadSchemas() {
	c := jsonschema.NewCompiler()
	c.UseLoader(offlineLoader{})
	c.AssertFormat()
	raw, err := schemaFiles.ReadFile("schemas/manifest.json")
	if err != nil {
		schemaErr = err
		return
	}
	var manifest map[string]struct {
		File string `json:"file"`
	}
	if schemaErr = json.Unmarshal(raw, &manifest); schemaErr != nil {
		return
	}
	for uri, entry := range manifest {
		data, err := schemaFiles.ReadFile("schemas/" + entry.File)
		if err != nil {
			schemaErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			schemaErr = err
			return
		}
		if schemaErr = c.AddResource(uri, doc); schemaErr != nil {
			return
		}
	}
	compiled = map[string]*jsonschema.Schema{}
	for _, version := range []string{"1.0.0", "1.1.0"} {
		for _, kind := range []string{"item", "collection"} {
			schema, err := c.Compile("https://schemas.stacspec.org/v" + version + "/" + kind + "-spec/json-schema/" + kind + ".json")
			if err != nil {
				schemaErr = err
				return
			}
			compiled[version+"/"+kind] = schema
		}
	}
	for _, kind := range []string{"Geometry", "GeometryCollection"} {
		schema, err := c.Compile("https://geojson.org/schema/" + kind + ".json")
		if err != nil {
			schemaErr = err
			return
		}
		compiled[kind] = schema
	}
}

func Ready() error { schemasOnce.Do(loadSchemas); return schemaErr }

// Validate checks an Item or Collection against its pinned core schema and
// neoserver's publication constraints.
func Validate(d Document, kind string) error {
	if kind != "item" && kind != "collection" {
		return fmt.Errorf("unsupported STAC document kind %q", kind)
	}
	if err := Ready(); err != nil {
		return err
	}
	schema := compiled[d.String("stac_version")+"/"+kind]
	if schema == nil {
		return fmt.Errorf("stac_version must be 1.0.0 or 1.1.0")
	}
	// Normalize Go-constructed documents as well as imported JSON before validation.
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if err = schema.Validate(value); err != nil {
		return fmt.Errorf("invalid STAC %s: %w", kind, err)
	}
	if d.String("id") == "" || len(d.String("id")) > 512 || strings.ContainsAny(d.String("id"), "/\\\x00") || d.String("id") == "." || d.String("id") == ".." {
		return fmt.Errorf("id must be a nonempty URL path segment of at most 512 bytes")
	}
	if kind == "item" {
		if d.String("collection") == "" {
			return fmt.Errorf("every Item must belong to a Collection")
		}
		if _, _, err = ItemTime(d); err != nil {
			return err
		}
		if geometry := d.Object("geometry"); geometry != nil {
			if geometry.String("type") == "GeometryCollection" {
				return fmt.Errorf("Item geometry cannot be a GeometryCollection")
			}
			if _, err = GeometryBounds(geometry); err != nil {
				return err
			}
			if err = ValidateBBox(numbers(d["bbox"])); err != nil {
				return err
			}
		}
	}
	for key, v := range d.Object("assets") {
		asset, ok := v.(map[string]any)
		if !ok {
			if x, yes := v.(Document); yes {
				asset = x
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("invalid asset %s", key)
		}
		href, _ := asset["href"].(string)
		if err := PublicURL(href); err != nil {
			return fmt.Errorf("asset %s: %w", key, err)
		}
	}
	return nil
}

// PublicURL excludes local paths and embedded credentials from public metadata.
// Local delivery uses a separately authorized asset binding, never file:// URLs.
func PublicURL(href string) error {
	u, err := url.Parse(href)
	if err != nil || u.User != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "s3" && u.Scheme != "gs" && u.Scheme != "az") {
		return fmt.Errorf("asset href must be an absolute data URL without credentials")
	}
	for key := range u.Query() {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "signature") || strings.Contains(lower, "credential") || lower == "key" || lower == "password" {
			return fmt.Errorf("asset URLs must not contain credentials")
		}
	}
	return nil
}

const TimeLayout = "2006-01-02T15:04:05.000000000Z"

// time.Parse also accepts comma fractions, one-digit hours and out-of-range
// zone offsets. Require the RFC 3339 grammar before validating the calendar.
var timestampPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}[Tt][0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?([Zz]|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func Timestamp(s string) (string, error) {
	if len(s) > 40 || !timestampPattern.MatchString(s) {
		return "", fmt.Errorf("invalid RFC 3339 timestamp")
	}
	t, err := time.Parse(time.RFC3339Nano, strings.ToUpper(s))
	if err != nil {
		return "", fmt.Errorf("invalid RFC 3339 timestamp: %s", s)
	}
	return t.UTC().Format(TimeLayout), nil
}
func ItemTime(d Document) (string, string, error) {
	p := d.Object("properties")
	if v := p.String("datetime"); v != "" {
		t, e := Timestamp(v)
		return t, t, e
	}
	start, err := Timestamp(p.String("start_datetime"))
	if err != nil {
		return "", "", err
	}
	end, err := Timestamp(p.String("end_datetime"))
	if err != nil {
		return "", "", err
	}
	if start > end {
		return "", "", fmt.Errorf("start_datetime must not be after end_datetime")
	}
	return start, end, nil
}
func Interval(value string) (start, end string, err error) {
	if value == "" {
		return
	}
	parts := strings.Split(value, "/")
	if len(parts) == 1 {
		start, err = Timestamp(value)
		end = start
		return
	}
	if len(parts) != 2 {
		err = fmt.Errorf("datetime must be an instant or interval")
		return
	}
	if parts[0] != "" && parts[0] != ".." {
		start, err = Timestamp(parts[0])
		if err != nil {
			return
		}
	}
	if parts[1] != "" && parts[1] != ".." {
		end, err = Timestamp(parts[1])
		if err != nil {
			return
		}
	}
	if (start == "" && end == "") || (start != "" && end != "" && start > end) {
		err = fmt.Errorf("invalid datetime interval")
	}
	return
}
func numbers(value any) []float64 {
	raw, _ := json.Marshal(value)
	var out []float64
	_ = json.Unmarshal(raw, &out)
	return out
}
func ValidateBBox(b []float64) error {
	if len(b) != 4 && len(b) != 6 {
		return fmt.Errorf("bbox requires four or six coordinates")
	}
	n := len(b) / 2
	for _, v := range b {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("bbox coordinates must be finite")
		}
	}
	if b[0] < -180 || b[0] > 180 || b[n] < -180 || b[n] > 180 || b[1] < -90 || b[1] > 90 || b[n+1] < -90 || b[n+1] > 90 || b[1] > b[n+1] || (n == 3 && b[2] > b[5]) {
		return fmt.Errorf("invalid CRS84 bbox")
	}
	return nil
}

// GeometryBounds validates query geometries (including GeometryCollections),
// and retains elevation bounds even though the spatial index operates in 2D.
func GeometryBounds(g Document) ([6]float64, error) {
	var result = [6]float64{math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	if err := Ready(); err != nil {
		return result, err
	}
	raw, err := json.Marshal(g)
	if err != nil {
		return result, err
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return result, err
	}
	kind := "Geometry"
	if g.String("type") == "GeometryCollection" {
		kind = "GeometryCollection"
	}
	if err = compiled[kind].Validate(value); err != nil {
		return result, fmt.Errorf("invalid GeoJSON geometry: %w", err)
	}
	var normalized map[string]any
	_ = json.Unmarshal(raw, &normalized)
	count := 0
	var walk func(any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			if coords, ok := x["coordinates"]; ok {
				return walk(coords)
			}
			if children, ok := x["geometries"]; ok {
				return walk(children)
			}
		case []any:
			if len(x) > 0 {
				if _, ok := x[0].(float64); ok {
					if len(x) < 2 || len(x) > 3 {
						return fmt.Errorf("positions must have two or three coordinates")
					}
					a, ok := x[1].(float64)
					if !ok {
						return fmt.Errorf("invalid coordinate")
					}
					p := [3]float64{x[0].(float64), a, 0}
					if len(x) == 3 {
						p[2], ok = x[2].(float64)
						if !ok {
							return fmt.Errorf("invalid elevation")
						}
					}
					if p[0] < -180 || p[0] > 180 || p[1] < -90 || p[1] > 90 {
						return fmt.Errorf("geometry must use CRS84 longitude and latitude")
					}
					for i, v := range p {
						result[i] = math.Min(result[i], v)
						result[i+3] = math.Max(result[i+3], v)
					}
					count++
					return nil
				}
			}
			for _, child := range x {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err = walk(normalized); err != nil {
		return result, err
	}
	if count == 0 {
		return result, fmt.Errorf("geometry must contain coordinates")
	}
	return result, nil
}

func Collection(id, title, description, license string, bbox []float64) Document {
	if description == "" {
		description = title
	}
	if description == "" {
		description = id
	}
	if len(bbox) == 0 {
		bbox = []float64{-180, -90, 180, 90}
	}
	return Document{"type": "Collection", "stac_version": "1.1.0", "id": id, "title": title, "description": description, "license": license, "links": []any{}, "extent": Document{"spatial": Document{"bbox": [][]float64{bbox}}, "temporal": Document{"interval": [][]any{{nil, nil}}}}}
}

// GeometryBBox retains elevation dimensions in generated Item bboxes.
func GeometryBBox(g Document) ([]float64, error) {
	bounds, err := GeometryBounds(g)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(g["coordinates"])
	var coords any
	_ = json.Unmarshal(raw, &coords)
	var hasZ func(any) bool
	hasZ = func(value any) bool {
		positions, ok := value.([]any)
		if !ok || len(positions) == 0 {
			return false
		}
		if _, ok := positions[0].(float64); ok {
			return len(positions) == 3
		}
		for _, child := range positions {
			if hasZ(child) {
				return true
			}
		}
		return false
	}
	if hasZ(coords) {
		return bounds[:], nil
	}
	return []float64{bounds[0], bounds[1], bounds[3], bounds[4]}, nil
}
