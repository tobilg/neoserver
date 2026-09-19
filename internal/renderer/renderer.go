package renderer

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strconv"
	"strings"

	"github.com/fogleman/gg"
	"github.com/tobilg/neoserver/internal/sld"
	xdraw "golang.org/x/image/draw"
)

// MapRenderer renders map images from geometries.
type MapRenderer struct {
	dc              *gg.Context
	transform       *Transform
	width           int
	height          int
	transparent     bool
	background      color.Color
	scene           []sceneCommand
	labelsFinalized bool
	graphicResolver func(href, format string) (image.Image, error)
}

// SetGraphicResolver installs the request-scoped, policy-aware resolver used
// for ExternalGraphic, GraphicStroke, and GraphicFill.
func (r *MapRenderer) SetGraphicResolver(resolver func(href, format string) (image.Image, error)) {
	r.graphicResolver = resolver
}

type sceneCommand struct {
	geometry  *Geometry
	style     interface{}
	text      *sceneText
	raster    image.Image
	blendMode string
	opacity   float64
}

type sceneText struct {
	geometry   *Geometry
	style      *sld.TextStyle
	label      string
	placements []textPlacement
}

// NewMapRenderer creates a new map renderer.
func NewMapRenderer(transform *Transform, transparent bool, bgColor color.Color) *MapRenderer {
	width := transform.Width()
	height := transform.Height()

	dc := gg.NewContext(width, height)

	// Set background
	if !transparent {
		if bgColor != nil {
			dc.SetColor(bgColor)
		} else {
			dc.SetRGBA(1, 1, 1, 1) // White background
		}
		dc.Clear()
	}

	return &MapRenderer{
		dc:          dc,
		transform:   transform,
		width:       width,
		height:      height,
		transparent: transparent,
		background:  bgColor,
	}
}

// DrawGeometry draws a geometry with the given style.
func (r *MapRenderer) DrawGeometry(geom *Geometry, style interface{}) {
	if geom == nil || style == nil {
		return
	}
	r.scene = append(r.scene, sceneCommand{geometry: geom, style: style})
	r.drawGeometryRaster(geom, style)
}

func (r *MapRenderer) drawGeometryRaster(geom *Geometry, style interface{}) {
	switch geom.Type {
	case WKBPoint:
		if ps, ok := style.(*sld.PointStyle); ok {
			r.drawPoint(geom.Coordinates, ps)
		}
	case WKBLineString:
		if ls, ok := style.(*sld.LineStyle); ok {
			r.drawLineString(geom.Coordinates, ls)
		}
	case WKBPolygon:
		if ps, ok := style.(*sld.PolygonStyle); ok {
			r.drawPolygon(geom.Rings, ps)
		}
	case WKBMultiPoint:
		if ps, ok := style.(*sld.PointStyle); ok {
			for _, g := range geom.Geometries {
				r.drawPoint(g.Coordinates, ps)
			}
		}
	case WKBMultiLineString:
		if ls, ok := style.(*sld.LineStyle); ok {
			for _, g := range geom.Geometries {
				r.drawLineString(g.Coordinates, ls)
			}
		}
	case WKBMultiPolygon:
		if ps, ok := style.(*sld.PolygonStyle); ok {
			for _, g := range geom.Geometries {
				r.drawPolygon(g.Rings, ps)
			}
		}
	case WKBGeometryCollection:
		for _, g := range geom.Geometries {
			r.drawGeometryRaster(&g, style)
		}
	}
}

// drawPoint draws a point marker.
func (r *MapRenderer) drawPoint(coords [][]float64, style *sld.PointStyle) {
	if len(coords) == 0 || len(coords[0]) < 2 {
		return
	}

	x, y := r.transform.ToPixel(coords[0][0], coords[0][1])
	radius := style.Size / 2
	if style.ExternalGraphic != nil && r.drawExternalGraphic(x, y, style) {
		return
	}

	// Apply rotation if specified
	if style.Rotation != 0 {
		r.dc.Push()
		r.dc.RotateAbout(gg.Radians(style.Rotation), x, y)
	}

	switch style.Shape {
	case "circle":
		r.drawCircle(x, y, radius, style)
	case "square":
		r.drawSquare(x, y, radius, style)
	case "triangle":
		r.drawTriangle(x, y, radius, style)
	case "star":
		r.drawStar(x, y, radius, 5, style)
	case "cross":
		r.drawCross(x, y, radius, style)
	case "x":
		r.drawX(x, y, radius, style)
	default:
		r.drawCircle(x, y, radius, style)
	}

	if style.Rotation != 0 {
		r.dc.Pop()
	}
}

func (r *MapRenderer) drawCircle(x, y, radius float64, style *sld.PointStyle) {
	r.dc.DrawCircle(x, y, radius)
	r.setFillColor(style.FillColor, style.Opacity)
	r.dc.FillPreserve()
	r.setStrokeColor(style.StrokeColor, 1.0)
	r.dc.SetLineWidth(style.StrokeWidth)
	r.dc.Stroke()
}

func (r *MapRenderer) drawSquare(x, y, radius float64, style *sld.PointStyle) {
	r.dc.DrawRectangle(x-radius, y-radius, radius*2, radius*2)
	r.setFillColor(style.FillColor, style.Opacity)
	r.dc.FillPreserve()
	r.setStrokeColor(style.StrokeColor, 1.0)
	r.dc.SetLineWidth(style.StrokeWidth)
	r.dc.Stroke()
}

func (r *MapRenderer) drawTriangle(x, y, radius float64, style *sld.PointStyle) {
	r.dc.MoveTo(x, y-radius)
	r.dc.LineTo(x+radius*0.866, y+radius*0.5)
	r.dc.LineTo(x-radius*0.866, y+radius*0.5)
	r.dc.ClosePath()
	r.setFillColor(style.FillColor, style.Opacity)
	r.dc.FillPreserve()
	r.setStrokeColor(style.StrokeColor, 1.0)
	r.dc.SetLineWidth(style.StrokeWidth)
	r.dc.Stroke()
}

func (r *MapRenderer) drawStar(x, y, outerRadius float64, points int, style *sld.PointStyle) {
	innerRadius := outerRadius * 0.4
	angleStep := math.Pi / float64(points)

	for i := 0; i < points*2; i++ {
		angle := float64(i)*angleStep - math.Pi/2
		radius := outerRadius
		if i%2 == 1 {
			radius = innerRadius
		}
		px := x + radius*math.Cos(angle)
		py := y + radius*math.Sin(angle)
		if i == 0 {
			r.dc.MoveTo(px, py)
		} else {
			r.dc.LineTo(px, py)
		}
	}
	r.dc.ClosePath()
	r.setFillColor(style.FillColor, style.Opacity)
	r.dc.FillPreserve()
	r.setStrokeColor(style.StrokeColor, 1.0)
	r.dc.SetLineWidth(style.StrokeWidth)
	r.dc.Stroke()
}

func (r *MapRenderer) drawCross(x, y, radius float64, style *sld.PointStyle) {
	armWidth := radius * 0.3
	r.dc.MoveTo(x-armWidth, y-radius)
	r.dc.LineTo(x+armWidth, y-radius)
	r.dc.LineTo(x+armWidth, y-armWidth)
	r.dc.LineTo(x+radius, y-armWidth)
	r.dc.LineTo(x+radius, y+armWidth)
	r.dc.LineTo(x+armWidth, y+armWidth)
	r.dc.LineTo(x+armWidth, y+radius)
	r.dc.LineTo(x-armWidth, y+radius)
	r.dc.LineTo(x-armWidth, y+armWidth)
	r.dc.LineTo(x-radius, y+armWidth)
	r.dc.LineTo(x-radius, y-armWidth)
	r.dc.LineTo(x-armWidth, y-armWidth)
	r.dc.ClosePath()
	r.setFillColor(style.FillColor, style.Opacity)
	r.dc.FillPreserve()
	r.setStrokeColor(style.StrokeColor, 1.0)
	r.dc.SetLineWidth(style.StrokeWidth)
	r.dc.Stroke()
}

func (r *MapRenderer) drawX(x, y, radius float64, style *sld.PointStyle) {
	r.dc.SetLineWidth(style.StrokeWidth * 2)
	r.setStrokeColor(style.FillColor, style.Opacity)
	offset := radius * 0.707 // cos(45°)
	r.dc.DrawLine(x-offset, y-offset, x+offset, y+offset)
	r.dc.Stroke()
	r.dc.DrawLine(x-offset, y+offset, x+offset, y-offset)
	r.dc.Stroke()
}

// drawLineString draws a line string.
func (r *MapRenderer) drawLineString(coords [][]float64, style *sld.LineStyle) {
	if len(coords) < 2 {
		return
	}
	if style.GraphicStroke != nil {
		r.drawGraphicStroke(coords, style.GraphicStroke)
		return
	}

	// Set line style
	r.dc.SetLineWidth(style.Width)
	r.setStrokeColor(style.Color, style.Opacity)

	// Set line cap
	switch style.LineCap {
	case "butt":
		r.dc.SetLineCap(gg.LineCapButt)
	case "square":
		r.dc.SetLineCap(gg.LineCapSquare)
	default:
		r.dc.SetLineCap(gg.LineCapRound)
	}

	// Set line join
	switch style.LineJoin {
	case "miter":
		r.dc.SetLineJoin(gg.LineJoinBevel) // gg doesn't have miter, use bevel
	case "bevel":
		r.dc.SetLineJoin(gg.LineJoinBevel)
	default:
		r.dc.SetLineJoin(gg.LineJoinRound)
	}

	// Set dash pattern
	if len(style.DashArray) > 0 {
		r.dc.SetDash(style.DashArray...)
	} else {
		r.dc.SetDash() // Clear dash
	}

	// Draw path
	for i, coord := range coords {
		if len(coord) < 2 {
			continue
		}
		x, y := r.transform.ToPixel(coord[0], coord[1])
		if i == 0 {
			r.dc.MoveTo(x, y)
		} else {
			r.dc.LineTo(x, y)
		}
	}

	r.dc.Stroke()
	r.dc.SetDash() // Reset dash
}

// drawPolygon draws a polygon with fill and stroke.
func (r *MapRenderer) drawPolygon(rings [][][]float64, style *sld.PolygonStyle) {
	if len(rings) == 0 {
		return
	}

	// Draw each ring
	for _, ring := range rings {
		if len(ring) < 3 {
			continue
		}
		for i, coord := range ring {
			if len(coord) < 2 {
				continue
			}
			x, y := r.transform.ToPixel(coord[0], coord[1])
			if i == 0 {
				r.dc.MoveTo(x, y)
			} else {
				r.dc.LineTo(x, y)
			}
		}
		r.dc.ClosePath()
	}

	// Fill with even-odd rule for holes.
	if style.GraphicFill != nil {
		r.drawGraphicFill(rings, style.GraphicFill)
	} else {
		r.setFillColor(style.FillColor, style.FillOpacity)
		r.dc.FillPreserve()
	}
	if style.GraphicStroke != nil {
		for _, ring := range rings { r.drawGraphicStroke(ring, style.GraphicStroke) }
		r.dc.ClearPath()
		return
	}

	// Stroke
	if style.StrokeWidth > 0 {
		r.dc.SetLineWidth(style.StrokeWidth)
		r.setStrokeColor(style.StrokeColor, style.StrokeOpacity)
		r.dc.Stroke()
	} else {
		r.dc.ClearPath()
	}
}

func (r *MapRenderer) graphicImage(pattern *sld.GraphicPattern) image.Image {
	if pattern == nil || pattern.Point == nil {
		return nil
	}
	if external := pattern.Point.ExternalGraphic; external != nil && r.graphicResolver != nil {
		imageValue, _ := r.graphicResolver(external.Href, external.Format)
		return imageValue
	}
	size := int(math.Ceil(math.Max(1, pattern.Point.Size)))
	canvas := gg.NewContext(size, size)
	copyRenderer := &MapRenderer{dc: canvas, width: size, height: size}
	copyRenderer.drawPointDirect(nil, pattern.Point, float64(size)/2, float64(size)/2)
	return canvas.Image()
}

func (r *MapRenderer) drawExternalGraphic(x, y float64, style *sld.PointStyle) bool {
	if style.ExternalGraphic == nil {
		return false
	}
	if r.graphicResolver == nil {
		return true
	}
	value, err := r.graphicResolver(style.ExternalGraphic.Href, style.ExternalGraphic.Format)
	if err != nil || value == nil {
		return true
	}
	size := int(math.Ceil(style.Size))
	if size <= 0 {
		size = value.Bounds().Dx()
	}
	if size <= 0 {
		return false
	}
	scaled := image.NewNRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), value, value.Bounds(), draw.Over, nil)
	if style.Opacity < 1 {
		for py := 0; py < size; py++ {
			for px := 0; px < size; px++ {
				c := color.NRGBAModel.Convert(scaled.At(px, py)).(color.NRGBA)
				c.A = uint8(float64(c.A) * math.Max(0, style.Opacity))
				scaled.SetNRGBA(px, py, c)
			}
		}
	}
	r.dc.DrawImageAnchored(scaled, int(math.Round(x)), int(math.Round(y)), .5, .5)
	return true
}

func (r *MapRenderer) drawGraphicStroke(coords [][]float64, pattern *sld.GraphicPattern) {
	imageValue := r.graphicImage(pattern)
	if imageValue == nil {
		return
	}
	gap := pattern.Gap
	if gap <= 0 {
		gap = math.Max(1, float64(imageValue.Bounds().Dx()))
	}
	next := math.Max(0, pattern.InitialGap)
	elapsed := 0.0
	for i := 1; i < len(coords); i++ {
		x1, y1 := r.transform.ToPixel(coords[i-1][0], coords[i-1][1])
		x2, y2 := r.transform.ToPixel(coords[i][0], coords[i][1])
		length := math.Hypot(x2-x1, y2-y1)
		for length > 0 && next <= elapsed+length {
			ratio := (next - elapsed) / length
			r.dc.DrawImageAnchored(imageValue, int(math.Round(x1+(x2-x1)*ratio)), int(math.Round(y1+(y2-y1)*ratio)), .5, .5)
			next += gap
		}
		elapsed += length
	}
}

func (r *MapRenderer) drawGraphicFill(rings [][][]float64, pattern *sld.GraphicPattern) {
	value := r.graphicImage(pattern)
	if value == nil {
		r.dc.ClearPath()
		return
	}
	maskContext := gg.NewContext(r.width, r.height)
	minX, minY, maxX, maxY := float64(r.width), float64(r.height), 0.0, 0.0
	for _, ring := range rings {
		for i, coord := range ring {
			x, y := r.transform.ToPixel(coord[0], coord[1])
			minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
			if i == 0 {
				maskContext.MoveTo(x, y)
			} else {
				maskContext.LineTo(x, y)
			}
		}
		maskContext.ClosePath()
	}
	maskContext.SetColor(color.White)
	maskContext.Fill()
	patternLayer := image.NewNRGBA(image.Rect(0, 0, r.width, r.height))
	stepX, stepY := value.Bounds().Dx(), value.Bounds().Dy()
	if stepX < 1 || stepY < 1 {
		return
	}
	for y := int(math.Floor(minY)); y <= int(math.Ceil(maxY)); y += stepY {
		for x := int(math.Floor(minX)); x <= int(math.Ceil(maxX)); x += stepX {
			draw.Draw(patternLayer, image.Rect(x, y, x+stepX, y+stepY), value, value.Bounds().Min, draw.Over)
		}
	}
	destination, ok := r.dc.Image().(*image.RGBA)
	if !ok {
		return
	}
	draw.DrawMask(destination, destination.Bounds(), patternLayer, image.Point{}, maskContext.Image(), image.Point{}, draw.Over)
}

// setFillColor sets the fill color with opacity.
func (r *MapRenderer) setFillColor(c color.RGBA, opacity float64) {
	alpha := float64(c.A) / 255.0 * opacity
	r.dc.SetRGBA(float64(c.R)/255.0, float64(c.G)/255.0, float64(c.B)/255.0, alpha)
}

// setStrokeColor sets the stroke color with opacity.
func (r *MapRenderer) setStrokeColor(c color.RGBA, opacity float64) {
	alpha := float64(c.A) / 255.0 * opacity
	r.dc.SetRGBA(float64(c.R)/255.0, float64(c.G)/255.0, float64(c.B)/255.0, alpha)
}

// Image returns the rendered image.
func (r *MapRenderer) Image() image.Image {
	r.finalizeLabels()
	return r.dc.Image()
}

// Composite draws a pre-rendered raster over the current map canvas.
func (r *MapRenderer) Composite(img image.Image) {
	if img != nil {
		r.scene = append(r.scene, sceneCommand{raster: img, opacity: 1})
		r.dc.DrawImage(img, 0, 0)
	}
}

// SVG returns a self-contained SVG representation of the recorded map scene.
// Vector geometry and labels remain vector content; raster coverages are
// embedded as PNG data URLs so the document never references external assets.
func (r *MapRenderer) SVG() ([]byte, error) {
	r.finalizeLabels()
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, r.width, r.height, r.width, r.height)
	if !r.transparent {
		var bg color.Color = color.RGBA{R: 255, G: 255, B: 255, A: 255}
		if r.background != nil {
			bg = color.RGBAModel.Convert(r.background)
		}
		fmt.Fprintf(&out, `<rect width="100%%" height="100%%" fill="%s" fill-opacity="%s"/>`, svgColor(bg), svgOpacity(bg))
	}
	for _, command := range r.scene {
		switch {
		case command.geometry != nil:
			r.writeSVGGeometry(&out, command.geometry, command.style)
		case command.text != nil:
			r.writeSVGText(&out, command.text)
		case command.raster != nil:
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, command.raster); err != nil {
				return nil, err
			}
			style := ""
			if command.blendMode != "" && command.blendMode != "source-over" {
				style = ` style="mix-blend-mode:` + xmlEscapeAttr(command.blendMode) + `"`
			}
			opacity := command.opacity
			fmt.Fprintf(&out, `<image x="0" y="0" width="%d" height="%d" opacity="%s" preserveAspectRatio="none"%s href="data:image/png;base64,%s"/>`, r.width, r.height, formatFloat(opacity), style, base64.StdEncoding.EncodeToString(encoded.Bytes()))
		}
	}
	out.WriteString(`</svg>`)
	return []byte(out.String()), nil
}

func (r *MapRenderer) writeSVGGeometry(out *strings.Builder, geom *Geometry, style interface{}) {
	if geom == nil {
		return
	}
	switch geom.Type {
	case WKBMultiPoint, WKBMultiLineString, WKBMultiPolygon, WKBGeometryCollection:
		for i := range geom.Geometries {
			r.writeSVGGeometry(out, &geom.Geometries[i], style)
		}
		return
	}
	switch value := style.(type) {
	case *sld.PointStyle:
		if geom.Type != WKBPoint || len(geom.Coordinates) == 0 || len(geom.Coordinates[0]) < 2 {
			return
		}
		x, y := r.transform.ToPixel(geom.Coordinates[0][0], geom.Coordinates[0][1])
		r.writeSVGPoint(out, x, y, value)
	case *sld.LineStyle:
		if geom.Type != WKBLineString || len(geom.Coordinates) < 2 {
			return
		}
		fmt.Fprintf(out, `<path d="%s" fill="none" stroke="%s" stroke-opacity="%s" stroke-width="%s" stroke-linecap="%s" stroke-linejoin="%s"%s/>`, r.svgLinePath(geom.Coordinates, false), svgColor(value.Color), formatFloat(float64(value.Color.A)/255*value.Opacity), formatFloat(value.Width), xmlEscapeAttr(defaultString(value.LineCap, "round")), xmlEscapeAttr(defaultString(value.LineJoin, "round")), svgDash(value.DashArray))
	case *sld.PolygonStyle:
		if geom.Type != WKBPolygon || len(geom.Rings) == 0 {
			return
		}
		var path strings.Builder
		for _, ring := range geom.Rings {
			path.WriteString(r.svgLinePath(ring, true))
		}
		fmt.Fprintf(out, `<path d="%s" fill="%s" fill-opacity="%s" fill-rule="evenodd"`, path.String(), svgColor(value.FillColor), formatFloat(float64(value.FillColor.A)/255*value.FillOpacity))
		if value.StrokeWidth > 0 {
			fmt.Fprintf(out, ` stroke="%s" stroke-opacity="%s" stroke-width="%s"`, svgColor(value.StrokeColor), formatFloat(float64(value.StrokeColor.A)/255*value.StrokeOpacity), formatFloat(value.StrokeWidth))
		}
		out.WriteString(`/>`)
	}
}

func (r *MapRenderer) writeSVGPoint(out *strings.Builder, x, y float64, style *sld.PointStyle) {
	radius := style.Size / 2
	common := fmt.Sprintf(`fill="%s" fill-opacity="%s" stroke="%s" stroke-width="%s"`, svgColor(style.FillColor), formatFloat(float64(style.FillColor.A)/255*style.Opacity), svgColor(style.StrokeColor), formatFloat(style.StrokeWidth))
	transform := ""
	if style.Rotation != 0 {
		transform = fmt.Sprintf(` transform="rotate(%s %s %s)"`, formatFloat(style.Rotation), formatFloat(x), formatFloat(y))
	}
	switch style.Shape {
	case "square":
		fmt.Fprintf(out, `<rect x="%s" y="%s" width="%s" height="%s" %s%s/>`, formatFloat(x-radius), formatFloat(y-radius), formatFloat(radius*2), formatFloat(radius*2), common, transform)
	case "triangle":
		fmt.Fprintf(out, `<path d="M %s %s L %s %s L %s %s Z" %s%s/>`, formatFloat(x), formatFloat(y-radius), formatFloat(x+radius*.866), formatFloat(y+radius*.5), formatFloat(x-radius*.866), formatFloat(y+radius*.5), common, transform)
	case "star":
		var points []string
		for i := 0; i < 10; i++ {
			a := float64(i)*math.Pi/5 - math.Pi/2
			rr := radius
			if i%2 == 1 {
				rr *= .4
			}
			points = append(points, formatFloat(x+rr*math.Cos(a))+","+formatFloat(y+rr*math.Sin(a)))
		}
		fmt.Fprintf(out, `<polygon points="%s" %s%s/>`, strings.Join(points, " "), common, transform)
	case "cross":
		arm := radius * .3
		points := [][2]float64{{x - arm, y - radius}, {x + arm, y - radius}, {x + arm, y - arm}, {x + radius, y - arm}, {x + radius, y + arm}, {x + arm, y + arm}, {x + arm, y + radius}, {x - arm, y + radius}, {x - arm, y + arm}, {x - radius, y + arm}, {x - radius, y - arm}, {x - arm, y - arm}}
		fmt.Fprintf(out, `<polygon points="%s" %s%s/>`, svgPoints(points), common, transform)
	case "x":
		offset := radius * .707
		fmt.Fprintf(out, `<path d="M %s %s L %s %s M %s %s L %s %s" fill="none" stroke="%s" stroke-opacity="%s" stroke-width="%s"%s/>`, formatFloat(x-offset), formatFloat(y-offset), formatFloat(x+offset), formatFloat(y+offset), formatFloat(x-offset), formatFloat(y+offset), formatFloat(x+offset), formatFloat(y-offset), svgColor(style.FillColor), formatFloat(float64(style.FillColor.A)/255*style.Opacity), formatFloat(style.StrokeWidth*2), transform)
	default:
		fmt.Fprintf(out, `<circle cx="%s" cy="%s" r="%s" %s%s/>`, formatFloat(x), formatFloat(y), formatFloat(radius), common, transform)
	}
}

func (r *MapRenderer) svgLinePath(points [][]float64, close bool) string {
	var path strings.Builder
	for i, point := range points {
		if len(point) < 2 {
			continue
		}
		x, y := r.transform.ToPixel(point[0], point[1])
		if i == 0 {
			fmt.Fprintf(&path, "M %s %s ", formatFloat(x), formatFloat(y))
		} else {
			fmt.Fprintf(&path, "L %s %s ", formatFloat(x), formatFloat(y))
		}
	}
	if close {
		path.WriteString("Z ")
	}
	return path.String()
}

func (r *MapRenderer) writeSVGText(out *strings.Builder, text *sceneText) {
	style := text.style
	anchor := "middle"
	if style.AnchorX <= .25 {
		anchor = "start"
	} else if style.AnchorX >= .75 {
		anchor = "end"
	}
	baseline := "middle"
	if style.AnchorY <= .25 {
		baseline = "text-after-edge"
	} else if style.AnchorY >= .75 {
		baseline = "text-before-edge"
	}
	weight := defaultString(style.FontWeight, "normal")
	fontStyle := defaultString(style.FontStyle, "normal")
	fontSize := style.FontSize
	if fontSize <= 0 {
		fontSize = 12
	}
	for _, placement := range text.placements {
		for lineIndex, line := range placement.lines {
			y := placement.y + float64(lineIndex)*fontSize*1.15
			fmt.Fprintf(out, `<text x="%s" y="%s" text-anchor="%s" dominant-baseline="%s" font-family="Go, sans-serif" font-size="%s" font-weight="%s" font-style="%s" fill="%s"`, formatFloat(placement.x), formatFloat(y), anchor, baseline, formatFloat(fontSize), xmlEscapeAttr(weight), xmlEscapeAttr(fontStyle), svgColor(style.Color))
			if style.HaloRadius > 0 {
				fmt.Fprintf(out, ` stroke="%s" stroke-width="%s" paint-order="stroke fill" stroke-linejoin="round"`, svgColor(style.HaloColor), formatFloat(style.HaloRadius*2))
			}
			if placement.angle != 0 {
				fmt.Fprintf(out, ` transform="rotate(%s %s %s)"`, formatFloat(placement.angle), formatFloat(placement.x), formatFloat(placement.y))
			}
			out.WriteString(`>`)
			_ = xml.EscapeText((*builderWriter)(out), []byte(line))
			out.WriteString(`</text>`)
		}
	}
}

type builderWriter strings.Builder

func (w *builderWriter) Write(p []byte) (int, error) {
	(*strings.Builder)(w).Write(p)
	return len(p), nil
}

func svgColor(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

func svgOpacity(c color.Color) string {
	_, _, _, a := c.RGBA()
	return formatFloat(float64(a) / 65535)
}

func svgDash(values []float64) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = formatFloat(value)
	}
	return ` stroke-dasharray="` + strings.Join(parts, ",") + `"`
}

func svgPoints(points [][2]float64) string {
	result := make([]string, len(points))
	for i, p := range points {
		result[i] = formatFloat(p[0]) + "," + formatFloat(p[1])
	}
	return strings.Join(result, " ")
}

func formatFloat(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }
func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func xmlEscapeAttr(value string) string {
	var out strings.Builder
	_ = xml.EscapeText((*builderWriter)(&out), []byte(value))
	return out.String()
}

// DrawLegendSymbol draws a legend symbol for the given style.
func (r *MapRenderer) DrawLegendSymbol(x, y, width, height float64, geomType string, style interface{}) {
	switch geomType {
	case "Point", "MultiPoint":
		if ps, ok := style.(*sld.PointStyle); ok {
			coords := [][]float64{{x + width/2, y + height/2}}
			r.drawPointDirect(coords, ps, x+width/2, y+height/2)
		}
	case "LineString", "MultiLineString":
		if ls, ok := style.(*sld.LineStyle); ok {
			// Draw a diagonal line
			r.dc.SetLineWidth(ls.Width)
			r.setStrokeColor(ls.Color, ls.Opacity)
			r.dc.DrawLine(x+2, y+height-2, x+width-2, y+2)
			r.dc.Stroke()
		}
	case "Polygon", "MultiPolygon":
		if ps, ok := style.(*sld.PolygonStyle); ok {
			// Draw a rectangle
			r.dc.DrawRectangle(x+2, y+2, width-4, height-4)
			r.setFillColor(ps.FillColor, ps.FillOpacity)
			r.dc.FillPreserve()
			if ps.StrokeWidth > 0 {
				r.dc.SetLineWidth(ps.StrokeWidth)
				r.setStrokeColor(ps.StrokeColor, ps.StrokeOpacity)
				r.dc.Stroke()
			} else {
				r.dc.ClearPath()
			}
		}
	}
}

// drawPointDirect draws a point at absolute pixel coordinates.
func (r *MapRenderer) drawPointDirect(coords [][]float64, style *sld.PointStyle, x, y float64) {
	radius := style.Size / 2
	switch style.Shape {
	case "circle":
		r.drawCircle(x, y, radius, style)
	case "square":
		r.drawSquare(x, y, radius, style)
	case "triangle":
		r.drawTriangle(x, y, radius, style)
	case "star":
		r.drawStar(x, y, radius, 5, style)
	case "cross":
		r.drawCross(x, y, radius, style)
	case "x":
		r.drawX(x, y, radius, style)
	default:
		r.drawCircle(x, y, radius, style)
	}
}

// Context returns the underlying gg context for advanced operations.
func (r *MapRenderer) Context() *gg.Context {
	return r.dc
}
