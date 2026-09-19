package wfs

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/gdalcap"
)

type decodedExportFeature struct {
	ID         string
	Geometry   json.RawMessage
	Properties map[string]any
}

type exportField struct {
	Source string
	Output string
	Type   godal.FieldType
	ID     bool
}

type exportWorkspace struct {
	ctx       context.Context
	directory string
	release   func()
}

func (h *workspaceHandler) beginExport(ctx context.Context) (*exportWorkspace, error) {
	queueTimeout := time.Duration(h.cfg.WFS.ExportQueueTimeoutMS) * time.Millisecond
	if queueTimeout <= 0 {
		queueTimeout = 2 * time.Second
	}
	if h.exports == nil {
		h.exports = make(chan struct{}, 1)
	}
	timer := time.NewTimer(queueTimeout)
	defer timer.Stop()
	select {
	case h.exports <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, errors.New("WFS export queue is full")
	}
	releaseSlot := func() { <-h.exports }
	exportCtx := ctx
	cancel := func() {}
	if timeout := time.Duration(h.cfg.WFS.ExportTimeoutMS) * time.Millisecond; timeout > 0 {
		exportCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	if err := os.MkdirAll(h.cfg.WFS.TemporaryDirectory, 0o700); err != nil {
		cancel()
		releaseSlot()
		return nil, err
	}
	directory, err := os.MkdirTemp(h.cfg.WFS.TemporaryDirectory, "export-*")
	if err != nil {
		cancel()
		releaseSlot()
		return nil, err
	}
	return &exportWorkspace{
		ctx:       exportCtx,
		directory: directory,
		release: func() {
			cancel()
			_ = os.RemoveAll(directory)
			releaseSlot()
		},
	}, nil
}

func (h *workspaceHandler) enforceTemporaryLimit(directory string) error {
	maximum := h.cfg.WFS.MaxTemporaryBytes
	if maximum <= 0 {
		return nil
	}
	var total int64
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > maximum {
			return errors.New("WFS export exceeds the configured temporary byte limit")
		}
		return nil
	})
	return err
}

func decodeExportFeatures(features [][]byte) ([]decodedExportFeature, error) {
	decoded := make([]decodedExportFeature, 0, len(features))
	for index, raw := range features {
		var envelope struct {
			ID         any             `json:"id"`
			Geometry   json.RawMessage `json:"geometry"`
			Properties map[string]any  `json:"properties"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&envelope); err != nil {
			return nil, fmt.Errorf("decode feature %d: %w", index+1, err)
		}
		id := scalarExportValue(envelope.ID)
		if id == "" {
			sum := sha256.Sum256(raw)
			id = fmt.Sprintf("feature-%x", sum[:8])
		}
		if envelope.Properties == nil {
			envelope.Properties = make(map[string]any)
		}
		if string(envelope.Geometry) == "null" {
			envelope.Geometry = nil
		}
		decoded = append(decoded, decodedExportFeature{ID: id, Geometry: envelope.Geometry, Properties: envelope.Properties})
	}
	return decoded, nil
}

func scalarExportValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func buildExportFields(features []decodedExportFeature, info *datasource.LayerInfo, selected []string, shapefile bool) []exportField {
	names := make(map[string]bool)
	for _, feature := range features {
		for name := range feature.Properties {
			names[name] = true
		}
	}
	if len(features) == 0 && info != nil {
		selection := make(map[string]bool)
		for _, name := range selected {
			selection[strings.TrimSpace(strings.TrimPrefix(name, info.Name+"/"))] = true
		}
		for _, property := range info.Properties {
			if len(selection) == 0 || selection[property.Name] {
				names[property.Name] = true
			}
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	used := make(map[string]bool)
	idName := allocateExportName("feature_id", used, shapefile)
	fields := []exportField{{Output: idName, Type: godal.FTString, ID: true}}
	for _, name := range ordered {
		fields = append(fields, exportField{
			Source: name,
			Output: allocateExportName(name, used, shapefile),
			Type:   inferExportFieldType(name, features, info),
		})
	}
	return fields
}

func allocateExportName(value string, used map[string]bool, shapefile bool) string {
	base := strings.TrimSpace(value)
	if shapefile {
		base = strings.ToUpper(strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
				return r
			}
			return '_'
		}, base))
		if base == "" {
			base = "FIELD"
		}
		if base[0] >= '0' && base[0] <= '9' {
			base = "F_" + base
		}
		if len(base) > 10 {
			base = base[:10]
		}
	} else {
		if base == "" {
			base = "field"
		}
		if strings.EqualFold(base, "fid") || strings.EqualFold(base, "geom") {
			base = "property_" + base
		}
	}
	candidate := base
	for suffix := 2; used[strings.ToLower(candidate)]; suffix++ {
		ending := fmt.Sprintf("_%d", suffix)
		prefix := base
		if shapefile && len(prefix)+len(ending) > 10 {
			prefix = prefix[:10-len(ending)]
		}
		candidate = prefix + ending
	}
	used[strings.ToLower(candidate)] = true
	return candidate
}

type exportValueKind int

const (
	exportValueUnknown exportValueKind = iota
	exportValueBool
	exportValueInt
	exportValueReal
	exportValueString
)

func inferExportFieldType(name string, features []decodedExportFeature, info *datasource.LayerInfo) godal.FieldType {
	kind := exportValueUnknown
	for _, feature := range features {
		value, ok := feature.Properties[name]
		if !ok || value == nil {
			continue
		}
		kind = mergeExportValueKind(kind, classifyExportValue(value))
	}
	if kind == exportValueUnknown && info != nil {
		for _, property := range info.Properties {
			if property.Name != name {
				continue
			}
			typeName := strings.ToLower(property.Type + " " + string(property.JSONType))
			switch {
			case strings.Contains(typeName, "bool"):
				kind = exportValueBool
			case strings.Contains(typeName, "int"):
				kind = exportValueInt
			case strings.Contains(typeName, "number"), strings.Contains(typeName, "float"), strings.Contains(typeName, "double"), strings.Contains(typeName, "decimal"):
				kind = exportValueReal
			default:
				kind = exportValueString
			}
			break
		}
	}
	switch kind {
	case exportValueBool:
		return godal.FTInt
	case exportValueInt:
		return godal.FTInt64
	case exportValueReal:
		return godal.FTReal
	default:
		return godal.FTString
	}
}

func classifyExportValue(value any) exportValueKind {
	switch typed := value.(type) {
	case bool:
		return exportValueBool
	case json.Number:
		if _, err := typed.Int64(); err == nil {
			return exportValueInt
		}
		return exportValueReal
	case float32, float64:
		return exportValueReal
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return exportValueInt
	default:
		return exportValueString
	}
}

func mergeExportValueKind(left, right exportValueKind) exportValueKind {
	if left == exportValueUnknown {
		return right
	}
	if right == exportValueUnknown || left == right {
		return left
	}
	if left == exportValueString || right == exportValueString || left == exportValueBool || right == exportValueBool {
		return exportValueString
	}
	return exportValueReal
}

func exportFieldValue(value any, fieldType godal.FieldType) (any, bool) {
	if value == nil {
		return nil, false
	}
	switch fieldType {
	case godal.FTInt:
		if typed, ok := value.(bool); ok {
			if typed {
				return 1, true
			}
			return 0, true
		}
		number, err := strconv.Atoi(scalarExportValue(value))
		return number, err == nil
	case godal.FTInt64:
		number, err := strconv.ParseInt(scalarExportValue(value), 10, 64)
		return number, err == nil
	case godal.FTReal:
		number, err := strconv.ParseFloat(scalarExportValue(value), 64)
		return number, err == nil
	default:
		if typed, ok := value.(string); ok {
			return typed, true
		}
		if typed, ok := value.(json.Number); ok {
			return typed.String(), true
		}
		encoded, err := json.Marshal(value)
		return string(encoded), err == nil
	}
}

func createLayerOptions(fields []exportField) []godal.CreateLayerOption {
	options := make([]godal.CreateLayerOption, 0, len(fields))
	for _, field := range fields {
		options = append(options, godal.NewFieldDefinition(field.Output, field.Type))
	}
	return options
}

func findGDALField(fields map[string]godal.Field, name string) (godal.Field, bool) {
	if field, ok := fields[name]; ok {
		return field, true
	}
	for candidate, field := range fields {
		if strings.EqualFold(candidate, name) {
			return field, true
		}
	}
	return godal.Field{}, false
}

func writeFeatureToLayer(layer godal.Layer, feature decodedExportFeature, geometry json.RawMessage, fields []exportField) error {
	var outputGeometry *godal.Geometry
	var err error
	if len(geometry) > 0 && string(geometry) != "null" {
		outputGeometry, err = godal.NewGeometryFromGeoJSON(string(geometry))
		if err != nil {
			return fmt.Errorf("create output geometry: %w", err)
		}
		defer outputGeometry.Close()
	}
	outputFeature, err := layer.NewFeature(nil)
	if err != nil {
		return fmt.Errorf("create output feature: %w", err)
	}
	defer outputFeature.Close()
	if outputGeometry != nil {
		if err := outputFeature.SetGeometry(outputGeometry); err != nil {
			return fmt.Errorf("set output geometry: %w", err)
		}
	}
	available := outputFeature.Fields()
	for _, field := range fields {
		definition, ok := findGDALField(available, field.Output)
		if !ok {
			return fmt.Errorf("output field %q is unavailable", field.Output)
		}
		value := any(feature.ID)
		if !field.ID {
			value = feature.Properties[field.Source]
		}
		converted, present := exportFieldValue(value, field.Type)
		if !present {
			continue
		}
		if err := outputFeature.SetFieldValue(definition, converted); err != nil {
			return fmt.Errorf("set output field %q: %w", field.Output, err)
		}
	}
	if err := layer.CreateFeature(outputFeature); err != nil {
		return fmt.Errorf("finalize output feature: %w", err)
	}
	return nil
}

func (h *workspaceHandler) writeGeoPackage(ctx context.Context, w http.ResponseWriter, features [][]byte, layerName string, layerInfo *datasource.LayerInfo, outputSRID int, selected []string) error {
	if !gdalcap.Get().GeoPackage {
		return errors.New("GeoPackage output is unavailable because the GDAL GPKG driver is missing")
	}
	decoded, err := decodeExportFeatures(features)
	if err != nil {
		return err
	}
	workspace, err := h.beginExport(ctx)
	if err != nil {
		return err
	}
	defer workspace.release()
	baseName := safeOutputName(layerName)
	path := filepath.Join(workspace.directory, baseName+".gpkg")
	fields := buildExportFields(decoded, layerInfo, selected, false)
	dataset, err := godal.CreateVector(godal.GeoPackage, path)
	if err != nil {
		return fmt.Errorf("create GeoPackage: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = dataset.Close()
		}
	}()
	spatialRef, err := godal.NewSpatialRefFromEPSG(outputSRID)
	if err != nil {
		return fmt.Errorf("create GeoPackage CRS: %w", err)
	}
	layer, err := dataset.CreateLayer(baseName, spatialRef, commonGeometryType(decoded, layerInfo), createLayerOptions(fields)...)
	spatialRef.Close()
	if err != nil {
		return fmt.Errorf("create GeoPackage layer: %w", err)
	}
	_ = layer.SetGeometryColumnName("geom")
	if err := dataset.StartTransaction(); err != nil {
		return fmt.Errorf("start GeoPackage transaction: %w", err)
	}
	for index, feature := range decoded {
		if err := workspace.ctx.Err(); err != nil {
			_ = dataset.RollbackTransaction()
			return err
		}
		if err := writeFeatureToLayer(layer, feature, feature.Geometry, fields); err != nil {
			_ = dataset.RollbackTransaction()
			return err
		}
		if index%128 == 127 {
			if err := h.enforceTemporaryLimit(workspace.directory); err != nil {
				_ = dataset.RollbackTransaction()
				return err
			}
		}
	}
	if err := dataset.CommitTransaction(); err != nil {
		return fmt.Errorf("commit GeoPackage: %w", err)
	}
	if err := dataset.Close(); err != nil {
		return fmt.Errorf("finalize GeoPackage: %w", err)
	}
	closed = true
	if err := h.enforceTemporaryLimit(workspace.directory); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if h.cfg.WFS.MaxOutputBytes > 0 && info.Size() > h.cfg.WFS.MaxOutputBytes {
		return errors.New("GeoPackage exceeds the configured output byte limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	w.Header().Set("Content-Type", geoPackageMediaType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.gpkg"`, baseName))
	w.Header().Set("Content-Length", fmt.Sprint(info.Size()))
	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, file)
	return err
}

func commonGeometryType(features []decodedExportFeature, info *datasource.LayerInfo) godal.GeometryType {
	selected := godal.GTUnknown
	for _, feature := range features {
		geometryType := geometryTypeFromJSON(feature.Geometry)
		if geometryType == godal.GTNone {
			continue
		}
		if selected == godal.GTUnknown {
			selected = geometryType
		} else if selected != geometryType {
			return godal.GTUnknown
		}
	}
	if selected != godal.GTUnknown {
		return selected
	}
	if info != nil {
		return geometryTypeFromName(info.GeometryType)
	}
	return godal.GTUnknown
}

func geometryTypeFromJSON(raw json.RawMessage) godal.GeometryType {
	if len(raw) == 0 || string(raw) == "null" {
		return godal.GTNone
	}
	var envelope struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return godal.GTUnknown
	}
	return geometryTypeFromName(envelope.Type)
}

func geometryTypeFromName(value string) godal.GeometryType {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
	switch value {
	case "point":
		return godal.GTPoint
	case "multipoint":
		return godal.GTMultiPoint
	case "linestring", "line":
		return godal.GTLineString
	case "multilinestring", "multiline":
		return godal.GTMultiLineString
	case "polygon":
		return godal.GTPolygon
	case "multipolygon":
		return godal.GTMultiPolygon
	case "geometrycollection":
		return godal.GTGeometryCollection
	default:
		return godal.GTUnknown
	}
}

type shapePart struct {
	feature  decodedExportFeature
	geometry json.RawMessage
	multi    bool
}

type shapeFamily struct {
	name  string
	parts []shapePart
	multi bool
}

func shapeFamilyName(geometryType string) string {
	switch strings.ToLower(geometryType) {
	case "point", "multipoint":
		return "point"
	case "linestring", "multilinestring":
		return "line"
	case "polygon", "multipolygon":
		return "polygon"
	default:
		return ""
	}
}

func splitShapeGeometry(raw json.RawMessage) ([]struct {
	family   string
	geometry json.RawMessage
	multi    bool
}, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var envelope struct {
		Type       string            `json:"type"`
		Geometries []json.RawMessage `json:"geometries"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if strings.EqualFold(envelope.Type, "GeometryCollection") {
		var result []struct {
			family   string
			geometry json.RawMessage
			multi    bool
		}
		for _, member := range envelope.Geometries {
			parts, err := splitShapeGeometry(member)
			if err != nil {
				return nil, err
			}
			result = append(result, parts...)
		}
		return result, nil
	}
	family := shapeFamilyName(envelope.Type)
	if family == "" {
		return nil, fmt.Errorf("geometry type %q cannot be represented in SHAPE-ZIP", envelope.Type)
	}
	return []struct {
		family   string
		geometry json.RawMessage
		multi    bool
	}{{family: family, geometry: raw, multi: strings.HasPrefix(strings.ToLower(envelope.Type), "multi")}}, nil
}

func groupShapeFeatures(features []decodedExportFeature, info *datasource.LayerInfo) (map[string]*shapeFamily, error) {
	groups := make(map[string]*shapeFamily)
	expected := ""
	if info != nil {
		expected = shapeFamilyName(info.GeometryType)
	}
	for _, feature := range features {
		parts, err := splitShapeGeometry(feature.Geometry)
		if err != nil {
			return nil, err
		}
		if len(parts) == 0 {
			if expected == "" {
				return nil, errors.New("null geometry has no published Shapefile geometry family")
			}
			parts = append(parts, struct {
				family   string
				geometry json.RawMessage
				multi    bool
			}{family: expected})
		}
		for _, part := range parts {
			group := groups[part.family]
			if group == nil {
				group = &shapeFamily{name: part.family}
				groups[part.family] = group
			}
			group.multi = group.multi || part.multi
			group.parts = append(group.parts, shapePart{feature: feature, geometry: part.geometry, multi: part.multi})
		}
	}
	if len(groups) == 0 {
		if expected == "" {
			return nil, errors.New("published geometry type cannot be represented in SHAPE-ZIP")
		}
		groups[expected] = &shapeFamily{name: expected, multi: strings.HasPrefix(strings.ToLower(info.GeometryType), "multi")}
	}
	return groups, nil
}

func promoteShapeGeometry(raw json.RawMessage, family string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var envelope struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.ToLower(envelope.Type), "multi") {
		return raw, nil
	}
	typeName := map[string]string{"point": "MultiPoint", "line": "MultiLineString", "polygon": "MultiPolygon"}[family]
	return json.Marshal(map[string]any{"type": typeName, "coordinates": []json.RawMessage{envelope.Coordinates}})
}

func shapeGeometryType(family string, multi bool) godal.GeometryType {
	switch family {
	case "point":
		if multi {
			return godal.GTMultiPoint
		}
		return godal.GTPoint
	case "line":
		if multi {
			return godal.GTMultiLineString
		}
		return godal.GTLineString
	default:
		if multi {
			return godal.GTMultiPolygon
		}
		return godal.GTPolygon
	}
}

func (h *workspaceHandler) writeShapeDataset(workspace *exportWorkspace, basePath string, family *shapeFamily, fields []exportField, outputSRID int) error {
	path := basePath + ".shp"
	dataset, err := godal.CreateVector(godal.Shapefile, path)
	if err != nil {
		return fmt.Errorf("create Shapefile: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = dataset.Close()
		}
	}()
	spatialRef, err := godal.NewSpatialRefFromEPSG(outputSRID)
	if err != nil {
		return fmt.Errorf("create Shapefile CRS: %w", err)
	}
	layer, err := dataset.CreateLayer(filepath.Base(basePath), spatialRef, shapeGeometryType(family.name, family.multi), createLayerOptions(fields)...)
	spatialRef.Close()
	if err != nil {
		return fmt.Errorf("create Shapefile layer: %w", err)
	}
	for index, part := range family.parts {
		if err := workspace.ctx.Err(); err != nil {
			return err
		}
		geometry := part.geometry
		if family.multi && !part.multi {
			geometry, err = promoteShapeGeometry(geometry, family.name)
			if err != nil {
				return err
			}
		}
		if err := writeFeatureToLayer(layer, part.feature, geometry, fields); err != nil {
			return err
		}
		if index%128 == 127 {
			if err := h.enforceTemporaryLimit(workspace.directory); err != nil {
				return err
			}
		}
	}
	if err := dataset.Close(); err != nil {
		return fmt.Errorf("finalize Shapefile: %w", err)
	}
	closed = true
	return os.WriteFile(basePath+".cpg", []byte("UTF-8\n"), 0o600)
}

var safeArchiveName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func shapeZipFilename(r *http.Request, layerName string) (string, error) {
	name := safeOutputName(layerName) + ".zip"
	value := NormalizeQuery(r).Get("FORMAT_OPTIONS")
	if strings.TrimSpace(value) == "" {
		return name, nil
	}
	for _, option := range strings.Split(value, ";") {
		parts := strings.SplitN(strings.TrimSpace(option), ":", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "filename") {
			return "", errors.New("SHAPE-ZIP supports only FORMAT_OPTIONS=filename:<basename>.zip")
		}
		name = strings.TrimSpace(parts[1])
	}
	if !strings.HasSuffix(strings.ToLower(name), ".zip") {
		name += ".zip"
	}
	if !safeArchiveName.MatchString(name) || name == ".zip" || name == "..zip" || filepath.Base(name) != name {
		return "", errors.New("SHAPE-ZIP filename must be a safe basename of at most 128 characters")
	}
	return name, nil
}

func (h *workspaceHandler) writeShapeZip(ctx context.Context, w http.ResponseWriter, r *http.Request, features [][]byte, layerName string, layerInfo *datasource.LayerInfo, outputSRID int, selected []string) error {
	if !gdalcap.Get().Shapefile {
		return errors.New("SHAPE-ZIP output is unavailable because the GDAL ESRI Shapefile driver is missing")
	}
	archiveName, err := shapeZipFilename(r, layerName)
	if err != nil {
		return err
	}
	decoded, err := decodeExportFeatures(features)
	if err != nil {
		return err
	}
	groups, err := groupShapeFeatures(decoded, layerInfo)
	if err != nil {
		return err
	}
	workspace, err := h.beginExport(ctx)
	if err != nil {
		return err
	}
	defer workspace.release()
	fields := buildExportFields(decoded, layerInfo, selected, true)
	baseName := safeOutputName(layerName)
	families := make([]string, 0, len(groups))
	for family := range groups {
		families = append(families, family)
	}
	sort.Strings(families)
	for _, familyName := range families {
		outputName := baseName
		if len(families) > 1 {
			outputName += "_" + familyName
		}
		if err := h.writeShapeDataset(workspace, filepath.Join(workspace.directory, outputName), groups[familyName], fields, outputSRID); err != nil {
			return err
		}
	}
	if err := h.enforceTemporaryLimit(workspace.directory); err != nil {
		return err
	}
	entries, err := os.ReadDir(workspace.directory)
	if err != nil {
		return err
	}
	buffer := &limitedBuffer{maximum: h.cfg.WFS.MaxOutputBytes}
	archive := zip.NewWriter(buffer)
	fixedTime := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		header := &zip.FileHeader{Name: entry.Name(), Method: zip.Deflate, Modified: fixedTime}
		header.SetMode(0o600)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			_ = archive.Close()
			return err
		}
		file, err := os.Open(filepath.Join(workspace.directory, entry.Name()))
		if err != nil {
			_ = archive.Close()
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			_ = archive.Close()
			return copyErr
		}
		if closeErr != nil {
			_ = archive.Close()
			return closeErr
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	w.Header().Set("Content-Type", shapeZipMediaType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, archiveName))
	w.Header().Set("Content-Length", strconv.Itoa(buffer.Len()))
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(buffer.Bytes())
	return err
}
