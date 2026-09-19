// Package stylegraphics resolves managed and allowlisted remote graphics for
// request-scoped portrayal. It is deliberately independent of the catalog so
// render workers never need to expose arbitrary filesystem access.
package stylegraphics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fogleman/gg"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/sld"
)

type Resolver struct {
	cfg         conf.WMS
	workspaceID string
	allowRemote bool
	client      *http.Client
	mu          sync.Mutex
	resolved    int
	assets      map[string]string
}

func New(cfg conf.WMS, workspaceID string, allowRemote bool, manifests ...map[string]string) *Resolver {
	timeout := time.Duration(cfg.ExternalGraphicTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	resolver := &Resolver{cfg: cfg, workspaceID: workspaceID, allowRemote: allowRemote}
	if len(manifests) > 0 {
		resolver.assets = manifests[0]
	}
	resolver.client = &http.Client{Timeout: timeout, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return resolver.validateRemoteURL(request.URL)
	}}
	return resolver
}

func (r *Resolver) Resolve(href, format string) (image.Image, error) {
	r.mu.Lock()
	r.resolved++
	count := r.resolved
	r.mu.Unlock()
	if maximum := r.cfg.MaxExternalGraphicsPerRender; maximum > 0 && count > maximum {
		return nil, fmt.Errorf("external graphic count exceeds %d", maximum)
	}
	href = strings.TrimSpace(href)
	var body []byte
	var err error
	switch {
	case strings.HasPrefix(href, "asset://") || strings.HasPrefix(href, "asset:"):
		name := strings.TrimPrefix(strings.TrimPrefix(href, "asset://"), "asset:")
		body, err = r.readAsset(name)
	case strings.HasPrefix(href, "data:"):
		body, format, err = r.readDataURL(href)
	case strings.HasPrefix(href, "https://"):
		body, err = r.readRemote(href)
	default:
		return nil, fmt.Errorf("ExternalGraphic must use asset:, data:, or allowlisted https")
	}
	if err != nil {
		return nil, err
	}
	return r.decode(body, format)
}

func (r *Resolver) maxBytes() int64 {
	if r.cfg.MaxStyleAssetBytes > 0 {
		return r.cfg.MaxStyleAssetBytes
	}
	return 5 << 20
}

func (r *Resolver) readAsset(name string) ([]byte, error) {
	if !sld.ValidStyleName(name) {
		return nil, fmt.Errorf("invalid managed graphic name")
	}
	root := r.cfg.StyleAssetPath
	if root == "" {
		root = "./data/style-assets"
	}
	if r.assets != nil {
		digest, ok := r.assets[name]
		if !ok {
			return nil, fmt.Errorf("managed graphic is not in the workspace asset manifest")
		}
		return ReadAssetObject(filepath.Join(root, r.workspaceID), name, digest, r.maxBytes())
	}
	return readBounded(filepath.Join(root, r.workspaceID, name), r.maxBytes())
}

func (r *Resolver) readDataURL(href string) ([]byte, string, error) {
	header, data, ok := strings.Cut(href, ",")
	if !ok {
		return nil, "", fmt.Errorf("invalid data URL")
	}
	format := strings.TrimPrefix(strings.Split(header, ";")[0], "data:")
	var body []byte
	var err error
	if strings.Contains(header, ";base64") {
		body, err = base64.StdEncoding.DecodeString(data)
	} else {
		var decoded string
		decoded, err = url.QueryUnescape(data)
		body = []byte(decoded)
	}
	if err != nil {
		return nil, "", fmt.Errorf("invalid data URL: %w", err)
	}
	if int64(len(body)) > r.maxBytes() {
		return nil, "", fmt.Errorf("graphic exceeds byte limit")
	}
	return body, format, nil
}

func (r *Resolver) validateRemoteURL(value *url.URL) error {
	if !r.allowRemote {
		return fmt.Errorf("remote graphics extension is disabled")
	}
	if value.Scheme != "https" || value.User != nil {
		return fmt.Errorf("remote graphics require an HTTPS URL without credentials")
	}
	origin := strings.ToLower(value.Scheme + "://" + value.Host)
	for _, allowed := range r.cfg.ExternalGraphicAllowedOrigins {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(allowed), "/"), origin) {
			return nil
		}
	}
	return fmt.Errorf("remote graphic origin %q is not allowlisted", origin)
}

func (r *Resolver) readRemote(href string) ([]byte, error) {
	parsed, err := url.Parse(href)
	if err != nil {
		return nil, fmt.Errorf("invalid remote graphic URL")
	}
	if err = r.validateRemoteURL(parsed); err != nil {
		return nil, err
	}
	cacheRoot := r.cfg.ExternalGraphicCachePath
	if cacheRoot == "" {
		cacheRoot = "./data/external-graphic-cache"
	}
	digest := sha256.Sum256([]byte(href))
	cachePath := filepath.Join(cacheRoot, hex.EncodeToString(digest[:]))
	if body, cacheErr := readBounded(cachePath, r.maxBytes()); cacheErr == nil {
		return body, nil
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, href, nil)
	response, err := r.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("remote graphic fetch failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("remote graphic returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, r.maxBytes()+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > r.maxBytes() {
		return nil, fmt.Errorf("remote graphic exceeds byte limit")
	}
	if err = os.MkdirAll(cacheRoot, 0o750); err == nil {
		if temporary, tempErr := os.CreateTemp(cacheRoot, ".graphic-*"); tempErr == nil {
			temporaryName := temporary.Name()
			if _, tempErr = temporary.Write(body); tempErr == nil {
				tempErr = temporary.Close()
			} else {
				_ = temporary.Close()
			}
			if tempErr == nil {
				tempErr = os.Rename(temporaryName, cachePath)
			}
			if tempErr != nil {
				_ = os.Remove(temporaryName)
			}
		}
	}
	return body, nil
}

func (r *Resolver) decode(body []byte, declared string) (image.Image, error) {
	declared = strings.ToLower(strings.TrimSpace(declared))
	trimmed := bytes.TrimSpace(body)
	if declared == "image/svg+xml" || bytes.HasPrefix(trimmed, []byte("<svg")) || bytes.HasPrefix(trimmed, []byte("<?xml")) {
		return renderSVG(body, r.cfg.MaxExternalGraphicDimension)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("invalid external graphic: %w", err)
	}
	if r.cfg.MaxExternalGraphicDimension > 0 && (config.Width > r.cfg.MaxExternalGraphicDimension || config.Height > r.cfg.MaxExternalGraphicDimension) {
		return nil, fmt.Errorf("external graphic dimensions exceed %d", r.cfg.MaxExternalGraphicDimension)
	}
	value, _, err := image.Decode(bytes.NewReader(body))
	return value, err
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(body)) > limit {
		return nil, fmt.Errorf("graphic exceeds byte limit")
	}
	return body, err
}

type svgRoot struct {
	XMLName xml.Name `xml:"svg"`
	Width   string   `xml:"width,attr"`
	Height  string   `xml:"height,attr"`
	ViewBox string   `xml:"viewBox,attr"`
}

func renderSVG(body []byte, maxDimension int) (image.Image, error) {
	var root svgRoot
	if err := xml.Unmarshal(body, &root); err != nil || root.XMLName.Local != "svg" {
		return nil, fmt.Errorf("invalid SVG")
	}
	width, height := svgLength(root.Width), svgLength(root.Height)
	var minX, minY, viewWidth, viewHeight float64
	if fields := strings.Fields(strings.ReplaceAll(root.ViewBox, ",", " ")); len(fields) == 4 {
		minX, _ = strconv.ParseFloat(fields[0], 64)
		minY, _ = strconv.ParseFloat(fields[1], 64)
		viewWidth, _ = strconv.ParseFloat(fields[2], 64)
		viewHeight, _ = strconv.ParseFloat(fields[3], 64)
	}
	if width <= 0 {
		width = viewWidth
	}
	if height <= 0 {
		height = viewHeight
	}
	if width <= 0 {
		width = 64
	}
	if height <= 0 {
		height = 64
	}
	if maxDimension > 0 && (width > float64(maxDimension) || height > float64(maxDimension)) {
		return nil, fmt.Errorf("SVG dimensions exceed %d", maxDimension)
	}
	dc := gg.NewContext(int(width), int(height))
	if viewWidth > 0 && viewHeight > 0 {
		dc.Scale(width/viewWidth, height/viewHeight)
		dc.Translate(-minX, -minY)
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "svg", "g", "title", "desc", "defs":
			continue
		case "circle":
			attrs := svgAttrs(start)
			applySVGStyle(dc, attrs)
			dc.DrawCircle(svgNumber(attrs["cx"]), svgNumber(attrs["cy"]), svgNumber(attrs["r"]))
			paintSVG(dc, attrs)
		case "ellipse":
			attrs := svgAttrs(start)
			applySVGStyle(dc, attrs)
			dc.Push()
			dc.Scale(1, svgNumber(attrs["ry"])/maxFloat(svgNumber(attrs["rx"]), .0001))
			dc.DrawCircle(svgNumber(attrs["cx"]), svgNumber(attrs["cy"])*maxFloat(svgNumber(attrs["rx"]), .0001)/maxFloat(svgNumber(attrs["ry"]), .0001), svgNumber(attrs["rx"]))
			paintSVG(dc, attrs)
			dc.Pop()
		case "rect":
			attrs := svgAttrs(start)
			applySVGStyle(dc, attrs)
			dc.DrawRectangle(svgNumber(attrs["x"]), svgNumber(attrs["y"]), svgNumber(attrs["width"]), svgNumber(attrs["height"]))
			paintSVG(dc, attrs)
		case "line":
			attrs := svgAttrs(start)
			applySVGStyle(dc, attrs)
			dc.DrawLine(svgNumber(attrs["x1"]), svgNumber(attrs["y1"]), svgNumber(attrs["x2"]), svgNumber(attrs["y2"]))
			dc.Stroke()
		case "polygon", "polyline":
			attrs := svgAttrs(start)
			applySVGStyle(dc, attrs)
			points := svgPoints(attrs["points"])
			for i, point := range points {
				if i == 0 {
					dc.MoveTo(point[0], point[1])
				} else {
					dc.LineTo(point[0], point[1])
				}
			}
			if start.Name.Local == "polygon" {
				dc.ClosePath()
			}
			paintSVG(dc, attrs)
		case "path":
			return nil, fmt.Errorf("SVG path elements are not supported by the bounded mark renderer")
		default:
			return nil, fmt.Errorf("unsupported SVG element %q", start.Name.Local)
		}
	}
	return dc.Image(), nil
}

// ValidateSVG applies the same bounded, no-reference SVG grammar used by the
// renderer. Management uploads use it to avoid storing active SVG content.
func ValidateSVG(body []byte, maxDimension int) error {
	_, err := renderSVG(body, maxDimension)
	return err
}

func svgAttrs(start xml.StartElement) map[string]string {
	result := map[string]string{}
	for _, attr := range start.Attr {
		result[attr.Name.Local] = attr.Value
	}
	if style := result["style"]; style != "" {
		for _, part := range strings.Split(style, ";") {
			key, value, ok := strings.Cut(part, ":")
			if ok {
				result[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
	}
	return result
}
func svgLength(value string) float64 {
	value = strings.TrimSpace(strings.TrimSuffix(value, "px"))
	result, _ := strconv.ParseFloat(value, 64)
	return result
}
func svgNumber(value string) float64 {
	result, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return result
}
func svgPoints(value string) [][2]float64 {
	fields := strings.Fields(strings.ReplaceAll(value, ",", " "))
	result := make([][2]float64, 0, len(fields)/2)
	for index := 0; index+1 < len(fields); index += 2 {
		x, _ := strconv.ParseFloat(fields[index], 64)
		y, _ := strconv.ParseFloat(fields[index+1], 64)
		result = append(result, [2]float64{x, y})
	}
	return result
}
func applySVGStyle(dc *gg.Context, attrs map[string]string) {
	stroke := attrs["stroke"]
	if stroke == "" {
		stroke = "#000000"
	}
	dc.SetLineWidth(maxFloat(1, svgNumber(attrs["stroke-width"])))
	dc.SetColor(parseSVGColor(stroke))
}
func paintSVG(dc *gg.Context, attrs map[string]string) {
	fill := attrs["fill"]
	if fill == "none" {
		dc.Stroke()
		return
	}
	if fill == "" {
		fill = "#000000"
	}
	dc.SetColor(parseSVGColor(fill))
	dc.FillPreserve()
	if attrs["stroke"] != "" && attrs["stroke"] != "none" {
		dc.SetColor(parseSVGColor(attrs["stroke"]))
		dc.Stroke()
	} else {
		dc.ClearPath()
	}
}
func parseSVGColor(value string) color.Color {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "#") {
		hexValue := strings.TrimPrefix(value, "#")
		if len(hexValue) == 3 {
			hexValue = string([]byte{hexValue[0], hexValue[0], hexValue[1], hexValue[1], hexValue[2], hexValue[2]})
		}
		if decoded, err := hex.DecodeString(hexValue); err == nil && len(decoded) >= 3 {
			return color.NRGBA{R: decoded[0], G: decoded[1], B: decoded[2], A: 255}
		}
	}
	return color.Black
}
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
