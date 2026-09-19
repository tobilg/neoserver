// Package rastergrid provides shared GDAL target-grid operations for raster
// file and database-backed coverage sources.
package rastergrid

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/airbusgeo/godal"
	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/crs"
	"github.com/tobilg/neoserver/internal/datasource"
)

func init() {
	godal.RegisterAll()
}

// WarpDataset reprojects a GDAL dataset to the exact requested output grid.
func WarpDataset(ds *godal.Dataset, request datasource.CoverageRenderRequest, bandInfo []datasource.CoverageBand) (*datasource.CoverageRenderGrid, error) {
	if ds == nil || request.Width < 1 || request.Height < 1 || request.BBox[0] >= request.BBox[2] || request.BBox[1] >= request.BBox[3] {
		return nil, fmt.Errorf("invalid coverage render grid")
	}
	target, err := normalizeCRS(request.TargetCRS)
	if err != nil {
		return nil, err
	}
	if len(request.Bands) == 0 {
		request.Bands = make([]int, len(bandInfo))
		for i := range request.Bands {
			request.Bands[i] = i + 1
		}
	}
	for _, number := range request.Bands {
		if number < 1 || number > len(bandInfo) {
			return nil, fmt.Errorf("raster band %d does not exist", number)
		}
	}
	resampling := strings.ToLower(strings.TrimSpace(request.Resampling))
	switch resampling {
	case "", "bilinear":
		resampling = "bilinear"
	case "nearest":
		resampling = "near"
	case "cubic":
	default:
		return nil, fmt.Errorf("unsupported raster resampling %q", request.Resampling)
	}
	name := "/vsimem/neoserver-render-" + uuid.NewString() + ".tif"
	defer godal.VSIUnlink(name)
	switches := []string{
		"-t_srs", target, "-te_srs", target,
		"-te", format(request.BBox[0]), format(request.BBox[1]), format(request.BBox[2]), format(request.BBox[3]),
		"-ts", strconv.Itoa(request.Width), strconv.Itoa(request.Height),
		"-r", resampling, "-dstalpha",
	}
	out, err := ds.Warp(name, switches, godal.GTiff, godal.CreationOption("COMPRESS=DEFLATE"))
	if err != nil {
		return nil, fmt.Errorf("warp coverage to %s: %w", target, err)
	}
	defer out.Close()
	structure := out.Structure()
	if structure.SizeX != request.Width || structure.SizeY != request.Height {
		return nil, fmt.Errorf("warped coverage has unexpected dimensions")
	}
	count := request.Width * request.Height
	result := &datasource.CoverageRenderGrid{
		Width: request.Width, Height: request.Height,
		BandNumbers: append([]int(nil), request.Bands...),
		Bands:       make([][]float64, len(request.Bands)), Valid: make([]bool, count),
		BandInfo: make([]datasource.CoverageBand, len(request.Bands)),
	}
	bands := out.Bands()
	for i, number := range request.Bands {
		values := make([]float64, count)
		if err := bands[number-1].Read(0, 0, values, request.Width, request.Height); err != nil {
			return nil, fmt.Errorf("read warped band %d: %w", number, err)
		}
		result.Bands[i] = values
		result.BandInfo[i] = bandInfo[number-1]
	}
	alphaIndex := -1
	for i := len(bands) - 1; i >= 0; i-- {
		if bands[i].ColorInterp() == godal.CIAlpha {
			alphaIndex = i
			break
		}
	}
	if alphaIndex >= 0 {
		alpha := make([]uint8, count)
		if err := bands[alphaIndex].Read(0, 0, alpha, request.Width, request.Height); err != nil {
			return nil, fmt.Errorf("read warped alpha: %w", err)
		}
		for i, value := range alpha {
			result.Valid[i] = value != 0
		}
	} else {
		for i := range result.Valid {
			result.Valid[i] = true
		}
	}
	for bandIndex, values := range result.Bands {
		var noData float64
		hasNoData := false
		if len(result.BandInfo[bandIndex].NilValues) > 0 {
			noData, err = strconv.ParseFloat(result.BandInfo[bandIndex].NilValues[0], 64)
			hasNoData = err == nil
		}
		for i, value := range values {
			if math.IsNaN(value) || (hasNoData && value == noData) {
				result.Valid[i] = false
			}
		}
	}
	return result, nil
}

// NativeWindow returns the clipped source pixel window covering the target
// request. Nine transformed points provide a modest densification for curved
// projected bounds.
func NativeWindow(info *datasource.CoverageInfo, request datasource.CoverageRenderRequest) (datasource.CoverageWindow, bool, error) {
	if info == nil {
		return datasource.CoverageWindow{}, false, fmt.Errorf("coverage metadata is missing")
	}
	targetEPSG, err := crs.Parse(request.TargetCRS)
	if err != nil {
		return datasource.CoverageWindow{}, false, err
	}
	xs := []float64{request.BBox[0], request.BBox[2], request.BBox[2], request.BBox[0], (request.BBox[0] + request.BBox[2]) / 2, request.BBox[2], (request.BBox[0] + request.BBox[2]) / 2, request.BBox[0], (request.BBox[0] + request.BBox[2]) / 2}
	ys := []float64{request.BBox[1], request.BBox[1], request.BBox[3], request.BBox[3], request.BBox[1], (request.BBox[1] + request.BBox[3]) / 2, request.BBox[3], (request.BBox[1] + request.BBox[3]) / 2, (request.BBox[1] + request.BBox[3]) / 2}
	if targetEPSG != info.SRID {
		sourceRef, e := godal.NewSpatialRefFromEPSG(targetEPSG)
		if e != nil {
			return datasource.CoverageWindow{}, false, e
		}
		defer sourceRef.Close()
		destRef, e := godal.NewSpatialRefFromEPSG(info.SRID)
		if e != nil {
			return datasource.CoverageWindow{}, false, e
		}
		defer destRef.Close()
		transform, e := godal.NewTransform(sourceRef, destRef)
		if e != nil {
			return datasource.CoverageWindow{}, false, e
		}
		defer transform.Close()
		success := make([]bool, len(xs))
		if e := transform.TransformEx(xs, ys, nil, success); e != nil {
			return datasource.CoverageWindow{}, false, e
		}
		for _, ok := range success {
			if !ok {
				return datasource.CoverageWindow{}, false, fmt.Errorf("cannot transform requested raster bounds")
			}
		}
	}
	minPX, minPY := math.Inf(1), math.Inf(1)
	maxPX, maxPY := math.Inf(-1), math.Inf(-1)
	for i := range xs {
		px := (xs[i] - info.OriginX) / info.ResolutionX
		py := (ys[i] - info.OriginY) / info.ResolutionY
		minPX, maxPX = math.Min(minPX, px), math.Max(maxPX, px)
		minPY, maxPY = math.Min(minPY, py), math.Max(maxPY, py)
	}
	x0, y0 := maxInt(0, int(math.Floor(minPX))), maxInt(0, int(math.Floor(minPY)))
	x1, y1 := minInt(info.Width, int(math.Ceil(maxPX))), minInt(info.Height, int(math.Ceil(maxPY)))
	if x0 >= x1 || y0 >= y1 {
		return datasource.CoverageWindow{}, false, nil
	}
	return datasource.CoverageWindow{XOff: x0, YOff: y0, Width: x1 - x0, Height: y1 - y0}, true, nil
}

// TransformBBox transforms a bounding box between two supported CRSs. Points
// are sampled along every edge so nonlinear projections do not clip the
// transformed domain.
func TransformBBox(bbox [4]float64, sourceCRS, targetCRS string) ([4]float64, error) {
	if bbox[0] >= bbox[2] || bbox[1] >= bbox[3] {
		return [4]float64{}, fmt.Errorf("invalid bounding box")
	}
	sourceEPSG, err := crs.Parse(sourceCRS)
	if err != nil {
		return [4]float64{}, err
	}
	targetEPSG, err := crs.Parse(targetCRS)
	if err != nil {
		return [4]float64{}, err
	}
	if sourceEPSG == targetEPSG {
		return bbox, nil
	}
	sourceRef, err := godal.NewSpatialRefFromEPSG(sourceEPSG)
	if err != nil {
		return [4]float64{}, err
	}
	defer sourceRef.Close()
	targetRef, err := godal.NewSpatialRefFromEPSG(targetEPSG)
	if err != nil {
		return [4]float64{}, err
	}
	defer targetRef.Close()
	transform, err := godal.NewTransform(sourceRef, targetRef)
	if err != nil {
		return [4]float64{}, err
	}
	defer transform.Close()
	const segments = 16
	xs := make([]float64, 0, (segments+1)*4)
	ys := make([]float64, 0, (segments+1)*4)
	for i := 0; i <= segments; i++ {
		t := float64(i) / segments
		x := bbox[0] + t*(bbox[2]-bbox[0])
		y := bbox[1] + t*(bbox[3]-bbox[1])
		xs = append(xs, x, x, bbox[0], bbox[2])
		ys = append(ys, bbox[1], bbox[3], y, y)
	}
	success := make([]bool, len(xs))
	if err := transform.TransformEx(xs, ys, nil, success); err != nil {
		return [4]float64{}, err
	}
	result := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for i, ok := range success {
		if !ok || math.IsNaN(xs[i]) || math.IsNaN(ys[i]) || math.IsInf(xs[i], 0) || math.IsInf(ys[i], 0) {
			return [4]float64{}, fmt.Errorf("cannot transform coverage bounds")
		}
		result[0], result[2] = math.Min(result[0], xs[i]), math.Max(result[2], xs[i])
		result[1], result[3] = math.Min(result[1], ys[i]), math.Max(result[3], ys[i])
	}
	return result, nil
}

func EmptyGrid(request datasource.CoverageRenderRequest, bands []int, info []datasource.CoverageBand) *datasource.CoverageRenderGrid {
	result := &datasource.CoverageRenderGrid{Width: request.Width, Height: request.Height, BandNumbers: append([]int(nil), bands...), Valid: make([]bool, request.Width*request.Height), Bands: make([][]float64, len(bands)), BandInfo: make([]datasource.CoverageBand, len(bands))}
	for i, number := range bands {
		result.Bands[i] = make([]float64, request.Width*request.Height)
		if number > 0 && number <= len(info) {
			result.BandInfo[i] = info[number-1]
		}
	}
	return result
}

func normalizeCRS(value string) (string, error) {
	code, err := crs.Parse(value)
	if err != nil {
		return "", err
	}
	ref, err := godal.NewSpatialRefFromEPSG(code)
	if err != nil {
		return "", fmt.Errorf("unresolvable EPSG:%d: %w", code, err)
	}
	ref.Close()
	return fmt.Sprintf("EPSG:%d", code), nil
}
func format(value float64) string { return strconv.FormatFloat(value, 'g', 17, 64) }
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
