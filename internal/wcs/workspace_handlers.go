package wcs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/cache"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/rastergrid"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/workspace"
)

type WorkspaceDependencies struct {
	Config   conf.Config
	Logger   *slog.Logger
	Registry *workspace.Registry
	Cache    *cache.Manager
}
type handler struct {
	cfg    conf.Config
	logger *slog.Logger
	slots  chan struct{}
	cache  *cache.Manager
}

func RegisterWorkspaceRoutes(r chi.Router, deps WorkspaceDependencies) {
	count := deps.Config.WCS.MaxConcurrentRequests
	if count <= 0 {
		count = runtime.GOMAXPROCS(0) / 2
		if count < 1 {
			count = 1
		}
		if count > 4 {
			count = 4
		}
	}
	h := &handler{cfg: deps.Config, logger: deps.Logger, slots: make(chan struct{}, count), cache: deps.Cache}
	r.Get("/", h.handle)
	r.Post("/", h.handlePost)
}

func normalizeQuery(r *http.Request) url.Values {
	q := url.Values{}
	for k, v := range r.URL.Query() {
		key := strings.ToUpper(k)
		q[key] = append(q[key], v...)
	}
	return q
}

func (h *handler) handle(w http.ResponseWriter, r *http.Request) {
	ws, ok := workspace.FromContext(r.Context())
	if !ok {
		writeException(w, invalid("workspace", "workspace not found"))
		return
	}
	if ws.Settings == nil || !ws.Settings.WCS.Enabled {
		writeException(w, &requestError{Code: "OperationNotSupported", Text: "WCS is not enabled for this workspace", Status: http.StatusNotFound})
		return
	}
	if h.requireAuth(w, r, ws, ws.Settings.WCS.Public) {
		return
	}
	q := normalizeQuery(r)
	request := strings.ToUpper(q.Get("REQUEST"))
	if request == "" {
		writeException(w, missing("request"))
		return
	}
	if service := q.Get("SERVICE"); !strings.EqualFold(service, "WCS") {
		if service == "" {
			writeException(w, missing("service"))
		} else {
			writeException(w, invalid("service", "SERVICE must be WCS"))
		}
		return
	}
	version, err := negotiateVersion(request, q)
	if err != nil {
		writeException(w, err)
		return
	}
	switch request {
	case "GETCAPABILITIES":
		h.getCapabilities(w, r, ws, version)
	case "DESCRIBECOVERAGE":
		h.describeCoverage(w, r, ws, version, q)
	case "GETCOVERAGE":
		h.getCoverage(w, r, ws, version, q)
	default:
		writeException(w, &requestError{Code: "OperationNotSupported", Locator: "request", Text: "unsupported WCS request"})
	}
}

func negotiateVersion(request string, q url.Values) (string, *requestError) {
	if request == "GETCAPABILITIES" {
		values := q.Get("ACCEPTVERSIONS")
		if values == "" {
			if v := q.Get("VERSION"); v != "" {
				values = v
			} else {
				return "2.1.0", nil
			}
		}
		for _, v := range strings.Split(values, ",") {
			v = strings.TrimSpace(v)
			if v == "2.1.0" || v == "2.0.1" {
				return v, nil
			}
		}
		return "", &requestError{Code: "VersionNegotiationFailed", Locator: "acceptversions", Text: "supported versions are 2.1.0 and 2.0.1"}
	}
	v := q.Get("VERSION")
	if v == "" {
		return "", missing("version")
	}
	if v != "2.1.0" && v != "2.0.1" {
		return "", invalid("version", "supported versions are 2.1.0 and 2.0.1")
	}
	return v, nil
}

func (h *handler) getCapabilities(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, version string) {
	callerRole := role(r, ws.ID)
	cacheKey := cache.WCSCapabilitiesKey(ws.ID) + "|version=" + version + "|role=" + callerRole
	cacheFill := h.cache.BeginFill(cache.CacheTypeCapabilities, cacheKey)
	if h.cache != nil {
		if body, ok := h.cache.GetCapabilities(cacheKey); ok {
			w.Header().Set("X-Cache", "HIT")
			writeXML(w, body)
			return
		}
	}
	items := ws.VisibleCoverages(callerRole)
	sort.Slice(items, func(i, j int) bool { return items[i].PublicID < items[j].PublicID })
	capabilities := newCapabilityRegistry(version, ws.Settings.WCS)
	coverageInfo := make(map[string]*datasource.CoverageInfo, len(items))
	for _, item := range items {
		svc, cov := ws.GetCoverage(item.PublicID)
		if cov == nil || svc == nil || svc.CoverageSource == nil {
			continue
		}
		if info, err := svc.CoverageSource.GetCoverageInfo(r.Context(), cov.SourceCoverage); err == nil {
			coverageInfo[item.PublicID] = info
			if capabilities.enabled(extCRS) && len(capabilities.subsettingCRS) == 0 && len(capabilities.outputCRS) == 0 && info.CRS != "" {
				capabilities.subsettingCRS = appendUnique(capabilities.subsettingCRS, info.CRS)
				capabilities.outputCRS = appendUnique(capabilities.outputCRS, info.CRS)
			}
		}
	}
	title := ws.Settings.WCS.Title
	if title == "" {
		title = ws.Name + " WCS"
	}
	endpoint := strings.TrimRight(h.cfg.Server.UrlBase, "/") + h.cfg.Server.BasePath + "/workspaces/" + url.PathEscape(ws.Name) + "/wcs"
	body := capabilitiesXML(version, endpoint, title, ws.Settings.WCS.Abstract, items, coverageInfo, capabilities)
	if h.cache != nil {
		h.cache.SetCapabilities(cacheKey, body, cacheFill)
	}
	writeXML(w, body)
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func coverageIDs(q url.Values) []string {
	var out []string
	for _, raw := range append(q["COVERAGEID"], q["COVERAGEIDS"]...) {
		for _, id := range strings.Split(raw, ",") {
			if id = strings.TrimSpace(id); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

func (h *handler) describeCoverage(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, version string, q url.Values) {
	ids := coverageIDs(q)
	if len(ids) == 0 {
		writeException(w, missing("coverageId"))
		return
	}
	var docs [][]byte
	for _, id := range ids {
		svc, cov := visibleCoverage(ws, id, role(r, ws.ID))
		if cov == nil || svc.CoverageSource == nil {
			writeException(w, noCoverage())
			return
		}
		info, err := svc.CoverageSource.GetCoverageInfo(r.Context(), cov.SourceCoverage)
		if err != nil {
			h.internal(w, err)
			return
		}
		descriptor := datasource.DescriptorFromInfo(cov.SourceCoverage, info)
		if generalized, ok := svc.CoverageSource.(datasource.CoverageQueryDataSource); ok {
			if described, describeErr := generalized.DescribeCoverage(r.Context(), cov.SourceCoverage); describeErr == nil && described != nil {
				descriptor = described
			}
		}
		doc := coverageDescriptionXML(version, cov, info, descriptor)
		docs = append(docs, doc)
	}
	if len(docs) == 1 {
		writeXML(w, docs[0])
		return
	}
	body := joinDescriptions(version, docs)
	writeXML(w, body)
}

func joinDescriptions(version string, docs [][]byte) []byte {
	ns := "http://www.opengis.net/wcs/2.1"
	if version == "2.0.1" {
		ns = "http://www.opengis.net/wcs/2.0"
	} else {
		ns = "http://www.opengis.net/wcs/2.1/gml"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?><wcs:CoverageDescriptions xmlns:wcs="%s" xmlns:cis="http://www.opengis.net/cis/1.1/gml" xmlns:gml="http://www.opengis.net/gml/3.2" xmlns:gmlcov="http://www.opengis.net/gmlcov/1.0" xmlns:swe="http://www.opengis.net/swe/2.0">`, ns)
	for _, doc := range docs {
		s := string(doc)
		// Match the singular child, not the CoverageDescriptions root whose
		// name has the same prefix.
		start := strings.Index(s, "<wcs:CoverageDescription ")
		if start < 0 {
			start = strings.Index(s, "<wcs:CoverageDescription>")
		}
		end := strings.LastIndex(s, "</wcs:CoverageDescription>")
		if start >= 0 && end >= start {
			b.WriteString(s[start : end+len("</wcs:CoverageDescription>")])
		}
	}
	b.WriteString(`</wcs:CoverageDescriptions>`)
	return []byte(b.String())
}

func (h *handler) getCoverage(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, version string, q url.Values) {
	ids := coverageIDs(q)
	if len(ids) != 1 {
		if len(ids) == 0 {
			writeException(w, missing("coverageId"))
		} else {
			writeException(w, invalid("coverageId", "GetCoverage requires exactly one coverage identifier"))
		}
		return
	}
	svc, cov := visibleCoverage(ws, ids[0], role(r, ws.ID))
	if cov == nil || svc.CoverageSource == nil {
		writeException(w, noCoverage())
		return
	}
	info, err := svc.CoverageSource.GetCoverageInfo(r.Context(), cov.SourceCoverage)
	if err != nil {
		h.internal(w, err)
		return
	}
	capabilities := newCapabilityRegistry(version, ws.Settings.WCS)
	if capabilities.enabled(extCRS) && len(capabilities.subsettingCRS) == 0 && len(capabilities.outputCRS) == 0 {
		capabilities.subsettingCRS = []string{info.CRS}
		capabilities.outputCRS = []string{info.CRS}
	}
	plan, reqErr := buildCoveragePlan(q, version, info, cov, capabilities)
	if reqErr != nil {
		writeException(w, reqErr)
		return
	}
	descriptor := datasource.DescriptorFromInfo(cov.SourceCoverage, info)
	if generalized, ok := svc.CoverageSource.(datasource.CoverageQueryDataSource); ok {
		if described, describeErr := generalized.DescribeCoverage(r.Context(), cov.SourceCoverage); describeErr == nil && described != nil {
			descriptor = described
		}
	}
	maxDimensions := h.cfg.WCS.MaxDimensions
	if maxDimensions <= 0 {
		maxDimensions = 8
	}
	if ws.Settings.WCS.MaxDimensions > 0 {
		maxDimensions = ws.Settings.WCS.MaxDimensions
	}
	if descriptor != nil && len(descriptor.Axes) > maxDimensions {
		writeException(w, &requestError{Code: "OperationProcessingFailed", Locator: "subset", Text: "coverage exceeds the dimension limit"})
		return
	}
	maxAxisValues := h.cfg.WCS.MaxAxisValues
	if maxAxisValues <= 0 {
		maxAxisValues = 1_000_000
	}
	if ws.Settings.WCS.MaxAxisValues > 0 {
		maxAxisValues = ws.Settings.WCS.MaxAxisValues
	}
	if retainedAxisValues(descriptor, plan.query.DomainSubsets) > maxAxisValues {
		writeException(w, &requestError{Code: "OperationProcessingFailed", Locator: "subset", Text: "requested coverage exceeds the nonspatial axis-value limit"})
		return
	}
	plan.query.MaxSourceGranules = h.cfg.WCS.MaxSourceGranules
	if plan.query.MaxSourceGranules <= 0 {
		plan.query.MaxSourceGranules = 10_000
	}
	if ws.Settings.WCS.MaxSourceGranules > 0 {
		plan.query.MaxSourceGranules = ws.Settings.WCS.MaxSourceGranules
	}
	plan.query.MaxTemporaryBytes = h.cfg.WCS.MaxTemporaryBytes
	if plan.query.MaxTemporaryBytes <= 0 {
		plan.query.MaxTemporaryBytes = h.cfg.WCS.MaxOutputBytes
	}
	if ws.Settings.WCS.MaxTemporaryBytes > 0 {
		plan.query.MaxTemporaryBytes = ws.Settings.WCS.MaxTemporaryBytes
	}
	plan.query.TemporaryDirectory = h.cfg.WCS.TemporaryDirectory
	maxCells := h.cfg.WCS.MaxCells
	if ws.Settings.WCS.MaxCells > 0 {
		maxCells = ws.Settings.WCS.MaxCells
	}
	cellCount := int64(plan.output.Width) * int64(plan.output.Height) * int64(maxInt(1, len(plan.bands)))
	if cellCount <= 0 || cellCount > maxCells {
		writeException(w, &requestError{Code: "OperationProcessingFailed", Locator: "subset", Text: "requested coverage exceeds the cell limit"})
		return
	}
	queue := time.NewTimer(time.Duration(h.cfg.WCS.QueueTimeoutMS) * time.Millisecond)
	defer queue.Stop()
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-queue.C:
		writeException(w, &requestError{Code: "OperationProcessingFailed", Text: "coverage processing queue timed out", Status: http.StatusServiceUnavailable})
		return
	case <-r.Context().Done():
		return
	}
	timeout := h.cfg.WCS.ProcessingTimeoutMS
	if ws.Settings.WCS.ProcessingTimeoutMS > 0 {
		timeout = ws.Settings.WCS.ProcessingTimeoutMS
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeout)*time.Millisecond)
	defer cancel()
	maxBytes := h.cfg.WCS.MaxOutputBytes
	if ws.Settings.WCS.MaxOutputBytes > 0 {
		maxBytes = ws.Settings.WCS.MaxOutputBytes
	}
	executionBytes := maxBytes
	if plan.query.MaxTemporaryBytes > 0 && plan.query.MaxTemporaryBytes < executionBytes {
		executionBytes = plan.query.MaxTemporaryBytes
	}
	payload, executeErr := executeCoveragePlan(ctx, svc.CoverageSource, cov, plan, executionBytes, version)
	if executeErr != nil {
		if errors.Is(executeErr, rastergrid.ErrUnsupportedNumericEncoding) {
			writeException(w, &requestError{Code: "InvalidParameterValue", Locator: "format", Text: executeErr.Error(), Status: http.StatusBadRequest})
			return
		}
		if ctx.Err() != nil {
			writeException(w, &requestError{Code: "OperationProcessingFailed", Text: "coverage processing timed out", Status: http.StatusServiceUnavailable})
			return
		}
		h.internal(w, executeErr)
		return
	}
	if int64(len(payload.body)) > maxBytes {
		writeException(w, &requestError{Code: "OperationProcessingFailed", Text: "coverage output exceeds the byte limit"})
		return
	}
	w.Header().Set("Content-Type", payload.contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload.body)
}

func retainedAxisValues(descriptor *datasource.CoverageDescriptor, subsets []datasource.CoverageDomainSubset) int64 {
	if descriptor == nil {
		return 1
	}
	result := int64(1)
	for _, axis := range descriptor.Axes {
		if axis.Kind == datasource.CoverageAxisSpatialX || axis.Kind == datasource.CoverageAxisSpatialY {
			continue
		}
		count := axis.GridHigh - axis.GridLow + 1
		for _, subset := range subsets {
			if strings.EqualFold(axis.Label, subset.Axis) {
				count = retainedAxisSubsetCount(axis, subset)
				break
			}
		}
		if count < 1 {
			count = 1
		}
		if result > math.MaxInt64/count {
			return math.MaxInt64
		}
		result *= count
	}
	return result
}

func retainedAxisSubsetCount(axis datasource.CoverageAxis, subset datasource.CoverageDomainSubset) int64 {
	if subset.Slice {
		return 1
	}
	if len(axis.Coordinates) > 0 {
		var count int64
		for _, coordinate := range axis.Coordinates {
			if coverageValueBetween(coordinate, subset.Low, subset.High) {
				count++
			}
		}
		return count
	}
	if axis.Regular && axis.Resolution.Number != nil && subset.Low.Number != nil && subset.High.Number != nil {
		resolution := math.Abs(*axis.Resolution.Number)
		if resolution > 0 {
			return int64(math.Floor(math.Abs(*subset.High.Number-*subset.Low.Number)/resolution)) + 1
		}
	}
	return axis.GridHigh - axis.GridLow + 1
}

func coverageValueBetween(value, low, high datasource.CoverageAxisValue) bool {
	if value.Number != nil && low.Number != nil && high.Number != nil {
		return *value.Number >= *low.Number && *value.Number <= *high.Number
	}
	if value.Time != nil && low.Time != nil && high.Time != nil {
		return !value.Time.Before(*low.Time) && !value.Time.After(*high.Time)
	}
	return value.Text >= low.Text && value.Text <= high.Text
}

func multipartCoverage(version string, cov *workspace.Coverage, info *datasource.CoverageInfo, window datasource.CoverageWindow, tiff []byte) ([]byte, string) {
	const cid = "coverage.tif"
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	xmlHeader := textproto.MIMEHeader{}
	xmlHeader.Set("Content-Type", "application/gml+xml")
	xmlHeader.Set("Content-ID", "<coverage.xml>")
	xmlPart, _ := writer.CreatePart(xmlHeader)
	_, _ = xmlPart.Write(gmlCoverage(version, cov, info, window, nil, cid))
	tiffHeader := textproto.MIMEHeader{}
	tiffHeader.Set("Content-Type", "image/tiff")
	tiffHeader.Set("Content-ID", "<"+cid+">")
	tiffPart, _ := writer.CreatePart(tiffHeader)
	_, _ = tiffPart.Write(tiff)
	_ = writer.Close()
	return body.Bytes(), `multipart/related; type="application/gml+xml"; boundary=` + writer.Boundary()
}

func (h *handler) requireAuth(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace, public bool) bool {
	if public {
		return false
	}
	if h.cfg.Auth.RequireHTTPS && !identity.IsSecureTransport(r.Context()) {
		http.Error(w, "HTTPS required", http.StatusUpgradeRequired)
		return true
	}
	id, ok := identity.FromContext(r.Context())
	if !ok || id == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return true
	}
	if !id.HasWorkspaceAccess(ws.ID) {
		http.Error(w, "no access to this workspace", http.StatusForbidden)
		return true
	}
	return false
}
func role(r *http.Request, workspaceID string) string {
	if id, ok := identity.FromContext(r.Context()); ok && id != nil {
		return id.GetWorkspaceRole(workspaceID)
	}
	return ""
}
func visibleCoverage(ws *workspace.Workspace, id, role string) (*workspace.Service, *workspace.Coverage) {
	svc, cov := ws.GetCoverage(id)
	if cov == nil || !cov.VisibleToRole(role) {
		return nil, nil
	}
	return svc, cov
}
func writeXML(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
func (h *handler) internal(w http.ResponseWriter, err error) {
	h.logger.Error("wcs request failed", "error", err)
	writeException(w, &requestError{Code: "OperationProcessingFailed", Text: "coverage processing failed", Status: http.StatusInternalServerError})
}
