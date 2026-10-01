package stacmodel

import "testing"

func TestTimestampRFC3339Grammar(t *testing.T) {
	for _, value := range []string{
		"1985-04-12T23:20:50,52Z", "2026-01-01T1:00:00Z",
		"2026-01-01T00:00:00+24:00", "2026-01-01T00:00:00+00:60",
		"2026-02-30T00:00:00Z", "2026-01-01T24:00:00Z",
	} {
		if _, err := Timestamp(value); err == nil {
			t.Errorf("accepted invalid timestamp %q", value)
		}
	}
	for _, value := range []string{
		"2026-01-01t00:00:00.123456789z", "2026-01-01T08:00:00.123456789+08:00",
		"2025-12-31T16:00:00.123456789-08:00",
	} {
		got, err := Timestamp(value)
		if err != nil || got != "2026-01-01T00:00:00.123456789Z" {
			t.Errorf("Timestamp(%q) = %q, %v", value, got, err)
		}
	}
}

func TestPinnedSchemasAndSemantics(t *testing.T) {
	if err := Ready(); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0.0", "1.1.0"} {
		c := Collection("scenes", "Scenes", "Scene footprints", "proprietary", nil)
		c["stac_version"] = version
		c["license"] = "other"
		if err := Validate(c, "collection"); err != nil {
			t.Fatal(err)
		}
		d, err := Decode([]byte(`{"type":"Feature","stac_version":"` + version + `","id":"scene-1","collection":"scenes","geometry":{"type":"Point","coordinates":[7,52]},"bbox":[7,52,7,52],"properties":{"datetime":"2025-01-01T00:00:00.123456789Z","custom:quality":100},"assets":{},"links":[{"rel":"collection","href":"https://example.org/collections/scenes"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if err = Validate(d, "item"); err != nil {
			t.Fatal(err)
		}
		d["geometry"] = nil
		delete(d, "bbox")
		if err = Validate(d, "item"); err != nil {
			t.Fatal(err)
		}
		d["bbox"] = []float64{0, 0, 1, 1}
		if err = Validate(d, "item"); err == nil {
			t.Fatal("null geometry with bbox accepted")
		}
	}
}
func TestTemporalPrecisionAndGeometryCollections(t *testing.T) {
	start, end, err := Interval("2025-01-01t00:00:00.123456789z/2025-01-01T01:00:00.123456790+01:00")
	if err != nil || start >= end {
		t.Fatalf("nanosecond/offset handling: %s %s %v", start, end, err)
	}
	g, err := Decode([]byte(`{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[7,52,100]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	bounds, err := GeometryBounds(g)
	if err != nil || bounds[2] != 100 {
		t.Fatalf("bounds %v: %v", bounds, err)
	}
	for _, v := range []string{"../secret", "file:///etc/passwd", "https://user:secret@example.org/data", "https://example.org/data?token=secret"} {
		if PublicURL(v) == nil {
			t.Errorf("accepted unsafe asset URL %s", v)
		}
	}
}

func TestGeneratedBBoxRetainsZeroElevation(t *testing.T) {
	g, _ := Decode([]byte(`{"type":"Point","coordinates":[1,2,0]}`))
	bbox, err := GeometryBBox(g)
	if err != nil || len(bbox) != 6 || bbox[2] != 0 || bbox[5] != 0 {
		t.Fatalf("3D bbox: %v %v", bbox, err)
	}
}
