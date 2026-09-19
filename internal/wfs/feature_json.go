package wfs

import "github.com/tobilg/neoserver/internal/datasource"

// Preserve source scalar tokens: float64 decoding can merge distinct feature
// IDs and round integer/decimal properties before we even write the response.
// GML coordinate writers accept json.Number through their existing %v format.
func decodeFeatureJSON(data []byte) (map[string]interface{}, error) {
	return datasource.DecodeFeatureJSON(data)
}
