// Package gdalmd provides the small portion of GDAL's multidimensional C API
// that is not exposed by the pinned godal dependency.
package gdalmd

/*
#cgo pkg-config: ${SRCDIR}/gdal-headers.pc
#include <stdlib.h>
#include <gdal.h>
#include <gdal_utils.h>
#include <ogr_srs_api.h>
#include <cpl_string.h>

static int ns_authority_axis_swap(int epsg) {
	OGRSpatialReferenceH srs = OSRNewSpatialReference(NULL);
	if (srs == NULL) return -1;
	if (OSRImportFromEPSG(srs, epsg) != OGRERR_NONE) { OSRDestroySpatialReference(srs); return -1; }
	OSRSetAxisMappingStrategy(srs, OAMS_TRADITIONAL_GIS_ORDER);
	int count = 0;
	const int *mapping = OSRGetDataAxisToSRSAxisMapping(srs, &count);
	int swap = count >= 2 && mapping != NULL && mapping[0] == 2 && mapping[1] == 1;
	OSRDestroySpatialReference(srs);
	return swap;
}

static const char* ns_string_list_get(char **values, int index) {
	return values[index];
}

static GDALDatasetH ns_mdim_translate(const char *destination,
	GDALDatasetH source, char **arguments, int *usage_error) {
	GDALMultiDimTranslateOptions *options = GDALMultiDimTranslateOptionsNew(arguments, NULL);
	if (options == NULL) return NULL;
	GDALDatasetH result = GDALMultiDimTranslate(destination, NULL, 1, &source, options, usage_error);
	GDALMultiDimTranslateOptionsFree(options);
	return result;
}

// GDALGroupGetMDArrayFullNamesRecursive was added in GDAL 3.11. Keep the
// server buildable with the older multidimensional API shipped by supported
// Linux distributions by composing the same result from the group traversal
// primitives that have existed since GDAL 3.1.
static char **ns_group_get_md_array_full_names_recursive(GDALGroupH group) {
	char **result = NULL;
	char **array_names = GDALGroupGetMDArrayNames(group, NULL);
	if (array_names != NULL) {
		for (int i = 0; array_names[i] != NULL; i++) {
			GDALMDArrayH array = GDALGroupOpenMDArray(group, array_names[i], NULL);
			if (array != NULL) {
				result = CSLAddString(result, GDALMDArrayGetFullName(array));
				GDALMDArrayRelease(array);
			}
		}
		CSLDestroy(array_names);
	}

	char **group_names = GDALGroupGetGroupNames(group, NULL);
	if (group_names != NULL) {
		for (int i = 0; group_names[i] != NULL; i++) {
			GDALGroupH child = GDALGroupOpenGroup(group, group_names[i], NULL);
			if (child != NULL) {
				char **child_names = ns_group_get_md_array_full_names_recursive(child);
				if (child_names != NULL) {
					for (int j = 0; child_names[j] != NULL; j++) {
						result = CSLAddString(result, child_names[j]);
					}
					CSLDestroy(child_names);
				}
				GDALGroupRelease(child);
			}
		}
		CSLDestroy(group_names);
	}
	return result;
}
*/
import "C"

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"unsafe"

	// godal owns the GDAL linker flags. Keep it in the dependency graph even
	// when this package is used on its own; gdal-headers.pc supplies only headers.
	"github.com/airbusgeo/godal"
)

// AuthorityAxisSwap reports whether traditional GIS XY coordinates must be
// exchanged for the authoritative EPSG axis order used by GML URNs. This also
// covers projected northing/easting CRSs, not just geographic EPSG:4326.
func AuthorityAxisSwap(srid int) (bool, error) {
	result := C.ns_authority_axis_swap(C.int(srid))
	if result < 0 {
		return false, fmt.Errorf("cannot resolve axis order for EPSG:%d", srid)
	}
	return result == 1, nil
}

type Dataset struct {
	mu     sync.Mutex
	handle C.GDALDatasetH
	root   C.GDALGroupH
	driver string
}

type Dimension struct {
	Name                string
	FullName            string
	Type                string
	Direction           string
	Size                uint64
	Unit                string
	Coordinates         []float64
	CoordinatesComplete bool
}

type Array struct {
	Name       string
	FullName   string
	DataType   string
	Unit       string
	CRS        string
	NoData     *float64
	Scale      *float64
	Offset     *float64
	Attributes map[string]string
	Dimensions []Dimension
}

func Open(path string, drivers ...string) (*Dataset, error) {
	return OpenWithOptions(path, drivers, nil)
}

func OpenWithOptions(path string, drivers []string, openOptions map[string]string) (*Dataset, error) {
	godal.RegisterAll()
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var allowed **C.char
	for _, driver := range drivers {
		cValue := C.CString(driver)
		allowed = C.CSLAddString(allowed, cValue)
		C.free(unsafe.Pointer(cValue))
	}
	if allowed != nil {
		defer C.CSLDestroy(allowed)
	}
	var options **C.char
	for key, value := range openOptions {
		cValue := C.CString(key + "=" + value)
		options = C.CSLAddString(options, cValue)
		C.free(unsafe.Pointer(cValue))
	}
	if options != nil {
		defer C.CSLDestroy(options)
	}
	handle := C.GDALOpenEx(cPath, C.GDAL_OF_MULTIDIM_RASTER|C.GDAL_OF_READONLY|C.GDAL_OF_VERBOSE_ERROR, allowed, options, nil)
	if handle == nil {
		return nil, fmt.Errorf("open multidimensional GDAL dataset")
	}
	root := C.GDALDatasetGetRootGroup(handle)
	if root == nil {
		C.GDALClose(handle)
		return nil, fmt.Errorf("dataset has no multidimensional root group")
	}
	driver := C.GDALGetDatasetDriver(handle)
	return &Dataset{handle: handle, root: root, driver: C.GoString(C.GDALGetDriverShortName(driver))}, nil
}

func (d *Dataset) Driver() string { return d.driver }

func (d *Dataset) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.handle == nil {
		return nil
	}
	C.GDALGroupRelease(d.root)
	C.GDALClose(d.handle)
	d.root, d.handle = nil, nil
	return nil
}

func (d *Dataset) ArrayNames() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.handle == nil {
		return nil, fmt.Errorf("multidimensional dataset is closed")
	}
	values := C.ns_group_get_md_array_full_names_recursive(d.root)
	if values == nil {
		return nil, nil
	}
	defer C.CSLDestroy(values)
	count := int(C.CSLCount(values))
	result := make([]string, 0, count)
	for i := 0; i < count; i++ {
		result = append(result, C.GoString(C.ns_string_list_get(values, C.int(i))))
	}
	return result, nil
}

func (d *Dataset) DescribeArray(fullName string, coordinateLimit uint64) (*Array, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.handle == nil {
		return nil, fmt.Errorf("multidimensional dataset is closed")
	}
	array, err := d.openArray(fullName)
	if err != nil {
		return nil, err
	}
	defer C.GDALMDArrayRelease(array)
	result := &Array{
		Name: C.GoString(C.GDALMDArrayGetName(array)), FullName: C.GoString(C.GDALMDArrayGetFullName(array)),
		Unit: C.GoString(C.GDALMDArrayGetUnit(array)), Attributes: readAttributes(array),
	}
	dtype := C.GDALMDArrayGetDataType(array)
	if dtype == nil {
		return nil, fmt.Errorf("array %s has no data type", fullName)
	}
	result.DataType = C.GoString(C.GDALExtendedDataTypeGetName(dtype))
	if C.GDALExtendedDataTypeGetClass(dtype) != C.GEDTC_NUMERIC {
		C.GDALExtendedDataTypeRelease(dtype)
		return nil, fmt.Errorf("array %s is not numeric", fullName)
	}
	C.GDALExtendedDataTypeRelease(dtype)
	var has C.int
	value := float64(C.GDALMDArrayGetNoDataValueAsDouble(array, &has))
	if has != 0 {
		result.NoData = &value
	}
	value = float64(C.GDALMDArrayGetScale(array, &has))
	if has != 0 {
		result.Scale = &value
	}
	value = float64(C.GDALMDArrayGetOffset(array, &has))
	if has != 0 {
		result.Offset = &value
	}
	if ref := C.GDALMDArrayGetSpatialRef(array); ref != nil {
		_ = C.OSRAutoIdentifyEPSG(ref)
		authority := C.OSRGetAuthorityName(ref, nil)
		code := C.OSRGetAuthorityCode(ref, nil)
		if authority != nil && code != nil {
			result.CRS = "http://www.opengis.net/def/crs/" + C.GoString(authority) + "/0/" + C.GoString(code)
		}
	}
	var dimensionCount C.size_t
	dimensions := C.GDALMDArrayGetDimensions(array, &dimensionCount)
	if dimensions != nil {
		defer C.GDALReleaseDimensions(dimensions, dimensionCount)
	}
	// cgo cannot index an opaque handle array directly; use a Go view over the
	// returned contiguous pointer array.
	dimensionSlice := unsafe.Slice((*C.GDALDimensionH)(unsafe.Pointer(dimensions)), int(dimensionCount))
	for _, dimension := range dimensionSlice {
		item := Dimension{
			Name: C.GoString(C.GDALDimensionGetName(dimension)), FullName: C.GoString(C.GDALDimensionGetFullName(dimension)),
			Type: C.GoString(C.GDALDimensionGetType(dimension)), Direction: C.GoString(C.GDALDimensionGetDirection(dimension)),
			Size: uint64(C.GDALDimensionGetSize(dimension)),
		}
		if indexing := C.GDALDimensionGetIndexingVariable(dimension); indexing != nil {
			item.Unit = C.GoString(C.GDALMDArrayGetUnit(indexing))
			if item.Unit == "" {
				item.Unit = readAttribute(indexing, "units")
			}
			if item.Size <= coordinateLimit {
				item.Coordinates, err = readNumericArray(indexing, []uint64{0}, []uint64{item.Size})
				item.CoordinatesComplete = err == nil
			} else if item.Size > 1 {
				first, _ := readNumericArray(indexing, []uint64{0}, []uint64{2})
				last, _ := readNumericArray(indexing, []uint64{item.Size - 1}, []uint64{1})
				item.Coordinates = append(first, last...)
			}
			C.GDALMDArrayRelease(indexing)
		}
		result.Dimensions = append(result.Dimensions, item)
	}
	return result, nil
}

func (d *Dataset) ReadFloat64(fullName string, start, count []uint64) ([]float64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	array, err := d.openArray(fullName)
	if err != nil {
		return nil, err
	}
	defer C.GDALMDArrayRelease(array)
	return readNumericArray(array, start, count)
}

// Translate runs GDALMultiDimTranslate against the already-open dataset. The
// caller owns destination cleanup after the returned dataset has been closed.
func (d *Dataset) Translate(destination, driver, array string, subsets []string, scaleAxes string, creationOptions []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.handle == nil {
		return fmt.Errorf("multidimensional dataset is closed")
	}
	arguments := []string{"-of", driver, "-array", array, "-strict"}
	for _, subset := range subsets {
		arguments = append(arguments, "-subset", subset)
	}
	if scaleAxes != "" {
		arguments = append(arguments, "-scaleaxes", scaleAxes)
	}
	for _, option := range creationOptions {
		arguments = append(arguments, "-co", option)
	}
	var list **C.char
	for _, argument := range arguments {
		value := C.CString(argument)
		list = C.CSLAddString(list, value)
		C.free(unsafe.Pointer(value))
	}
	defer C.CSLDestroy(list)
	cDestination := C.CString(destination)
	defer C.free(unsafe.Pointer(cDestination))
	var usage C.int
	output := C.ns_mdim_translate(cDestination, d.handle, list, &usage)
	if output == nil {
		if usage != 0 {
			return fmt.Errorf("invalid multidimensional translation options")
		}
		return fmt.Errorf("multidimensional translation failed")
	}
	C.GDALClose(output)
	return nil
}

func (d *Dataset) openArray(fullName string) (C.GDALMDArrayH, error) {
	value := C.CString(fullName)
	defer C.free(unsafe.Pointer(value))
	array := C.GDALGroupOpenMDArrayFromFullname(d.root, value, nil)
	if array == nil {
		return nil, fmt.Errorf("multidimensional array %q not found", fullName)
	}
	return array, nil
}

func readNumericArray(array C.GDALMDArrayH, start, count []uint64) ([]float64, error) {
	if len(start) != len(count) || len(start) == 0 {
		return nil, fmt.Errorf("invalid multidimensional read window")
	}
	total := uint64(1)
	for _, value := range count {
		if value == 0 || total > math.MaxInt/value {
			return nil, fmt.Errorf("invalid multidimensional read size")
		}
		total *= value
	}
	starts := make([]C.GUInt64, len(start))
	counts := make([]C.size_t, len(count))
	for i := range start {
		starts[i], counts[i] = C.GUInt64(start[i]), C.size_t(count[i])
	}
	result := make([]float64, int(total))
	dtype := C.GDALExtendedDataTypeCreate(C.GDT_Float64)
	if dtype == nil {
		return nil, fmt.Errorf("create GDAL Float64 data type")
	}
	defer C.GDALExtendedDataTypeRelease(dtype)
	pointer := unsafe.Pointer(&result[0])
	if C.GDALMDArrayRead(array, &starts[0], &counts[0], nil, nil, dtype, pointer, pointer, C.size_t(len(result)*8)) == 0 {
		return nil, fmt.Errorf("read multidimensional array")
	}
	return result, nil
}

func readAttributes(array C.GDALMDArrayH) map[string]string {
	result := map[string]string{}
	var count C.size_t
	attributes := C.GDALMDArrayGetAttributes(array, &count, nil)
	if attributes == nil {
		return result
	}
	defer C.GDALReleaseAttributes(attributes, count)
	values := unsafe.Slice((*C.GDALAttributeH)(unsafe.Pointer(attributes)), int(count))
	for _, attribute := range values {
		name := C.GoString(C.GDALAttributeGetName(attribute))
		if value := C.GDALAttributeReadAsString(attribute); value != nil {
			result[name] = C.GoString(value)
		}
	}
	return result
}

func readAttribute(array C.GDALMDArrayH, name string) string {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	attribute := C.GDALMDArrayGetAttribute(array, cName)
	if attribute == nil {
		return ""
	}
	defer C.GDALAttributeRelease(attribute)
	value := C.GDALAttributeReadAsString(attribute)
	if value == nil {
		return ""
	}
	return strings.TrimSpace(C.GoString(value))
}
