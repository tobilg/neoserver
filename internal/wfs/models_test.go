package wfs

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// ============================================================================
// NormalizeQuery Tests
// ============================================================================

func TestNormalizeQuery(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantKeys []string
	}{
		{
			name:     "lowercase to uppercase",
			url:      "/wfs?service=wfs&request=getcapabilities",
			wantKeys: []string{"SERVICE", "REQUEST"},
		},
		{
			name:     "mixed case",
			url:      "/wfs?Service=WFS&REQUEST=GetCapabilities",
			wantKeys: []string{"SERVICE", "REQUEST"},
		},
		{
			name:     "already uppercase",
			url:      "/wfs?SERVICE=WFS&REQUEST=GETCAPABILITIES",
			wantKeys: []string{"SERVICE", "REQUEST"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tt.url, nil)
			q := NormalizeQuery(r)

			for _, key := range tt.wantKeys {
				if _, ok := q[key]; !ok {
					t.Errorf("expected key %s in normalized query", key)
				}
			}
		})
	}
}

// ============================================================================
// ParseQName Tests
// ============================================================================

func TestParseQName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantPre   string
		wantLocal string
	}{
		{
			name:      "with prefix",
			input:     "cite:Bridges",
			wantPre:   "cite",
			wantLocal: "Bridges",
		},
		{
			name:      "without prefix",
			input:     "Bridges",
			wantPre:   "",
			wantLocal: "Bridges",
		},
		{
			name:      "gml prefix",
			input:     "gml:Feature",
			wantPre:   "gml",
			wantLocal: "Feature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qn := ParseQName(tt.input)
			if qn.Prefix != tt.wantPre {
				t.Errorf("ParseQName() prefix = %q, want %q", qn.Prefix, tt.wantPre)
			}
			if qn.LocalPart != tt.wantLocal {
				t.Errorf("ParseQName() local = %q, want %q", qn.LocalPart, tt.wantLocal)
			}
		})
	}
}

// ============================================================================
// GetNamespaceForPrefix Tests
// ============================================================================

func TestGetNamespaceForPrefix(t *testing.T) {
	tests := []struct {
		prefix string
		want   string
	}{
		{"cite", NSCite},
		{"gml", NSGml},
		{"wfs", NSWfs},
		{"fes", NSFes},
		{"ows", NSOws},
		{"xlink", NSXlink},
		{"unknown", NSDefault},
		{"CITE", NSCite}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			got := GetNamespaceForPrefix(tt.prefix)
			if got != tt.want {
				t.Errorf("GetNamespaceForPrefix(%q) = %q, want %q", tt.prefix, got, tt.want)
			}
		})
	}
}

// ============================================================================
// GetPrefixesFromCollections Tests
// ============================================================================

func TestGetPrefixesFromCollections(t *testing.T) {
	collections := []string{"cite:Bridges", "cite:Roads", "app:Buildings", "simple"}
	prefixes := GetPrefixesFromCollections(collections)

	if _, ok := prefixes["cite"]; !ok {
		t.Error("expected 'cite' prefix")
	}
	if _, ok := prefixes["app"]; !ok {
		t.Error("expected 'app' prefix")
	}
	if len(prefixes) != 2 {
		t.Errorf("expected 2 prefixes, got %d", len(prefixes))
	}
}

// ============================================================================
// ParseSRSName Tests
// ============================================================================

func TestParseSRSName(t *testing.T) {
	tests := []struct {
		name    string
		srs     string
		want    int
		wantErr bool
	}{
		{
			name: "EPSG simple",
			srs:  "EPSG:4326",
			want: 4326,
		},
		{
			name: "EPSG lowercase",
			srs:  "epsg:4326",
			want: 4326,
		},
		{
			name: "URN format",
			srs:  "urn:ogc:def:crs:EPSG::4326",
			want: 4326,
		},
		{
			name: "URN format with 0",
			srs:  "urn:ogc:def:crs:EPSG:0:4326",
			want: 4326,
		},
		{
			name: "HTTP format",
			srs:  "http://www.opengis.net/def/crs/EPSG/0/4326",
			want: 4326,
		},
		{
			name: "CRS:84",
			srs:  "CRS:84",
			want: 4326,
		},
		{
			name: "OGC:CRS84",
			srs:  "OGC:CRS84",
			want: 4326,
		},
		{
			name:    "invalid EPSG",
			srs:     "EPSG:invalid",
			wantErr: true,
		},
		{
			name:    "unsupported format",
			srs:     "something:else",
			wantErr: true,
		},
		{
			name:    "invalid URN",
			srs:     "urn:invalid:format",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSRSName(tt.srs)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSRSName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseSRSName() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ============================================================================
// SRSNameFromSRID Tests
// ============================================================================

func TestSRSNameFromSRID(t *testing.T) {
	tests := []struct {
		srid int
		want string
	}{
		{4326, "urn:ogc:def:crs:EPSG::4326"},
		{3857, "urn:ogc:def:crs:EPSG::3857"},
		{32632, "urn:ogc:def:crs:EPSG::32632"},
	}

	for _, tt := range tests {
		got := SRSNameFromSRID(tt.srid)
		if got != tt.want {
			t.Errorf("SRSNameFromSRID(%d) = %q, want %q", tt.srid, got, tt.want)
		}
	}
}

// ============================================================================
// parseBBox Tests
// ============================================================================

func TestParseBBox(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantMinX float64
		wantMinY float64
		wantMaxX float64
		wantMaxY float64
		wantSRID int
		wantErr  bool
	}{
		{
			name:     "comma separated",
			input:    "-180,-90,180,90",
			wantMinX: -180,
			wantMinY: -90,
			wantMaxX: 180,
			wantMaxY: 90,
			wantSRID: 4326,
		},
		{
			name:     "space separated",
			input:    "-180 -90 180 90",
			wantMinX: -180,
			wantMinY: -90,
			wantMaxX: 180,
			wantMaxY: 90,
			wantSRID: 4326,
		},
		{
			name:     "with SRS",
			input:    "-180,-90,180,90,EPSG:4326",
			wantMinX: -180,
			wantMinY: -90,
			wantMaxX: 180,
			wantMaxY: 90,
			wantSRID: 4326,
		},
		{
			name:     "different SRS",
			input:    "0,0,100,100,EPSG:3857",
			wantMinX: 0,
			wantMinY: 0,
			wantMaxX: 100,
			wantMaxY: 100,
			wantSRID: 3857,
		},
		{
			name:    "too few values",
			input:   "-180,-90,180",
			wantErr: true,
		},
		{
			name:    "too many values",
			input:   "-180,-90,180,90,EPSG:4326,extra",
			wantErr: true,
		},
		{
			name:    "invalid value",
			input:   "-180,invalid,180,90",
			wantErr: true,
		},
		{
			name:    "min > max",
			input:   "180,-90,-180,90",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bbox, srid, err := parseBBox(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseBBox() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if bbox.MinX != tt.wantMinX || bbox.MinY != tt.wantMinY ||
				bbox.MaxX != tt.wantMaxX || bbox.MaxY != tt.wantMaxY {
				t.Errorf("parseBBox() bbox = %+v, want min(%v,%v) max(%v,%v)",
					bbox, tt.wantMinX, tt.wantMinY, tt.wantMaxX, tt.wantMaxY)
			}
			if srid != tt.wantSRID {
				t.Errorf("parseBBox() srid = %d, want %d", srid, tt.wantSRID)
			}
		})
	}
}

// ============================================================================
// parseSortBy Tests
// ============================================================================

func TestParseSortBy(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []SortField
		wantErr bool
	}{
		{
			name:  "single field ascending",
			input: "name A",
			want:  []SortField{{Name: "name", Desc: false}},
		},
		{
			name:  "single field descending",
			input: "name D",
			want:  []SortField{{Name: "name", Desc: true}},
		},
		{
			name:  "single field DESC",
			input: "name DESC",
			want:  []SortField{{Name: "name", Desc: true}},
		},
		{
			name:  "single field ASC",
			input: "name ASC",
			want:  []SortField{{Name: "name", Desc: false}},
		},
		{
			name:  "single field no order",
			input: "name",
			want:  []SortField{{Name: "name", Desc: false}},
		},
		{
			name:  "multiple fields",
			input: "name A,date D",
			want:  []SortField{{Name: "name", Desc: false}, {Name: "date", Desc: true}},
		},
		{
			name:    "invalid order",
			input:   "name X",
			wantErr: true,
		},
		{
			name:  "empty string",
			input: "",
			want:  []SortField{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSortBy(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSortBy() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("parseSortBy() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i, sf := range got {
				if sf.Name != tt.want[i].Name || sf.Desc != tt.want[i].Desc {
					t.Errorf("parseSortBy()[%d] = %+v, want %+v", i, sf, tt.want[i])
				}
			}
		})
	}
}

// ============================================================================
// parseTypeNames Tests
// ============================================================================

func TestParseTypeNames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single name",
			input: "cities",
			want:  []string{"cities"},
		},
		{
			name:  "multiple names comma-separated",
			input: "cities,roads,bridges",
			want:  []string{"cities", "roads", "bridges"},
		},
		{
			name:  "with namespace prefix",
			input: "cite:Bridges,app:Roads",
			want:  []string{"cite:Bridges", "app:Roads"},
		},
		{
			name:  "with spaces around commas",
			input: " cities , roads ",
			want:  []string{"cities", "roads"},
		},
		{
			name:  "space-separated (WFS 2.0 spatial join)",
			input: "ns47:NamedPlaces ns47:Lakes",
			want:  []string{"ns47:NamedPlaces", "ns47:Lakes"},
		},
		{
			name:  "multiple space-separated",
			input: "type1 type2 type3",
			want:  []string{"type1", "type2", "type3"},
		},
		{
			name:  "empty string",
			input: "",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTypeNames(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseTypeNames() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("parseTypeNames()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

// ============================================================================
// parseNamespacesParam Tests
// ============================================================================

func TestParseNamespacesParam(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{
			name:  "single namespace",
			input: "xmlns(ns42,http://cite.opengeospatial.org/gmlsf)",
			want:  map[string]string{"ns42": "cite"},
		},
		{
			name:  "multiple namespaces",
			input: "xmlns(ns1,http://cite.opengeospatial.org/gmlsf),xmlns(wfs,http://www.opengis.net/wfs/2.0)",
			want:  map[string]string{"ns1": "cite", "wfs": "wfs"},
		},
		{
			name:  "empty string",
			input: "",
			want:  map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNamespacesParam(tt.input)
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("parseNamespacesParam()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// ============================================================================
// resolveTypeName Tests
// ============================================================================

func TestResolveTypeName(t *testing.T) {
	nsMap := map[string]string{
		"ns30": "cite",
		"ns42": "app",
	}

	tests := []struct {
		name     string
		typeName string
		want     string
	}{
		{
			name:     "with mapped prefix",
			typeName: "ns30:Bridges",
			want:     "cite:Bridges",
		},
		{
			name:     "with unknown prefix",
			typeName: "unknown:Bridges",
			want:     "unknown:Bridges",
		},
		{
			name:     "no prefix",
			typeName: "Bridges",
			want:     "Bridges",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTypeName(tt.typeName, nsMap)
			if got != tt.want {
				t.Errorf("resolveTypeName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ============================================================================
// RequestError Tests
// ============================================================================

func TestRequestError(t *testing.T) {
	err := &RequestError{
		Code:    ExceptionInvalidParameterValue,
		Locator: "bbox",
		Message: "invalid bbox",
	}

	errStr := err.Error()
	if !strings.Contains(errStr, ExceptionInvalidParameterValue) {
		t.Errorf("Error() should contain code, got %q", errStr)
	}
	if !strings.Contains(errStr, "invalid bbox") {
		t.Errorf("Error() should contain message, got %q", errStr)
	}
}

// ============================================================================
// ParseGetFeatureRequest Tests
// ============================================================================

func TestParseGetFeatureRequest(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantErr    bool
		wantTypes  []string
		wantCount  int
		wantFormat string
	}{
		{
			name:       "basic request",
			url:        "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=cities",
			wantTypes:  []string{"cities"},
			wantCount:  1000,
			wantFormat: FormatGML32,
		},
		{
			name:       "with count",
			url:        "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=cities&COUNT=50",
			wantTypes:  []string{"cities"},
			wantCount:  50,
			wantFormat: FormatGML32,
		},
		{
			name:    "invalid count",
			url:     "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=cities&COUNT=invalid",
			wantErr: true,
		},
		{
			name:    "negative count",
			url:     "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=cities&COUNT=-5",
			wantErr: true,
		},
		{
			name:       "geojson format",
			url:        "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=cities&OUTPUTFORMAT=application/json",
			wantTypes:  []string{"cities"},
			wantCount:  1000,
			wantFormat: FormatGeoJSON,
		},
		{
			name:    "unsupported output format",
			url:     "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=cities&OUTPUTFORMAT=invalid/format",
			wantErr: true,
		},
		{
			name:    "invalid result type",
			url:     "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=cities&RESULTTYPE=invalid",
			wantErr: true,
		},
		{
			name:    "invalid startindex",
			url:     "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&TYPENAMES=cities&STARTINDEX=-1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tt.url, nil)
			req, err := ParseGetFeatureRequest(r, 10000, 1000)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGetFeatureRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if len(req.TypeNames) != len(tt.wantTypes) {
				t.Errorf("TypeNames = %v, want %v", req.TypeNames, tt.wantTypes)
			}
			if req.Count != tt.wantCount {
				t.Errorf("Count = %d, want %d", req.Count, tt.wantCount)
			}
			if req.OutputFormat != tt.wantFormat {
				t.Errorf("OutputFormat = %q, want %q", req.OutputFormat, tt.wantFormat)
			}
		})
	}
}

// ============================================================================
// ParseGetPropertyValueRequest Tests
// ============================================================================

func TestParseGetPropertyValueRequest(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errCode string
	}{
		{
			name:    "missing valuereference",
			url:     "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetPropertyValue&TYPENAMES=cities",
			wantErr: true,
			errCode: ExceptionMissingParameterValue,
		},
		{
			name:    "empty valuereference",
			url:     "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetPropertyValue&TYPENAMES=cities&VALUEREFERENCE=",
			wantErr: true,
			errCode: ExceptionInvalidParameterValue, // Empty value is InvalidParameterValue per WFS 2.0 spec
		},
		{
			name:    "valid request",
			url:     "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetPropertyValue&TYPENAMES=cities&VALUEREFERENCE=name",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tt.url, nil)
			_, err := ParseGetPropertyValueRequest(r, 10000, 1000)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGetPropertyValueRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil {
				if reqErr, ok := err.(*RequestError); ok {
					if reqErr.Code != tt.errCode {
						t.Errorf("error code = %q, want %q", reqErr.Code, tt.errCode)
					}
				}
			}
		})
	}
}

// ============================================================================
// ParseDescribeFeatureTypeRequest Tests
// ============================================================================

func TestParseDescribeFeatureTypeRequest(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantTypes []string
		wantErr   bool
	}{
		{
			name:      "single type",
			url:       "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=DescribeFeatureType&TYPENAMES=cities",
			wantTypes: []string{"cities"},
		},
		{
			name:      "multiple types",
			url:       "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=DescribeFeatureType&TYPENAMES=cities,roads",
			wantTypes: []string{"cities", "roads"},
		},
		{
			name:      "no types",
			url:       "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=DescribeFeatureType",
			wantTypes: nil,
		},
		{
			name:    "unsupported output format",
			url:     "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=DescribeFeatureType&TYPENAMES=cities&OUTPUTFORMAT=invalid/format",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tt.url, nil)
			req, err := ParseDescribeFeatureTypeRequest(r)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDescribeFeatureTypeRequest() error = %v", err)
			}
			if tt.wantErr {
				return
			}

			if len(req.TypeNames) != len(tt.wantTypes) {
				t.Errorf("TypeNames len = %d, want %d", len(req.TypeNames), len(tt.wantTypes))
			}
		})
	}
}

// ============================================================================
// ParseDescribeStoredQueriesRequest Tests
// ============================================================================

func TestParseDescribeStoredQueriesRequest(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantIDs []string
	}{
		{
			name:    "with stored query id",
			url:     "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=DescribeStoredQueries&STOREDQUERY_ID=urn:ogc:def:query:OGC-WFS::GetFeatureById",
			wantIDs: []string{"urn:ogc:def:query:OGC-WFS::GetFeatureById"},
		},
		{
			name:    "without stored query id",
			url:     "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=DescribeStoredQueries",
			wantIDs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tt.url, nil)
			req, err := ParseDescribeStoredQueriesRequest(r)

			if err != nil {
				t.Fatalf("ParseDescribeStoredQueriesRequest() error = %v", err)
			}

			if len(req.StoredQueryIds) != len(tt.wantIDs) {
				t.Errorf("StoredQueryIds len = %d, want %d", len(req.StoredQueryIds), len(tt.wantIDs))
			}
		})
	}
}

// ============================================================================
// extractFilterFromXML Tests
// ============================================================================

func TestExtractFilterFromXML(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantHas bool
	}{
		{
			name:    "simple filter",
			input:   `<Filter><PropertyIsEqualTo><ValueReference>name</ValueReference><Literal>test</Literal></PropertyIsEqualTo></Filter>`,
			wantHas: true,
		},
		{
			name:    "namespaced filter",
			input:   `<fes:Filter><fes:PropertyIsEqualTo><fes:ValueReference>name</fes:ValueReference><fes:Literal>test</fes:Literal></fes:PropertyIsEqualTo></fes:Filter>`,
			wantHas: true,
		},
		{
			name:    "no filter",
			input:   `<Query typeNames="cities"/>`,
			wantHas: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractFilterFromXML(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			hasFilter := got != ""
			if hasFilter != tt.wantHas {
				t.Errorf("extractFilterFromXML() hasFilter = %v, want %v", hasFilter, tt.wantHas)
			}
		})
	}
}

// ============================================================================
// XML POST Body Parsing Tests
// ============================================================================

func TestParseGetFeatureRequest_XMLPost(t *testing.T) {
	xmlBody := `<?xml version="1.0" encoding="UTF-8"?>
<wfs:GetFeature xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0" count="100">
  <wfs:Query typeNames="cities"/>
</wfs:GetFeature>`

	r := httptest.NewRequest("POST", "/wfs", strings.NewReader(xmlBody))
	r.Header.Set("Content-Type", "application/xml")

	req, err := ParseGetFeatureRequest(r, 10000, 1000)
	if err != nil {
		t.Fatalf("ParseGetFeatureRequest() error = %v", err)
	}

	if req.Count != 100 {
		t.Errorf("Count = %d, want 100", req.Count)
	}

	if len(req.TypeNames) != 1 || req.TypeNames[0] != "cities" {
		t.Errorf("TypeNames = %v, want [cities]", req.TypeNames)
	}
}

func TestParseGetFeatureRequest_StoredQuery(t *testing.T) {
	url := "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&STOREDQUERY_ID=urn:ogc:def:query:OGC-WFS::GetFeatureById&ID=cities.1"

	r := httptest.NewRequest("GET", url, nil)
	req, err := ParseGetFeatureRequest(r, 10000, 1000)
	if err != nil {
		t.Fatalf("ParseGetFeatureRequest() error = %v", err)
	}

	if got := req.StoredQueryParams["ID"]; got != "cities.1" {
		t.Fatalf("ID = %q, want cities.1", got)
	}
}

func TestParseGetFeatureRequest_StoredQueryQNameParameters(t *testing.T) {
	tests := []struct {
		name string
		req  *http.Request
	}{
		{
			name: "KVP",
			req: httptest.NewRequest("GET", "/wfs?SERVICE=WFS&VERSION=2.0.0&REQUEST=GetFeature&STOREDQUERY_ID=urn:example:GetFeatureByTypeName&NAMESPACES="+
				url.QueryEscape("xmlns(ns52,"+NSCite+")")+"&typeName="+url.QueryEscape("ns52:RoadSegments"), nil),
		},
		{
			name: "XML POST",
			req: httptest.NewRequest("POST", "/wfs", strings.NewReader(`<wfs:GetFeature xmlns:wfs="http://www.opengis.net/wfs/2.0" service="WFS" version="2.0.0">
  <wfs:StoredQuery id="urn:example:GetFeatureByTypeName">
    <wfs:Parameter xmlns:ns52="http://cite.opengeospatial.org/gmlsf" name="typeName">ns52:RoadSegments</wfs:Parameter>
  </wfs:StoredQuery>
</wfs:GetFeature>`)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := ParseGetFeatureRequest(tt.req, 10000, 1000)
			if err != nil {
				t.Fatal(err)
			}
			if got := req.StoredQueryParams["TYPENAME"]; got != "cite:RoadSegments" {
				t.Fatalf("typeName = %q, want cite:RoadSegments", got)
			}
		})
	}
}

// ============================================================================
// parseXMLWithNamespaces Tests
// ============================================================================

func TestParseXMLWithNamespaces(t *testing.T) {
	xmlBody := `<?xml version="1.0" encoding="UTF-8"?>
<wfs:GetFeature xmlns:wfs="http://www.opengis.net/wfs/2.0"
                xmlns:cite="http://cite.opengeospatial.org/gmlsf"
                service="WFS" version="2.0.0">
  <wfs:Query typeNames="cite:Bridges"/>
</wfs:GetFeature>`

	nsMap := parseXMLWithNamespaces([]byte(xmlBody))

	if nsMap["wfs"] != "wfs" {
		t.Errorf("expected wfs namespace mapping, got %v", nsMap)
	}
	if nsMap["cite"] != "cite" {
		t.Errorf("expected cite namespace mapping, got %v", nsMap)
	}
}

func TestExtractFilterFromXML_ResourceId(t *testing.T) {
	// This is the inner XML of a Query element with a ResourceId filter
	rawXML := `
      <Filter xmlns="http://www.opengis.net/fes/2.0">
         <ResourceId rid="NamedPlaces.1"/>
      </Filter>
   `

	filterXML, extractErr := extractFilterFromXML(rawXML)
	if extractErr != nil {
		t.Fatal(extractErr)
	}
	if filterXML == "" {
		t.Fatal("expected filter to be extracted, got empty string")
	}

	t.Logf("Extracted filter: %q", filterXML)

	if !strings.Contains(filterXML, "ResourceId") {
		t.Errorf("extracted filter should contain ResourceId: %q", filterXML)
	}

	// Now verify we can parse this as FES
	fesFilter, err := ParseFESFilter(filterXML)
	if err != nil {
		t.Fatalf("failed to parse FES filter: %v", err)
	}

	if len(fesFilter.ResourceId) != 1 {
		t.Errorf("expected 1 ResourceId, got %d", len(fesFilter.ResourceId))
	}

	if fesFilter.ResourceId[0].Rid != "NamedPlaces.1" {
		t.Errorf("ResourceId.Rid = %q, expected 'NamedPlaces.1'", fesFilter.ResourceId[0].Rid)
	}
}
