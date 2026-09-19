package wms

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/gdalcap"
	"github.com/tobilg/neoserver/internal/query"
	"github.com/tobilg/neoserver/internal/renderer"
	_ "golang.org/x/image/tiff"
)

func testMapRenderer() *renderer.MapRenderer {
	return renderer.NewMapRenderer(renderer.NewTransform(query.BBox{MinX: 0, MinY: 0, MaxX: 1, MaxY: 1}, 17, 11), false, color.White)
}

func testMapRequest(format string) *GetMapRequest {
	return &GetMapRequest{
		Version: Version130, Layers: []string{"roads"}, Styles: []string{""},
		CRS: "CRS:84", SRID: 4326, BBox: query.BBox{MinX: 0, MinY: 0, MaxX: 1, MaxY: 1},
		Width: 17, Height: 11, Format: format, BgColor: color.RGBA{A: 255},
	}
}

func TestRasterGetMapFormatsEncodeAdvertisedDimensions(t *testing.T) {
	for _, format := range []string{FormatPNG, FormatPNG8, FormatJPEG, FormatGIF, FormatTIFF, FormatTIFF8} {
		t.Run(format, func(t *testing.T) {
			body, err := encodeMap(format, testMapRenderer())
			if err != nil {
				t.Fatalf("encodeMap: %v", err)
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
			if err != nil {
				t.Fatalf("decode %s: %v", format, err)
			}
			if cfg.Width != 17 || cfg.Height != 11 {
				t.Fatalf("dimensions = %dx%d", cfg.Width, cfg.Height)
			}
		})
	}
}

func TestDocumentGetMapFormatsHaveSemanticPayloads(t *testing.T) {
	for _, format := range []string{FormatSVG, FormatKML, FormatKMZ, FormatMapML} {
		t.Run(format, func(t *testing.T) {
			req := testMapRequest(format)
			body, err := encodeMapOutput(format, testMapRenderer(), mapEncodeContext{Request: req, BaseURL: "https://maps.example/workspaces/demo/wms"})
			if err != nil {
				t.Fatal(err)
			}
			switch format {
			case FormatKMZ:
				archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
				if err != nil {
					t.Fatal(err)
				}
				names := make([]string, 0, len(archive.File))
				for _, entry := range archive.File {
					names = append(names, entry.Name)
				}
				if !slices.Equal(names, []string{"doc.kml", "map.png"}) {
					t.Fatalf("KMZ entries = %v", names)
				}
			default:
				var root struct{ XMLName xml.Name }
				if err := xml.Unmarshal(body, &root); err != nil {
					t.Fatalf("decode XML: %v", err)
				}
				want := map[string]string{FormatSVG: "svg", FormatKML: "kml", FormatMapML: "mapml-"}[format]
				if root.XMLName.Local != want {
					t.Fatalf("root = %q, want %q", root.XMLName.Local, want)
				}
			}
		})
	}
}

func TestGDALGetMapFormatsHaveSemanticPayloads(t *testing.T) {
	capabilities := gdalcap.Get()
	formats := []string{}
	if capabilities.GeoTIFF {
		formats = append(formats, FormatGeoTIFF)
	}
	if capabilities.PDF {
		formats = append(formats, FormatPDF)
	}
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			body, err := encodeMapOutput(format, testMapRenderer(), mapEncodeContext{Request: testMapRequest(format)})
			if err != nil {
				t.Fatal(err)
			}
			if format == FormatPDF {
				if !bytes.HasPrefix(body, []byte("%PDF-")) {
					t.Fatal("PDF signature is missing")
				}
				return
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
			if err != nil || cfg.Width != 17 || cfg.Height != 11 {
				t.Fatalf("GeoTIFF dimensions = %dx%d, err=%v", cfg.Width, cfg.Height, err)
			}
		})
	}
}

func TestCapabilitiesAdvertisesOnlyAvailableGetMapFormats(t *testing.T) {
	for _, format := range getMapFormats {
		if !supportedGetMapFormat(format) {
			t.Fatalf("advertised format %q is not supported", format)
		}
	}
	want := []string{FormatPNG, FormatPNG8, FormatJPEG, FormatGIF, FormatTIFF, FormatTIFF8}
	capabilities := gdalcap.Get()
	if capabilities.GeoTIFF {
		want = append(want, FormatGeoTIFF)
	}
	want = append(want, FormatSVG)
	if capabilities.PDF {
		want = append(want, FormatPDF)
	}
	want = append(want, FormatKML, FormatKMZ, FormatMapML, FormatUTFGrid)
	if !slices.Equal(getMapFormats, want) {
		t.Fatalf("formats = %v, want %v", getMapFormats, want)
	}
	if !supportedGetMapFormat("kml") || getMapContentType("kml") != FormatKML || !supportedGetMapFormat("kmz") {
		t.Fatal("KML/KMZ aliases are not canonicalized")
	}
	if supportedGetMapFormat("application/x-not-a-map") {
		t.Fatal("unknown output format accepted")
	}
}

func TestMapOutputLimit(t *testing.T) {
	_, err := encodeMapOutput(FormatKMZ, testMapRenderer(), mapEncodeContext{Request: testMapRequest(FormatKMZ), MaxBytes: 8})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("output limit error = %v", err)
	}
}

func TestKMZContainsReadableImage(t *testing.T) {
	body, err := encodeMapOutput(FormatKMZ, testMapRenderer(), mapEncodeContext{Request: testMapRequest(FormatKMZ)})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range archive.File {
		if entry.Name != "map.png" {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		payload, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read image: %v / %v", readErr, closeErr)
		}
		if _, _, err := image.DecodeConfig(bytes.NewReader(payload)); err != nil {
			t.Fatalf("decode image: %v", err)
		}
		return
	}
	t.Fatal("map.png is missing")
}
