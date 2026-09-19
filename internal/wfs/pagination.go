package wfs

import (
	"fmt"
	"strconv"
	"strings"
)

// Serialize only the effective query, not raw URL parameters (which can carry
// credentials). XML POST queries use the same canonical KVP continuation.
func featurePageURL(base, typeName string, offset, count int, requests ...*GetFeatureRequest) string {
	values := map[string]string{"service": "WFS", "version": "2.0.0", "request": "GetFeature", "typeNames": typeName, "startIndex": strconv.Itoa(offset), "count": strconv.Itoa(count)}
	if len(requests) > 0 && requests[0] != nil {
		req := requests[0]
		for key, value := range map[string]string{"srsName": req.SrsName, "filter": req.Filter, "outputFormat": req.OutputFormat, "resultType": req.ResultType, "propertyName": strings.Join(req.PropertyName, ","), "resourceID": strings.Join(req.ResourceID, ","), "resolve": req.Resolve, "resolveDepth": req.ResolveDepth} {
			if value != "" {
				values[key] = value
			}
		}
		if req.FESFilter != "" {
			values["filter"] = req.FESFilter
		}
		if req.SrsName == "" && req.SRID > 0 {
			values["srsName"] = fmt.Sprintf("EPSG:%d", req.SRID)
		}
		if req.BBox != nil {
			bbox := req.BBox
			values["bbox"] = fmt.Sprintf("%g,%g,%g,%g", bbox.MinX, bbox.MinY, bbox.MaxX, bbox.MaxY)
			if req.BBoxSRID > 0 {
				values["bbox"] += fmt.Sprintf(",EPSG:%d", req.BBoxSRID)
			}
		}
		var sort []string
		for _, field := range req.SortBy {
			direction := " A"
			if field.Desc {
				direction = " D"
			}
			sort = append(sort, field.Name+direction)
		}
		if len(sort) > 0 {
			values["sortBy"] = strings.Join(sort, ",")
		}
	}
	return wfsURL(base, values)
}
