package wfs

import (
	"strings"
	"testing"
)

// ============================================================================
// ParseFESFilter Tests
// ============================================================================

func TestParseFESFilter(t *testing.T) {
	t.Run("ValidPropertyIsEqualTo", func(t *testing.T) {
		xml := `<Filter xmlns="http://www.opengis.net/fes/2.0">
			<PropertyIsEqualTo>
				<ValueReference>name</ValueReference>
				<Literal>Test</Literal>
			</PropertyIsEqualTo>
		</Filter>`
		filter, err := ParseFESFilter(xml)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filter.PropertyIsEqualTo == nil {
			t.Error("expected PropertyIsEqualTo to be set")
		}
		if filter.PropertyIsEqualTo.ValueReference != "name" {
			t.Errorf("ValueReference = %q, want %q", filter.PropertyIsEqualTo.ValueReference, "name")
		}
		if filter.PropertyIsEqualTo.Literal != "Test" {
			t.Errorf("Literal = %q, want %q", filter.PropertyIsEqualTo.Literal, "Test")
		}
	})

	t.Run("ValidAnd", func(t *testing.T) {
		xml := `<Filter xmlns="http://www.opengis.net/fes/2.0">
			<And>
				<PropertyIsEqualTo>
					<ValueReference>status</ValueReference>
					<Literal>active</Literal>
				</PropertyIsEqualTo>
				<PropertyIsGreaterThan>
					<ValueReference>count</ValueReference>
					<Literal>10</Literal>
				</PropertyIsGreaterThan>
			</And>
		</Filter>`
		filter, err := ParseFESFilter(xml)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filter.And == nil {
			t.Error("expected And to be set")
		}
		if len(filter.And.PropertyIsEqualTo) != 1 {
			t.Errorf("expected 1 PropertyIsEqualTo, got %d", len(filter.And.PropertyIsEqualTo))
		}
		if len(filter.And.PropertyIsGreaterThan) != 1 {
			t.Errorf("expected 1 PropertyIsGreaterThan, got %d", len(filter.And.PropertyIsGreaterThan))
		}
	})

	t.Run("ValidOr", func(t *testing.T) {
		xml := `<Filter xmlns="http://www.opengis.net/fes/2.0">
			<Or>
				<PropertyIsEqualTo>
					<ValueReference>type</ValueReference>
					<Literal>A</Literal>
				</PropertyIsEqualTo>
				<PropertyIsEqualTo>
					<ValueReference>type</ValueReference>
					<Literal>B</Literal>
				</PropertyIsEqualTo>
			</Or>
		</Filter>`
		filter, err := ParseFESFilter(xml)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filter.Or == nil {
			t.Error("expected Or to be set")
		}
		if len(filter.Or.PropertyIsEqualTo) != 2 {
			t.Errorf("expected 2 PropertyIsEqualTo, got %d", len(filter.Or.PropertyIsEqualTo))
		}
	})

	t.Run("ValidNot", func(t *testing.T) {
		xml := `<Filter xmlns="http://www.opengis.net/fes/2.0">
			<Not>
				<PropertyIsEqualTo>
					<ValueReference>status</ValueReference>
					<Literal>deleted</Literal>
				</PropertyIsEqualTo>
			</Not>
		</Filter>`
		filter, err := ParseFESFilter(xml)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filter.Not == nil {
			t.Error("expected Not to be set")
		}
		if filter.Not.PropertyIsEqualTo == nil {
			t.Error("expected PropertyIsEqualTo inside Not")
		}
	})

	t.Run("ValidPropertyIsLike", func(t *testing.T) {
		xml := `<Filter xmlns="http://www.opengis.net/fes/2.0">
			<PropertyIsLike wildCard="*" singleChar="?" escapeChar="\">
				<ValueReference>name</ValueReference>
				<Literal>Test*</Literal>
			</PropertyIsLike>
		</Filter>`
		filter, err := ParseFESFilter(xml)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filter.PropertyIsLike == nil {
			t.Error("expected PropertyIsLike to be set")
		}
		if filter.PropertyIsLike.WildCard != "*" {
			t.Errorf("WildCard = %q, want %q", filter.PropertyIsLike.WildCard, "*")
		}
	})

	t.Run("ValidPropertyIsNull", func(t *testing.T) {
		xml := `<Filter xmlns="http://www.opengis.net/fes/2.0">
			<PropertyIsNull>
				<ValueReference>description</ValueReference>
			</PropertyIsNull>
		</Filter>`
		filter, err := ParseFESFilter(xml)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filter.PropertyIsNull == nil {
			t.Error("expected PropertyIsNull to be set")
		}
	})

	t.Run("ValidPropertyIsBetween", func(t *testing.T) {
		xml := `<Filter xmlns="http://www.opengis.net/fes/2.0">
			<PropertyIsBetween>
				<ValueReference>value</ValueReference>
				<LowerBoundary><Literal>10</Literal></LowerBoundary>
				<UpperBoundary><Literal>100</Literal></UpperBoundary>
			</PropertyIsBetween>
		</Filter>`
		filter, err := ParseFESFilter(xml)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filter.PropertyIsBetween == nil {
			t.Error("expected PropertyIsBetween to be set")
		}
		if filter.PropertyIsBetween.LowerBoundary.Literal != "10" {
			t.Errorf("LowerBoundary = %q, want %q", filter.PropertyIsBetween.LowerBoundary.Literal, "10")
		}
	})

	t.Run("ValidBBOX", func(t *testing.T) {
		xml := `<Filter xmlns="http://www.opengis.net/fes/2.0">
			<BBOX>
				<ValueReference>geom</ValueReference>
				<Envelope srsName="EPSG:4326">
					<lowerCorner>-180 -90</lowerCorner>
					<upperCorner>180 90</upperCorner>
				</Envelope>
			</BBOX>
		</Filter>`
		filter, err := ParseFESFilter(xml)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filter.BBOX == nil {
			t.Error("expected BBOX to be set")
		}
		if filter.BBOX.Envelope.SrsName != "EPSG:4326" {
			t.Errorf("SrsName = %q, want %q", filter.BBOX.Envelope.SrsName, "EPSG:4326")
		}
	})

	t.Run("ValidResourceId", func(t *testing.T) {
		xml := `<Filter xmlns="http://www.opengis.net/fes/2.0">
			<ResourceId rid="places.1"/>
			<ResourceId rid="places.2"/>
		</Filter>`
		filter, err := ParseFESFilter(xml)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(filter.ResourceId) != 2 {
			t.Errorf("expected 2 ResourceId, got %d", len(filter.ResourceId))
		}
	})

	t.Run("InvalidXML", func(t *testing.T) {
		xml := `<Filter><NotClosed>`
		_, err := ParseFESFilter(xml)
		if err == nil {
			t.Error("expected error for invalid XML")
		}
	})

	t.Run("WrongRootElement", func(t *testing.T) {
		xml := `<NotAFilter><Something/></NotAFilter>`
		_, err := ParseFESFilter(xml)
		if err == nil {
			t.Error("expected error for wrong root element")
		}
		if !strings.Contains(err.Error(), "expected element type <Filter>") {
			t.Errorf("error should mention expected Filter element: %v", err)
		}
	})
}

// ============================================================================
// CompileFES Tests
// ============================================================================

func TestCompileFES(t *testing.T) {
	opts := FESCompileOptions{
		StartParamIndex:   1,
		SourceSRID:        4326,
		GeometryProperty:  "geom",
		AllowedProperties: map[string]struct{}{"name": {}, "status": {}, "count": {}, "value": {}},
	}

	t.Run("PropertyIsEqualTo", func(t *testing.T) {
		xml := `<Filter><PropertyIsEqualTo><ValueReference>name</ValueReference><Literal>Test</Literal></PropertyIsEqualTo></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, args, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, `t."name" = $1`) {
			t.Errorf("SQL = %q, expected to contain name = $1", sql)
		}
		if len(args) != 1 || args[0] != "Test" {
			t.Errorf("args = %v, expected [Test]", args)
		}
	})

	t.Run("PropertyIsNotEqualTo", func(t *testing.T) {
		xml := `<Filter><PropertyIsNotEqualTo><ValueReference>status</ValueReference><Literal>deleted</Literal></PropertyIsNotEqualTo></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, `t."status" <> $1`) {
			t.Errorf("SQL = %q, expected to contain status <> $1", sql)
		}
	})

	t.Run("GMLIdentifierComparison", func(t *testing.T) {
		xml := `<Filter><PropertyIsEqualTo><ValueReference>@gml:id</ValueReference><Literal>places.42</Literal></PropertyIsEqualTo></Filter>`
		filter, _ := ParseFESFilter(xml)
		optsWithID := opts
		optsWithID.CollectionID = "cite:places"
		optsWithID.IDColumn = "feature_id"
		sql, args, _, err := CompileFES(filter, optsWithID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, `t."feature_id"::text = $1`) {
			t.Fatalf("SQL = %q", sql)
		}
		if len(args) != 1 || args[0] != "42" {
			t.Fatalf("args = %v", args)
		}
	})

	t.Run("PropertyIsLessThan", func(t *testing.T) {
		xml := `<Filter><PropertyIsLessThan><ValueReference>count</ValueReference><Literal>100</Literal></PropertyIsLessThan></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, `t."count" < $1`) {
			t.Errorf("SQL = %q, expected to contain count < $1", sql)
		}
	})

	t.Run("PropertyIsGreaterThan", func(t *testing.T) {
		xml := `<Filter><PropertyIsGreaterThan><ValueReference>count</ValueReference><Literal>10</Literal></PropertyIsGreaterThan></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, `t."count" > $1`) {
			t.Errorf("SQL = %q, expected to contain count > $1", sql)
		}
	})

	t.Run("PropertyIsLessThanOrEqualTo", func(t *testing.T) {
		xml := `<Filter><PropertyIsLessThanOrEqualTo><ValueReference>count</ValueReference><Literal>50</Literal></PropertyIsLessThanOrEqualTo></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, `t."count" <= $1`) {
			t.Errorf("SQL = %q, expected to contain count <= $1", sql)
		}
	})

	t.Run("PropertyIsGreaterThanOrEqualTo", func(t *testing.T) {
		xml := `<Filter><PropertyIsGreaterThanOrEqualTo><ValueReference>count</ValueReference><Literal>5</Literal></PropertyIsGreaterThanOrEqualTo></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, `t."count" >= $1`) {
			t.Errorf("SQL = %q, expected to contain count >= $1", sql)
		}
	})

	t.Run("PropertyIsLike", func(t *testing.T) {
		xml := `<Filter><PropertyIsLike wildCard="*" singleChar="?"><ValueReference>name</ValueReference><Literal>Test*</Literal></PropertyIsLike></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, args, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "LIKE") {
			t.Errorf("SQL = %q, expected to contain LIKE", sql)
		}
		if args[0] != "Test%" {
			t.Errorf("args[0] = %v, expected Test%%", args[0])
		}
	})

	t.Run("PropertyIsLikeCaseInsensitive", func(t *testing.T) {
		xml := `<Filter><PropertyIsLike wildCard="*" singleChar="?" matchCase="false"><ValueReference>name</ValueReference><Literal>Test*</Literal></PropertyIsLike></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "ILIKE") {
			t.Errorf("SQL = %q, expected to contain ILIKE", sql)
		}
	})

	t.Run("PropertyIsNull", func(t *testing.T) {
		xml := `<Filter><PropertyIsNull><ValueReference>name</ValueReference></PropertyIsNull></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, `t."name" IS NULL`) {
			t.Errorf("SQL = %q, expected to contain IS NULL", sql)
		}
	})

	t.Run("PropertyIsNil", func(t *testing.T) {
		xml := `<Filter><PropertyIsNil><ValueReference>name</ValueReference></PropertyIsNil></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, `t."name" IS NULL`) {
			t.Errorf("SQL = %q, expected to contain IS NULL", sql)
		}
	})

	t.Run("PropertyIsBetween", func(t *testing.T) {
		xml := `<Filter><PropertyIsBetween><ValueReference>value</ValueReference><LowerBoundary><Literal>10</Literal></LowerBoundary><UpperBoundary><Literal>100</Literal></UpperBoundary></PropertyIsBetween></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, args, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "BETWEEN") {
			t.Errorf("SQL = %q, expected to contain BETWEEN", sql)
		}
		if len(args) != 2 {
			t.Errorf("expected 2 args, got %d", len(args))
		}
	})

	t.Run("And", func(t *testing.T) {
		xml := `<Filter><And><PropertyIsEqualTo><ValueReference>status</ValueReference><Literal>active</Literal></PropertyIsEqualTo><PropertyIsGreaterThan><ValueReference>count</ValueReference><Literal>10</Literal></PropertyIsGreaterThan></And></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, " AND ") {
			t.Errorf("SQL = %q, expected to contain AND", sql)
		}
	})

	t.Run("Or", func(t *testing.T) {
		xml := `<Filter><Or><PropertyIsEqualTo><ValueReference>status</ValueReference><Literal>A</Literal></PropertyIsEqualTo><PropertyIsEqualTo><ValueReference>status</ValueReference><Literal>B</Literal></PropertyIsEqualTo></Or></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, " OR ") {
			t.Errorf("SQL = %q, expected to contain OR", sql)
		}
	})

	t.Run("Not", func(t *testing.T) {
		xml := `<Filter><Not><PropertyIsEqualTo><ValueReference>status</ValueReference><Literal>deleted</Literal></PropertyIsEqualTo></Not></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "NOT") {
			t.Errorf("SQL = %q, expected to contain NOT", sql)
		}
	})

	t.Run("BBOX", func(t *testing.T) {
		xml := `<Filter><BBOX><Envelope srsName="EPSG:4326"><lowerCorner>-10 -10</lowerCorner><upperCorner>10 10</upperCorner></Envelope></BBOX></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "ST_Intersects") && !strings.Contains(sql, "ST_MakeEnvelope") {
			t.Errorf("SQL = %q, expected spatial query", sql)
		}
	})

	t.Run("ResourceId", func(t *testing.T) {
		xml := `<Filter><ResourceId rid="places.1"/><ResourceId rid="places.2"/></Filter>`
		filter, _ := ParseFESFilter(xml)
		optsWithCollection := opts
		optsWithCollection.CollectionID = "places"
		sql, _, _, err := CompileFES(filter, optsWithCollection)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, `t."id"::text IN`) {
			t.Errorf("SQL = %q, expected id IN query", sql)
		}
	})

	t.Run("ResourceIdTypeMismatch", func(t *testing.T) {
		// ResourceId refers to NamedPlaces but we're querying Buildings - should return InvalidParameterValue error per WFS 2.0 spec
		xml := `<Filter xmlns="http://www.opengis.net/fes/2.0"><ResourceId rid="NamedPlaces.1"/></Filter>`
		filter, err := ParseFESFilter(xml)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		optsWithCollection := opts
		optsWithCollection.CollectionID = "Buildings"
		_, _, _, err = CompileFES(filter, optsWithCollection)
		// Per WFS 2.0 spec, ResourceId type mismatch should return an error
		if err == nil {
			t.Error("expected error for ResourceId type mismatch")
		}
		// Check it's an InvalidParameterValue error
		if reqErr, ok := err.(*RequestError); ok {
			if reqErr.Code != ExceptionInvalidParameterValue {
				t.Errorf("expected InvalidParameterValue, got %s", reqErr.Code)
			}
		} else {
			t.Errorf("expected *RequestError, got %T", err)
		}
	})

	t.Run("UnknownProperty", func(t *testing.T) {
		xml := `<Filter><PropertyIsEqualTo><ValueReference>unknown_prop</ValueReference><Literal>value</Literal></PropertyIsEqualTo></Filter>`
		filter, _ := ParseFESFilter(xml)
		_, _, _, err := CompileFES(filter, opts)
		if err == nil {
			t.Error("expected error for unknown property")
		}
	})

	t.Run("EmptyFilter", func(t *testing.T) {
		xml := `<Filter></Filter>`
		if _, err := ParseFESFilter(xml); err == nil {
			t.Fatal("empty filter must not become an unrestricted query")
		}
	})
}

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestStripNSPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"tns:geom", "geom"},
		{"gml:name", "name"},
		{"fes:PropertyIsEqualTo", "PropertyIsEqualTo"},
		{"geom", "geom"},
		{"", ""},
		{"a:b:c", "c"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripNSPrefix(tt.input)
			if got != tt.expected {
				t.Errorf("stripNSPrefix(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"name", `"name"`},
		{"geom", `"geom"`},
		{`col"name`, `"col""name"`},
		{"", `""`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := quoteIdent(tt.input)
			if got != tt.expected {
				t.Errorf("quoteIdent(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestValidateNumericCoords(t *testing.T) {
	tests := []struct {
		name     string
		coords   []string
		expected bool
	}{
		{"Valid integers", []string{"10", "20"}, true},
		{"Valid floats", []string{"10.5", "20.7"}, true},
		{"Valid negative", []string{"-10", "-20.5"}, true},
		{"Invalid string", []string{"abc", "20"}, false},
		{"Empty string", []string{"", "20"}, false},
		{"Mixed valid/invalid", []string{"10", "xyz"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateNumericCoords(tt.coords)
			if got != tt.expected {
				t.Errorf("validateNumericCoords(%v) = %v, want %v", tt.coords, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// Spatial Operator Tests
// ============================================================================

func TestCompileFESSpatialOperators(t *testing.T) {
	opts := FESCompileOptions{
		StartParamIndex:   1,
		SourceSRID:        4326,
		GeometryProperty:  "geom",
		AllowedProperties: map[string]struct{}{"name": {}},
	}

	t.Run("Intersects with Point", func(t *testing.T) {
		xml := `<Filter><Intersects><ValueReference>geom</ValueReference><Point srsName="EPSG:4326"><pos>10 20</pos></Point></Intersects></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "ST_Intersects") {
			t.Errorf("SQL = %q, expected to contain ST_Intersects", sql)
		}
	})

	t.Run("Within with Polygon", func(t *testing.T) {
		xml := `<Filter><Within><ValueReference>geom</ValueReference><Polygon srsName="EPSG:4326"><exterior><LinearRing><posList>0 0 10 0 10 10 0 10 0 0</posList></LinearRing></exterior></Polygon></Within></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "ST_Within") {
			t.Errorf("SQL = %q, expected to contain ST_Within", sql)
		}
	})

	t.Run("Contains", func(t *testing.T) {
		xml := `<Filter><Contains><ValueReference>geom</ValueReference><Point srsName="EPSG:4326"><pos>5 5</pos></Point></Contains></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "ST_Contains") {
			t.Errorf("SQL = %q, expected to contain ST_Contains", sql)
		}
	})

	t.Run("Disjoint", func(t *testing.T) {
		xml := `<Filter><Disjoint><ValueReference>geom</ValueReference><Point srsName="EPSG:4326"><pos>100 100</pos></Point></Disjoint></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "ST_Disjoint") {
			t.Errorf("SQL = %q, expected to contain ST_Disjoint", sql)
		}
	})

	t.Run("Touches", func(t *testing.T) {
		xml := `<Filter><Touches><ValueReference>geom</ValueReference><Point srsName="EPSG:4326"><pos>0 0</pos></Point></Touches></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "ST_Touches") {
			t.Errorf("SQL = %q, expected to contain ST_Touches", sql)
		}
	})

	t.Run("Crosses with LineString", func(t *testing.T) {
		xml := `<Filter><Crosses><ValueReference>geom</ValueReference><LineString srsName="EPSG:4326"><posList>0 0 10 10</posList></LineString></Crosses></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "ST_Crosses") {
			t.Errorf("SQL = %q, expected to contain ST_Crosses", sql)
		}
	})

	t.Run("Overlaps", func(t *testing.T) {
		xml := `<Filter><Overlaps><ValueReference>geom</ValueReference><Polygon srsName="EPSG:4326"><exterior><LinearRing><posList>5 5 15 5 15 15 5 15 5 5</posList></LinearRing></exterior></Polygon></Overlaps></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "ST_Overlaps") {
			t.Errorf("SQL = %q, expected to contain ST_Overlaps", sql)
		}
	})

	t.Run("DWithin", func(t *testing.T) {
		xml := `<Filter><DWithin><ValueReference>geom</ValueReference><Point srsName="EPSG:4326"><pos>10 20</pos></Point><Distance uom="m">1000</Distance></DWithin></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "ST_DWithin") {
			t.Errorf("SQL = %q, expected to contain ST_DWithin", sql)
		}
	})

	t.Run("InvalidNonGeometryValueReference", func(t *testing.T) {
		xml := `<Filter><Intersects><ValueReference>name</ValueReference><Point srsName="EPSG:4326"><pos>10 20</pos></Point></Intersects></Filter>`
		filter, _ := ParseFESFilter(xml)
		_, _, _, err := CompileFES(filter, opts)
		if err == nil {
			t.Error("expected error for non-geometry value reference in spatial operator")
		}
	})
}

// ============================================================================
// GML Property Handling Tests
// ============================================================================

func TestGMLPropertyHandling(t *testing.T) {
	opts := FESCompileOptions{
		StartParamIndex:   1,
		SourceSRID:        4326,
		GeometryProperty:  "geom",
		AllowedProperties: map[string]struct{}{"status": {}},
	}

	t.Run("GMLNameNotMapped", func(t *testing.T) {
		// gml:name is a standard property but not in AllowedProperties
		xml := `<Filter><PropertyIsEqualTo><ValueReference>gml:name</ValueReference><Literal>Test</Literal></PropertyIsEqualTo></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sql != "(FALSE)" {
			t.Errorf("SQL = %q, expected (FALSE) for unmapped GML property comparison", sql)
		}
	})

	t.Run("GMLNameNullWhenNotMapped", func(t *testing.T) {
		xml := `<Filter><PropertyIsNull><ValueReference>gml:name</ValueReference></PropertyIsNull></Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sql != "(TRUE)" {
			t.Errorf("SQL = %q, expected (TRUE) for unmapped GML property IS NULL", sql)
		}
	})

	t.Run("GeometryPropertyInComparison", func(t *testing.T) {
		// Using geometry column in comparison should fail
		xml := `<Filter><PropertyIsEqualTo><ValueReference>geom</ValueReference><Literal>POINT(0 0)</Literal></PropertyIsEqualTo></Filter>`
		filter, _ := ParseFESFilter(xml)
		_, _, _, err := CompileFES(filter, opts)
		if err == nil {
			t.Error("expected error for geometry property in comparison operator")
		}
	})
}

// ============================================================================
// Complex Nested Filter Tests
// ============================================================================

func TestNestedFilters(t *testing.T) {
	opts := FESCompileOptions{
		StartParamIndex:   1,
		SourceSRID:        4326,
		GeometryProperty:  "geom",
		AllowedProperties: map[string]struct{}{"name": {}, "status": {}, "count": {}},
	}

	t.Run("NestedAndOr", func(t *testing.T) {
		xml := `<Filter>
			<And>
				<PropertyIsEqualTo><ValueReference>status</ValueReference><Literal>active</Literal></PropertyIsEqualTo>
				<Or>
					<PropertyIsEqualTo><ValueReference>name</ValueReference><Literal>A</Literal></PropertyIsEqualTo>
					<PropertyIsEqualTo><ValueReference>name</ValueReference><Literal>B</Literal></PropertyIsEqualTo>
				</Or>
			</And>
		</Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "AND") || !strings.Contains(sql, "OR") {
			t.Errorf("SQL = %q, expected nested AND/OR", sql)
		}
	})

	t.Run("NotWithAnd", func(t *testing.T) {
		xml := `<Filter>
			<Not>
				<And>
					<PropertyIsEqualTo><ValueReference>status</ValueReference><Literal>deleted</Literal></PropertyIsEqualTo>
					<PropertyIsGreaterThan><ValueReference>count</ValueReference><Literal>10</Literal></PropertyIsGreaterThan>
				</And>
			</Not>
		</Filter>`
		filter, _ := ParseFESFilter(xml)
		sql, _, _, err := CompileFES(filter, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(sql, "NOT") {
			t.Errorf("SQL = %q, expected NOT", sql)
		}
	})
}

func TestResourceIdTypeMismatchFullFlow(t *testing.T) {
	// This simulates the exact test case from inconsistentFeatureIdentifierAndType
	// Query type: LakesWithElevation
	// ResourceId: NamedPlaces.1 (wrong type)
	// Per WFS 2.0 spec, this should return InvalidParameterValue error

	// First, extract filter from raw XML (like ParseGetFeatureRequest does)
	rawQueryXML := `
      <Filter xmlns="http://www.opengis.net/fes/2.0">
         <ResourceId rid="NamedPlaces.1"/>
      </Filter>
   `
	filterXML, extractErr := extractFilterFromXML(rawQueryXML)
	if extractErr != nil {
		t.Fatal(extractErr)
	}
	t.Logf("Extracted filter: %q", filterXML)

	if filterXML == "" {
		t.Fatal("filter extraction failed")
	}

	// Parse the FES filter
	fesFilter, err := ParseFESFilter(filterXML)
	if err != nil {
		t.Fatalf("ParseFESFilter error: %v", err)
	}

	t.Logf("Parsed FES filter: ResourceIds=%+v", fesFilter.ResourceId)

	// Compile with LakesWithElevation as the collection (type mismatch with NamedPlaces)
	opts := FESCompileOptions{
		StartParamIndex:   1,
		SourceSRID:        4326,
		GeometryProperty:  "geom",
		AllowedProperties: map[string]struct{}{"name": {}, "elev": {}},
		CollectionID:      "LakesWithElevation",
	}

	_, _, _, err = CompileFES(fesFilter, opts)

	// Per WFS 2.0 spec, ResourceId type mismatch should return InvalidParameterValue error
	if err == nil {
		t.Fatal("Expected error for type mismatch, got nil")
	}

	// Check it's an InvalidParameterValue error
	if reqErr, ok := err.(*RequestError); ok {
		if reqErr.Code != ExceptionInvalidParameterValue {
			t.Errorf("expected InvalidParameterValue, got %s", reqErr.Code)
		}
		t.Logf("Got expected error: %s - %s", reqErr.Code, reqErr.Message)
	} else {
		t.Errorf("expected *RequestError, got %T: %v", err, err)
	}
}
