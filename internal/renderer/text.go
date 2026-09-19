package renderer

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/fogleman/gg"
	"github.com/tobilg/neoserver/internal/sld"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

var fontCache sync.Map
var configuredFontData sync.Map

// ConfigureFontPaths loads operator-controlled TTF/OTF files once. A style's
// font-family matches the case-insensitive filename without its extension.
func ConfigureFontPaths(paths []string) {
	for _, path := range paths {
		entries, err := os.ReadDir(path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			extension := strings.ToLower(filepath.Ext(entry.Name()))
			if extension != ".ttf" && extension != ".otf" {
				continue
			}
			data, readErr := os.ReadFile(filepath.Join(path, entry.Name()))
			if readErr == nil {
				configuredFontData.Store(strings.ToLower(strings.TrimSuffix(entry.Name(), extension)), data)
			}
		}
	}
}

func bundledFace(style *sld.TextStyle) font.Face {
	key := strings.ToLower(style.FontFamily) + "|" + style.FontWeight + "|" + style.FontStyle + "|" + formatFontSize(style.FontSize)
	if value, ok := fontCache.Load(key); ok {
		return value.(font.Face)
	}
	data := goregular.TTF
	if configured, ok := configuredFontData.Load(strings.ToLower(strings.TrimSpace(style.FontFamily))); ok {
		data = configured.([]byte)
	} else {
		if style.FontWeight == "bold" && (style.FontStyle == "italic" || style.FontStyle == "oblique") {
			data = gobolditalic.TTF
		} else if style.FontWeight == "bold" {
			data = gobold.TTF
		} else if style.FontStyle == "italic" || style.FontStyle == "oblique" {
			data = goitalic.TTF
		}
	}
	parsed, parseErr := opentype.Parse(data)
	if parseErr != nil {
		parsed, _ = opentype.Parse(goregular.TTF)
	}
	size := style.FontSize
	if size <= 0 {
		size = 12
	}
	face, faceErr := opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if faceErr != nil {
		fallback, _ := opentype.Parse(goregular.TTF)
		face, _ = opentype.NewFace(fallback, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	}
	fontCache.Store(key, face)
	return face
}

type textPlacement struct {
	x, y, angle float64
	lines       []string
}

type labelBox struct{ minX, minY, maxX, maxY float64 }

// DrawText queues a label. Placement is finalized once all features are known,
// allowing deterministic priority ordering and collision resolution.
func (r *MapRenderer) DrawText(geom *Geometry, style *sld.TextStyle, label string) {
	if geom == nil || style == nil || label == "" {
		return
	}
	r.labelsFinalized = false
	r.scene = append(r.scene, sceneCommand{text: &sceneText{geometry: geom, style: style, label: label}})
}

func (r *MapRenderer) finalizeLabels() {
	if r.labelsFinalized {
		return
	}
	r.labelsFinalized = true
	type candidate struct {
		text  *sceneText
		order int
	}
	var candidates []candidate
	for index := range r.scene {
		if text := r.scene[index].text; text != nil {
			text.placements = nil
			candidates = append(candidates, candidate{text: text, order: index})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].text.style.Priority > candidates[j].text.style.Priority
	})
	occupied := r.labelObstacles()
	usedGroups := make(map[string]bool)
	for _, candidate := range candidates {
		text, style := candidate.text, candidate.text.style
		if style.Group != "" && usedGroups[style.Group] && !style.LabelAllGroup {
			continue
		}
		anchors := labelAnchors(text.geometry, r.transform, style)
		lines := wrapLabel(text.label, style.AutoWrap)
		r.dc.SetFontFace(bundledFace(style))
		width, height := measureLabel(r.dc, lines, style.FontSize)
		accepted := false
		for _, anchor := range anchors {
			placement, box, ok := r.placeLabel(anchor, lines, width, height, style, occupied)
			if !ok {
				continue
			}
			text.placements = append(text.placements, placement)
			if style.ConflictResolution {
				occupied = append(occupied, box)
			}
			r.drawTextPlacement(placement, style)
			accepted = true
			if style.Repeat <= 0 {
				break
			}
		}
		if accepted && style.Group != "" {
			usedGroups[style.Group] = true
		}
	}
}

func (r *MapRenderer) labelObstacles() []labelBox {
	var result []labelBox
	for _, command := range r.scene {
		if command.geometry == nil {
			continue
		}
		margin, enabled := 0.0, false
		switch style := command.style.(type) {
		case *sld.PointStyle:
			enabled, margin = style.LabelObstacle, style.ObstacleMargin
		case *sld.LineStyle:
			enabled, margin = style.LabelObstacle, style.ObstacleMargin
		case *sld.PolygonStyle:
			enabled, margin = style.LabelObstacle, style.ObstacleMargin
		}
		if !enabled {
			continue
		}
		box, ok := geometryLabelBox(command.geometry, r.transform, margin)
		if ok {
			result = append(result, box)
		}
	}
	return result
}

func geometryLabelBox(geometry *Geometry, transform *Transform, margin float64) (labelBox, bool) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	var visit func(*Geometry)
	visit = func(value *Geometry) {
		for _, coordinate := range value.Coordinates {
			if len(coordinate) >= 2 {
				x, y := transform.ToPixel(coordinate[0], coordinate[1])
				minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
			}
		}
		for _, ring := range value.Rings {
			for _, coordinate := range ring {
				if len(coordinate) >= 2 {
					x, y := transform.ToPixel(coordinate[0], coordinate[1])
					minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
				}
			}
		}
		for index := range value.Geometries {
			visit(&value.Geometries[index])
		}
	}
	visit(geometry)
	if math.IsInf(minX, 1) {
		return labelBox{}, false
	}
	return labelBox{minX - margin, minY - margin, maxX + margin, maxY + margin}, true
}

func (r *MapRenderer) placeLabel(anchor textPlacement, lines []string, width, height float64, style *sld.TextStyle, occupied []labelBox) (textPlacement, labelBox, bool) {
	space := math.Max(0, style.SpaceAround) + style.HaloRadius
	step, maximum := 4.0, math.Max(0, style.MaxDisplacement)
	for distance := 0.0; distance <= maximum; distance += step {
		attempts := [][2]float64{{0, 0}}
		if distance > 0 {
			attempts = [][2]float64{{distance, 0}, {-distance, 0}, {0, distance}, {0, -distance}, {distance, distance}, {-distance, distance}, {distance, -distance}, {-distance, -distance}}
		}
		for _, offset := range attempts {
			placement := anchor
			placement.x += offset[0]
			placement.y += offset[1]
			placement.lines = lines
			box := rotatedLabelBox(placement.x, placement.y, width, height, placement.angle, style.AnchorX, style.AnchorY, space)
			if !style.Partials && (box.minX < 0 || box.minY < 0 || box.maxX > float64(r.width) || box.maxY > float64(r.height)) {
				continue
			}
			if style.ConflictResolution && overlapsAny(box, occupied) {
				continue
			}
			return placement, box, true
		}
		if maximum == 0 {
			break
		}
	}
	return textPlacement{}, labelBox{}, false
}

func (r *MapRenderer) drawTextPlacement(placement textPlacement, style *sld.TextStyle) {
	r.dc.Push()
	defer r.dc.Pop()
	r.dc.SetFontFace(bundledFace(style))
	if placement.angle != 0 {
		r.dc.RotateAbout(gg.Radians(placement.angle), placement.x, placement.y)
	}
	fontSize := style.FontSize
	if fontSize <= 0 {
		fontSize = 12
	}
	for index, line := range placement.lines {
		y := placement.y + float64(index)*fontSize*1.15
		if style.HaloRadius > 0 {
			r.dc.SetColor(style.HaloColor)
			radius := style.HaloRadius
			for _, offset := range [][2]float64{{-radius, 0}, {radius, 0}, {0, -radius}, {0, radius}, {-radius, -radius}, {radius, -radius}, {-radius, radius}, {radius, radius}} {
				r.dc.DrawStringAnchored(line, placement.x+offset[0], y+offset[1], style.AnchorX, style.AnchorY)
			}
		}
		r.dc.SetColor(style.Color)
		r.dc.DrawStringAnchored(line, placement.x, y, style.AnchorX, style.AnchorY)
	}
}

func labelAnchors(geom *Geometry, transform *Transform, style *sld.TextStyle) []textPlacement {
	if geom == nil {
		return nil
	}
	if geom.Type == WKBLineString && style.FollowLine && style.Repeat > 0 {
		return repeatedLineAnchors(geom.Coordinates, transform, style)
	}
	x, y, angle, ok := labelAnchor(geom, transform)
	if !ok {
		return nil
	}
	if !style.FollowLine {
		angle = 0
	}
	angle += style.Rotation
	if style.ForceLeftToRight && (angle > 90 || angle < -90) {
		angle += 180
	}
	return []textPlacement{{x: x + style.DisplacementX, y: y - style.DisplacementY, angle: angle}}
}

func repeatedLineAnchors(points [][]float64, transform *Transform, style *sld.TextStyle) []textPlacement {
	if len(points) < 2 {
		return nil
	}
	var result []textPlacement
	elapsed, next := 0.0, style.Repeat/2
	for index := 1; index < len(points); index++ {
		x1, y1 := transform.ToPixel(points[index-1][0], points[index-1][1])
		x2, y2 := transform.ToPixel(points[index][0], points[index][1])
		length := math.Hypot(x2-x1, y2-y1)
		for length > 0 && next <= elapsed+length {
			ratio := (next - elapsed) / length
			angle := math.Atan2(y2-y1, x2-x1)*180/math.Pi + style.Rotation
			if style.ForceLeftToRight && (angle > 90 || angle < -90) {
				angle += 180
			}
			result = append(result, textPlacement{x: x1 + (x2-x1)*ratio + style.DisplacementX, y: y1 + (y2-y1)*ratio - style.DisplacementY, angle: angle})
			next += style.Repeat
		}
		elapsed += length
	}
	if len(result) == 0 {
		x, y, angle, ok := lineMidpoint(points, transform)
		if ok {
			result = append(result, textPlacement{x: x, y: y, angle: angle})
		}
	}
	return result
}

func wrapLabel(label string, width int) []string {
	if width <= 0 || len([]rune(label)) <= width {
		return []string{label}
	}
	var lines []string
	current := ""
	for _, word := range strings.Fields(label) {
		if current != "" && len([]rune(current))+1+len([]rune(word)) > width {
			lines = append(lines, current)
			current = word
		} else if current == "" {
			current = word
		} else {
			current += " " + word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{label}
	}
	return lines
}

func measureLabel(dc *gg.Context, lines []string, fontSize float64) (float64, float64) {
	width := 0.0
	for _, line := range lines {
		w, _ := dc.MeasureString(line)
		width = math.Max(width, w)
	}
	if fontSize <= 0 {
		fontSize = 12
	}
	return width, math.Max(fontSize, float64(len(lines))*fontSize*1.15)
}

func rotatedLabelBox(x, y, width, height, angle, anchorX, anchorY, space float64) labelBox {
	minX, minY := x-width*anchorX, y-height*(1-anchorY)
	if angle != 0 {
		radians := math.Abs(angle) * math.Pi / 180
		width, height = math.Abs(width*math.Cos(radians))+math.Abs(height*math.Sin(radians)), math.Abs(width*math.Sin(radians))+math.Abs(height*math.Cos(radians))
		minX, minY = x-width/2, y-height/2
	}
	return labelBox{minX: minX - space, minY: minY - space, maxX: minX + width + space, maxY: minY + height + space}
}

func overlapsAny(box labelBox, boxes []labelBox) bool {
	for _, other := range boxes {
		if box.minX < other.maxX && box.maxX > other.minX && box.minY < other.maxY && box.maxY > other.minY {
			return true
		}
	}
	return false
}

func labelAnchor(geom *Geometry, transform *Transform) (x, y, angle float64, ok bool) {
	switch geom.Type {
	case WKBPoint:
		if len(geom.Coordinates) > 0 {
			x, y = transform.ToPixel(geom.Coordinates[0][0], geom.Coordinates[0][1])
			ok = true
		}
	case WKBLineString:
		x, y, angle, ok = lineMidpoint(geom.Coordinates, transform)
	case WKBPolygon:
		if len(geom.Rings) > 0 {
			var sx, sy float64
			var n int
			for _, p := range geom.Rings[0] {
				if len(p) >= 2 {
					px, py := transform.ToPixel(p[0], p[1])
					sx += px
					sy += py
					n++
				}
			}
			if n > 0 {
				x = sx / float64(n)
				y = sy / float64(n)
				ok = true
			}
		}
	case WKBMultiPoint, WKBMultiLineString, WKBMultiPolygon, WKBGeometryCollection:
		if len(geom.Geometries) > 0 {
			return labelAnchor(&geom.Geometries[0], transform)
		}
	}
	return
}
func lineMidpoint(points [][]float64, t *Transform) (x, y, angle float64, ok bool) {
	if len(points) < 2 {
		return
	}
	mid := (len(points) - 1) / 2
	a, b := points[mid], points[mid+1]
	if len(a) < 2 || len(b) < 2 {
		return
	}
	x1, y1 := t.ToPixel(a[0], a[1])
	x2, y2 := t.ToPixel(b[0], b[1])
	return (x1 + x2) / 2, (y1 + y2) / 2, math.Atan2(y2-y1, x2-x1) * 180 / math.Pi, true
}
func formatFontSize(value float64) string {
	if value == 0 {
		return "12"
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}
