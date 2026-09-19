package filter

import "testing"

func TestCompile_ParameterizedStrings(t *testing.T) {
	sql, args, next, err := Compile("name = 'bob' AND age >= 21", Options{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"name": {},
			"age":  {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if next != 3 {
		t.Fatalf("next=%d, want 3", next)
	}
	if len(args) != 2 {
		t.Fatalf("args=%v", args)
	}
	if sql == "" {
		t.Fatalf("expected SQL")
	}
	if sql != `("name" = $1 AND ("age" >= $2))` && sql != `(("name" = $1) AND ("age" >= $2))` {
		// allow minor paren differences, but ensure placeholders exist
		if !(contains(sql, "$1") && contains(sql, "$2")) {
			t.Fatalf("unexpected sql: %s", sql)
		}
	}
}

func TestCompile_CQL2BasicSpatialNamesAndWrappedBBox(t *testing.T) {
	sql, args, _, err := Compile("S_INTERSECTS(geom, BBOX(170, -10, -170, 10))", Options{
		StartParamIndex: 1, FilterSRID: 4326, SourceSRID: 4326,
		AllowedProperties: map[string]struct{}{}, GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if !contains(sql, "ST_Intersects") {
		t.Fatalf("expected ST_Intersects, got %s", sql)
	}
	if len(args) != 1 || !contains(args[0].(string), "MULTIPOLYGON") {
		t.Fatalf("expected wrapped BBOX multipolygon, args=%v", args)
	}
}

func TestCompile_RejectsInvalidTrailingInput(t *testing.T) {
	_, _, _, err := Compile("name = 'ok' @ ignored", Options{AllowedProperties: map[string]struct{}{"name": {}}})
	if err == nil {
		t.Fatal("expected invalid trailing input to be rejected")
	}
}

func TestCompile_SpatialPointHasSpaceBetweenNumbers(t *testing.T) {
	sql, args, _, err := Compile("INTERSECTS(geom, POINT(1 2))", Options{
		StartParamIndex: 1,
		FilterSRID:      4326,
		SourceSRID:      4326,
		AllowedProperties: map[string]struct{}{
			"geom": {},
		},
		GeometryProperty: "geom",
	})
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}
	if len(args) != 1 {
		t.Fatalf("args=%v", args)
	}
	ewkt, ok := args[0].(string)
	if !ok {
		t.Fatalf("expected ewkt string, got %T", args[0])
	}
	if ewkt != "SRID=4326;POINT(1 2)" {
		t.Fatalf("unexpected ewkt: %q", ewkt)
	}
	if !contains(sql, "ST_Intersects") || !contains(sql, "::geometry") {
		t.Fatalf("unexpected sql: %s", sql)
	}
}

func TestCompile_NilAllowlistDeniesProperties(t *testing.T) {
	// Fail closed: with no AllowedProperties set, any property reference must be
	// rejected rather than passed through to SQL.
	_, _, _, err := Compile("name = 'bob'", Options{
		StartParamIndex:  1,
		FilterSRID:       4326,
		SourceSRID:       4326,
		GeometryProperty: "geom",
	})
	if err == nil {
		t.Fatal("expected error for property reference with nil allowlist")
	}
}

func TestCompile_UnknownPropertyDenied(t *testing.T) {
	_, _, _, err := Compile("secret = 'x'", Options{
		StartParamIndex:   1,
		AllowedProperties: map[string]struct{}{"name": {}},
		GeometryProperty:  "geom",
	})
	if err == nil {
		t.Fatal("expected error for unknown property")
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	// tiny helper to avoid importing strings in tests
outer:
	for i := 0; i+len(sub) <= len(s); i++ {
		for j := range sub {
			if s[i+j] != sub[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}
