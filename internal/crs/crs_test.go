package crs

import (
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		crs     string
		want    int
		wantErr bool
	}{
		// EPSG format
		{"EPSG:4326", "EPSG:4326", 4326, false},
		{"EPSG:3857", "EPSG:3857", 3857, false},
		{"epsg:4326 lowercase", "epsg:4326", 4326, false},
		{"EPSG with spaces", "  EPSG:4326  ", 4326, false},

		// CRS:84 / OGC:CRS84
		{"CRS:84", "CRS:84", 4326, false},
		{"OGC:CRS84", "OGC:CRS84", 4326, false},
		{"crs:84 lowercase", "crs:84", 4326, false},

		// URN format
		{"URN with double colon", "urn:ogc:def:crs:EPSG::4326", 4326, false},
		{"URN with 0", "urn:ogc:def:crs:EPSG:0:4326", 4326, false},
		{"URN uppercase", "URN:OGC:DEF:CRS:EPSG::3857", 3857, false},

		// HTTP URI format
		{"HTTP URI", "http://www.opengis.net/def/crs/EPSG/0/4326", 4326, false},
		{"HTTP URI 3857", "http://www.opengis.net/def/crs/EPSG/0/3857", 3857, false},

		// Errors
		{"empty string", "", 0, true},
		{"invalid EPSG", "EPSG:abc", 0, true},
		{"unsupported format", "unknown:4326", 0, true},
		{"invalid URN", "urn:invalid:format", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.crs)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", tt.crs, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %v, want %v", tt.crs, got, tt.want)
			}
		})
	}
}

func TestMustParse(t *testing.T) {
	tests := []struct {
		name string
		crs  string
		want int
	}{
		{"valid EPSG", "EPSG:4326", 4326},
		{"invalid returns 0", "invalid", 0},
		{"empty returns 0", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MustParse(tt.crs); got != tt.want {
				t.Errorf("MustParse(%q) = %v, want %v", tt.crs, got, tt.want)
			}
		})
	}
}

func TestToURN(t *testing.T) {
	tests := []struct {
		srid int
		want string
	}{
		{4326, "urn:ogc:def:crs:EPSG::4326"},
		{3857, "urn:ogc:def:crs:EPSG::3857"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := ToURN(tt.srid); got != tt.want {
				t.Errorf("ToURN(%d) = %v, want %v", tt.srid, got, tt.want)
			}
		})
	}
}

func TestToEPSG(t *testing.T) {
	tests := []struct {
		srid int
		want string
	}{
		{4326, "EPSG:4326"},
		{3857, "EPSG:3857"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := ToEPSG(tt.srid); got != tt.want {
				t.Errorf("ToEPSG(%d) = %v, want %v", tt.srid, got, tt.want)
			}
		})
	}
}

func TestToHTTPURI(t *testing.T) {
	tests := []struct {
		srid int
		want string
	}{
		{4326, "http://www.opengis.net/def/crs/EPSG/0/4326"},
		{3857, "http://www.opengis.net/def/crs/EPSG/0/3857"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := ToHTTPURI(tt.srid); got != tt.want {
				t.Errorf("ToHTTPURI(%d) = %v, want %v", tt.srid, got, tt.want)
			}
		})
	}
}

func TestIsLatLonOrder(t *testing.T) {
	tests := []struct {
		name string
		crs  string
		want bool
	}{
		{"CRS:84 is lon/lat", "CRS:84", false},
		{"OGC:CRS84 is lon/lat", "OGC:CRS84", false},
		{"EPSG:4326 is lat/lon", "EPSG:4326", true},
		{"URN 4326 is lat/lon", "urn:ogc:def:crs:EPSG::4326", true},
		{"EPSG:3857 is not lat/lon", "EPSG:3857", false},
		{"invalid is false", "invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLatLonOrder(tt.crs); got != tt.want {
				t.Errorf("IsLatLonOrder(%q) = %v, want %v", tt.crs, got, tt.want)
			}
		})
	}
}
