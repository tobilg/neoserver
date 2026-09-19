package wms

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/renderer"
	"github.com/tobilg/neoserver/internal/workspace"
)

// featureInfoResult represents a feature info result.
type featureInfoResult struct {
	LayerName  string
	Properties map[string]interface{}
}

func (h *workspaceHandler) handleGetFeatureInfo(w http.ResponseWriter, r *http.Request, ws *workspace.Workspace) {
	ctx := r.Context()

	// Parse request
	maxEnvVariables, maxEnvValueBytes := h.environmentLimits()
	req, err := parseGetFeatureInfoRequestWithLimits(r, h.cfg.WMS.MaxWidth, h.cfg.WMS.MaxHeight, maxEnvVariables, maxEnvValueBytes)
	if err != nil {
		h.writeError(w, err)
		return
	}

	// Create transform to convert pixel to map coordinates
	transform := renderer.NewTransform(req.BBox, req.Width, req.Height)

	// Convert pixel coordinates to map coordinates
	mapX, mapY := transform.ToMap(float64(req.I), float64(req.J))

	// Calculate click tolerance in map units (e.g., 5 pixels)
	tolerance := transform.PixelSize() * 5

	// Query features for each query layer
	var results []featureInfoResult
	var queryLayers []string
	for _, name := range req.QueryLayers {
		expanded, expandErr := expandRenderLayer(ws, name, "", workspaceRole(r, ws.ID), h.cfg.WMS.MaxGroupDepth)
		if expandErr != nil {
			WriteException(w, ExceptionLayerNotDefined, expandErr.Error())
			return
		}
		for _, layer := range expanded {
			queryLayers = append(queryLayers, layer.Name)
		}
	}
	for _, layerName := range queryLayers {
		// Find the layer and its service. A layer the caller may not read is
		// treated as undefined so its existence is not disclosed.
		resource := ws.GetResource(layerName)
		if resource == nil || resource.Service == nil ||
			(resource.Layer != nil && !resource.Layer.VisibleToRole(workspaceRole(r, ws.ID))) ||
			(resource.Coverage != nil && !resource.Coverage.VisibleToRole(workspaceRole(r, ws.ID))) {
			WriteException(w, ExceptionLayerNotDefined, fmt.Sprintf("Layer not found: %s", layerName))
			return
		}
		if resource.Kind == workspace.ResourceCoverage {
			properties, found, err := rasterFeatureInfo(ctx, resource, req, mapX, mapY, transform.PixelSize())
			if err != nil {
				WriteException(w, ExceptionInvalidParameterValue, "Coverage query failed")
				return
			}
			if found {
				results = append(results, featureInfoResult{LayerName: layerName, Properties: properties})
			}
			continue
		}
		layer, service := resource.Layer, resource.Service

		if service.DataSource == nil {
			WriteException(w, ExceptionLayerNotDefined, fmt.Sprintf("Service not available for layer: %s", layerName))
			return
		}

		// Get layer info
		layerInfo, err := layer.FeatureInfo(ctx, service.DataSource)
		if err != nil {
			h.logger.Error("feature info metadata failed", "error", err)
			WriteException(w, ExceptionInvalidParameterValue, "Failed to get layer info")
			return
		}

		// Query features at the click point
		features, err := h.queryFeaturesAtPoint(ctx, service.DataSource, layer, layerInfo, req, mapX, mapY, tolerance)
		if err != nil {
			h.logger.Error("feature info query failed", "error", err)
			WriteException(w, ExceptionInvalidParameterValue, "Query failed")
			return
		}

		for _, feat := range features {
			results = append(results, featureInfoResult{
				LayerName:  layerName,
				Properties: feat,
			})
		}
	}

	// Write response in requested format
	switch req.InfoFormat {
	case InfoFormatJSON:
		h.writeFeatureInfoJSON(w, results)
	case InfoFormatHTML:
		h.writeFeatureInfoHTML(w, results)
	case InfoFormatText:
		h.writeFeatureInfoText(w, results)
	default:
		h.writeFeatureInfoXML(w, results)
	}
}

func rasterFeatureInfo(ctx context.Context, resource *workspace.PublishedResource, req *GetFeatureInfoRequest, x, y, pixelSize float64) (map[string]interface{}, bool, error) {
	coverage := resource.Coverage
	renderSource, ok := resource.Service.CoverageSource.(datasource.CoverageRenderDataSource)
	if !ok {
		return nil, false, fmt.Errorf("coverage portrayal unavailable")
	}
	info, err := resource.Service.CoverageSource.GetCoverageInfo(ctx, coverage.SourceCoverage)
	if err != nil {
		return nil, false, err
	}
	properties := make(map[string]interface{})
	timeValue, elevationValue := coverageDimensionValues(coverage, req.Time, req.Elevation)
	half := pixelSize / 2
	if half <= 0 {
		half = 1e-9
	}
	for start := 0; start < len(info.Bands); start += 4 {
		end := start + 4
		if end > len(info.Bands) {
			end = len(info.Bands)
		}
		bands := make([]int, end-start)
		for i := range bands {
			bands[i] = start + i + 1
		}
		grid, err := renderSource.RenderCoverage(ctx, coverage.SourceCoverage, datasource.CoverageRenderRequest{TargetCRS: req.CRS, BBox: [4]float64{x - half, y - half, x + half, y + half}, Width: 1, Height: 1, Bands: bands, Resampling: "nearest", Time: timeValue, Elevation: elevationValue})
		if err != nil {
			return nil, false, err
		}
		if len(grid.Valid) == 0 || !grid.Valid[0] {
			return nil, false, nil
		}
		applyRangeFieldNames(grid, coverage)
		for i, band := range grid.BandInfo {
			properties[band.Name] = grid.Bands[i][0]
		}
	}
	return properties, true, nil
}

// queryFeaturesAtPoint queries features at a specific point using the Query method.
func (h *workspaceHandler) queryFeaturesAtPoint(
	ctx context.Context,
	ds datasource.DataSource,
	layer *workspace.Layer,
	layerInfo *datasource.LayerInfo,
	req *GetFeatureInfoRequest,
	x, y, tolerance float64,
) ([]map[string]interface{}, error) {
	// Build a bbox around the click point
	params := datasource.QueryParams{
		BBox: &datasource.BBox{
			MinX: x - tolerance,
			MinY: y - tolerance,
			MaxX: x + tolerance,
			MaxY: y + tolerance,
		},
		BBoxSRID:   req.SRID,
		OutputSRID: req.SRID,
		Limit:      req.FeatureCount,
	}
	if err := applyDimensionFilters(&req.GetMapRequest, layer, &params); err != nil {
		return nil, err
	}

	// Query features
	features, err := layer.QueryFeatures(ctx, ds, params)
	if err != nil {
		return nil, err
	}

	// Parse the JSON features to extract properties
	var results []map[string]interface{}
	for _, feat := range features {
		feature, err := datasource.DecodeFeatureJSON(feat)
		if err != nil {
			continue
		}
		// Extract properties
		if props, ok := feature["properties"].(map[string]interface{}); ok {
			results = append(results, props)
		}
	}

	return results, nil
}

// XML types for GetFeatureInfo response
type xmlFeatureInfoProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

type xmlFeatureInfoFeature struct {
	XMLName    xml.Name                 `xml:"Feature"`
	LayerName  string                   `xml:"layer,attr"`
	Properties []xmlFeatureInfoProperty `xml:"Property"`
}

type xmlFeatureInfoResponse struct {
	XMLName  xml.Name                `xml:"FeatureInfoResponse"`
	Features []xmlFeatureInfoFeature `xml:"Feature"`
}

// writeFeatureInfoXML writes feature info in XML format.
func (h *workspaceHandler) writeFeatureInfoXML(w http.ResponseWriter, results []featureInfoResult) {
	response := xmlFeatureInfoResponse{}
	for _, result := range results {
		feat := xmlFeatureInfoFeature{LayerName: result.LayerName}
		for k, v := range result.Properties {
			feat.Properties = append(feat.Properties, xmlFeatureInfoProperty{
				Name:  k,
				Value: fmt.Sprintf("%v", v),
			})
		}
		response.Features = append(response.Features, feat)
	}

	w.Header().Set("Content-Type", InfoFormatXML)
	w.WriteHeader(http.StatusOK)

	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(response)
}

// writeFeatureInfoJSON writes feature info in JSON format.
func (h *workspaceHandler) writeFeatureInfoJSON(w http.ResponseWriter, results []featureInfoResult) {
	type jsonFeature struct {
		Type       string                 `json:"type"`
		LayerName  string                 `json:"layer"`
		Properties map[string]interface{} `json:"properties"`
	}

	type jsonResponse struct {
		Type     string        `json:"type"`
		Features []jsonFeature `json:"features"`
	}

	response := jsonResponse{
		Type: "FeatureCollection",
	}
	for _, result := range results {
		response.Features = append(response.Features, jsonFeature{
			Type:       "Feature",
			LayerName:  result.LayerName,
			Properties: result.Properties,
		})
	}

	w.Header().Set("Content-Type", InfoFormatJSON)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// writeFeatureInfoHTML writes feature info in HTML format.
func (h *workspaceHandler) writeFeatureInfoHTML(w http.ResponseWriter, results []featureInfoResult) {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html>\n<head>\n")
	sb.WriteString("<meta charset=\"UTF-8\">\n")
	sb.WriteString("<title>Feature Info</title>\n")
	sb.WriteString("<style>\n")
	sb.WriteString("body { font-family: Arial, sans-serif; margin: 10px; }\n")
	sb.WriteString("table { border-collapse: collapse; margin-bottom: 10px; }\n")
	sb.WriteString("th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }\n")
	sb.WriteString("th { background-color: #4CAF50; color: white; }\n")
	sb.WriteString("tr:nth-child(even) { background-color: #f2f2f2; }\n")
	sb.WriteString("h3 { margin-top: 20px; }\n")
	sb.WriteString("</style>\n")
	sb.WriteString("</head>\n<body>\n")

	if len(results) == 0 {
		sb.WriteString("<p>No features found at this location.</p>\n")
	} else {
		currentLayer := ""
		for _, result := range results {
			if result.LayerName != currentLayer {
				if currentLayer != "" {
					sb.WriteString("</table>\n")
				}
				currentLayer = result.LayerName
				sb.WriteString(fmt.Sprintf("<h3>Layer: %s</h3>\n", html.EscapeString(currentLayer)))
				sb.WriteString("<table>\n<tr><th>Property</th><th>Value</th></tr>\n")
			}
			for k, v := range result.Properties {
				sb.WriteString(fmt.Sprintf("<tr><td>%s</td><td>%v</td></tr>\n",
					html.EscapeString(k), html.EscapeString(fmt.Sprintf("%v", v))))
			}
		}
		if currentLayer != "" {
			sb.WriteString("</table>\n")
		}
	}

	sb.WriteString("</body>\n</html>")

	w.Header().Set("Content-Type", InfoFormatHTML)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(sb.String()))
}

// writeFeatureInfoText writes feature info in plain text format.
func (h *workspaceHandler) writeFeatureInfoText(w http.ResponseWriter, results []featureInfoResult) {
	var sb strings.Builder

	if len(results) == 0 {
		sb.WriteString("No features found at this location.\n")
	} else {
		for i, result := range results {
			if i > 0 {
				sb.WriteString("\n---\n\n")
			}
			sb.WriteString(fmt.Sprintf("Layer: %s\n", result.LayerName))
			for k, v := range result.Properties {
				sb.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
			}
		}
	}

	w.Header().Set("Content-Type", InfoFormatText)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(sb.String()))
}
