// Package gdalcap centralizes the optional GDAL drivers used by protocol
// encoders. Driver registration is process-global, so capability discovery is
// performed once and shared by WMS and WFS.
package gdalcap

import (
	"sync"

	"github.com/airbusgeo/godal"
)

// Capabilities describes the output drivers available in the linked GDAL
// runtime. A missing optional driver disables only the corresponding format.
type Capabilities struct {
	GeoPackage bool
	Shapefile  bool
	GeoTIFF    bool
	PDF        bool
}

var (
	once       sync.Once
	discovered Capabilities
)

// Get returns the immutable process-wide GDAL output capabilities.
func Get() Capabilities {
	once.Do(func() {
		// Registering individual drivers keeps the dependency explicit and also
		// works with GDAL builds that do not expose every optional driver.
		_ = godal.RegisterVector(godal.GeoPackage)
		_ = godal.RegisterVector(godal.Shapefile)
		_ = godal.RegisterRaster(godal.GTiff)
		_ = godal.RegisterRaster(godal.DriverName("PDF"))
		_, discovered.GeoPackage = godal.VectorDriver(godal.GeoPackage)
		_, discovered.Shapefile = godal.VectorDriver(godal.Shapefile)
		_, discovered.GeoTIFF = godal.RasterDriver(godal.GTiff)
		_, discovered.PDF = godal.RasterDriver(godal.DriverName("PDF"))
	})
	return discovered
}
