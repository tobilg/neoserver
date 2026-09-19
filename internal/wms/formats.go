package wms

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"

	"github.com/tobilg/neoserver/internal/gdalcap"
	"github.com/tobilg/neoserver/internal/renderer"
	"golang.org/x/image/tiff"
)

type getMapFormat struct {
	Token           string
	ContentType     string
	Aliases         []string
	Cacheable       bool
	ImageExceptions bool
	Available       func(gdalcap.Capabilities) bool
}

var getMapFormatRegistry = []getMapFormat{
	{Token: FormatPNG, ContentType: FormatPNG, Cacheable: true, ImageExceptions: true},
	{Token: FormatPNG8, ContentType: FormatPNG8, Cacheable: true, ImageExceptions: true},
	{Token: FormatJPEG, ContentType: FormatJPEG, Cacheable: true, ImageExceptions: true},
	{Token: FormatGIF, ContentType: FormatGIF, Cacheable: true, ImageExceptions: true},
	{Token: FormatTIFF, ContentType: FormatTIFF, Cacheable: true, ImageExceptions: true},
	{Token: FormatTIFF8, ContentType: FormatTIFF8, Cacheable: true, ImageExceptions: true},
	{Token: FormatGeoTIFF, ContentType: FormatGeoTIFF, Cacheable: true, Available: func(c gdalcap.Capabilities) bool { return c.GeoTIFF }},
	{Token: FormatSVG, ContentType: FormatSVG, Cacheable: true},
	{Token: FormatPDF, ContentType: FormatPDF, Cacheable: true, Available: func(c gdalcap.Capabilities) bool { return c.PDF }},
	{Token: FormatKML, ContentType: FormatKML, Aliases: []string{"kml"}},
	{Token: FormatKMZ, ContentType: FormatKMZ, Aliases: []string{"kmz"}, Cacheable: true},
	{Token: FormatMapML, ContentType: FormatMapML},
	{Token: FormatUTFGrid, ContentType: FormatUTFGrid},
}

var getMapFormats = advertisedGetMapFormats()

func advertisedGetMapFormats() []string {
	capabilities := gdalcap.Get()
	formats := make([]string, 0, len(getMapFormatRegistry))
	for _, definition := range getMapFormatRegistry {
		if definition.Available == nil || definition.Available(capabilities) {
			formats = append(formats, definition.Token)
		}
	}
	return formats
}

func lookupGetMapFormat(format string) (getMapFormat, bool) {
	format = strings.ToLower(strings.TrimSpace(format))
	capabilities := gdalcap.Get()
	for _, definition := range getMapFormatRegistry {
		matches := format == definition.Token
		for _, alias := range definition.Aliases {
			matches = matches || format == alias
		}
		if matches && (definition.Available == nil || definition.Available(capabilities)) {
			return definition, true
		}
	}
	return getMapFormat{}, false
}

func supportedGetMapFormat(format string) bool {
	_, ok := lookupGetMapFormat(format)
	return ok
}

func getMapContentType(format string) string {
	if definition, ok := lookupGetMapFormat(format); ok {
		return definition.ContentType
	}
	return format
}

func setMapContentHeaders(w http.ResponseWriter, format string) {
	w.Header().Set("Content-Type", getMapContentType(format))
	if definition, ok := lookupGetMapFormat(format); ok && !definition.Cacheable {
		w.Header().Set("Cache-Control", "no-store")
	}
}

func encodeMap(format string, rendered *renderer.MapRenderer) ([]byte, error) {
	if format == FormatSVG {
		return rendered.SVG()
	}
	return encodeImage(format, rendered.Image())
}

func encodeImage(format string, img image.Image) ([]byte, error) {
	var out bytes.Buffer
	var err error
	switch format {
	case FormatPNG:
		err = png.Encode(&out, img)
	case FormatPNG8:
		err = png.Encode(&out, palettedImage(img))
	case FormatJPEG:
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 90})
	case FormatGIF:
		err = gif.Encode(&out, palettedImage(img), nil)
	case FormatTIFF:
		err = tiff.Encode(&out, img, nil)
	case FormatTIFF8:
		err = tiff.Encode(&out, palettedImage(img), nil)
	default:
		return nil, fmt.Errorf("unsupported map format %q", format)
	}
	return out.Bytes(), err
}

func palettedImage(img image.Image) *image.Paletted {
	pal := make(color.Palette, 0, 256)
	pal = append(pal, color.NRGBA{})
	// The stable palette reserves index zero for full transparency.
	pal = append(pal, plan9Palette()...)
	if len(pal) > 256 {
		pal = pal[:256]
	}
	indexed := image.NewPaletted(img.Bounds(), pal)
	draw.FloydSteinberg.Draw(indexed, indexed.Rect, img, img.Bounds().Min)
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			if alpha < 0x8000 {
				indexed.SetColorIndex(x, y, 0)
			}
		}
	}
	return indexed
}

func plan9Palette() color.Palette {
	// The standard palette is copied so callers cannot mutate the shared value.
	result := make(color.Palette, 0, 250)
	for r := 0; r < 6; r++ {
		for g := 0; g < 6; g++ {
			for b := 0; b < 6; b++ {
				result = append(result, color.RGBA{uint8(r * 51), uint8(g * 51), uint8(b * 51), 255})
			}
		}
	}
	for gray := 1; gray < 32 && len(result) < 250; gray++ {
		value := uint8(gray * 8)
		result = append(result, color.RGBA{value, value, value, 255})
	}
	return result
}
