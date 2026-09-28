package wms

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/query"
)

func pdfTestImage(width, height int, fill color.NRGBA) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	return img
}

func TestEncodeGeoPDFStructure(t *testing.T) {
	for name, fill := range map[string]color.NRGBA{
		"opaque":      {R: 200, G: 40, B: 10, A: 255},
		"transparent": {R: 200, G: 40, B: 10, A: 128},
	} {
		t.Run(name, func(t *testing.T) {
			body, err := encodeGeoPDF(pdfTestImage(17, 11, fill), testMapRequest(FormatPDF), 0)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(body, []byte("%PDF-1.7\n")) || !bytes.HasSuffix(body, []byte("%%EOF\n")) {
				t.Fatal("missing PDF header or trailer")
			}
			for _, want := range []string{"/Subtype /GEO", "/GPTS [", "/GCS 7 0 R", "/Type /GEOGCS", "/EPSG 4326", "/Width 17 /Height 11"} {
				if !bytes.Contains(body, []byte(want)) {
					t.Errorf("PDF lacks %q", want)
				}
			}
			if hasSMask := bytes.Contains(body, []byte("/SMask")); hasSMask != (fill.A != 255) {
				t.Errorf("SMask present = %v for alpha %d", hasSMask, fill.A)
			}
			// Every cross-reference offset must point at its object header.
			start, err := strconv.Atoi(string(regexp.MustCompile(`startxref\n(\d+)`).FindSubmatch(body)[1]))
			if err != nil || !bytes.HasPrefix(body[start:], []byte("xref\n")) {
				t.Fatalf("startxref does not point at the xref table")
			}
			entries := regexp.MustCompile(`(\d{10}) 00000 n `).FindAllSubmatch(body[start:], -1)
			for index, entry := range entries {
				offset, _ := strconv.Atoi(string(entry[1]))
				if want := strconv.Itoa(index+1) + " 0 obj\n"; !bytes.HasPrefix(body[offset:], []byte(want)) {
					t.Errorf("xref entry %d points at %q", index+1, body[offset:offset+10])
				}
			}
		})
	}
}

func TestEncodeGeoPDFEnforcesOutputLimit(t *testing.T) {
	if _, err := encodeGeoPDF(pdfTestImage(17, 11, color.NRGBA{A: 255}), testMapRequest(FormatPDF), 100); err == nil {
		t.Fatal("expected the output limit to reject the PDF")
	}
}

func TestPDFLiteralsAndNumbers(t *testing.T) {
	if got := pdfString(`NAD83(HARN) \ x`); got != `(NAD83\(HARN\) \\ x)` {
		t.Errorf("pdfString = %s", got)
	}
	for value, want := range map[float64]string{1e-7: "0.0000001", -0.0: "0", 20037508.342789244: "20037508.342789244", 12: "12"} {
		if got := pdfNumber(value); got != want {
			t.Errorf("pdfNumber(%v) = %s, want %s", value, got, want)
		}
	}
}

// TestEncodeGeoPDFRoundTripsThroughGDAL reads the PDF back with GDAL's own PDF
// reader, which needs a read backend such as poppler. The container image
// omits it, so the test only runs where the native GDAL build has one.
func TestEncodeGeoPDFRoundTripsThroughGDAL(t *testing.T) {
	godal.RegisterAll()
	cases := []struct {
		name string
		srid int
		crs  string
		bbox query.BBox
	}{
		{"geographic", 4326, "EPSG:4326", query.BBox{MinX: 7, MinY: 50, MaxX: 10, MaxY: 52}},
		{"web mercator", 3857, "EPSG:3857", query.BBox{MinX: 779236.4, MinY: 6446275.8, MaxX: 1113194.9, MaxY: 6800125.5}},
		{"utm", 32632, "EPSG:32632", query.BBox{MinX: 400000, MinY: 5500000, MaxX: 430000, MaxY: 5520000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := testMapRequest(FormatPDF)
			req.SRID, req.CRS, req.BBox, req.Width, req.Height = tc.srid, tc.crs, tc.bbox, 40, 30
			body, err := encodeGeoPDF(pdfTestImage(40, 30, color.NRGBA{R: 200, G: 40, B: 10, A: 255}), req, 0)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "map.pdf")
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			ds, err := godal.Open(path, godal.RasterOnly(), godal.Drivers("PDF"), godal.DriverOpenOption("DPI=96"))
			if err != nil {
				t.Skipf("GDAL cannot read PDFs in this build: %v", err)
			}
			defer ds.Close()

			structure := ds.Structure()
			if structure.SizeX != 40 || structure.SizeY != 30 {
				t.Fatalf("size = %dx%d, want 40x30", structure.SizeX, structure.SizeY)
			}
			if code := ds.SpatialRef().AuthorityCode(""); code != strconv.Itoa(tc.srid) {
				t.Errorf("CRS authority code = %q, want %d", code, tc.srid)
			}
			gt, err := ds.GeoTransform()
			if err != nil {
				t.Fatal(err)
			}
			want := [6]float64{tc.bbox.MinX, (tc.bbox.MaxX - tc.bbox.MinX) / 40, 0, tc.bbox.MaxY, 0, -(tc.bbox.MaxY - tc.bbox.MinY) / 30}
			tolerance := (tc.bbox.MaxX - tc.bbox.MinX) * 1e-6
			for i := range want {
				if math.Abs(gt[i]-want[i]) > tolerance {
					t.Fatalf("geotransform = %v, want %v", gt, want)
				}
			}
			red := make([]uint8, 40*30)
			if err := ds.Bands()[0].Read(0, 0, red, 40, 30); err != nil {
				t.Fatal(err)
			}
			if red[40*15+20] != 200 {
				t.Errorf("red sample = %d, want 200", red[40*15+20])
			}
		})
	}
}
