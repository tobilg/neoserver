package wms

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"

	"github.com/airbusgeo/godal"
)

// pdfDPI matches the resolution GDAL's PDF driver was previously asked for, so
// a map keeps its physical page size.
const pdfDPI = 96

// encodeGeoPDF writes the rendered map as a one-page, georeferenced PDF.
//
// The map is already a raster, so the PDF is a single image, georeferenced with
// the ISO 32000 geospatial viewport (a /VP /Measure dictionary with /Subtype
// /GEO), the encoding GDAL's PDF driver writes by default. Writing it here
// avoids GDAL's PDF driver plugin, whose poppler dependency is GPL-licensed
// and only needed to read PDFs. GDAL is still used for the coordinate
// transformation and the CRS WKT.
func encodeGeoPDF(img image.Image, req *GetMapRequest, maximum int64) ([]byte, error) {
	if req == nil {
		return nil, errors.New("georeferenced output requires a parsed GetMap request")
	}
	width, height := req.Width, req.Height
	if width < 1 || height < 1 || img == nil {
		return nil, errors.New("rendered map is empty")
	}
	wkt, corners, err := pdfGeoreference(req)
	if err != nil {
		return nil, err
	}
	rgb, alpha, opaque := splitRGBA(img, width, height)
	rgbStream, err := deflate(rgb)
	if err != nil {
		return nil, err
	}

	pageWidth := float64(width) * 72 / pdfDPI
	pageHeight := float64(height) * 72 / pdfDPI
	content := fmt.Sprintf("q %s 0 0 %s 0 0 cm /Im0 Do Q\n", pdfNumber(pageWidth), pdfNumber(pageHeight))

	// GPTS lists latitude/longitude pairs for the upper-left, lower-left,
	// lower-right and upper-right corners, matching LPTS in the unit square of
	// the viewport (PDF space has its origin at the lower left).
	gpts := make([]string, 0, 8)
	for _, corner := range corners {
		gpts = append(gpts, pdfNumber(corner[1]), pdfNumber(corner[0]))
	}
	gcsType := "/PROJCS"
	if req.SRID == 4326 || isGeographicWKT(wkt) {
		gcsType = "/GEOGCS"
	}

	w := &pdfWriter{}
	w.header()
	w.object(1, "<< /Type /Catalog /Pages 2 0 R >>")
	w.object(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	w.object(3, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R "+
		"/VP [<< /Type /Viewport /Name (Map) /BBox [0 0 %s %s] /Measure 6 0 R >>] >>",
		pdfNumber(pageWidth), pdfNumber(pageHeight), pdfNumber(pageWidth), pdfNumber(pageHeight)))
	smask := ""
	if !opaque {
		smask = " /SMask 8 0 R"
	}
	w.stream(4, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode%s", width, height, smask), rgbStream)
	w.stream(5, "<<", []byte(content))
	w.object(6, "<< /Type /Measure /Subtype /GEO /Bounds [0 1 0 0 1 0 1 1] /LPTS [0 1 0 0 1 0 1 1] /GPTS ["+strings.Join(gpts, " ")+"] /GCS 7 0 R >>")
	gcs := "<< /Type " + gcsType + " /WKT " + pdfString(wkt)
	if req.SRID > 0 {
		gcs += " /EPSG " + strconv.Itoa(req.SRID)
	}
	w.object(7, gcs+" >>")
	objects := 7
	if !opaque {
		alphaStream, err := deflate(alpha)
		if err != nil {
			return nil, err
		}
		w.stream(8, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode", width, height), alphaStream)
		objects = 8
	}
	w.object(objects+1, "<< /Producer (neoserver) >>")
	body := w.finish(objects+1, 1, objects+1)
	return enforceMapOutputLimit(body, maximum, nil)
}

// pdfGeoreference returns the CRS as WKT and the map corners (upper-left,
// lower-left, lower-right, upper-right) as longitude/latitude.
func pdfGeoreference(req *GetMapRequest) (string, [4][2]float64, error) {
	var corners [4][2]float64
	source, err := godal.NewSpatialRefFromEPSG(req.SRID)
	if err != nil {
		return "", corners, fmt.Errorf("PDF georeferencing: unsupported CRS %q: %w", req.CRS, err)
	}
	defer source.Close()
	wkt, err := source.WKT()
	if err != nil {
		return "", corners, fmt.Errorf("PDF georeferencing: %w", err)
	}
	b := req.BBox
	x := []float64{b.MinX, b.MinX, b.MaxX, b.MaxX}
	y := []float64{b.MaxY, b.MinY, b.MinY, b.MaxY}
	if req.SRID != 4326 {
		target, err := godal.NewSpatialRefFromEPSG(4326)
		if err != nil {
			return "", corners, err
		}
		defer target.Close()
		transform, err := godal.NewTransform(source, target)
		if err != nil {
			return "", corners, fmt.Errorf("PDF georeferencing: %w", err)
		}
		defer transform.Close()
		ok := make([]bool, 4)
		if err := transform.TransformEx(x, y, nil, ok); err != nil {
			return "", corners, fmt.Errorf("PDF georeferencing: %w", err)
		}
		for _, success := range ok {
			if !success {
				return "", corners, errors.New("PDF georeferencing: map corners cannot be expressed in longitude/latitude")
			}
		}
	}
	for i := range corners {
		corners[i] = [2]float64{x[i], y[i]}
	}
	return wkt, corners, nil
}

func isGeographicWKT(wkt string) bool {
	return strings.HasPrefix(wkt, "GEOGCS") || strings.HasPrefix(wkt, "GEOGCRS")
}

// splitRGBA separates un-premultiplied RGB samples from the alpha channel and
// reports whether every pixel is fully opaque.
func splitRGBA(img image.Image, width, height int) (rgb, alpha []byte, opaque bool) {
	rgb = make([]byte, 0, width*height*3)
	alpha = make([]byte, 0, width*height)
	opaque = true
	bounds := img.Bounds()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixel := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			rgb = append(rgb, pixel.R, pixel.G, pixel.B)
			alpha = append(alpha, pixel.A)
			opaque = opaque && pixel.A == 0xff
		}
	}
	return rgb, alpha, opaque
}

func deflate(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zlib.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// pdfNumber formats a real number without exponent notation, which PDF lacks.
func pdfNumber(value float64) string {
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if text == "-0" {
		return "0"
	}
	return text
}

// pdfString encodes a literal string, escaping the characters PDF reserves.
func pdfString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`, "\r", `\r`, "\n", `\n`)
	return "(" + replacer.Replace(value) + ")"
}

// pdfWriter assembles numbered indirect objects and the cross-reference table.
type pdfWriter struct {
	buffer  bytes.Buffer
	offsets map[int]int
}

func (w *pdfWriter) header() {
	w.offsets = map[int]int{}
	// The binary comment marks the file as binary for transfer tools.
	w.buffer.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
}

func (w *pdfWriter) object(number int, body string) {
	w.offsets[number] = w.buffer.Len()
	fmt.Fprintf(&w.buffer, "%d 0 obj\n%s\nendobj\n", number, body)
}

// stream writes a stream object; dictionary is an open "<< ..." prefix that
// receives the /Length entry.
func (w *pdfWriter) stream(number int, dictionary string, data []byte) {
	w.offsets[number] = w.buffer.Len()
	fmt.Fprintf(&w.buffer, "%d 0 obj\n%s /Length %d >>\nstream\n", number, dictionary, len(data))
	w.buffer.Write(data)
	w.buffer.WriteString("\nendstream\nendobj\n")
}

func (w *pdfWriter) finish(last, root, info int) []byte {
	start := w.buffer.Len()
	fmt.Fprintf(&w.buffer, "xref\n0 %d\n0000000000 65535 f \n", last+1)
	for number := 1; number <= last; number++ {
		fmt.Fprintf(&w.buffer, "%010d 00000 n \n", w.offsets[number])
	}
	fmt.Fprintf(&w.buffer, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", last+1, root, info, start)
	return w.buffer.Bytes()
}
