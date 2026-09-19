package query

import "testing"

func TestParseCRS(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{in: CRS84URI, want: 4326},
		{in: "CRS84", want: 4326},
		{in: "EPSG:4326", want: 4326},
		{in: "http://www.opengis.net/def/crs/EPSG/0/3857", want: 3857},
		{in: "3857", want: 3857},
	}
	for _, tt := range tests {
		got, err := ParseCRS(tt.in)
		if err != nil {
			t.Fatalf("ParseCRS(%q) error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("ParseCRS(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestCRSURIFromSRID(t *testing.T) {
	if got := CRSURIFromSRID(4326); got != CRS84URI {
		t.Fatalf("CRSURIFromSRID(4326) = %q, want %q", got, CRS84URI)
	}
	if got := CRSURIFromSRID(3857); got == "" {
		t.Fatalf("CRSURIFromSRID(3857) should not be empty")
	}
}


