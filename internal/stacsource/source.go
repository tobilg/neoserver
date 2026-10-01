// Package stacsource maps existing neoserver publications to STAC metadata.
package stacsource

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/pathpolicy"
	"github.com/tobilg/neoserver/internal/mosaiccatalog"
	"github.com/tobilg/neoserver/internal/staccatalog"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

type Adapter struct {
	Config conf.Config
	Mosaic *mosaiccatalog.Manager
}
type Resource struct {
	ServiceID  string                    `json:"service_id"`
	ID         string                    `json:"id"`
	PublicID   string                    `json:"public_id"`
	Kind       string                    `json:"kind"`
	Provider   string                    `json:"provider"`
	Title      string                    `json:"title"`
	Modes      []string                  `json:"modes"`
	Properties []datasource.PropertyInfo `json:"properties,omitempty"`
	IDProperty string                    `json:"id_property,omitempty"`
}

func Resolve(ws *workspace.Workspace, b *staccatalog.Binding) (*workspace.Service, *workspace.Layer, *workspace.Coverage) {
	if b == nil {
		return nil, nil, nil
	}
	s := ws.Services[b.ServiceID]
	if s == nil || !s.Enabled {
		return nil, nil, nil
	}
	if b.ResourceKind == "layer" {
		for _, l := range s.Layers {
			if l.ID == b.ResourceID && l.Enabled && s.DataSource != nil {
				return s, l, nil
			}
		}
	}
	if b.ResourceKind == "coverage" {
		for _, c := range s.Coverages {
			if c.ID == b.ResourceID && c.Enabled && s.CoverageSource != nil {
				return s, nil, c
			}
		}
	}
	return nil, nil, nil
}
func Visible(ws *workspace.Workspace, c *staccatalog.Collection, role string) bool {
	allowed := c.Public || role == "super_admin" || (role != "" && len(c.AllowedRoles) == 0)
	if role != "" {
		for _, r := range c.AllowedRoles {
			if r == role {
				allowed = true
			}
		}
	}
	if !allowed {
		return false
	}
	if c.Binding == nil {
		return true
	}
	s, l, r := Resolve(ws, c.Binding)
	if s == nil {
		return false
	}
	if l != nil {
		return l.VisibleToRole(role)
	}
	return r.VisibleToRole(role)
}
func (a *Adapter) Resources(ctx context.Context, ws *workspace.Workspace) ([]Resource, error) {
	out := []Resource{}
	for _, s := range ws.Services {
		if !s.Enabled {
			continue
		}
		for _, l := range s.Layers {
			if !l.Enabled || s.DataSource == nil {
				continue
			}
			info, err := l.FeatureInfo(ctx, s.DataSource)
			if err != nil {
				return nil, err
			}
			out = append(out, Resource{s.ID, l.ID, l.PublicID, "layer", string(s.Type), l.Title, []string{"dataset", "mapped"}, info.Properties, info.IDColumn})
		}
		for _, c := range s.Coverages {
			if !c.Enabled || s.CoverageSource == nil {
				continue
			}
			modes := []string{"dataset"}
			if s.Type == store.ServiceTypeRasterFile || s.Type == store.ServiceTypeRasterMosaic {
				modes = append(modes, "raster")
			}
			out = append(out, Resource{ServiceID: s.ID, ID: c.ID, PublicID: c.PublicID, Kind: "coverage", Provider: string(s.Type), Title: c.Title, Modes: modes})
		}
	}
	return out, nil
}
func (a *Adapter) Validate(ctx context.Context, ws *workspace.Workspace, b *staccatalog.Binding) error {
	s, l, c := Resolve(ws, b)
	if s == nil {
		return fmt.Errorf("source publication is missing or disabled")
	}
	if b.RefreshIntervalSec != 0 && b.RefreshIntervalSec < 60 {
		return fmt.Errorf("refresh interval must be at least 60 seconds")
	}
	switch b.Mode {
	case "dataset":
		return nil
	case "mapped":
		if l == nil {
			return fmt.Errorf("mapped asset records require a feature layer")
		}
		info, err := l.FeatureInfo(ctx, s.DataSource)
		if err != nil {
			return err
		}
		if b.Mapping.ID == "" {
			return fmt.Errorf("select an explicit stable unique ID property")
		}
		available := map[string]bool{}
		if info.IDColumn != "" {
			available[info.IDColumn] = true
		}
		for _, p := range info.Properties {
			available[p.Name] = true
		}
		if !available[b.Mapping.ID] {
			return fmt.Errorf("ID property is not exposed by the publication")
		}
		check := func(v staccatalog.Value) error {
			if v.Property != "" && v.Constant != nil {
				return fmt.Errorf("mapping must use either a property or a constant")
			}
			if v.Property != "" && !available[v.Property] {
				return fmt.Errorf("property %q is not exposed by the publication", v.Property)
			}
			return nil
		}
		for _, v := range []staccatalog.Value{b.Mapping.Datetime, b.Mapping.Start, b.Mapping.End} {
			if err := check(v); err != nil {
				return err
			}
		}
		if len(b.Mapping.Assets) == 0 {
			return fmt.Errorf("map at least one asset URL")
		}
		for _, v := range b.Mapping.Assets {
			if err := check(v.Href); err != nil {
				return err
			}
		}
		for _, v := range b.Mapping.Properties {
			if err := check(v); err != nil {
				return err
			}
		}
	case "raster":
		if c == nil || (s.Type != store.ServiceTypeRasterFile && s.Type != store.ServiceTypeRasterMosaic) {
			return fmt.Errorf("raster assets require a file-backed coverage or raster mosaic")
		}
		if s.Type == store.ServiceTypeRasterMosaic && a.Mosaic == nil {
			return fmt.Errorf("raster mosaic publication requires MosaicCatalog")
		}
	default:
		return fmt.Errorf("mode must be dataset, mapped, or raster")
	}
	return nil
}
func (a *Adapter) Base(ws *workspace.Workspace) string {
	return strings.TrimRight(a.Config.Server.UrlBase, "/") + strings.TrimRight(a.Config.Server.BasePath, "/") + "/workspaces/" + url.PathEscape(ws.Name)
}
func (a *Adapter) Scan(ctx context.Context, ws *workspace.Workspace, c *staccatalog.Collection, limit int64, emit func(stacmodel.Document, []staccatalog.LocalAsset) error) (stacmodel.Document, error) {
	b := c.Binding
	if err := a.Validate(ctx, ws, b); err != nil {
		return nil, err
	}
	s, l, r := Resolve(ws, b)
	doc := c.Document.Clone()
	base := a.Base(ws)
	// Replace only generated service links, retaining publisher-supplied metadata.
	links := []any{}
	if old, ok := doc["links"].([]any); ok {
		for _, v := range old {
			x, ok := v.(map[string]any)
			if ok && x["neoserver:generated"] != true {
				links = append(links, v)
			}
		}
	}
	link := func(href, rel, typ string) {
		links = append(links, stacmodel.Document{"href": href, "rel": rel, "type": typ, "neoserver:generated": true})
	}
	if l != nil {
		if ws.Settings != nil && ws.Settings.OGCAPI.Enabled {
			link(base+"/ogc/collections/"+url.PathEscape(l.PublicID), "data", "application/json")
		}
		if a.Config.WFS.Enabled && ws.Settings != nil && ws.Settings.WFS.Enabled {
			link(base+"/wfs?service=WFS&version=2.0.0&request=GetCapabilities", "service", "application/xml")
		}
	}
	if r != nil && a.Config.WCS.Enabled && ws.Settings != nil && ws.Settings.WCS.Enabled {
		link(base+"/wcs?service=WCS&version=2.0.1&request=DescribeCoverage&coverageId="+url.QueryEscape(r.PublicID), "describedby", "application/xml")
	}
	if a.Config.WMS.Enabled && ws.Settings != nil && ws.Settings.WMS.Enabled {
		link(base+"/wms?service=WMS&version=1.3.0&request=GetCapabilities", "service", "application/xml")
	}
	doc["links"] = links
	if b.Mode == "dataset" {
		if r != nil {
			info, err := s.CoverageSource.GetCoverageInfo(ctx, r.SourceCoverage)
			if err != nil {
				return nil, err
			}
			g, err := footprint(info.Envelope, info.SRID)
			if err != nil {
				return nil, err
			}
			bounds, err := stacmodel.GeometryBounds(g)
			if err != nil {
				return nil, err
			}
			setExtent(doc, bounds)
		} else {
			info, err := l.FeatureInfo(ctx, s.DataSource)
			if err != nil {
				return nil, err
			}
			if info.Extent == nil && !l.IsSQLView {
				if extents, ok := s.DataSource.(datasource.LayerExtentDataSource); ok {
					info.Extent, err = extents.GetLayerExtent(ctx, l.SourceLayer)
					if err != nil {
						return nil, err
					}
				}
			}
			if info.Extent != nil && b.Filter == "" {
				e := info.Extent
				g, err := footprint([4]float64{e.MinX, e.MinY, e.MaxX, e.MaxY}, e.SRID)
				if err != nil {
					return nil, err
				}
				bounds, err := stacmodel.GeometryBounds(g)
				if err != nil {
					return nil, err
				}
				setExtent(doc, bounds)
			} else {
				stream, err := l.StreamFeatures(ctx, s.DataSource, datasource.QueryParams{OutputSRID: 4326, Filter: b.Filter})
				if err != nil {
					return nil, err
				}
				defer stream.Close()
				bounds := [6]float64{180, 90, 0, -180, -90, 0}
				found := false
				for stream.Next() {
					f, err := stacmodel.Decode(stream.Feature())
					if err != nil {
						return nil, err
					}
					g := f.Object("geometry")
					if g == nil {
						continue
					}
					extent, err := stacmodel.GeometryBounds(g)
					if err != nil {
						return nil, err
					}
					for i := 0; i < 3; i++ {
						bounds[i] = math.Min(bounds[i], extent[i])
						bounds[i+3] = math.Max(bounds[i+3], extent[i+3])
					}
					found = true
				}
				if err = stream.Err(); err != nil {
					return nil, err
				}
				if found {
					setExtent(doc, bounds)
				} else {
					setExtent(doc, [6]float64{-180, -90, 0, 180, 90, 0})
				}
			}
		}
		return doc, nil
	}
	if b.Mode == "raster" {
		return doc, a.scanRaster(ctx, ws, c, s, r, limit, emit)
	}
	info, err := l.FeatureInfo(ctx, s.DataSource)
	if err != nil {
		return nil, err
	}
	check, release, err := fileGuard(ctx, s)
	if err != nil {
		return nil, err
	}
	defer release()
	stream, err := l.StreamFeatures(ctx, s.DataSource, datasource.QueryParams{Limit: int(limit + 1), OutputSRID: 4326, Filter: b.Filter})
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	var count int64
	for stream.Next() {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		count++
		if count > limit {
			return nil, fmt.Errorf("source exceeds the configured Item limit")
		}
		feature, err := stacmodel.Decode(stream.Feature())
		if err != nil {
			return nil, err
		}
		props := feature.Object("properties")
		if props == nil {
			props = stacmodel.Document{}
		}
		if info.IDColumn != "" {
			props[info.IDColumn] = feature["id"]
		}
		d, err := mappedItem(c.Document.String("id"), b, feature, props)
		if err != nil {
			return nil, fmt.Errorf("source record %d: %w", count, err)
		}
		if err = emit(d, nil); err != nil {
			return nil, err
		}
	}
	if err = stream.Err(); err != nil {
		return nil, err
	}
	if err = check(); err != nil {
		return nil, err
	}
	return doc, nil
}
func value(v staccatalog.Value, p stacmodel.Document) any {
	if v.Property != "" {
		return p[v.Property]
	}
	return v.Constant
}

// GDAL and SQL engines may serialize typed timestamps using SQL's space
// separator and hour-only offsets. Preserve their offset while converting to
// the RFC 3339 representation required by STAC. Unzoned SQL timestamps use UTC,
// matching the catalog's timestamp policy; constants must already be RFC 3339.
func temporalValue(v staccatalog.Value, p stacmodel.Document) any {
	raw := value(v, p)
	s, ok := raw.(string)
	if !ok || v.Property == "" {
		return raw
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999-07", "2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339Nano)
		}
	}
	return raw
}
func scalarID(v any) (string, error) {
	switch x := v.(type) {
	case string:
		if x != "" {
			return x, nil
		}
	case json.Number:
		return string(x), nil
	case float64:
		if !math.IsNaN(x) && !math.IsInf(x, 0) {
			return fmt.Sprint(x), nil
		}
	}
	return "", fmt.Errorf("Item ID must be a nonempty scalar string or number")
}
func mappedItem(collection string, b *staccatalog.Binding, f, p stacmodel.Document) (stacmodel.Document, error) {
	id, err := scalarID(p[b.Mapping.ID])
	if err != nil {
		return nil, err
	}
	props := stacmodel.Document{}
	for k, v := range b.Mapping.Properties {
		props[k] = value(v, p)
	}
	props["datetime"] = temporalValue(b.Mapping.Datetime, p)
	if props["datetime"] == nil {
		props["start_datetime"] = temporalValue(b.Mapping.Start, p)
		props["end_datetime"] = temporalValue(b.Mapping.End, p)
	}
	assets := stacmodel.Document{}
	for k, v := range b.Mapping.Assets {
		asset := stacmodel.Document{"href": value(v.Href, p)}
		if v.Type != "" {
			asset["type"] = v.Type
		}
		if v.Title != "" {
			asset["title"] = v.Title
		}
		if len(v.Roles) > 0 {
			asset["roles"] = v.Roles
		}
		assets[k] = asset
	}
	d := stacmodel.Document{"type": "Feature", "stac_version": "1.1.0", "id": id, "collection": collection, "geometry": f["geometry"], "properties": props, "assets": assets, "links": []any{stacmodel.Document{"rel": "collection", "href": "https://neoserver.invalid/collections/" + url.PathEscape(collection)}}}
	if len(b.Mapping.Extensions) > 0 {
		d["stac_extensions"] = b.Mapping.Extensions
	}
	if g := f.Object("geometry"); g != nil {
		bounds, err := stacmodel.GeometryBBox(g)
		if err != nil {
			return nil, err
		}
		d["bbox"] = bounds
	}
	if err = stacmodel.Validate(d, "item"); err != nil {
		return nil, err
	}
	return d, nil
}
func setExtent(d stacmodel.Document, b [6]float64) {
	d["extent"] = stacmodel.Document{"spatial": stacmodel.Document{"bbox": [][]float64{{b[0], b[1], b[3], b[4]}}}, "temporal": stacmodel.Document{"interval": [][]any{{nil, nil}}}}
}
func footprint(b [4]float64, srid int) (stacmodel.Document, error) {
	if srid <= 0 {
		return nil, fmt.Errorf("source CRS is unknown")
	}
	xs, ys := []float64{}, []float64{}
	for edge := 0; edge < 4; edge++ {
		for i := 0; i < 16; i++ {
			t := float64(i) / 16
			switch edge {
			case 0:
				xs = append(xs, b[0]+t*(b[2]-b[0]))
				ys = append(ys, b[1])
			case 1:
				xs = append(xs, b[2])
				ys = append(ys, b[1]+t*(b[3]-b[1]))
			case 2:
				xs = append(xs, b[2]-t*(b[2]-b[0]))
				ys = append(ys, b[3])
			case 3:
				xs = append(xs, b[0])
				ys = append(ys, b[3]-t*(b[3]-b[1]))
			}
		}
	}
	if srid != 4326 {
		src, err := godal.NewSpatialRefFromEPSG(srid)
		if err != nil {
			return nil, err
		}
		defer src.Close()
		dst, err := godal.NewSpatialRefFromEPSG(4326)
		if err != nil {
			return nil, err
		}
		defer dst.Close()
		tr, err := godal.NewTransform(src, dst)
		if err != nil {
			return nil, err
		}
		defer tr.Close()
		ok := make([]bool, len(xs))
		if err = tr.TransformEx(xs, ys, nil, ok); err != nil {
			return nil, err
		}
		for _, v := range ok {
			if !v {
				return nil, fmt.Errorf("cannot transform source footprint")
			}
		}
	}
	ring := [][]float64{}
	for i := range xs {
		ring = append(ring, []float64{xs[i], ys[i]})
	}
	ring = append(ring, ring[0])
	return stacmodel.Document{"type": "Polygon", "coordinates": [][][]float64{ring}}, nil
}
func fileGuard(ctx context.Context, s *workspace.Service) (func() error, func(), error) {
	noop := func() error { return nil }
	release := func() {}
	if s.Type != store.ServiceTypeGeoParquet && s.Type != store.ServiceTypeVectorFile {
		return noop, release, nil
	}
	var cfg struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(s.ConnectionInfo, &cfg); err != nil {
		return nil, release, err
	}
	lease, err := pathpolicy.Acquire(ctx, cfg.Path)
	if err != nil {
		return nil, release, err
	}
	release = lease.Release
	if pathpolicy.IsRemote(lease.Path) {
		return noop, release, nil
	}
	before, err := os.Stat(lease.Path)
	if err != nil {
		release()
		return nil, func() {}, err
	}
	return func() error {
		after, err := os.Stat(lease.Path)
		if err != nil {
			return err
		}
		if !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
			return fmt.Errorf("source file changed during publication; retry refresh")
		}
		return nil
	}, release, nil
}
func (a *Adapter) scanRaster(ctx context.Context, ws *workspace.Workspace, c *staccatalog.Collection, s *workspace.Service, r *workspace.Coverage, limit int64, emit func(stacmodel.Document, []staccatalog.LocalAsset) error) error {
	count := int64(0)
	add := func(path, acquired string, bbox [4]float64, srid int) error {
		count++
		if count > limit {
			return fmt.Errorf("raster source exceeds Item limit")
		}
		canonical := path
		if !pathpolicy.IsRemote(path) {
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			canonical = abs
		}
		hash := sha256.Sum256([]byte(c.Binding.ID + "\x00" + canonical))
		id := fmt.Sprintf("%x", hash[:16])
		geometry, err := footprint(bbox, srid)
		if err != nil {
			return err
		}
		mapping := *c.Binding
		mapping.Mapping.ID = "id"
		mapping.Mapping.Assets = map[string]staccatalog.AssetMapping{"data": {Href: staccatalog.Value{Constant: path}, Type: "image/tiff; application=geotiff", Roles: []string{"data"}}}
		local := []staccatalog.LocalAsset{}
		if !pathpolicy.IsRemote(path) {
			mapping.Mapping.Assets["data"] = staccatalog.AssetMapping{Href: staccatalog.Value{Constant: "https://neoserver.invalid/assets/" + id}, Type: "image/tiff; application=geotiff", Roles: []string{"data"}}
			local = append(local, staccatalog.LocalAsset{CollectionID: c.Document.String("id"), ItemID: id, Key: "data", Path: path, MediaType: "image/tiff"})
		}
		if mapping.Mapping.Datetime.Property == "" && mapping.Mapping.Datetime.Constant == nil && acquired != "" {
			mapping.Mapping.Datetime = staccatalog.Value{Constant: acquired}
		}
		d, err := mappedItem(c.Document.String("id"), &mapping, stacmodel.Document{"geometry": geometry}, stacmodel.Document{"id": id})
		if err != nil {
			return err
		}
		return emit(d, local)
	}
	if s.Type == store.ServiceTypeRasterMosaic {
		return a.Mosaic.ScanGranules(ctx, ws.ID, s.ID, func(g *mosaiccatalog.Granule) error { return add(g.SourceURI, g.Time, g.BBox, g.SRID) })
	}
	var cfg store.RasterFileConnectionInfo
	if err := json.Unmarshal(s.ConnectionInfo, &cfg); err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(strings.Split(cfg.Path, "?")[0]))
	if ext != ".tif" && ext != ".tiff" {
		return fmt.Errorf("direct raster assets require a single GeoTIFF/COG; publish this container in dataset mode")
	}
	if len(s.Coverages) != 1 {
		return fmt.Errorf("direct raster assets require whole-file publication through one coverage")
	}
	info, err := s.CoverageSource.GetCoverageInfo(ctx, r.SourceCoverage)
	if err != nil {
		return err
	}
	return add(cfg.Path, "", info.Envelope, info.SRID)
}
