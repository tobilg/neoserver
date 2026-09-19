package datasource

import "testing"

func TestParseExtentWKT(t *testing.T) {
	extent, err := ParseExtentWKT("POLYGON ((-10.5 2, -10.5 9, 4.25 9, 4.25 2, -10.5 2))", 4326)
	if err != nil {
		t.Fatal(err)
	}
	if extent.MinX != -10.5 || extent.MinY != 2 || extent.MaxX != 4.25 || extent.MaxY != 9 || extent.SRID != 4326 {
		t.Fatalf("extent = %+v", extent)
	}
}

func TestParseExtentWKTScientificNotation(t *testing.T) {
	extent, err := ParseExtentWKT("POLYGON ((-1e2 2.5E+1, 3e2 4e1, -1e2 2.5E+1))", 3857)
	if err != nil {
		t.Fatal(err)
	}
	if extent.MinX != -100 || extent.MinY != 25 || extent.MaxX != 300 || extent.MaxY != 40 {
		t.Fatalf("extent = %+v", extent)
	}
}
