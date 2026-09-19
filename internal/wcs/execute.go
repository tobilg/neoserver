package wcs

import (
	"context"
	"fmt"
	"io"

	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/rastergrid"
	"github.com/tobilg/neoserver/internal/store"
	"github.com/tobilg/neoserver/internal/workspace"
)

type coveragePayload struct {
	body        []byte
	contentType string
}

func executeCoveragePlan(ctx context.Context, source datasource.CoverageDataSource, coverage *workspace.Coverage, plan *coveragePlan, maxBytes int64, version string) (*coveragePayload, error) {
	if generalized, ok := source.(datasource.CoverageQueryDataSource); ok && useGeneralizedSource(ctx, generalized, coverage.SourceCoverage) {
		result, err := generalized.ExecuteCoverage(ctx, coverage.SourceCoverage, plan.query)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, fmt.Errorf("coverage query returned no result")
		}
		if result.Body != nil {
			defer result.Body.Close()
			body, err := io.ReadAll(io.LimitReader(result.Body, maxBytes+1))
			if err != nil {
				return nil, err
			}
			return &coveragePayload{body: body, contentType: result.ContentType}, nil
		}
		if result.Grid != nil {
			return encodeCoverageGrid(version, coverage, plan, result.Grid)
		}
		if result.Raster != nil {
			return encodeCoverageRaster(version, coverage, plan, result.Raster)
		}
		return nil, fmt.Errorf("coverage query returned an empty result")
	}

	if !plan.requiresGrid && plan.query.Format == "image/tiff" {
		body, err := source.ExtractCoverage(ctx, coverage.SourceCoverage, plan.source)
		return &coveragePayload{body: body, contentType: "image/tiff"}, err
	}
	if !plan.requiresGrid && plan.query.Format == "multipart/related" {
		tiff, err := source.ExtractCoverage(ctx, coverage.SourceCoverage, plan.source)
		if err != nil {
			return nil, err
		}
		body, contentType := multipartCoverage(version, coverage, plan.outputInfo, plan.output, tiff)
		return &coveragePayload{body: body, contentType: contentType}, nil
	}
	if !plan.requiresGrid && plan.query.Format == "application/gml+xml" {
		raster, err := source.ReadCoverage(ctx, coverage.SourceCoverage, plan.source)
		if err != nil {
			return nil, err
		}
		return encodeCoverageRaster(version, coverage, plan, raster)
	}

	grid, err := renderCoverageGrid(ctx, source, coverage, plan)
	if err != nil {
		return nil, err
	}
	return encodeCoverageGrid(version, coverage, plan, grid)
}

func useGeneralizedSource(ctx context.Context, source datasource.CoverageQueryDataSource, coverage string) bool {
	if source.Type() == store.ServiceTypeRasterMosaic {
		return true
	}
	descriptor, err := source.DescribeCoverage(ctx, coverage)
	return err == nil && descriptor != nil && len(descriptor.Axes) > 2
}

func renderCoverageGrid(ctx context.Context, source datasource.CoverageDataSource, coverage *workspace.Coverage, plan *coveragePlan) (*datasource.CoverageRenderGrid, error) {
	if renderer, ok := source.(datasource.CoverageRenderDataSource); ok {
		grid, err := renderer.RenderCoverage(ctx, coverage.SourceCoverage, datasource.CoverageRenderRequest{
			TargetCRS:  plan.outputInfo.CRS,
			BBox:       plan.outputInfo.Envelope,
			Width:      plan.output.Width,
			Height:     plan.output.Height,
			Bands:      append([]int(nil), plan.bands...),
			Resampling: plan.resampling,
			Time:       plan.time,
			Elevation:  plan.elevation,
		})
		if err != nil {
			return nil, err
		}
		grid.BandInfo = append([]datasource.CoverageBand(nil), plan.outputInfo.Bands...)
		grid.BandNumbers = append([]int(nil), plan.bands...)
		return grid, nil
	}
	if plan.output.Width != plan.source.Width || plan.output.Height != plan.source.Height ||
		!sameCRS(plan.outputInfo.CRS, plan.nativeCRS) || plan.query.Interpolation != "" || plan.time != "" || plan.elevation != "" {
		return nil, fmt.Errorf("coverage source does not support target-grid extraction")
	}
	raster, err := source.ReadCoverage(ctx, coverage.SourceCoverage, plan.source)
	if err != nil {
		return nil, err
	}
	grid := &datasource.CoverageRenderGrid{Width: raster.Width, Height: raster.Height, BandNumbers: append([]int(nil), plan.bands...), BandInfo: append([]datasource.CoverageBand(nil), plan.outputInfo.Bands...), Valid: make([]bool, raster.Width*raster.Height)}
	for index := range grid.Valid {
		grid.Valid[index] = true
	}
	grid.Bands = make([][]float64, len(plan.bands))
	for index, band := range plan.bands {
		if band < 1 || band > len(raster.Bands) {
			return nil, fmt.Errorf("coverage range component %d is unavailable", band)
		}
		grid.Bands[index] = raster.Bands[band-1]
	}
	return grid, nil
}

func encodeCoverageGrid(version string, coverage *workspace.Coverage, plan *coveragePlan, grid *datasource.CoverageRenderGrid) (*coveragePayload, error) {
	if plan.query.Format == "application/gml+xml" {
		return encodeCoverageRaster(version, coverage, plan, rasterFromGrid(grid))
	}
	format := plan.query.Format
	if format == "multipart/related" {
		format = "image/tiff"
	}
	body, err := rastergrid.EncodeGridWithOptions(grid, plan.outputInfo, format, rastergrid.EncodingOptions{
		TemporaryDirectory: plan.query.TemporaryDirectory,
		MaxBytes:           plan.query.MaxTemporaryBytes,
	})
	if err != nil {
		return nil, err
	}
	if plan.query.Format == "multipart/related" {
		multipartBody, contentType := multipartCoverage(version, coverage, plan.outputInfo, plan.output, body)
		return &coveragePayload{body: multipartBody, contentType: contentType}, nil
	}
	return &coveragePayload{body: body, contentType: format}, nil
}

func encodeCoverageRaster(version string, coverage *workspace.Coverage, plan *coveragePlan, raster *datasource.CoverageRaster) (*coveragePayload, error) {
	if plan.query.Format != "application/gml+xml" {
		grid := &datasource.CoverageRenderGrid{Width: raster.Width, Height: raster.Height, Bands: raster.Bands, BandInfo: append([]datasource.CoverageBand(nil), plan.outputInfo.Bands...), Valid: make([]bool, raster.Width*raster.Height)}
		for index := range grid.Valid {
			grid.Valid[index] = true
		}
		return encodeCoverageGrid(version, coverage, plan, grid)
	}
	body := gmlCoverage(version, coverage, plan.outputInfo, plan.output, raster, "")
	return &coveragePayload{body: body, contentType: "application/gml+xml"}, nil
}

func rasterFromGrid(grid *datasource.CoverageRenderGrid) *datasource.CoverageRaster {
	return &datasource.CoverageRaster{Width: grid.Width, Height: grid.Height, Bands: grid.Bands}
}
