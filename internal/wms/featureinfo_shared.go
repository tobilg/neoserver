package wms

import (
	"context"
	"fmt"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/workspace"
)

// PointFeatureInfoRequest is the protocol-neutral part of a feature-info query.
// Coordinates and PixelSize are expressed in CRS/SRID.
type PointFeatureInfoRequest struct {
	X, Y         float64
	PixelSize    float64
	CRS          string
	SRID         int
	FeatureCount int
	Time         string
	Elevation    string
}

// QueryResourceFeatureInfo lets WMTS reuse WMS' vector and coverage identify
// behavior without coupling either protocol's HTTP parameter model.
func QueryResourceFeatureInfo(ctx context.Context, ws *workspace.Workspace, resource *workspace.PublishedResource, request PointFeatureInfoRequest) ([]map[string]interface{}, error) {
	return queryResourceFeatureInfo(ctx, ws, resource, request, make(map[string]bool), 0)
}

func queryResourceFeatureInfo(ctx context.Context, ws *workspace.Workspace, resource *workspace.PublishedResource, request PointFeatureInfoRequest, visiting map[string]bool, depth int) ([]map[string]interface{}, error) {
	if resource == nil {
		return nil, errorsNewFeatureInfo("resource is unavailable")
	}
	if request.FeatureCount <= 0 {
		request.FeatureCount = 1
	}
	if resource.Kind == workspace.ResourceGroup {
		if ws == nil || resource.Group == nil || depth > 32 || visiting[resource.Group.PublicID] {
			return nil, errorsNewFeatureInfo("invalid layer group")
		}
		visiting[resource.Group.PublicID] = true
		defer delete(visiting, resource.Group.PublicID)
		var result []map[string]interface{}
		for _, member := range resource.Group.Members {
			child := ws.GetResource(member.Resource)
			remaining := request
			remaining.FeatureCount = request.FeatureCount - len(result)
			if remaining.FeatureCount <= 0 {
				break
			}
			values, err := queryResourceFeatureInfo(ctx, ws, child, remaining, visiting, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, values...)
		}
		return result, nil
	}
	if resource.Service == nil {
		return nil, errorsNewFeatureInfo("resource is unavailable")
	}
	if resource.Kind == workspace.ResourceCoverage {
		mapRequest := &GetFeatureInfoRequest{GetMapRequest: GetMapRequest{CRS: request.CRS, SRID: request.SRID, Time: request.Time, Elevation: request.Elevation}}
		properties, found, err := rasterFeatureInfo(ctx, resource, mapRequest, request.X, request.Y, request.PixelSize)
		if err != nil || !found {
			return nil, err
		}
		return []map[string]interface{}{properties}, nil
	}
	if resource.Layer == nil || resource.Service.DataSource == nil {
		return nil, errorsNewFeatureInfo("feature data source is unavailable")
	}
	tolerance := request.PixelSize * 5
	if tolerance <= 0 {
		tolerance = 1e-9
	}
	params := datasource.QueryParams{
		BBox: &datasource.BBox{
			MinX: request.X - tolerance, MinY: request.Y - tolerance,
			MaxX: request.X + tolerance, MaxY: request.Y + tolerance,
		},
		BBoxSRID: request.SRID, OutputSRID: request.SRID, Limit: request.FeatureCount,
	}
	if err := applyDimensionFilters(&GetMapRequest{Time: request.Time, Elevation: request.Elevation}, resource.Layer, &params); err != nil {
		return nil, err
	}
	features, err := resource.Layer.QueryFeatures(ctx, resource.Service.DataSource, params)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, 0, len(features))
	for _, encoded := range features {
		feature, err := datasource.DecodeFeatureJSON(encoded)
		if err != nil {
			continue
		}
		if properties, ok := feature["properties"].(map[string]interface{}); ok {
			result = append(result, properties)
		}
	}
	return result, nil
}

func errorsNewFeatureInfo(message string) error { return fmt.Errorf("feature info: %s", message) }
