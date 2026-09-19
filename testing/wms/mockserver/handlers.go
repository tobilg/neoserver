package mockserver

import (
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"strconv"
	"strings"
)

// Handler handles WMS requests for the mock server.
type Handler struct {
	baseURL string
	layers  []MockLayer
}

// NewHandler creates a new WMS handler.
func NewHandler(baseURL string) *Handler {
	return &Handler{
		baseURL: baseURL,
		layers:  DefaultMockLayers(),
	}
}

// ServeHTTP handles WMS requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters (case-insensitive)
	params := make(map[string]string)
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			params[strings.ToUpper(key)] = values[0]
		}
	}

	// Check SERVICE parameter
	service := params["SERVICE"]
	if service != "" && !strings.EqualFold(service, "WMS") {
		h.sendException(w, "InvalidParameterValue", "SERVICE must be WMS", "")
		return
	}

	// Route based on REQUEST parameter
	request := params["REQUEST"]
	switch strings.ToUpper(request) {
	case "GETCAPABILITIES":
		h.handleGetCapabilities(w, r, params)
	case "GETMAP":
		h.handleGetMap(w, r, params)
	case "GETFEATUREINFO":
		h.handleGetFeatureInfo(w, r, params)
	case "GETLEGENDGRAPHIC":
		h.handleGetLegendGraphic(w, r, params)
	case "":
		h.sendException(w, "MissingParameterValue", "REQUEST parameter is required", "")
	default:
		h.sendException(w, "OperationNotSupported", fmt.Sprintf("Request type '%s' is not supported", request), "")
	}
}

func (h *Handler) handleGetCapabilities(w http.ResponseWriter, r *http.Request, params map[string]string) {
	// Check VERSION for negotiation
	version := params["VERSION"]
	if version != "" && version != "1.3.0" {
		// For WMS 1.3.0 conformance, we only support 1.3.0
		// Return 1.3.0 capabilities anyway (version negotiation)
	}

	// Check UpdateSequence
	updateSeq := params["UPDATESEQUENCE"]
	if updateSeq != "" {
		currentSeq := "1"
		if updateSeq == currentSeq {
			h.sendException(w, "CurrentUpdateSequence", "Request UpdateSequence is equal to server", "")
			return
		}
		// Try to parse as number
		reqSeq, err1 := strconv.Atoi(updateSeq)
		curSeq, err2 := strconv.Atoi(currentSeq)
		if err1 == nil && err2 == nil && reqSeq > curSeq {
			h.sendException(w, "InvalidUpdateSequence", "Request UpdateSequence is higher than server", "")
			return
		}
	}

	cfg := DefaultCapabilitiesConfig(h.baseURL)
	capsXML, err := GenerateCapabilities(cfg)
	if err != nil {
		h.sendException(w, "", fmt.Sprintf("Failed to generate capabilities: %v", err), "")
		return
	}

	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n"))
	w.Write(capsXML)
}

func (h *Handler) handleGetMap(w http.ResponseWriter, r *http.Request, params map[string]string) {
	// Get exception format for error responses
	exceptionFormat := params["EXCEPTIONS"]
	if exceptionFormat == "" {
		exceptionFormat = "XML"
	}

	// Validate required parameters
	if err := h.validateGetMapParams(params); err != nil {
		h.sendExceptionWithFormat(w, err.code, err.message, exceptionFormat, params)
		return
	}

	// Parse dimensions
	width, _ := strconv.Atoi(params["WIDTH"])
	height, _ := strconv.Atoi(params["HEIGHT"])
	format := params["FORMAT"]
	transparent := strings.EqualFold(params["TRANSPARENT"], "TRUE")
	bgcolor := params["BGCOLOR"]

	// Generate image
	img := h.generateMapImage(width, height, transparent, bgcolor)

	// Encode and send response
	switch strings.ToLower(format) {
	case "image/png", "image/png; mode=8bit":
		w.Header().Set("Content-Type", "image/png")
		png.Encode(w, img)
	case "image/jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
		jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	case "image/gif":
		w.Header().Set("Content-Type", "image/gif")
		gif.Encode(w, img, nil)
	default:
		h.sendExceptionWithFormat(w, "InvalidFormat", fmt.Sprintf("Format '%s' is not supported", format), exceptionFormat, params)
	}
}

func (h *Handler) handleGetFeatureInfo(w http.ResponseWriter, r *http.Request, params map[string]string) {
	// Validate GetMap parameters first (GetFeatureInfo is an extension of GetMap)
	if err := h.validateGetMapParams(params); err != nil {
		h.sendException(w, err.code, err.message, "")
		return
	}

	// Validate GetFeatureInfo-specific parameters
	queryLayers := params["QUERY_LAYERS"]
	if queryLayers == "" {
		h.sendException(w, "MissingParameterValue", "QUERY_LAYERS parameter is required", "")
		return
	}

	infoFormat := params["INFO_FORMAT"]
	if infoFormat == "" {
		infoFormat = "text/xml" // Default
	}

	// Validate I and J parameters
	iStr := params["I"]
	jStr := params["J"]
	if iStr == "" || jStr == "" {
		h.sendException(w, "MissingParameterValue", "I and J parameters are required", "")
		return
	}

	i, err := strconv.Atoi(iStr)
	if err != nil {
		h.sendException(w, "InvalidParameterValue", fmt.Sprintf("I parameter '%s' is not a valid integer", iStr), "")
		return
	}

	j, err := strconv.Atoi(jStr)
	if err != nil {
		h.sendException(w, "InvalidParameterValue", fmt.Sprintf("J parameter '%s' is not a valid integer", jStr), "")
		return
	}

	width, _ := strconv.Atoi(params["WIDTH"])
	height, _ := strconv.Atoi(params["HEIGHT"])

	// Validate I and J are within bounds
	if i < 0 || i >= width {
		h.sendException(w, "InvalidPoint", fmt.Sprintf("I value %d is out of range [0, %d)", i, width), "")
		return
	}
	if j < 0 || j >= height {
		h.sendException(w, "InvalidPoint", fmt.Sprintf("J value %d is out of range [0, %d)", j, height), "")
		return
	}

	// Check if query layers are queryable
	for _, layerName := range strings.Split(queryLayers, ",") {
		layerName = strings.TrimSpace(layerName)
		layer := GetLayerByName(layerName)
		if layer == nil {
			h.sendException(w, "LayerNotDefined", fmt.Sprintf("Layer '%s' is not defined", layerName), "")
			return
		}
		if !layer.Queryable {
			h.sendException(w, "LayerNotQueryable", fmt.Sprintf("Layer '%s' is not queryable", layerName), "")
			return
		}
	}

	// Generate mock feature info response
	features := []map[string]interface{}{
		{"name": "Mock Feature 1", "value": 42, "x": i, "y": j},
		{"name": "Mock Feature 2", "value": 17, "x": i + 1, "y": j + 1},
	}

	switch strings.ToLower(infoFormat) {
	case "text/xml", "application/vnd.ogc.wms_xml":
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.Write(GenerateFeatureInfoXML(queryLayers, features))
	case "application/vnd.ogc.gml":
		w.Header().Set("Content-Type", "application/vnd.ogc.gml")
		w.Write(GenerateFeatureInfoGML(queryLayers, features))
	case "application/json", "application/geo+json":
		w.Header().Set("Content-Type", "application/json")
		w.Write(GenerateFeatureInfoJSON(queryLayers, features))
	case "text/plain":
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(fmt.Sprintf("Feature Info for %s at (%d, %d)\nFeatures: 2\n", queryLayers, i, j)))
	case "text/html":
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf("<html><body><h1>Feature Info</h1><p>Layer: %s</p><p>Position: (%d, %d)</p></body></html>", queryLayers, i, j)))
	default:
		h.sendException(w, "InvalidFormat", fmt.Sprintf("INFO_FORMAT '%s' is not supported", infoFormat), "")
	}
}

func (h *Handler) handleGetLegendGraphic(w http.ResponseWriter, r *http.Request, params map[string]string) {
	// Validate LAYER parameter
	layerName := params["LAYER"]
	if layerName == "" {
		h.sendException(w, "MissingParameterValue", "LAYER parameter is required", "")
		return
	}

	layer := GetLayerByName(layerName)
	if layer == nil {
		h.sendException(w, "LayerNotDefined", fmt.Sprintf("Layer '%s' is not defined", layerName), "")
		return
	}

	// Get format (default to PNG)
	format := params["FORMAT"]
	if format == "" {
		format = "image/png"
	}

	// Get dimensions
	width := 20
	height := 20
	if w := params["WIDTH"]; w != "" {
		width, _ = strconv.Atoi(w)
	}
	if h := params["HEIGHT"]; h != "" {
		height, _ = strconv.Atoi(h)
	}

	// Generate simple legend graphic
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Fill with a color based on layer name hash
	hashColor := hashToColor(layerName)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, hashColor)
		}
	}

	// Encode response
	switch strings.ToLower(format) {
	case "image/png":
		w.Header().Set("Content-Type", "image/png")
		png.Encode(w, img)
	case "image/jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
		jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	case "image/gif":
		w.Header().Set("Content-Type", "image/gif")
		gif.Encode(w, img, nil)
	default:
		h.sendException(w, "InvalidFormat", fmt.Sprintf("FORMAT '%s' is not supported", format), "")
	}
}

type validationError struct {
	code    string
	message string
}

func (h *Handler) validateGetMapParams(params map[string]string) *validationError {
	// Check VERSION
	version := params["VERSION"]
	if version == "" {
		return &validationError{"MissingParameterValue", "VERSION parameter is required"}
	}
	if version != "1.3.0" {
		return &validationError{"InvalidParameterValue", fmt.Sprintf("VERSION '%s' is not supported", version)}
	}

	// Check LAYERS
	layers := params["LAYERS"]
	if layers == "" {
		return &validationError{"MissingParameterValue", "LAYERS parameter is required"}
	}

	// Validate each layer exists
	for _, layerName := range strings.Split(layers, ",") {
		layerName = strings.TrimSpace(layerName)
		if GetLayerByName(layerName) == nil {
			return &validationError{"LayerNotDefined", fmt.Sprintf("Layer '%s' is not defined", layerName)}
		}
	}

	// Check STYLES (can be empty)
	styles, ok := params["STYLES"]
	if !ok {
		return &validationError{"MissingParameterValue", "STYLES parameter is required"}
	}

	// Validate styles - each non-empty style must be valid for its layer
	layerNames := strings.Split(layers, ",")
	styleNames := strings.Split(styles, ",")
	for i, styleName := range styleNames {
		styleName = strings.TrimSpace(styleName)
		if styleName == "" {
			continue // Empty string means use default style
		}
		// Get the corresponding layer
		if i < len(layerNames) {
			layerName := strings.TrimSpace(layerNames[i])
			layer := GetLayerByName(layerName)
			if layer != nil {
				// Check if the style is valid for this layer
				styleFound := false
				for _, s := range layer.Styles {
					if strings.EqualFold(s.Name, styleName) {
						styleFound = true
						break
					}
				}
				if !styleFound {
					return &validationError{"StyleNotDefined", fmt.Sprintf("Style '%s' is not defined for layer '%s'", styleName, layerName)}
				}
			}
		}
	}

	// Check CRS
	crs := params["CRS"]
	if crs == "" {
		return &validationError{"MissingParameterValue", "CRS parameter is required"}
	}
	if !h.isValidCRS(crs) {
		return &validationError{"InvalidCRS", fmt.Sprintf("CRS '%s' is not supported", crs)}
	}

	// Check BBOX
	bbox := params["BBOX"]
	if bbox == "" {
		return &validationError{"MissingParameterValue", "BBOX parameter is required"}
	}
	if err := h.validateBBox(bbox); err != nil {
		return err
	}

	// Check WIDTH
	widthStr := params["WIDTH"]
	if widthStr == "" {
		return &validationError{"MissingParameterValue", "WIDTH parameter is required"}
	}
	width, err := strconv.Atoi(widthStr)
	if err != nil || width <= 0 {
		return &validationError{"InvalidParameterValue", fmt.Sprintf("WIDTH '%s' is not a valid positive integer", widthStr)}
	}
	if width > 4096 {
		return &validationError{"InvalidParameterValue", fmt.Sprintf("WIDTH %d exceeds maximum of 4096", width)}
	}

	// Check HEIGHT
	heightStr := params["HEIGHT"]
	if heightStr == "" {
		return &validationError{"MissingParameterValue", "HEIGHT parameter is required"}
	}
	height, err := strconv.Atoi(heightStr)
	if err != nil || height <= 0 {
		return &validationError{"InvalidParameterValue", fmt.Sprintf("HEIGHT '%s' is not a valid positive integer", heightStr)}
	}
	if height > 4096 {
		return &validationError{"InvalidParameterValue", fmt.Sprintf("HEIGHT %d exceeds maximum of 4096", height)}
	}

	// Check FORMAT
	format := params["FORMAT"]
	if format == "" {
		return &validationError{"MissingParameterValue", "FORMAT parameter is required"}
	}
	if !h.isValidFormat(format) {
		return &validationError{"InvalidFormat", fmt.Sprintf("FORMAT '%s' is not supported", format)}
	}

	return nil
}

func (h *Handler) validateBBox(bbox string) *validationError {
	parts := strings.Split(bbox, ",")
	if len(parts) != 4 {
		return &validationError{"InvalidParameterValue", "BBOX must have exactly 4 values"}
	}

	values := make([]float64, 4)
	for i, part := range parts {
		val, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return &validationError{"InvalidParameterValue", fmt.Sprintf("BBOX value '%s' is not a valid number", part)}
		}
		values[i] = val
	}

	minX, minY, maxX, maxY := values[0], values[1], values[2], values[3]

	// Check for invalid bbox (minX > maxX or minY > maxY)
	if minX > maxX {
		return &validationError{"InvalidParameterValue", "BBOX minX is greater than maxX"}
	}
	if minY > maxY {
		return &validationError{"InvalidParameterValue", "BBOX minY is greater than maxY"}
	}

	return nil
}

func (h *Handler) isValidCRS(crs string) bool {
	validCRS := []string{
		"CRS:84",
		"EPSG:4326",
		"EPSG:3857",
		"EPSG:32632",
	}
	for _, valid := range validCRS {
		if strings.EqualFold(crs, valid) {
			return true
		}
	}
	return false
}

func (h *Handler) isValidFormat(format string) bool {
	validFormats := []string{
		"image/png",
		"image/jpeg",
		"image/gif",
		"image/png; mode=8bit",
	}
	for _, valid := range validFormats {
		if strings.EqualFold(format, valid) {
			return true
		}
	}
	return false
}

func (h *Handler) sendException(w http.ResponseWriter, code, message, locator string) {
	h.sendExceptionWithFormat(w, code, message, "XML", nil)
}

func (h *Handler) sendExceptionWithFormat(w http.ResponseWriter, code, message, format string, params map[string]string) {
	switch strings.ToUpper(format) {
	case "INIMAGE":
		// Return error as image
		width := 256
		height := 256
		if params != nil {
			if w := params["WIDTH"]; w != "" {
				width, _ = strconv.Atoi(w)
			}
			if h := params["HEIGHT"]; h != "" {
				height, _ = strconv.Atoi(h)
			}
		}
		img := h.generateErrorImage(width, height, code, message)
		w.Header().Set("Content-Type", "image/png")
		png.Encode(w, img)

	case "BLANK":
		// Return blank/transparent image
		width := 256
		height := 256
		transparent := true
		bgcolor := ""
		if params != nil {
			if w := params["WIDTH"]; w != "" {
				width, _ = strconv.Atoi(w)
			}
			if h := params["HEIGHT"]; h != "" {
				height, _ = strconv.Atoi(h)
			}
			transparent = strings.EqualFold(params["TRANSPARENT"], "TRUE")
			bgcolor = params["BGCOLOR"]
		}
		img := h.generateMapImage(width, height, transparent, bgcolor)
		w.Header().Set("Content-Type", "image/png")
		png.Encode(w, img)

	default: // XML
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK) // WMS returns 200 even for exceptions
		w.Write(GenerateExceptionXML(code, message))
	}
}

func (h *Handler) generateMapImage(width, height int, transparent bool, bgcolor string) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Set background color
	var bgColor color.RGBA
	if transparent {
		bgColor = color.RGBA{0, 0, 0, 0}
	} else if bgcolor != "" {
		bgColor = parseHexColor(bgcolor)
	} else {
		bgColor = color.RGBA{255, 255, 255, 255}
	}

	// Fill background
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, bgColor)
		}
	}

	// Draw some content to make it look like a map
	// Draw a simple grid
	gridColor := color.RGBA{200, 200, 200, 255}
	if transparent {
		gridColor = color.RGBA{100, 100, 100, 128}
	}
	gridSpacing := 32
	for y := 0; y < height; y += gridSpacing {
		for x := 0; x < width; x++ {
			img.Set(x, y, gridColor)
		}
	}
	for x := 0; x < width; x += gridSpacing {
		for y := 0; y < height; y++ {
			img.Set(x, y, gridColor)
		}
	}

	// Draw a colored shape in the center
	centerX, centerY := width/2, height/2
	shapeColor := color.RGBA{0, 128, 255, 200}
	radius := min(width, height) / 6
	for y := centerY - radius; y <= centerY+radius; y++ {
		for x := centerX - radius; x <= centerX+radius; x++ {
			if x >= 0 && x < width && y >= 0 && y < height {
				dx := x - centerX
				dy := y - centerY
				if dx*dx+dy*dy <= radius*radius {
					img.Set(x, y, shapeColor)
				}
			}
		}
	}

	return img
}

func (h *Handler) generateErrorImage(width, height int, code, message string) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Red background for error
	bgColor := color.RGBA{255, 200, 200, 255}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, bgColor)
		}
	}

	// Draw a red X
	lineColor := color.RGBA{255, 0, 0, 255}
	for i := 0; i < min(width, height); i++ {
		x1 := i
		y1 := i
		x2 := width - 1 - i
		if x1 < width && y1 < height {
			img.Set(x1, y1, lineColor)
		}
		if x2 >= 0 && y1 < height {
			img.Set(x2, y1, lineColor)
		}
	}

	return img
}

func parseHexColor(hex string) color.RGBA {
	hex = strings.TrimPrefix(hex, "0x")
	hex = strings.TrimPrefix(hex, "#")

	if len(hex) == 6 {
		var r, g, b uint8
		fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
		return color.RGBA{r, g, b, 255}
	}
	return color.RGBA{255, 255, 255, 255}
}

func hashToColor(s string) color.RGBA {
	var hash uint32
	for _, c := range s {
		hash = hash*31 + uint32(c)
	}
	return color.RGBA{
		R: uint8(hash & 0xFF),
		G: uint8((hash >> 8) & 0xFF),
		B: uint8((hash >> 16) & 0xFF),
		A: 255,
	}
}
