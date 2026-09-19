package wcs

import (
	"github.com/airbusgeo/godal"
	"strings"
	"testing"
)

func TestJPEG2000RejectsFloatingPointCoverageOverHTTP(t *testing.T) {
	if _, ok := godal.RasterDriver(godal.DriverName("JP2OpenJPEG")); !ok {
		t.Skip("JP2OpenJPEG unavailable")
	}
	h, ws, _ := testHandler()
	ws.Settings.WCS.OutputFormats = []string{"image/tiff", "image/jp2"}
	for _, version := range []string{"2.0.1", "2.1.0"} {
		response := perform(h, ws, "/wcs?SERVICE=WCS&REQUEST=GetCoverage&VERSION="+version+"&COVERAGEID=demo&FORMAT=image%2Fjp2")
		if response.Code != 400 || !strings.Contains(response.Body.String(), "use image/tiff") || !strings.Contains(response.Body.String(), `locator="format"`) {
			t.Fatalf("%s: %d %s", version, response.Code, response.Body)
		}
	}
}
