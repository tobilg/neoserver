package datasource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// DuckDBFeatureJSONExpression assembles already serialized JSON values without
// reparsing them. DuckDB's nested json_object calls convert DECIMAL values to
// doubles when they consume an intermediate JSON object. Each argument here is
// a trusted SQL expression, never a request value or an unquoted identifier.
func DuckDBFeatureJSONExpression(id, geometry, properties string) string {
	return fmt.Sprintf(`concat('{"type":"Feature","id":', coalesce(to_json(%s)::VARCHAR, 'null'), ',"geometry":', coalesce(ST_AsGeoJSON(%s)::VARCHAR, 'null'), ',"properties":', (%s)::VARCHAR, '}')`, id, geometry, properties)
}

// DecodeFeatureJSON preserves source number tokens, including nested values.
// Decoding into float64 first can irreversibly round identifiers and decimals.
func DecodeFeatureJSON(data []byte) (map[string]interface{}, error) {
	var feature map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&feature); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return nil, fmt.Errorf("feature contains trailing JSON data")
	}
	return feature, nil
}
