package postgis

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/airbusgeo/godal"
	"github.com/tobilg/neoserver/internal/datasource"
	"github.com/tobilg/neoserver/internal/datasource/rastergrid"
)

// DiscoverCoverages reads the standard PostGIS raster_columns catalog. Only
// north-up, non-skewed, two-dimensional regular grids with a known SRID are exposed.
func (ds *DataSource) DiscoverCoverages(ctx context.Context) ([]*datasource.DiscoveredCoverage, error) {
	rows, err := ds.pool.Query(ctx, `
		SELECT r_table_schema, r_table_name, r_raster_column, srid, scale_x, scale_y,
		       num_bands, pixel_types::text[], ARRAY(SELECT x::text FROM unnest(nodata_values) x),
		       ST_XMin(extent), ST_YMin(extent), ST_XMax(extent), ST_YMax(extent),
		       same_alignment, regular_blocking
		FROM raster_columns
		WHERE r_table_schema = ANY($1) AND srid <> 0 AND scale_x <> 0 AND scale_y <> 0
		ORDER BY r_table_schema, r_table_name, r_raster_column`, ds.schemas)
	if err != nil {
		return nil, fmt.Errorf("discover PostGIS coverages: %w", err)
	}
	defer rows.Close()
	var result []*datasource.DiscoveredCoverage
	for rows.Next() {
		var schema, table, column string
		var srid, bandCount int
		var resX, resY, minX, minY, maxX, maxY float64
		var pixelTypes []string
		var noData []string
		var aligned, regular bool
		if err := rows.Scan(&schema, &table, &column, &srid, &resX, &resY, &bandCount,
			&pixelTypes, &noData, &minX, &minY, &maxX, &maxY, &aligned, &regular); err != nil {
			return nil, fmt.Errorf("scan PostGIS coverage: %w", err)
		}
		_ = regular
		if !aligned || bandCount < 1 || len(pixelTypes) != bandCount {
			continue
		}
		width := int(roundPositive((maxX - minX) / abs(resX)))
		height := int(roundPositive((maxY - minY) / abs(resY)))
		if width < 1 || height < 1 {
			continue
		}
		bands := make([]datasource.CoverageBand, bandCount)
		for i := range bands {
			bands[i] = datasource.CoverageBand{Band: i + 1, Name: fmt.Sprintf("band%d", i+1), DataType: pixelTypes[i]}
			if i < len(noData) && noData[i] != "" {
				bands[i].NilValues = []string{noData[i]}
			}
		}
		originY := maxY
		if resY > 0 {
			originY = minY
		}
		source := schema + "." + table + ":" + column
		result = append(result, &datasource.DiscoveredCoverage{
			SourceCoverage: source,
			Title:          table,
			Info: datasource.CoverageInfo{
				CRS: "http://www.opengis.net/def/crs/EPSG/0/" + strconv.Itoa(srid), SRID: srid,
				AxisLabels: [2]string{"x", "y"}, Width: width, Height: height,
				OriginX: minX, OriginY: originY, ResolutionX: resX, ResolutionY: resY,
				Envelope: [4]float64{minX, minY, maxX, maxY}, Bands: bands,
			},
		})
	}
	return result, rows.Err()
}

func (ds *DataSource) GetCoverageInfo(ctx context.Context, sourceCoverage string) (*datasource.CoverageInfo, error) {
	items, err := ds.DiscoverCoverages(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.SourceCoverage == sourceCoverage {
			return &item.Info, nil
		}
	}
	return nil, fmt.Errorf("coverage %q not found", sourceCoverage)
}

func (ds *DataSource) ExtractCoverage(ctx context.Context, sourceCoverage string, window datasource.CoverageWindow) ([]byte, error) {
	schema, table, column, err := parseRasterSource(sourceCoverage)
	if err != nil || !contains(ds.schemas, schema) {
		return nil, fmt.Errorf("invalid coverage source")
	}
	info, err := ds.GetCoverageInfo(ctx, sourceCoverage)
	if err != nil {
		return nil, err
	}
	if window.XOff < 0 || window.YOff < 0 || window.Width < 1 || window.Height < 1 ||
		window.XOff+window.Width > info.Width || window.YOff+window.Height > info.Height {
		return nil, fmt.Errorf("coverage window is outside the grid")
	}
	x1 := info.OriginX + float64(window.XOff)*info.ResolutionX
	x2 := info.OriginX + float64(window.XOff+window.Width)*info.ResolutionX
	y1 := info.OriginY + float64(window.YOff)*info.ResolutionY
	y2 := info.OriginY + float64(window.YOff+window.Height)*info.ResolutionY
	minX, maxX := minmax(x1, x2)
	minY, maxY := minmax(y1, y2)
	query := fmt.Sprintf(`
		WITH clipped AS (
			SELECT ST_Clip(%s, ST_MakeEnvelope($1,$2,$3,$4,$5), true) AS rast
			FROM %s.%s
			WHERE ST_Intersects(%s, ST_MakeEnvelope($1,$2,$3,$4,$5))
		), mosaic AS (SELECT ST_Union(rast) AS rast FROM clipped)
		SELECT ST_AsGDALRaster(rast, 'GTiff', ARRAY['COMPRESS=DEFLATE']) FROM mosaic WHERE rast IS NOT NULL`,
		quoteIdent(column), quoteIdent(schema), quoteIdent(table), quoteIdent(column))
	var data []byte
	if err := ds.pool.QueryRow(ctx, query, minX, minY, maxX, maxY, info.SRID).Scan(&data); err != nil {
		return nil, fmt.Errorf("extract PostGIS coverage: %w", err)
	}
	return data, nil
}

func (ds *DataSource) ReadCoverage(ctx context.Context, sourceCoverage string, window datasource.CoverageWindow) (*datasource.CoverageRaster, error) {
	data, err := ds.ExtractCoverage(ctx, sourceCoverage, window)
	if err != nil {
		return nil, err
	}
	// PostGIS can expose the generated GeoTIFF values as JSON without a second
	// native decoder or a temporary filesystem file.
	var bandsJSON string
	if err := ds.pool.QueryRow(ctx, `
		WITH r AS (SELECT ST_FromGDALRaster($1::bytea) AS rast)
		SELECT json_agg(ST_DumpValues(rast, band, false) ORDER BY band)::text
		FROM r, generate_series(1, ST_NumBands(rast)) AS band`, data).Scan(&bandsJSON); err != nil {
		return nil, fmt.Errorf("read PostGIS coverage values: %w", err)
	}
	var nested [][][]float64
	if err := json.Unmarshal([]byte(bandsJSON), &nested); err != nil {
		return nil, fmt.Errorf("decode coverage values: %w", err)
	}
	result := &datasource.CoverageRaster{Width: window.Width, Height: window.Height, Bands: make([][]float64, len(nested))}
	for band := range nested {
		for _, row := range nested[band] {
			result.Bands[band] = append(result.Bands[band], row...)
		}
	}
	return result, nil
}

func (ds *DataSource) RenderCoverage(ctx context.Context, sourceCoverage string, request datasource.CoverageRenderRequest) (*datasource.CoverageRenderGrid, error) {
	info, err := ds.GetCoverageInfo(ctx, sourceCoverage)
	if err != nil {
		return nil, err
	}
	if len(request.Bands) == 0 {
		limit := len(info.Bands)
		if limit > 4 {
			limit = 4
		}
		request.Bands = make([]int, limit)
		for i := range request.Bands {
			request.Bands[i] = i + 1
		}
	}
	window, intersects, err := rastergrid.NativeWindow(info, request)
	if err != nil {
		return nil, fmt.Errorf("resolve PostGIS raster window: %w", err)
	}
	if !intersects {
		return rastergrid.EmptyGrid(request, request.Bands, info.Bands), nil
	}
	data, err := ds.ExtractCoverage(ctx, sourceCoverage, window)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", "neoserver-postgis-raster-*.tif")
	if err != nil {
		return nil, fmt.Errorf("create raster staging file: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return nil, fmt.Errorf("write raster staging file: %w", err)
	}
	if err = file.Close(); err != nil {
		return nil, fmt.Errorf("close raster staging file: %w", err)
	}
	dataset, err := godal.Open(name, godal.RasterOnly(), godal.Drivers("GTiff"))
	if err != nil {
		return nil, fmt.Errorf("open PostGIS raster mosaic: %w", err)
	}
	defer dataset.Close()
	return rastergrid.WarpDataset(dataset, request, info.Bands)
}

func parseRasterSource(value string) (string, string, string, error) {
	parts := strings.Split(value, ":")
	name := strings.Split(parts[0], ".")
	if len(parts) != 2 || len(name) != 2 || name[0] == "" || name[1] == "" || parts[1] == "" {
		return "", "", "", fmt.Errorf("invalid raster source")
	}
	return name[0], name[1], parts[1], nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
func roundPositive(value float64) float64 { return float64(int(value + .5)) }
func minmax(a, b float64) (float64, float64) {
	if a < b {
		return a, b
	}
	return b, a
}

var _ datasource.CoverageDataSource = (*DataSource)(nil)
var _ datasource.CoverageRenderDataSource = (*DataSource)(nil)
