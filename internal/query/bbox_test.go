package query

import "testing"

func TestParseBBox(t *testing.T) {
	b, err := ParseBBox("1,2,3,4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.MinX != 1 || b.MinY != 2 || b.MaxX != 3 || b.MaxY != 4 {
		t.Fatalf("unexpected bbox: %+v", b)
	}

	if _, err := ParseBBox("1,2,3"); err == nil {
		t.Fatalf("expected error for 3-part bbox")
	}
	if _, err := ParseBBox("x,2,3,4"); err == nil {
		t.Fatalf("expected error for non-numeric bbox")
	}
	if got, err := ParseBBox("177,65,-177,70"); err != nil || got.MinX != 177 || got.MaxX != -177 {
		t.Fatalf("expected antimeridian bbox, got %+v, %v", got, err)
	}
	if _, err := ParseBBox("1,4,3,2"); err == nil {
		t.Fatalf("expected error for south>north")
	}
	if _, err := ParseBBox("NaN,2,3,4"); err == nil {
		t.Fatalf("expected error for non-finite bbox")
	}
}
