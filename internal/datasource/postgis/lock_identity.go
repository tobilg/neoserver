package postgis

import "fmt"

// Services connected to the same configured endpoint/database/table coordinate
// locks even when credentials or publication names differ. DNS aliases for a
// database must use the same configured endpoint to share this identity.
func (ds *DataSource) FeatureLockSource(layer string) string {
	c := ds.pool.Config().ConnConfig
	schema, table := parseLayerName(layer)
	if schema == "" {
		schema = "public"
		if len(ds.schemas) > 0 {
			schema = ds.schemas[0]
		}
	}
	layer = schema + "." + table
	return fmt.Sprintf("postgis/%s/%d/%s/%s", c.Host, c.Port, c.Database, layer)
}
