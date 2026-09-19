package vectorfile

import (
	"fmt"
	"sort"
	"strings"
)

// ReadPolicySQL restricts the actual GDAL open, including files disguised with
// another extension. Never pass an empty driver list (GDAL treats it as all).
// Indirect and XML/schema-fetching formats must be converted by a trusted
// operator before ingestion. Keep import conversion and discovery on this policy.
const ReadPolicySQL = ", allowed_drivers=['GeoJSON','GPKG','ESRI Shapefile','FlatGeobuf']"

// readOptions excludes options which can name another file, schema or endpoint.
// Values are SQL literals, not fragments, and GDAL expects KEY=VALUE strings.
func readOptions(options map[string]string) (string, error) {
	allowed := map[string]bool{
		"FLATTEN_NESTED_ATTRIBUTES": true, "NESTED_ATTRIBUTE_SEPARATOR": true,
		"ARRAY_AS_STRING": true, "DATE_AS_STRING": true,
		"ADJUST_GEOM_TYPE": true, "ADJUST_TYPE": true,
		"LIST_ALL_TABLES": true,
	}
	values := make([]string, 0, len(options))
	for key, value := range options {
		if !allowed[key] {
			return "", fmt.Errorf("GDAL open option %q is not supported for untrusted files", key)
		}
		values = append(values, quoteLiteral(key+"="+value))
	}
	sort.Strings(values)
	if len(values) == 0 {
		return "", nil
	}
	return ", open_options=[" + strings.Join(values, ",") + "]", nil
}
