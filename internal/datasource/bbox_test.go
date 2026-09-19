package datasource

import "testing"

func TestBBoxPartsSplitsCRS84Antimeridian(t *testing.T) {
	bbox := BBox{MinX: 177, MinY: 65, MaxX: -177, MaxY: 70}
	parts := bbox.Parts(4326)
	if len(parts) != 2 {
		t.Fatalf("Parts() returned %d envelopes, want 2", len(parts))
	}
	if parts[0].MinX != 177 || parts[0].MaxX != 180 || parts[1].MinX != -180 || parts[1].MaxX != -177 {
		t.Fatalf("Parts() = %+v", parts)
	}
	if got := bbox.Parts(3857); len(got) != 1 || got[0] != bbox {
		t.Fatalf("projected Parts() = %+v, want unsplit bbox", got)
	}
}
