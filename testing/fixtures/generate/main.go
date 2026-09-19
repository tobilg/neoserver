// Regenerate the synthetic runtime-format fixtures from the repository root:
// make fixture-formats (requires native GDAL drivers and DuckDB spatial).
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/airbusgeo/godal"
	_ "github.com/duckdb/duckdb-go/v2"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	godal.RegisterAll()
	raster := "testing/fixtures/raster"
	vector := "testing/fixtures/vector"
	must(os.MkdirAll(raster, 0755))
	must(os.MkdirAll(vector, 0755))
	ref, err := godal.NewSpatialRefFromEPSG(4326)
	must(err)
	defer ref.Close()
	ds, err := godal.Create(godal.GTiff, filepath.Join(raster, "runtime-grid.tif"), 1, godal.Int16, 3, 2)
	must(err)
	must(ds.SetSpatialRef(ref))
	must(ds.SetGeoTransform([6]float64{7, 1, 0, 52, 0, -1}))
	must(ds.Bands()[0].SetDescription("temperature"))
	must(ds.Bands()[0].Write(0, 0, []int16{1, 2, 3, 4, 5, 6}, 3, 2))
	for _, out := range []struct{ name, driver string }{{"runtime-grid.nc", "netCDF"}, {"runtime-grid.grib2", "GRIB"}} {
		translated, err := ds.Translate(filepath.Join(raster, out.name), nil, godal.DriverName(out.driver), godal.ErrLogger(func(category godal.ErrorCategory, _ int, message string) error {
			if category >= godal.CE_Failure {
				return fmt.Errorf("%s", message)
			}
			return nil
		}))
		must(err)
		must(translated.Close())
	}
	must(ds.Close())
	nad27, err := godal.NewSpatialRefFromEPSG(4267)
	must(err)
	defer nad27.Close()
	nad, err := godal.Create(godal.GTiff, filepath.Join(raster, "runtime-nad27.tif"), 1, godal.Int16, 3, 2)
	must(err)
	must(nad.SetSpatialRef(nad27))
	must(nad.SetGeoTransform([6]float64{-100, 0.3, 0, 31, 0, -0.5}))
	must(nad.Bands()[0].Write(0, 0, []int16{1, 2, 3, 4, 5, 6}, 3, 2))
	must(nad.Close())
	geojson := `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"id":1,"name":"München"},"geometry":{"type":"Point","coordinates":[8,51]}}]}`
	must(os.WriteFile(filepath.Join(vector, "runtime-points.geojson"), []byte(geojson+"\n"), 0644))
	vectors, err := godal.Open(filepath.Join(vector, "runtime-points.geojson"), godal.VectorOnly())
	must(err)
	shp, err := vectors.VectorTranslate(filepath.Join(vector, "runtime-cp1252.shp"), []string{"-f", "ESRI Shapefile", "-lco", "ENCODING=CP1252"})
	must(err)
	must(shp.Close())
	must(vectors.Close())
	must(os.WriteFile(filepath.Join(vector, "runtime-nad27.geojson"), []byte(strings.ReplaceAll(geojson, "[8,51]", "[-100,30]")+"\n"), 0644))
	nadVectors, err := godal.Open(filepath.Join(vector, "runtime-nad27.geojson"), godal.VectorOnly())
	must(err)
	nadShp, err := nadVectors.VectorTranslate(filepath.Join(vector, "runtime-nad27.shp"), []string{"-f", "ESRI Shapefile", "-a_srs", "EPSG:4267", "-lco", "ENCODING=UTF-8"})
	must(err)
	must(nadShp.Close())
	must(nadVectors.Close())
	must(os.Remove(filepath.Join(vector, "runtime-nad27.geojson")))
	path := filepath.Join(vector, "runtime-points.duckdb")
	// This command owns only these reproducible synthetic fixtures.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		must(err)
	}
	db, err := sql.Open("duckdb", path)
	must(err)
	defer db.Close()
	_, err = db.Exec("INSTALL spatial; LOAD spatial; CREATE TABLE points AS SELECT 1::INTEGER AS id, 'München' AS name, ST_Point(8,51) AS geom;")
	must(err)
	parquet := strings.ReplaceAll(filepath.Join(vector, "runtime-points.parquet"), "'", "''")
	_, err = db.Exec("COPY points TO '" + parquet + "' (FORMAT PARQUET)")
	must(err)
}
