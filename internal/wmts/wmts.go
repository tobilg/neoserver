// Package wmts implements the WMTS 1.0.0 KVP and REST bindings.
package wmts

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"image/png"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/tiles"
	"github.com/tobilg/neoserver/internal/wms"
	"github.com/tobilg/neoserver/internal/workspace"
)

const version = "1.0.0"

type WorkspaceDependencies struct {
	Config conf.Config
	Logger *slog.Logger
	Engine *tiles.Engine
}

type handler struct {
	cfg    conf.Config
	logger *slog.Logger
	engine *tiles.Engine
}

func RegisterWorkspaceRoutes(r chi.Router, deps WorkspaceDependencies) {
	h := &handler{cfg: deps.Config, logger: deps.Logger, engine: deps.Engine}
	if h.logger == nil {
		h.logger = slog.Default()
	}
	r.Get("/", h.kvp)
	r.Get("/1.0.0/WMTSCapabilities.xml", h.capabilities)
	r.Get("/1.0.0/{layer}/{style}/legend.png", h.legend)
	r.Get("/1.0.0/{layer}/{style}/{tileMatrixSet}/{tileMatrix}/{tileRow}/{tileCol}.{extension}", h.restTile)
	r.Get("/1.0.0/{layer}/{style}/{tileMatrixSet}/{tileMatrix}/{tileRow}/{tileCol}/{i}/{j}.{extension}", h.restFeatureInfo)
}

func (h *handler) kvp(w http.ResponseWriter, r *http.Request) {
	parameters := normalizedQuery(r.URL.Query())
	service := parameters.Get("SERVICE")
	if service == "" {
		h.exception(w, http.StatusBadRequest, "MissingParameterValue", "service", "SERVICE is required")
		return
	}
	if !strings.EqualFold(service, "WMTS") {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "service", "SERVICE must be WMTS")
		return
	}
	switch strings.ToLower(parameters.Get("REQUEST")) {
	case "getcapabilities":
		h.capabilities(w, r)
	case "gettile":
		h.getTile(w, r, tileRequestFromKVP(parameters))
	case "getfeatureinfo":
		h.getFeatureInfo(w, r, featureInfoRequestFromKVP(parameters))
	case "":
		h.exception(w, http.StatusBadRequest, "MissingParameterValue", "request", "REQUEST is required")
	default:
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "request", "unsupported WMTS operation")
	}
}

func (h *handler) capabilities(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.workspace(w, r)
	if !ok {
		return
	}
	if h.requireAuth(w, r, ws) {
		return
	}
	parameters := normalizedQuery(r.URL.Query())
	if versions := parameters.Get("ACCEPTVERSIONS"); versions != "" && !containsTrimmed(strings.Split(versions, ","), version) {
		h.exception(w, http.StatusBadRequest, "VersionNegotiationFailed", "", "Only WMTS 1.0.0 is supported")
		return
	}
	mediaType := "application/xml"
	for _, candidate := range strings.Split(parameters.Get("ACCEPTFORMATS"), ",") {
		if candidate = strings.TrimSpace(candidate); candidate == "application/xml" || candidate == "text/xml" {
			mediaType = candidate
			break
		}
	}
	sections, errCode, errMessage := requestedCapabilitySections(parameters)
	if errCode != "" {
		h.exception(w, http.StatusBadRequest, errCode, "sections", errMessage)
		return
	}
	sequence := ws.CapabilitiesRevision()
	equalSequence := false
	if value := parameters.Get("UPDATESEQUENCE"); value != "" {
		requested, err := strconv.ParseInt(value, 10, 64)
		if err != nil || requested < 0 || requested > sequence {
			h.exception(w, 400, "InvalidUpdateSequence", "", "invalid or newer update sequence")
			return
		}
		equalSequence = requested == sequence
	}
	base := h.baseURL(r, ws.Name)
	capabilities := capabilitiesFor(ws)
	resources := ws.VisibleResources(workspaceRole(r, ws.ID))
	var document bytes.Buffer
	document.WriteString(xml.Header)
	document.WriteString(`<Capabilities xmlns="http://www.opengis.net/wmts/1.0" xmlns:ows="http://www.opengis.net/ows/1.1" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://www.opengis.net/wmts/1.0 http://schemas.opengis.net/wmts/1.0/wmtsGetCapabilities_response.xsd" version="1.0.0" updateSequence="`)
	fmt.Fprintf(&document, `%d">`, sequence)
	if equalSequence {
		document.WriteString(`</Capabilities>`)
		w.Header().Set("Content-Type", mediaType)
		_, _ = w.Write(document.Bytes())
		return
	}
	if sections.includes("ServiceIdentification") {
		writeServiceIdentification(&document, ws)
	}
	if sections.includes("ServiceProvider") {
		writeServiceProvider(&document, ws)
	}
	if sections.includes("OperationsMetadata") {
		writeOperationsMetadata(&document, capabilities, base)
	}
	if sections.includes("Contents") {
		writeContents(&document, base, ws, resources, capabilities, h.cfg.Tiles.MinZoom, h.cfg.Tiles.MaxZoom)
	}
	if sections.includes("Themes") && len(resources) > 0 {
		writeThemes(&document, resources)
	}
	if sections.all {
		document.WriteString(`<ServiceMetadataURL xlink:href="`)
		xmlAttr(&document, base+"/1.0.0/WMTSCapabilities.xml")
		document.WriteString(`"/>`)
	}
	document.WriteString(`</Capabilities>`)
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(document.Bytes())
}

func containsTrimmed(values []string, expected string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == expected {
			return true
		}
	}
	return false
}

func (h *handler) legend(w http.ResponseWriter, r *http.Request) {
	ws, ok := h.workspace(w, r)
	if !ok || h.requireAuth(w, r, ws) {
		return
	}
	resource := ws.GetResource(chi.URLParam(r, "layer"))
	if resource == nil || resource.Kind == workspace.ResourceGroup || !resourceVisible(ws, resource, workspaceRole(r, ws.ID)) {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "layer", "layer is not available")
		return
	}
	style := chi.URLParam(r, "style")
	if !styleAvailable(resource, style) {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "style", "style is not available for the layer")
		return
	}
	legend, err := wms.RenderResourceLegend(r.Context(), h.cfg, ws, resource, style, 20, 20)
	if err != nil {
		h.logger.Error("WMTS legend failed", "layer", resource.PublicID(), "error", err)
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "style", "legend is unavailable")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_ = png.Encode(w, legend)
}

type tileRequest struct {
	Version, Layer, Style, Format, MatrixSet string
	Time, Elevation                          string
	Matrix, Row, Column                      int
	MissingParameter                         string
	InvalidParameter                         string
	AllowAnyFormat                           bool
}

func tileRequestFromKVP(q url.Values) tileRequest {
	request := tileRequest{Version: q.Get("VERSION"), Layer: q.Get("LAYER"), Style: q.Get("STYLE"), Format: q.Get("FORMAT"), MatrixSet: q.Get("TILEMATRIXSET"), Time: q.Get("TIME"), Elevation: q.Get("ELEVATION")}
	required := []struct {
		value   string
		locator string
	}{
		{request.Version, "version"}, {request.Layer, "layer"}, {request.Style, "style"},
		{request.Format, "format"}, {request.MatrixSet, "TileMatrixSet"},
		{q.Get("TILEMATRIX"), "TileMatrix"}, {q.Get("TILEROW"), "TileRow"}, {q.Get("TILECOL"), "TileCol"},
	}
	for _, parameter := range required {
		if parameter.value == "" {
			request.MissingParameter = parameter.locator
			return request
		}
	}
	parseTileCoordinates(&request, q.Get("TILEMATRIX"), q.Get("TILEROW"), q.Get("TILECOL"))
	return request
}

func (h *handler) restTile(w http.ResponseWriter, r *http.Request) {
	request := tileRequest{Version: version, Layer: chi.URLParam(r, "layer"), Style: chi.URLParam(r, "style"), MatrixSet: chi.URLParam(r, "tileMatrixSet"), Format: tileFormatForExtension(chi.URLParam(r, "extension")), Time: r.URL.Query().Get("time"), Elevation: r.URL.Query().Get("elevation")}
	if request.Format == "" {
		request.InvalidParameter = "format"
	}
	parseTileCoordinates(&request, chi.URLParam(r, "tileMatrix"), chi.URLParam(r, "tileRow"), chi.URLParam(r, "tileCol"))
	h.getTile(w, r, request)
}

func parseTileCoordinates(request *tileRequest, matrix, row, column string) {
	coordinates := []struct {
		raw     string
		locator string
		target  *int
	}{{matrix, "TileMatrix", &request.Matrix}, {row, "TileRow", &request.Row}, {column, "TileCol", &request.Column}}
	for _, coordinate := range coordinates {
		value, err := strconv.Atoi(coordinate.raw)
		if err != nil {
			if request.InvalidParameter == "" {
				request.InvalidParameter = coordinate.locator
			}
			return
		}
		*coordinate.target = value
	}
}

func (h *handler) getTile(w http.ResponseWriter, r *http.Request, request tileRequest) {
	ws, resource, tileType, ok := h.validateTileRequest(w, r, request)
	if !ok {
		return
	}
	if !h.resolveDimensions(w, resource, &request) {
		return
	}
	result, err := h.engine.Fetch(r.Context(), tiles.EngineRequest{
		Workspace: ws, Resource: resource, TileType: tileType, MatrixSet: request.MatrixSet,
		Zoom: request.Matrix, Column: request.Column, Row: request.Row,
		Format: request.Format, Style: request.Style, UseCache: ws.Settings.OGCTilesAPI.Settings.CacheEnabled,
		Time: request.Time, Elevation: request.Elevation,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, tiles.ErrRenderQueueFull) {
			status = http.StatusServiceUnavailable
		}
		h.logger.Error("WMTS tile failed", "layer", request.Layer, "error", err)
		h.exception(w, status, "NoApplicableCode", "", "tile generation failed")
		return
	}
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("X-Cache", result.CacheStatus)
	w.Header().Set("X-Cache-Tier", result.CacheTier)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Data)
}

type featureInfoRequest struct {
	tileRequest
	I, J       int
	InfoFormat string
	ParseInfo  error
}

func featureInfoRequestFromKVP(q url.Values) featureInfoRequest {
	request := featureInfoRequest{tileRequest: tileRequestFromKVP(q), InfoFormat: q.Get("INFOFORMAT")}
	request.I, request.ParseInfo = strconv.Atoi(q.Get("I"))
	if request.ParseInfo == nil {
		request.J, request.ParseInfo = strconv.Atoi(q.Get("J"))
	}
	return request
}

func (h *handler) restFeatureInfo(w http.ResponseWriter, r *http.Request) {
	i, iErr := strconv.Atoi(chi.URLParam(r, "i"))
	j, jErr := strconv.Atoi(chi.URLParam(r, "j"))
	request := featureInfoRequest{tileRequest: tileRequest{Version: version, Layer: chi.URLParam(r, "layer"), Style: chi.URLParam(r, "style"), MatrixSet: chi.URLParam(r, "tileMatrixSet"), Time: r.URL.Query().Get("time"), Elevation: r.URL.Query().Get("elevation"), AllowAnyFormat: true}, I: i, J: j, InfoFormat: infoFormatForExtension(chi.URLParam(r, "extension")), ParseInfo: errors.Join(iErr, jErr)}
	parseTileCoordinates(&request.tileRequest, chi.URLParam(r, "tileMatrix"), chi.URLParam(r, "tileRow"), chi.URLParam(r, "tileCol"))
	h.getFeatureInfo(w, r, request)
}

func (h *handler) getFeatureInfo(w http.ResponseWriter, r *http.Request, request featureInfoRequest) {
	ws, resource, _, ok := h.validateTileRequest(w, r, request.tileRequest)
	if !ok {
		return
	}
	if !h.resolveDimensions(w, resource, &request.tileRequest) {
		return
	}
	if !ws.Settings.WMTS.FeatureInfoEnabled {
		h.exception(w, http.StatusBadRequest, "OperationNotSupported", "request", "GetFeatureInfo is disabled")
		return
	}
	if request.ParseInfo != nil || request.I < 0 || request.I >= 256 || request.J < 0 || request.J >= 256 {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "i/j", "I and J must identify a pixel in the 256 by 256 tile")
		return
	}
	if !supportedInfoFormat(request.InfoFormat) {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "infoformat", "unsupported feature info format")
		return
	}
	bounds, _ := tiles.TileBBox(request.MatrixSet, request.Matrix, request.Column, request.Row)
	pixelX := (bounds.MaxX - bounds.MinX) / 256
	pixelY := (bounds.MaxY - bounds.MinY) / 256
	properties, err := wms.QueryResourceFeatureInfo(r.Context(), ws, resource, wms.PointFeatureInfoRequest{
		X:         bounds.MinX + (float64(request.I)+0.5)*pixelX,
		Y:         bounds.MaxY - (float64(request.J)+0.5)*pixelY,
		PixelSize: max(pixelX, pixelY), CRS: fmt.Sprintf("EPSG:%d", tiles.GetTMSSRID(request.MatrixSet)),
		SRID: tiles.GetTMSSRID(request.MatrixSet), FeatureCount: h.cfg.WMTS.MaxFeatureInfoResults,
		Time: request.Time, Elevation: request.Elevation,
	})
	if err != nil {
		h.logger.Error("WMTS feature info failed", "layer", request.Layer, "error", err)
		h.exception(w, http.StatusInternalServerError, "NoApplicableCode", "", "feature info query failed")
		return
	}
	writeFeatureInfo(w, request.InfoFormat, request.Layer, properties)
}

func (h *handler) validateTileRequest(w http.ResponseWriter, r *http.Request, request tileRequest) (*workspace.Workspace, *workspace.PublishedResource, string, bool) {
	ws, ok := h.workspace(w, r)
	if !ok || h.requireAuth(w, r, ws) {
		return nil, nil, "", false
	}
	if request.MissingParameter != "" {
		h.exception(w, http.StatusBadRequest, "MissingParameterValue", request.MissingParameter, request.MissingParameter+" is required")
		return nil, nil, "", false
	}
	if request.InvalidParameter != "" {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", request.InvalidParameter, request.InvalidParameter+" is invalid")
		return nil, nil, "", false
	}
	if request.Version != version {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "version", "VERSION must be 1.0.0")
		return nil, nil, "", false
	}
	capabilities := capabilitiesFor(ws)
	if !contains(capabilities.TileMatrixSets, request.MatrixSet) {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "TileMatrixSet", "tile matrix set is not enabled")
		return nil, nil, "", false
	}
	definition, err := tiles.GetTileMatrixSetDefinition(request.MatrixSet)
	var matrix *tiles.TileMatrix
	if err == nil {
		for i := range definition.TileMatrices {
			if definition.TileMatrices[i].ID == strconv.Itoa(request.Matrix) {
				matrix = &definition.TileMatrices[i]
				break
			}
		}
	}
	if matrix == nil || request.Matrix < h.cfg.Tiles.MinZoom || request.Matrix > h.cfg.Tiles.MaxZoom {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "TileMatrix", "tile matrix is not enabled")
		return nil, nil, "", false
	}
	if request.Row < 0 || request.Row >= matrix.MatrixHeight {
		h.exception(w, http.StatusBadRequest, "TileOutOfRange", "TileRow", "tile row is outside the configured matrix")
		return nil, nil, "", false
	}
	if request.Column < 0 || request.Column >= matrix.MatrixWidth {
		h.exception(w, http.StatusBadRequest, "TileOutOfRange", "TileCol", "tile column is outside the configured matrix")
		return nil, nil, "", false
	}
	resource := ws.GetResource(request.Layer)
	if resource == nil || !resourceVisible(ws, resource, workspaceRole(r, ws.ID)) {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "layer", "layer is not available")
		return nil, nil, "", false
	}
	if ws.Settings.WMTS.TileMatrixLimitsEnabled {
		if limits, ok := resourceMatrixLimits(resource, request.MatrixSet, *matrix); ok {
			if request.Row < limits.minRow || request.Row > limits.maxRow {
				h.exception(w, 400, "TileOutOfRange", "TileRow", "tile row is outside the layer extent")
				return nil, nil, "", false
			}
			if request.Column < limits.minCol || request.Column > limits.maxCol {
				h.exception(w, 400, "TileOutOfRange", "TileCol", "tile column is outside the layer extent")
				return nil, nil, "", false
			}
		}
	}
	if !styleAvailable(resource, request.Style) {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "style", "style is not available for the layer")
		return nil, nil, "", false
	}
	if request.AllowAnyFormat {
		return ws, resource, "map", true
	}
	tileType, supported := tileRequestType(ws, resource, request.Format)
	if !supported {
		h.exception(w, http.StatusBadRequest, "InvalidParameterValue", "format", "tile format is not enabled for this layer")
		return nil, nil, "", false
	}
	if h.engine == nil {
		h.exception(w, http.StatusServiceUnavailable, "NoApplicableCode", "", "tile engine is unavailable")
		return nil, nil, "", false
	}
	return ws, resource, tileType, true
}

func (h *handler) workspace(w http.ResponseWriter, r *http.Request) (*workspace.Workspace, bool) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok || ws.Settings == nil || !ws.Settings.WMTS.Enabled {
		h.exception(w, http.StatusForbidden, "OperationNotSupported", "service", "WMTS is not enabled for this workspace")
		return nil, false
	}
	return ws, true
}

func (h *handler) requireAuth(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) bool {
	if ws.Settings.WMTS.Public {
		return false
	}
	if h.cfg.Auth.RequireHTTPS && !identity.IsSecureTransport(r.Context()) {
		h.exception(w, http.StatusUpgradeRequired, "AccessDenied", "transport", "HTTPS required")
		return true
	}
	principal, ok := identity.FromContext(r.Context())
	if !ok || principal == nil {
		h.exception(w, http.StatusUnauthorized, "AccessDenied", "authorization", "authentication required")
		return true
	}
	if !principal.HasWorkspaceAccess(ws.ID) {
		h.exception(w, http.StatusForbidden, "AccessDenied", "authorization", "no access to this workspace")
		return true
	}
	return false
}

func (h *handler) exception(w http.ResponseWriter, status int, code, locator, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	var document bytes.Buffer
	document.WriteString(xml.Header)
	document.WriteString(`<ows:ExceptionReport xmlns:ows="http://www.opengis.net/ows/1.1" version="1.0.0"><ows:Exception exceptionCode="`)
	xmlAttr(&document, code)
	if locator != "" {
		document.WriteString(`" locator="`)
		xmlAttr(&document, locator)
	}
	document.WriteString(`"><ows:ExceptionText>`)
	xmlText(&document, message)
	document.WriteString(`</ows:ExceptionText></ows:Exception></ows:ExceptionReport>`)
	_, _ = w.Write(document.Bytes())
}

func normalizedQuery(input url.Values) url.Values {
	result := make(url.Values, len(input))
	for key, values := range input {
		for _, value := range values {
			result.Add(strings.ToUpper(key), value)
		}
	}
	return result
}

func workspaceRole(r *http.Request, workspaceID string) string {
	if principal, ok := identity.FromContext(r.Context()); ok && principal != nil {
		return principal.GetWorkspaceRole(workspaceID)
	}
	return ""
}

func resourceVisible(ws *workspace.Workspace, resource *workspace.PublishedResource, role string) bool {
	if resource.Layer != nil {
		return resource.Layer.VisibleToRole(role)
	}
	if resource.Coverage != nil {
		return resource.Coverage.VisibleToRole(role)
	}
	return resource.Group != nil && ws != nil && ws.GroupVisibleToRole(resource.Group, role)
}

func styleAvailable(resource *workspace.PublishedResource, style string) bool {
	if style == "" || style == "default" {
		return true
	}
	if resource.Layer != nil {
		return resource.Layer.DefaultStyle == style || contains(resource.Layer.Styles, style)
	}
	if resource.Coverage != nil {
		return resource.Coverage.DefaultStyle == style || contains(resource.Coverage.Styles, style)
	}
	return resource.Group != nil && (resource.Group.DefaultStyle == style || contains(resource.Group.Styles, style))
}

func (h *handler) baseURL(r *http.Request, workspaceName string) string {
	base := strings.TrimRight(h.cfg.Server.UrlBase, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base + strings.TrimRight(h.cfg.Server.BasePath, "/") + "/workspaces/" + url.PathEscape(workspaceName) + "/wmts"
}

var capabilitySectionOrder = []string{"ServiceIdentification", "ServiceProvider", "OperationsMetadata", "Contents", "Themes"}

type capabilitySectionSet struct {
	all      bool
	selected map[string]bool
}

func (s capabilitySectionSet) includes(section string) bool {
	return s.all || s.selected[section]
}

func requestedCapabilitySections(parameters url.Values) (capabilitySectionSet, string, string) {
	values, present := parameters["SECTIONS"]
	if !present {
		return capabilitySectionSet{all: true}, "", ""
	}
	raw := strings.TrimSpace(strings.Join(values, ","))
	if raw == "" {
		return capabilitySectionSet{}, "MissingParameterValue", "SECTIONS must not be empty"
	}
	result := capabilitySectionSet{selected: make(map[string]bool)}
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			return capabilitySectionSet{}, "InvalidParameterValue", "SECTIONS contains an empty section name"
		}
		if strings.EqualFold(value, "All") {
			result.all = true
			continue
		}
		matched := ""
		for _, section := range capabilitySectionOrder {
			if strings.EqualFold(value, section) {
				matched = section
				break
			}
		}
		if matched == "" {
			return capabilitySectionSet{}, "InvalidParameterValue", "unsupported SECTIONS value"
		}
		result.selected[matched] = true
	}
	return result, "", ""
}

func writeServiceIdentification(document *bytes.Buffer, ws *workspace.Workspace) {
	document.WriteString(`<ows:ServiceIdentification><ows:Title>`)
	xmlText(document, firstNonEmpty(ws.Settings.WMTS.Title, ws.Name+" WMTS"))
	document.WriteString(`</ows:Title>`)
	if abstract := firstNonEmpty(ws.Settings.WMTS.Abstract, ws.Description); abstract != "" {
		document.WriteString(`<ows:Abstract>`)
		xmlText(document, abstract)
		document.WriteString(`</ows:Abstract>`)
	}
	document.WriteString(`<ows:ServiceType>OGC WMTS</ows:ServiceType><ows:ServiceTypeVersion>1.0.0</ows:ServiceTypeVersion></ows:ServiceIdentification>`)
}

func writeServiceProvider(document *bytes.Buffer, ws *workspace.Workspace) {
	settings := ws.Settings.WMTS
	document.WriteString(`<ows:ServiceProvider><ows:ProviderName>`)
	xmlText(document, firstNonEmpty(strings.TrimSpace(settings.ProviderName), "neoserver"))
	document.WriteString(`</ows:ProviderName>`)
	if settings.ProviderSite != "" {
		document.WriteString(`<ows:ProviderSite xlink:href="`)
		xmlAttr(document, settings.ProviderSite)
		document.WriteString(`"/>`)
	}
	document.WriteString(`<ows:ServiceContact>`)
	if settings.ContactName != "" {
		document.WriteString(`<ows:IndividualName>`)
		xmlText(document, settings.ContactName)
		document.WriteString(`</ows:IndividualName>`)
	}
	if settings.ContactPosition != "" {
		document.WriteString(`<ows:PositionName>`)
		xmlText(document, settings.ContactPosition)
		document.WriteString(`</ows:PositionName>`)
	}
	if settings.ContactEmail != "" {
		document.WriteString(`<ows:ContactInfo><ows:Address><ows:ElectronicMailAddress>`)
		xmlText(document, settings.ContactEmail)
		document.WriteString(`</ows:ElectronicMailAddress></ows:Address></ows:ContactInfo>`)
	}
	document.WriteString(`</ows:ServiceContact></ows:ServiceProvider>`)
}

func writeOperationsMetadata(document *bytes.Buffer, capabilities effectiveCapabilities, base string) {
	document.WriteString(`<ows:OperationsMetadata>`)
	for _, operation := range capabilities.Operations {
		writeOperation(document, operation, base)
	}
	document.WriteString(`</ows:OperationsMetadata>`)
}

func writeOperation(document *bytes.Buffer, name, base string) {
	document.WriteString(`<ows:Operation name="`)
	xmlAttr(document, name)
	document.WriteString(`"><ows:DCP><ows:HTTP><ows:Get xlink:href="`)
	xmlAttr(document, base+"?")
	document.WriteString(`"><ows:Constraint name="GetEncoding"><ows:AllowedValues><ows:Value>KVP</ows:Value></ows:AllowedValues></ows:Constraint></ows:Get></ows:HTTP></ows:DCP>`)
	if name == "GetCapabilities" {
		document.WriteString(`<ows:Parameter name="AcceptFormats"><ows:AllowedValues><ows:Value>application/xml</ows:Value><ows:Value>text/xml</ows:Value></ows:AllowedValues></ows:Parameter>`)
		document.WriteString(`<ows:Parameter name="Sections"><ows:AllowedValues>`)
		for _, section := range append(append([]string(nil), capabilitySectionOrder...), "All") {
			document.WriteString(`<ows:Value>`)
			xmlText(document, section)
			document.WriteString(`</ows:Value>`)
		}
		document.WriteString(`</ows:AllowedValues></ows:Parameter>`)
	}
	document.WriteString(`</ows:Operation>`)
}

func writeContents(document *bytes.Buffer, base string, ws *workspace.Workspace, resources []*workspace.PublishedResource, capabilities effectiveCapabilities, minZoom, maxZoom int) {
	document.WriteString(`<Contents>`)
	for _, resource := range resources {
		writeLayer(document, base, ws, resource, capabilities, minZoom, maxZoom)
	}
	for _, matrixSetID := range capabilities.TileMatrixSets {
		definition, err := tiles.GetTileMatrixSetDefinition(matrixSetID)
		if err == nil {
			writeMatrixSet(document, definition, minZoom, maxZoom)
		}
	}
	document.WriteString(`</Contents>`)
}

func writeThemes(document *bytes.Buffer, resources []*workspace.PublishedResource) {
	document.WriteString(`<Themes><Theme><ows:Title>All layers</ows:Title><ows:Identifier>default</ows:Identifier>`)
	for _, resource := range resources {
		document.WriteString(`<LayerRef>`)
		xmlText(document, resource.PublicID())
		document.WriteString(`</LayerRef>`)
	}
	document.WriteString(`</Theme></Themes>`)
}

func writeLayer(document *bytes.Buffer, base string, ws *workspace.Workspace, resource *workspace.PublishedResource, capabilities effectiveCapabilities, minZoom, maxZoom int) {
	identifier, title, description := resource.PublicID(), resource.PublicID(), ""
	styles := []string{}
	defaultStyle := "default"
	if resource.Layer != nil {
		title, description, styles = firstNonEmpty(resource.Layer.Title, identifier), resource.Layer.Description, resource.Layer.Styles
		if resource.Layer.DefaultStyle != "" {
			defaultStyle = resource.Layer.DefaultStyle
		}
	} else if resource.Coverage != nil {
		title, description, styles = firstNonEmpty(resource.Coverage.Title, identifier), resource.Coverage.Description, resource.Coverage.Styles
		if resource.Coverage.DefaultStyle != "" {
			defaultStyle = resource.Coverage.DefaultStyle
		}
	} else if resource.Group != nil {
		title, description, styles = firstNonEmpty(resource.Group.Title, identifier), resource.Group.Description, resource.Group.Styles
		if resource.Group.DefaultStyle != "" {
			defaultStyle = resource.Group.DefaultStyle
		}
	}
	document.WriteString(`<Layer><ows:Title>`)
	xmlText(document, title)
	document.WriteString(`</ows:Title>`)
	if description != "" {
		document.WriteString(`<ows:Abstract>`)
		xmlText(document, description)
		document.WriteString(`</ows:Abstract>`)
	}
	document.WriteString(`<ows:Identifier>`)
	xmlText(document, identifier)
	document.WriteString(`</ows:Identifier><Style isDefault="true"><ows:Identifier>`)
	xmlText(document, defaultStyle)
	document.WriteString(`</ows:Identifier>`)
	writeLegendURL(document, base, resource, defaultStyle)
	document.WriteString(`</Style>`)
	for _, style := range styles {
		if style == defaultStyle {
			continue
		}
		document.WriteString(`<Style isDefault="false"><ows:Identifier>`)
		xmlText(document, style)
		document.WriteString(`</ows:Identifier>`)
		writeLegendURL(document, base, resource, style)
		document.WriteString(`</Style>`)
	}

	formats := tileFormatsForResource(ws, resource)
	for _, format := range formats {
		document.WriteString(`<Format>`)
		xmlText(document, format)
		document.WriteString(`</Format>`)
	}
	if capabilities.FeatureInfo {
		for _, format := range infoFormats() {
			document.WriteString(`<InfoFormat>`)
			xmlText(document, format)
			document.WriteString(`</InfoFormat>`)
		}
	}
	writeDimensions(document, resource)
	for _, matrixSet := range capabilities.TileMatrixSets {
		document.WriteString(`<TileMatrixSetLink><TileMatrixSet>`)
		xmlText(document, matrixSet)
		document.WriteString(`</TileMatrixSet>`)
		if ws.Settings.WMTS.TileMatrixLimitsEnabled {
			writeMatrixLimits(document, resource, matrixSet, minZoom, maxZoom)
		}
		document.WriteString(`</TileMatrixSetLink>`)
	}
	for _, format := range formats {
		extension := extensionForTileFormat(format)
		if extension == "" {
			continue
		}
		document.WriteString(`<ResourceURL format="`)
		xmlAttr(document, format)
		document.WriteString(`" resourceType="tile" template="`)
		xmlAttr(document, base+"/1.0.0/{Layer}/{Style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}."+extension)
		document.WriteString(`"/>`)
	}
	if capabilities.FeatureInfo {
		for _, format := range infoFormats() {
			document.WriteString(`<ResourceURL format="`)
			xmlAttr(document, format)
			document.WriteString(`" resourceType="FeatureInfo" template="`)
			xmlAttr(document, base+"/1.0.0/{Layer}/{Style}/{TileMatrixSet}/{TileMatrix}/{TileRow}/{TileCol}/{I}/{J}."+extensionForInfoFormat(format))
			document.WriteString(`"/>`)
		}
	}
	document.WriteString(`</Layer>`)
}

func writeMatrixSet(document *bytes.Buffer, definition *tiles.TileMatrixSetDefinition, minZoom, maxZoom int) {
	document.WriteString(`<TileMatrixSet><ows:Title>`)
	xmlText(document, definition.Title)
	document.WriteString(`</ows:Title><ows:Identifier>`)
	xmlText(document, definition.ID)
	document.WriteString(`</ows:Identifier><ows:SupportedCRS>`)
	crs := definition.CRS
	if definition.ID == tiles.TMSWebMercatorQuad {
		crs = "urn:ogc:def:crs:EPSG:6.18:3:3857"
	} else if definition.ID == tiles.TMSWorldCRS84Quad {
		crs = "urn:ogc:def:crs:OGC:1.3:CRS84"
	}
	xmlText(document, crs)
	document.WriteString(`</ows:SupportedCRS>`)
	// WorldCRS84Quad starts at 2x1 tiles, omitting the coarsest GoogleCRS84Quad
	// scale. Do not claim that complete legacy scale set for this grid.
	if definition.WellKnownScaleSet != "" && definition.ID != tiles.TMSWorldCRS84Quad && minZoom == 0 {
		document.WriteString(`<WellKnownScaleSet>`)
		xmlText(document, strings.Replace(definition.WellKnownScaleSet, "http://www.opengis.net/def/wkss/OGC/1.0/", "urn:ogc:def:wkss:OGC:1.0:", 1))
		document.WriteString(`</WellKnownScaleSet>`)
	}
	for _, matrix := range definition.TileMatrices {
		zoom, err := strconv.Atoi(matrix.ID)
		if err != nil || zoom < minZoom || zoom > maxZoom {
			continue
		}
		document.WriteString(`<TileMatrix><ows:Identifier>`)
		xmlText(document, matrix.ID)
		fmt.Fprintf(document, `</ows:Identifier><ScaleDenominator>%.15g</ScaleDenominator><TopLeftCorner>%.15g %.15g</TopLeftCorner><TileWidth>%d</TileWidth><TileHeight>%d</TileHeight><MatrixWidth>%d</MatrixWidth><MatrixHeight>%d</MatrixHeight></TileMatrix>`, matrix.ScaleDenominator, matrix.PointOfOrigin[0], matrix.PointOfOrigin[1], matrix.TileWidth, matrix.TileHeight, matrix.MatrixWidth, matrix.MatrixHeight)
	}
	document.WriteString(`</TileMatrixSet>`)
}

func writeLegendURL(document *bytes.Buffer, base string, resource *workspace.PublishedResource, style string) {
	if resource.Kind == workspace.ResourceGroup {
		return
	}
	document.WriteString(`<LegendURL format="image/png" width="20" height="20" xlink:href="`)
	xmlAttr(document, base+"/1.0.0/"+url.PathEscape(resource.PublicID())+"/"+url.PathEscape(style)+"/legend.png")
	document.WriteString(`"/>`)
}

func writeFeatureInfo(w http.ResponseWriter, format, layer string, properties []map[string]interface{}) {
	switch format {
	case "application/json", "application/geo+json":
		features := make([]map[string]interface{}, len(properties))
		for index, values := range properties {
			features[index] = map[string]interface{}{"type": "Feature", "geometry": nil, "properties": values}
		}
		w.Header().Set("Content-Type", format)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"type": "FeatureCollection", "name": layer, "features": features})
	case "text/html":
		w.Header().Set("Content-Type", format)
		_, _ = fmt.Fprintf(w, "<!doctype html><html><body><h1>%s</h1>", html.EscapeString(layer))
		for _, values := range properties {
			_, _ = w.Write([]byte("<dl>"))
			for key, value := range values {
				_, _ = fmt.Fprintf(w, "<dt>%s</dt><dd>%s</dd>", html.EscapeString(key), html.EscapeString(fmt.Sprint(value)))
			}
			_, _ = w.Write([]byte("</dl>"))
		}
		_, _ = w.Write([]byte("</body></html>"))
	case "text/plain":
		w.Header().Set("Content-Type", format)
		for _, values := range properties {
			for key, value := range values {
				_, _ = fmt.Fprintf(w, "%s=%v\n", key, value)
			}
		}
	default:
		w.Header().Set("Content-Type", "application/xml")
		var document bytes.Buffer
		document.WriteString(xml.Header + `<FeatureInfoResponse><Layer name="`)
		xmlAttr(&document, layer)
		document.WriteString(`">`)
		for _, values := range properties {
			document.WriteString(`<Feature>`)
			for key, value := range values {
				document.WriteString(`<Property name="`)
				xmlAttr(&document, key)
				document.WriteString(`">`)
				xmlText(&document, fmt.Sprint(value))
				document.WriteString(`</Property>`)
			}
			document.WriteString(`</Feature>`)
		}
		document.WriteString(`</Layer></FeatureInfoResponse>`)
		_, _ = w.Write(document.Bytes())
	}
}

func tileFormatForExtension(extension string) string {
	switch strings.ToLower(extension) {
	case "png":
		return tiles.MediaTypePNG
	case "jpg", "jpeg":
		return tiles.MediaTypeJPEG
	case "webp":
		return tiles.MediaTypeWEBP
	case "mvt", "pbf":
		return tiles.MediaTypeMVT
	default:
		return ""
	}
}

func extensionForTileFormat(format string) string {
	switch format {
	case tiles.MediaTypePNG:
		return "png"
	case tiles.MediaTypeJPEG:
		return "jpeg"
	case tiles.MediaTypeWEBP:
		return "webp"
	case tiles.MediaTypeMVT:
		return "mvt"
	default:
		return ""
	}
}

func infoFormats() []string {
	return []string{"application/json", "application/geo+json", "application/xml", "text/html", "text/plain"}
}
func supportedInfoFormat(format string) bool { return contains(infoFormats(), format) }
func infoFormatForExtension(extension string) string {
	switch strings.ToLower(extension) {
	case "json":
		return "application/json"
	case "geojson":
		return "application/geo+json"
	case "html":
		return "text/html"
	case "txt":
		return "text/plain"
	case "xml":
		return "application/xml"
	default:
		return ""
	}
}
func extensionForInfoFormat(format string) string {
	switch format {
	case "application/json":
		return "json"
	case "application/geo+json":
		return "geojson"
	case "text/html":
		return "html"
	case "text/plain":
		return "txt"
	default:
		return "xml"
	}
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func xmlText(document *bytes.Buffer, value string) { _ = xml.EscapeText(document, []byte(value)) }
func xmlAttr(document *bytes.Buffer, value string) { xmlText(document, value) }
