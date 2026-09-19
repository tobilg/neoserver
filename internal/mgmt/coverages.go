package mgmt

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/neoserver/internal/crs"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

type CreateCoverageRequest struct {
	SourceCoverage       string                     `json:"source_coverage"`
	PublicID             string                     `json:"public_id"`
	Title                string                     `json:"title,omitempty"`
	Description          string                     `json:"description,omitempty"`
	Enabled              *bool                      `json:"enabled,omitempty"`
	Public               bool                       `json:"public,omitempty"`
	AllowedRoles         []string                   `json:"allowed_roles,omitempty"`
	RangeFields          []store.CoverageRangeField `json:"range_fields,omitempty"`
	Dimensions           []*DimensionInput          `json:"dimensions,omitempty"`
	DefaultStyle         string                     `json:"default_style,omitempty"`
	Styles               []string                   `json:"styles,omitempty"`
	Resampling           string                     `json:"resampling,omitempty"`
	WCS20CoverageSubtype string                     `json:"wcs20_coverage_subtype,omitempty"`
	TileCacheQuotaBytes  int64                      `json:"tile_cache_quota_bytes,omitempty"`
}

type UpdateCoverageRequest struct {
	PublicID             *string                    `json:"public_id,omitempty"`
	Title                *string                    `json:"title,omitempty"`
	Description          *string                    `json:"description,omitempty"`
	Enabled              *bool                      `json:"enabled,omitempty"`
	Public               *bool                      `json:"public,omitempty"`
	AllowedRoles         []string                   `json:"allowed_roles,omitempty"`
	RangeFields          []store.CoverageRangeField `json:"range_fields,omitempty"`
	Dimensions           []*DimensionInput          `json:"dimensions,omitempty"`
	DefaultStyle         *string                    `json:"default_style,omitempty"`
	Styles               []string                   `json:"styles,omitempty"`
	Resampling           *string                    `json:"resampling,omitempty"`
	WCS20CoverageSubtype *string                    `json:"wcs20_coverage_subtype,omitempty"`
	TileCacheQuotaBytes  *int64                     `json:"tile_cache_quota_bytes,omitempty"`
}

func (h *handler) coverageContext(r *http.Request) (string, string, store.CoverageStore, error) {
	workspaceID, err := h.resolveWorkspaceID(r.Context(), chi.URLParam(r, "workspace"))
	if err != nil {
		return "", "", nil, err
	}
	serviceID, err := h.resolveServiceID(r.Context(), workspaceID, chi.URLParam(r, "service"))
	if err != nil {
		return "", "", nil, err
	}
	coverageStore, ok := h.store.(store.CoverageStore)
	if !ok {
		return "", "", nil, store.ErrNotInitialized
	}
	return workspaceID, serviceID, coverageStore, nil
}

func (h *handler) discoverCoverages(w http.ResponseWriter, r *http.Request) {
	workspaceID, serviceID, _, err := h.coverageContext(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace or service not found")
		return
	}
	items, err := h.registry.DiscoverCoverages(r.Context(), workspaceID, serviceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if items == nil {
		items = []*datasource.DiscoveredCoverage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"coverages": items})
}

func (h *handler) listCoverages(w http.ResponseWriter, r *http.Request) {
	_, serviceID, coverageStore, err := h.coverageContext(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace or service not found")
		return
	}
	items, err := coverageStore.ListCoverages(r.Context(), serviceID)
	if err != nil {
		h.logger.Error("list coverages failed", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to list coverages")
		return
	}
	if items == nil {
		items = []*store.Coverage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"coverages": items})
}

func (h *handler) createCoverage(w http.ResponseWriter, r *http.Request) {
	workspaceID, serviceID, coverageStore, err := h.coverageContext(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace or service not found")
		return
	}
	var req CreateCoverageRequest
	if readJSON(r, &req) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	if req.SourceCoverage == "" || req.PublicID == "" {
		writeError(w, http.StatusBadRequest, "Bad Request", "source_coverage and public_id are required")
		return
	}
	if !isXMLNCName(req.PublicID) {
		writeError(w, http.StatusBadRequest, "Bad Request", "public_id must be an XML NCName")
		return
	}
	if err := validateResampling(req.Resampling); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if err := validateWCS20CoverageSubtype(req.WCS20CoverageSubtype, true); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if err := h.validateStyleBindings(r.Context(), workspaceID, req.DefaultStyle, req.Styles, true); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if err := h.validateResourceCacheQuota(r.Context(), workspaceID, req.TileCacheQuotaBytes); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if _, err := coverageStore.GetCoverageByPublicID(r.Context(), workspaceID, req.PublicID); err == nil {
		writeError(w, http.StatusConflict, "Conflict", "public_id already exists in this workspace")
		return
	} else if err != store.ErrNotFound {
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to validate public_id")
		return
	}
	discovered, err := h.registry.DiscoverCoverages(r.Context(), workspaceID, serviceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "coverage source is unavailable")
		return
	}
	var source *datasource.DiscoveredCoverage
	for _, item := range discovered {
		if item.SourceCoverage == req.SourceCoverage {
			source = item
			break
		}
	}
	if source == nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "source_coverage was not discovered by this service")
		return
	}
	if err := validateRangeFields(req.RangeFields, source.Info.Bands); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if len(req.Dimensions) == 0 {
		req.Dimensions = discoveredCoverageDimensions(source.Descriptor)
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.Title == "" {
		req.Title = req.PublicID
	}
	extentSRID := source.Info.SRID
	if extentSRID == 0 {
		extentSRID, _ = crs.Parse(source.Info.CRS)
	}
	nativeExtent := &store.SpatialExtent{MinX: source.Info.Envelope[0], MinY: source.Info.Envelope[1], MaxX: source.Info.Envelope[2], MaxY: source.Info.Envelope[3], SRID: extentSRID}
	item, err := h.registry.CreateCoverage(r.Context(), workspaceID, store.CreateCoverageInput{ServiceID: serviceID, SourceCoverage: req.SourceCoverage, PublicID: req.PublicID, Title: req.Title, Description: req.Description, Enabled: enabled, Public: req.Public, AllowedRoles: req.AllowedRoles, RangeFields: req.RangeFields, Dimensions: coverageDimensions(req.Dimensions), DefaultStyle: req.DefaultStyle, Styles: req.Styles, Resampling: req.Resampling, WCS20CoverageSubtype: req.WCS20CoverageSubtype, NativeExtent: nativeExtent, TileCacheQuotaBytes: req.TileCacheQuotaBytes})
	if err != nil {
		status := http.StatusInternalServerError
		if err == store.ErrDuplicateKey {
			status = http.StatusConflict
		}
		writeError(w, status, "Error", "failed to create coverage")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *handler) findCoverage(r *http.Request, serviceID string, coverageStore store.CoverageStore) (*store.Coverage, error) {
	identifier := chi.URLParam(r, "coverage")
	if item, err := coverageStore.GetCoverage(r.Context(), identifier); err == nil {
		if item.ServiceID == serviceID {
			return item, nil
		}
		return nil, store.ErrNotFound
	}
	items, err := coverageStore.ListCoverages(r.Context(), serviceID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.PublicID == identifier {
			return item, nil
		}
	}
	return nil, store.ErrNotFound
}

func (h *handler) getCoverage(w http.ResponseWriter, r *http.Request) {
	_, serviceID, coverageStore, err := h.coverageContext(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace or service not found")
		return
	}
	item, err := h.findCoverage(r, serviceID, coverageStore)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "coverage not found")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *handler) updateCoverage(w http.ResponseWriter, r *http.Request) {
	workspaceID, serviceID, coverageStore, err := h.coverageContext(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace or service not found")
		return
	}
	item, err := h.findCoverage(r, serviceID, coverageStore)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "coverage not found")
		return
	}
	var req UpdateCoverageRequest
	if readJSON(r, &req) != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return
	}
	if req.PublicID != nil && !isXMLNCName(*req.PublicID) {
		writeError(w, http.StatusBadRequest, "Bad Request", "public_id must be an XML NCName")
		return
	}
	if req.Resampling != nil {
		if err := validateResampling(*req.Resampling); err != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
			return
		}
	}
	if req.WCS20CoverageSubtype != nil {
		if err := validateWCS20CoverageSubtype(*req.WCS20CoverageSubtype, false); err != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
			return
		}
	}
	defaultStyle := item.DefaultStyle
	if req.DefaultStyle != nil {
		defaultStyle = *req.DefaultStyle
	}
	styles := item.Styles
	if req.Styles != nil {
		styles = req.Styles
	}
	if err := h.validateStyleBindings(r.Context(), workspaceID, defaultStyle, styles, true); err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if req.TileCacheQuotaBytes != nil {
		if err := h.validateResourceCacheQuota(r.Context(), workspaceID, *req.TileCacheQuotaBytes); err != nil {
			writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
			return
		}
	}
	infoItems, err := h.registry.DiscoverCoverages(r.Context(), workspaceID, serviceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Bad Request", "coverage source is unavailable")
		return
	}
	foundSource := false
	for _, source := range infoItems {
		if source.SourceCoverage == item.SourceCoverage {
			foundSource = true
			if err := validateRangeFields(req.RangeFields, source.Info.Bands); err != nil {
				writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
				return
			}
		}
	}
	if !foundSource {
		writeError(w, http.StatusBadRequest, "Bad Request", "coverage source is unavailable")
		return
	}
	updated, err := h.registry.UpdateCoverage(r.Context(), workspaceID, serviceID, item.ID, store.UpdateCoverageInput{PublicID: req.PublicID, Title: req.Title, Description: req.Description, Enabled: req.Enabled, Public: req.Public, AllowedRoles: req.AllowedRoles, RangeFields: req.RangeFields, Dimensions: coverageDimensions(req.Dimensions), DefaultStyle: req.DefaultStyle, Styles: req.Styles, Resampling: req.Resampling, WCS20CoverageSubtype: req.WCS20CoverageSubtype, TileCacheQuotaBytes: req.TileCacheQuotaBytes})
	if err != nil {
		if errors.Is(err, workspace.ErrResourceReferenced) {
			writeError(w, http.StatusConflict, "Conflict", err.Error())
			return
		}
		if err == store.ErrDuplicateKey {
			writeError(w, http.StatusConflict, "Conflict", "public_id already exists in this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to update coverage")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func coverageDimensions(input []*DimensionInput) []*store.Dimension {
	if input == nil {
		return nil
	}
	result := make([]*store.Dimension, 0, len(input))
	for _, dim := range input {
		if dim == nil {
			continue
		}
		result = append(result, &store.Dimension{Name: dim.Name, Units: dim.Units, SourceAxis: dim.SourceAxis, SourceProperty: dim.SourceProperty, EndProperty: dim.EndProperty, Default: dim.Default, MultipleValues: dim.MultipleValues, NearestValue: dim.NearestValue, Current: dim.Current, Extent: dim.Extent})
	}
	return result
}

func discoveredCoverageDimensions(descriptor *datasource.CoverageDescriptor) []*DimensionInput {
	if descriptor == nil || len(descriptor.Axes) <= 2 {
		return nil
	}
	var result []*DimensionInput
	for _, axis := range descriptor.Axes {
		if axis.Kind == datasource.CoverageAxisSpatialX || axis.Kind == datasource.CoverageAxisSpatialY {
			continue
		}
		name := axis.Label
		units := axis.Unit
		switch axis.Kind {
		case datasource.CoverageAxisTime:
			name, units = "time", "ISO8601"
		case datasource.CoverageAxisElevation:
			name = "elevation"
		}
		low, high := coverageAxisEndpoints(axis)
		extent := low
		if high != "" && high != low {
			extent = low + "/" + high
			if axis.Regular {
				extent += "/" + coverageAxisResolution(axis)
			}
		}
		result = append(result, &DimensionInput{
			Name: name, Units: defaultDimensionUnits(units), SourceAxis: axis.Label,
			Default: low, NearestValue: true, Extent: extent,
		})
	}
	return result
}

func coverageAxisEndpoints(axis datasource.CoverageAxis) (string, string) {
	if len(axis.Coordinates) > 0 {
		return coverageAxisValueText(axis.Coordinates[0]), coverageAxisValueText(axis.Coordinates[len(axis.Coordinates)-1])
	}
	low := coverageAxisValueText(axis.Origin)
	if axis.GridHigh <= axis.GridLow || axis.Resolution.Number == nil {
		return low, low
	}
	steps := float64(axis.GridHigh - axis.GridLow)
	if axis.Origin.Number != nil {
		value := *axis.Origin.Number + steps**axis.Resolution.Number
		return low, strconv.FormatFloat(value, 'g', -1, 64)
	}
	if axis.Origin.Time != nil {
		value := axis.Origin.Time.Add(time.Duration(steps * *axis.Resolution.Number * coverageAxisUnitSeconds(axis.Unit) * float64(time.Second)))
		return low, value.UTC().Format(time.RFC3339Nano)
	}
	return low, low
}

func coverageAxisValueText(value datasource.CoverageAxisValue) string {
	if value.Number != nil {
		return strconv.FormatFloat(*value.Number, 'g', -1, 64)
	}
	if value.Time != nil {
		return value.Time.UTC().Format(time.RFC3339Nano)
	}
	return value.Text
}

func coverageAxisResolution(axis datasource.CoverageAxis) string {
	if axis.Resolution.Number == nil {
		return defaultDimensionUnits(axis.Resolution.Text)
	}
	value := strconv.FormatFloat(*axis.Resolution.Number, 'g', -1, 64)
	if axis.Kind != datasource.CoverageAxisTime {
		return value
	}
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(axis.Unit, " since ", 2)[0])) {
	case "day", "days", "d":
		return "P" + value + "D"
	case "hour", "hours", "h":
		return "PT" + value + "H"
	case "minute", "minutes", "min":
		return "PT" + value + "M"
	default:
		return "PT" + value + "S"
	}
}

func coverageAxisUnitSeconds(unit string) float64 {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(unit, " since ", 2)[0])) {
	case "day", "days", "d":
		return 86400
	case "hour", "hours", "h":
		return 3600
	case "minute", "minutes", "min":
		return 60
	case "second", "seconds", "sec", "s", "":
		return 1
	default:
		return 1
	}
}

func defaultDimensionUnits(value string) string {
	if strings.TrimSpace(value) == "" {
		return "1"
	}
	return value
}

func (h *handler) deleteCoverage(w http.ResponseWriter, r *http.Request) {
	workspaceID, serviceID, coverageStore, err := h.coverageContext(r)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "workspace or service not found")
		return
	}
	item, err := h.findCoverage(r, serviceID, coverageStore)
	if err != nil {
		writeError(w, http.StatusNotFound, "Not Found", "coverage not found")
		return
	}
	if err := h.registry.DeleteCoverage(r.Context(), workspaceID, serviceID, item.ID); err != nil {
		if errors.Is(err, workspace.ErrResourceReferenced) {
			writeError(w, http.StatusConflict, "Conflict", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Internal Error", "failed to delete coverage")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateRangeFields(fields []store.CoverageRangeField, bands []datasource.CoverageBand) error {
	if fields == nil {
		return nil
	}
	if len(fields) != len(bands) {
		return &validationError{"range_fields must contain exactly one entry per raster band"}
	}
	seen := map[int]bool{}
	for _, f := range fields {
		if f.Band < 1 || f.Band > len(bands) || seen[f.Band] || f.Name == "" {
			return &validationError{"range_fields must have unique valid band numbers and names"}
		}
		seen[f.Band] = true
	}
	return nil
}

type validationError struct{ message string }

func validateResampling(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "nearest", "bilinear", "cubic":
		return nil
	default:
		return &validationError{"resampling must be nearest, bilinear, or cubic"}
	}
}

func validateWCS20CoverageSubtype(value string, allowEmpty bool) error {
	if allowEmpty && value == "" {
		return nil
	}
	switch value {
	case store.WCS20CoverageSubtypeRectifiedGrid, store.WCS20CoverageSubtypeGrid:
		return nil
	default:
		return &validationError{"wcs20_coverage_subtype must be RectifiedGridCoverage or GridCoverage"}
	}
}

func (e *validationError) Error() string { return e.message }
func isXMLNCName(value string) bool {
	if value == "" || strings.Contains(value, ":") {
		return false
	}
	for i, rn := range value {
		if i == 0 {
			if !(rn == '_' || rn >= 'A' && rn <= 'Z' || rn >= 'a' && rn <= 'z') {
				return false
			}
		} else if !(rn == '_' || rn == '-' || rn == '.' || rn >= 'A' && rn <= 'Z' || rn >= 'a' && rn <= 'z' || rn >= '0' && rn <= '9') {
			return false
		}
	}
	return true
}
