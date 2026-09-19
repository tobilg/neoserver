package mgmt

import "testing"

func TestOpenAPILayerUpdateStyleBindings(t *testing.T) {
	doc := buildOpenAPI(testConfig())
	operation := doc.Paths.Value("/workspaces/{workspace}/services/{service}/layers/{layer}").Put
	properties := operation.RequestBody.Value.Content["application/json"].Schema.Value.Properties
	if properties["default_style"] == nil || !properties["default_style"].Value.Type.Is("string") {
		t.Fatal("layer updates must expose the default_style accepted by the handler")
	}
	if properties["styles"] == nil || !properties["styles"].Value.Type.Is("array") {
		t.Fatal("layer updates must expose advertised styles")
	}
}
