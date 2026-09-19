package wfs

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
)

func TestFESAuthorityAxisOrderAcrossDialects(t *testing.T) {
	for _, dialect := range []datasource.SQLDialect{datasource.SQLPostGIS, datasource.SQLDuckDB} {
		for _, tc := range []struct {
			crs  string
			srid int
			swap bool
		}{
			{"EPSG:4326", 4326, false},
			{"CRS:84", 4326, false},
			{"urn:ogc:def:crs:OGC:1.3:CRS84", 4326, false},
			{"urn:ogc:def:crs:EPSG::4326", 4326, true},
			{"http://www.opengis.net/def/crs/EPSG/0/4326", 4326, true},
			{"https://www.opengis.net/def/crs/EPSG/0/4326", 4326, true},
			{"urn:ogc:def:crs:EPSG::3857", 3857, false},
			{"urn:ogc:def:crs:EPSG::3035", 3035, true},
		} {
			t.Run(fmt.Sprintf("%s/%s", dialect, tc.crs), func(t *testing.T) {
				point, line, ring, lower, upper := "7 51", "7 51 8 52", "7 51 8 52 8 51", "6 50", "8 52"
				if tc.swap {
					point, line, ring, lower, upper = "51 7", "51 7 52 8", "51 7 52 8 51 8", "50 6", "52 8"
				}
				opts := FESCompileOptions{Dialect: dialect, SourceSRID: tc.srid, GeometryProperty: "geom"}
				kvp, srid, err := parseBBox(lower + " " + upper + " " + tc.crs)
				if err != nil || srid != tc.srid || kvp.MinX != 6 || kvp.MinY != 50 || kvp.MaxX != 8 || kvp.MaxY != 52 {
					t.Fatalf("KVP bounds = %+v, SRID = %d, error = %v", kvp, srid, err)
				}
				bbox := &FESFilter{BBOX: &FESBBOX{Envelope: FESEnvelope{SrsName: tc.crs, LowerCorner: lower, UpperCorner: upper}}}
				_, args, _, err := CompileFES(bbox, opts)
				if err != nil || !reflect.DeepEqual(args, []interface{}{float64(6), float64(50), float64(8), float64(52)}) {
					t.Fatalf("BBOX args = %v, error = %v", args, err)
				}
				for _, shape := range []struct {
					geometry *FESSpatial
					wkt      string
				}{
					{&FESSpatial{Point: &FESPoint{SrsName: tc.crs, Pos: point}}, "POINT(7 51)"},
					{&FESSpatial{LineString: &FESLineString{SrsName: tc.crs, PosList: line}}, "LINESTRING(7 51, 8 52)"},
					{&FESSpatial{Polygon: &FESPolygon{SrsName: tc.crs, Exterior: FESPolygonExterior{LinearRing: FESLinearRing{PosList: ring}}}}, "POLYGON((7 51, 8 52, 8 51, 7 51))"},
					{&FESSpatial{Envelope: &FESEnvelope{SrsName: tc.crs, LowerCorner: lower, UpperCorner: upper}}, "POLYGON((6 50, 8 50, 8 52, 6 52, 6 50))"},
				} {
					for attempt := 0; attempt < 2; attempt++ {
						_, args, _, err := CompileFES(&FESFilter{Intersects: shape.geometry}, opts)
						if err != nil || !reflect.DeepEqual(args, []interface{}{shape.wkt}) {
							t.Fatalf("geometry args = %v, want %s, error = %v", args, shape.wkt, err)
						}
					}
				}
				_, args, _, err = CompileFES(&FESFilter{DWithin: &FESDWithin{
					Point: &FESPoint{SrsName: tc.crs, Pos: point}, Distance: FESDistance{Value: "1"},
				}}, opts)
				if err != nil || !reflect.DeepEqual(args, []interface{}{"POINT(7 51)", "1"}) {
					t.Fatalf("DWithin args = %v, error = %v", args, err)
				}
			})
		}
	}
}

func TestFESRejectsInvalidExplicitCRS(t *testing.T) {
	for _, filter := range []*FESFilter{
		{BBOX: &FESBBOX{Envelope: FESEnvelope{SrsName: "not-a-crs", LowerCorner: "6 50", UpperCorner: "8 52"}}},
		{Intersects: &FESSpatial{Point: &FESPoint{SrsName: "not-a-crs", Pos: "7 51"}}},
	} {
		if _, _, _, err := CompileFES(filter, FESCompileOptions{GeometryProperty: "geom"}); err == nil {
			t.Fatal("invalid explicit CRS silently fell back to the layer CRS")
		}
	}
}
