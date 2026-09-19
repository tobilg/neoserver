package getmap

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"image"
	"mime"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/testing/wms"
)

// TestFormat_PNG tests that FORMAT=image/png returns PNG image.
func TestFormat_PNG(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("FORMAT", "image/png")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "png")
}

// TestFormat_JPEG tests that FORMAT=image/jpeg returns JPEG image.
func TestFormat_JPEG(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)
	skipIfNoFormats(t)

	// Check if JPEG is supported
	formats := ctx.GetMapFormats()
	hasJPEG := false
	for _, f := range formats {
		if strings.Contains(strings.ToLower(f), "jpeg") {
			hasJPEG = true
			break
		}
	}
	if !hasJPEG {
		t.Skip("JPEG format not supported")
	}

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("FORMAT", "image/jpeg")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "jpeg")
}

// TestFormat_GIF tests that FORMAT=image/gif returns GIF image.
func TestFormat_GIF(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)
	skipIfNoFormats(t)

	// Check if GIF is supported
	formats := ctx.GetMapFormats()
	hasGIF := false
	for _, f := range formats {
		if strings.Contains(strings.ToLower(f), "gif") {
			hasGIF = true
			break
		}
	}
	if !hasGIF {
		t.Skip("GIF format not supported")
	}

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("FORMAT", "image/gif")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertStatusCode(t, resp, 200)
	wms.AssertIsImage(t, resp, "gif")
}

// TestFormat_Invalid tests that invalid FORMAT returns InvalidFormat exception.
// Reference: WMS 1.3.0 section 7.3.3.9
func TestFormat_Invalid(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	params := ctx.BuildGetMapParams(layer)
	params.Set("FORMAT", "invalid/format")

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "InvalidFormat")
}

// TestFormat_Missing tests that missing FORMAT returns exception.
func TestFormat_Missing(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	crs := layer.GetFirstCRS()
	if crs == "" {
		crs = "CRS:84"
	}

	params := url.Values{
		"SERVICE": {"WMS"},
		"VERSION": {"1.3.0"},
		"REQUEST": {"GetMap"},
		"LAYERS":  {layer.Name},
		"CRS":     {crs},
		"BBOX":    {"-180,-90,180,90"},
		"WIDTH":   {"256"},
		"HEIGHT":  {"256"},
		// No FORMAT
		"STYLES": {""},
	}

	resp, err := ctx.Client.GetMap(params)
	if err != nil {
		t.Fatalf("GetMap request failed: %v", err)
	}

	wms.AssertIsException(t, resp)
	wms.AssertExceptionCode(t, resp, "MissingParameterValue")
}

// TestFormat_EachSupported tests each supported GetMap format.
func TestFormat_EachSupported(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)
	skipIfNoFormats(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	formats := ctx.GetMapFormats()
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			params := ctx.BuildGetMapParams(layer)
			params.Set("FORMAT", format)

			resp, err := ctx.Client.GetMap(params)
			if err != nil {
				t.Fatalf("GetMap request failed: %v", err)
			}

			wms.AssertStatusCode(t, resp, 200)
			wms.AssertMediaType(t, resp, format)
			wms.AssertNotException(t, resp)
			mediaType, _, _ := mime.ParseMediaType(strings.ToLower(format))
			switch mediaType {
			case "image/svg+xml":
				wms.AssertSVGDimensions(t, resp, wms.DefaultWidth, wms.DefaultHeight)
			case "application/pdf":
				if !bytes.HasPrefix(resp.Body, []byte("%PDF-")) {
					t.Fatal("PDF signature is missing")
				}
			case "application/vnd.google-earth.kml+xml":
				assertXMLRoot(t, resp.Body, "kml")
			case "application/vnd.google-earth.kmz":
				archive, err := zip.NewReader(bytes.NewReader(resp.Body), int64(len(resp.Body)))
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
			case "text/mapml":
				assertXMLRoot(t, resp.Body, "mapml-")
			case "application/json":
				var document struct {
					Grid []string       `json:"grid"`
					Keys []string       `json:"keys"`
					Data map[string]any `json:"data"`
				}
				if err := json.Unmarshal(resp.Body, &document); err != nil || len(document.Grid) == 0 || len(document.Keys) == 0 || document.Data == nil {
					t.Fatalf("invalid UTFGrid: grid=%d keys=%d data=%v err=%v", len(document.Grid), len(document.Keys), document.Data, err)
				}
			default:
				wms.AssertIsImage(t, resp, "")
				wms.AssertImageDimensions(t, resp, wms.DefaultWidth, wms.DefaultHeight)
				if mediaType == "image/geotiff" {
					if _, _, err := image.DecodeConfig(bytes.NewReader(resp.Body)); err != nil {
						t.Fatalf("invalid GeoTIFF: %v", err)
					}
				}
			}
		})
	}
}

func assertXMLRoot(t *testing.T, body []byte, expected string) {
	t.Helper()
	var document struct{ XMLName xml.Name }
	if err := xml.Unmarshal(body, &document); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if document.XMLName.Local != expected {
		t.Fatalf("XML root = %q, want %q", document.XMLName.Local, expected)
	}
}

// TestFormat_CaseInsensitive tests that FORMAT parameter value is case-insensitive.
func TestFormat_CaseInsensitive(t *testing.T) {
	ctx := getTestContext(t)
	skipIfNoLayers(t)

	layer := ctx.GetFirstNamedLayer()
	if layer == nil {
		t.Fatal("No named layers available")
	}

	testCases := []string{
		"IMAGE/PNG",
		"Image/Png",
		"image/PNG",
	}

	for _, format := range testCases {
		t.Run(format, func(t *testing.T) {
			params := ctx.BuildGetMapParams(layer)
			params.Set("FORMAT", format)

			resp, err := ctx.Client.GetMap(params)
			if err != nil {
				t.Fatalf("GetMap request failed: %v", err)
			}

			wms.AssertStatusCode(t, resp, 200)
			wms.AssertIsImage(t, resp, "png")
		})
	}
}
