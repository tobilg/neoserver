package wms

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/airbusgeo/godal"
	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/datasource/rastergrid"
	"github.com/tobilg/neoserver/internal/gdalcap"
	"github.com/tobilg/neoserver/internal/renderer"
)

type mapEncodeContext struct {
	Request  *GetMapRequest
	BaseURL  string
	MaxBytes int64
}

type limitedMapBuffer struct {
	bytes.Buffer
	maximum int64
}

func (b *limitedMapBuffer) Write(value []byte) (int, error) {
	if b.maximum > 0 && int64(b.Len()+len(value)) > b.maximum {
		return 0, errors.New("encoded map exceeds the configured output limit")
	}
	return b.Buffer.Write(value)
}

func encodeMapOutput(format string, rendered *renderer.MapRenderer, context mapEncodeContext) ([]byte, error) {
	switch format {
	case FormatGeoTIFF, FormatPDF:
		return encodeGDALMap(format, rendered.Image(), context.Request, context.MaxBytes)
	case FormatKML:
		return encodeKMLDocument(context.Request, buildPNGMapURL(context.BaseURL, context.Request), context.MaxBytes)
	case FormatKMZ:
		return encodeKMZ(rendered.Image(), context)
	case FormatMapML:
		return encodeMapML(context.Request, context.BaseURL, context.MaxBytes)
	case FormatUTFGrid:
		return nil, errors.New("UTFGrid requires feature hit rendering")
	default:
		body, err := encodeMap(format, rendered)
		return enforceMapOutputLimit(body, context.MaxBytes, err)
	}
}

func enforceMapOutputLimit(body []byte, maximum int64, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if maximum > 0 && int64(len(body)) > maximum {
		return nil, errors.New("encoded map exceeds the configured output limit")
	}
	return body, nil
}

func encodeGDALMap(format string, img image.Image, req *GetMapRequest, maximum int64) ([]byte, error) {
	if req == nil {
		return nil, errors.New("georeferenced output requires a parsed GetMap request")
	}
	capabilities := gdalcap.Get()
	driver := godal.GTiff
	suffix := ".tif"
	creationOptions := []godal.DatasetTranslateOption{godal.CreationOption("COMPRESS=DEFLATE", "TILED=YES")}
	if format == FormatPDF {
		if !capabilities.PDF {
			return nil, errors.New("PDF output is unavailable because the GDAL PDF driver is missing")
		}
		driver = godal.DriverName("PDF")
		suffix = ".pdf"
		creationOptions = []godal.DatasetTranslateOption{godal.CreationOption("COMPRESS=DEFLATE", "DPI=96")}
	} else if !capabilities.GeoTIFF {
		return nil, errors.New("GeoTIFF output is unavailable because the GDAL GTiff driver is missing")
	}
	dataset, err := rgbaDataset(img, req)
	if err != nil {
		return nil, err
	}
	defer dataset.Close()
	name := "/vsimem/neoserver-wms-" + uuid.NewString() + suffix
	defer godal.VSIUnlink(name)
	encoded, err := dataset.Translate(name, nil, append(creationOptions, driver)...)
	if err != nil {
		return nil, fmt.Errorf("encode %s map: %w", format, err)
	}
	if err := encoded.Close(); err != nil {
		return nil, fmt.Errorf("finalize %s map: %w", format, err)
	}
	file, err := godal.VSIOpen(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	return enforceMapOutputLimit(body, maximum, err)
}

func rgbaDataset(img image.Image, req *GetMapRequest) (*godal.Dataset, error) {
	width, height := req.Width, req.Height
	if width < 1 || height < 1 || img == nil {
		return nil, errors.New("rendered map is empty")
	}
	dataset, err := godal.Create(godal.Memory, "", 4, godal.Byte, width, height)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*godal.Dataset, error) {
		_ = dataset.Close()
		return nil, err
	}
	geoTransform := [6]float64{
		req.BBox.MinX,
		(req.BBox.MaxX - req.BBox.MinX) / float64(width),
		0,
		req.BBox.MaxY,
		0,
		-(req.BBox.MaxY - req.BBox.MinY) / float64(height),
	}
	if err := dataset.SetGeoTransform(geoTransform); err != nil {
		return fail(err)
	}
	spatialRef, err := godal.NewSpatialRefFromEPSG(req.SRID)
	if err != nil {
		return fail(err)
	}
	if err := dataset.SetSpatialRef(spatialRef); err != nil {
		spatialRef.Close()
		return fail(err)
	}
	spatialRef.Close()
	values := [4][]uint8{
		make([]uint8, width*height),
		make([]uint8, width*height),
		make([]uint8, width*height),
		make([]uint8, width*height),
	}
	bounds := img.Bounds()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixel := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			index := y*width + x
			values[0][index], values[1][index], values[2][index], values[3][index] = pixel.R, pixel.G, pixel.B, pixel.A
		}
	}
	interpretations := []godal.ColorInterp{godal.CIRed, godal.CIGreen, godal.CIBlue, godal.CIAlpha}
	for index, band := range dataset.Bands() {
		if err := band.Write(0, 0, values[index], width, height); err != nil {
			return fail(err)
		}
		if err := band.SetColorInterp(interpretations[index]); err != nil {
			return fail(err)
		}
	}
	return dataset, nil
}

func buildPNGMapURL(baseURL string, req *GetMapRequest) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	query := make(url.Values)
	query.Set("SERVICE", "WMS")
	query.Set("VERSION", req.Version)
	query.Set("REQUEST", RequestGetMap)
	query.Set("LAYERS", strings.Join(req.Layers, ","))
	query.Set("STYLES", strings.Join(req.Styles, ","))
	query.Set("CRS", req.CRS)
	query.Set("BBOX", wireBBox(req))
	query.Set("WIDTH", strconv.Itoa(req.Width))
	query.Set("HEIGHT", strconv.Itoa(req.Height))
	query.Set("FORMAT", FormatPNG)
	query.Set("TRANSPARENT", strconv.FormatBool(req.Transparent))
	query.Set("BGCOLOR", fmt.Sprintf("0x%02X%02X%02X", req.BgColor.R, req.BgColor.G, req.BgColor.B))
	if req.SLDBody != "" {
		query.Set("SLD_BODY", req.SLDBody)
	}
	if req.Time != "" {
		query.Set("TIME", req.Time)
	}
	if req.Elevation != "" {
		query.Set("ELEVATION", req.Elevation)
	}
	if len(req.Environment) > 0 {
		keys := make([]string, 0, len(req.Environment))
		for name := range req.Environment {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		values := make([]string, 0, len(keys))
		for _, name := range keys {
			values = append(values, name+":"+req.Environment[name])
		}
		query.Set("ENV", strings.Join(values, ";"))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func wireBBox(req *GetMapRequest) string {
	values := [4]float64{req.BBox.MinX, req.BBox.MinY, req.BBox.MaxX, req.BBox.MaxY}
	if req.SRID == 4326 && !strings.EqualFold(req.CRS, "CRS:84") && !strings.EqualFold(req.CRS, "OGC:CRS84") {
		values = [4]float64{req.BBox.MinY, req.BBox.MinX, req.BBox.MaxY, req.BBox.MaxX}
	}
	parts := make([]string, 4)
	for index, value := range values {
		parts[index] = strconv.FormatFloat(value, 'g', 17, 64)
	}
	return strings.Join(parts, ",")
}

func encodeKMLDocument(req *GetMapRequest, imageURL string, maximum int64) ([]byte, error) {
	bounds, err := rastergrid.TransformBBox([4]float64{req.BBox.MinX, req.BBox.MinY, req.BBox.MaxX, req.BBox.MaxY}, req.CRS, "EPSG:4326")
	if err != nil {
		return nil, fmt.Errorf("transform KML bounds: %w", err)
	}
	buffer := &limitedMapBuffer{maximum: maximum}
	_, err = fmt.Fprintf(buffer, `<?xml version="1.0" encoding="UTF-8"?>
<kml xmlns="http://www.opengis.net/kml/2.2"><Document><name>%s</name><GroundOverlay><name>%s</name><Icon><href>%s</href></Icon><LatLonBox><north>%.12g</north><south>%.12g</south><east>%.12g</east><west>%.12g</west></LatLonBox></GroundOverlay></Document></kml>`,
		xmlEscape(strings.Join(req.Layers, ",")), xmlEscape(strings.Join(req.Layers, ",")), xmlEscape(imageURL), bounds[3], bounds[1], bounds[2], bounds[0])
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func encodeKMZ(img image.Image, context mapEncodeContext) ([]byte, error) {
	pngBuffer := &limitedMapBuffer{maximum: context.MaxBytes}
	if err := png.Encode(pngBuffer, img); err != nil {
		return nil, err
	}
	kml, err := encodeKMLDocument(context.Request, "map.png", context.MaxBytes)
	if err != nil {
		return nil, err
	}
	buffer := &limitedMapBuffer{maximum: context.MaxBytes}
	archive := zip.NewWriter(buffer)
	fixedTime := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, entry := range []struct {
		name string
		body []byte
	}{{"doc.kml", kml}, {"map.png", pngBuffer.Bytes()}} {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate, Modified: fixedTime}
		header.SetMode(0o600)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			_ = archive.Close()
			return nil, err
		}
		if _, err := writer.Write(entry.body); err != nil {
			_ = archive.Close()
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func encodeMapML(req *GetMapRequest, baseURL string, maximum int64) ([]byte, error) {
	projection := ""
	switch req.SRID {
	case 4326:
		projection = "WGS84"
	case 3857:
		projection = "OSMTILE"
	default:
		return nil, &RequestError{Code: "InvalidCRS", Message: "MapML supports only EPSG:4326/CRS:84 and EPSG:3857"}
	}
	buffer := &limitedMapBuffer{maximum: maximum}
	_, err := fmt.Fprintf(buffer, `<?xml version="1.0" encoding="UTF-8"?>
<mapml- xmlns="http://www.w3.org/1999/xhtml"><map-head><map-title>%s</map-title><map-base href="%s"/><map-meta name="projection" content="%s"/></map-head><map-body><map-extent units="%s" label="%s" checked="checked"><map-input name="xmin" type="location" axis="easting" value="%.12g"/><map-input name="ymin" type="location" axis="northing" value="%.12g"/><map-input name="xmax" type="location" axis="easting" value="%.12g"/><map-input name="ymax" type="location" axis="northing" value="%.12g"/><map-input name="w" type="width" value="%d"/><map-input name="h" type="height" value="%d"/><map-link rel="image" tref="%s"/></map-extent></map-body></mapml->`,
		xmlEscape(strings.Join(req.Layers, ",")), xmlEscape(baseURL), projection, projection, xmlEscape(strings.Join(req.Layers, ",")), req.BBox.MinX, req.BBox.MinY, req.BBox.MaxX, req.BBox.MaxY, req.Width, req.Height, xmlEscape(buildPNGMapURL(baseURL, req)))
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
