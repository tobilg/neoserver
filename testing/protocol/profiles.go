package protocol

// Execution is one failure-independent live protocol test invocation.
type Execution struct {
	ID          string
	Packages    []string
	Environment map[string]string
	Profiles    []string
}

// Executions returns the complete protocol integration plan. Environment
// values are paths relative to the runner's base URL.
func Executions() []Execution {
	return []Execution{
		{
			ID: "ogcapi-features", Packages: []string{"./testing/ogcapi/..."},
			Environment: map[string]string{"OGC_TEST_URL": "/workspaces/demo/ogc", "OGC_INTEGRATION_TEST": "true"},
			Profiles:    []string{"ogcapi-features/core", "ogcapi-features/crs", "ogcapi-features/filtering"},
		},
		{
			ID: "wms", Packages: []string{"./testing/wms/..."},
			Environment: map[string]string{"WMS_TEST_URL": "/workspaces/demo/wms"},
		},
		{
			ID: "wfs", Packages: []string{"./testing/wfs/..."},
			Environment: map[string]string{"WFS_TEST_URL": "/workspaces/demo/wfs"},
		},
		{
			ID: "wcs", Packages: []string{"./testing/wcs"},
			Environment: map[string]string{"WCS_TEST_URL": "/workspaces/demo/wcs"},
			Profiles:    []string{"wcs20/core", "wcs/extensions", "wcs21/core", "wcs21/kvp", "wcs21/extensions"},
		},
		{
			ID: "ogcapi-tiles", Packages: []string{"./testing/ogcapitiles"},
			Environment: map[string]string{"OGC_TILES_TEST_URL": "/workspaces/demo/ogc-tiles"},
			Profiles:    []string{"ogcapi-tiles/core", "ogcapi-tiles/tilesets", "ogcapi-tiles/formats", "ogcapi-tiles/dataset-tilesets"},
		},
	}
}
