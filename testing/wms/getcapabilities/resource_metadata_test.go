package getcapabilities

import "testing"

// TestLayerResourceMetadata validates the WMS 1.3.0 layer metadata links and
// authority-qualified identifier invariants when those optional elements are
// advertised.
func TestLayerResourceMetadata(t *testing.T) {
	ctx := getTestContext(t)
	for _, layer := range ctx.Layers {
		t.Run(layer.Name, func(t *testing.T) {
			for _, metadata := range layer.MetadataURL {
				if metadata.Format == "" || metadata.OnlineResource.Href == "" {
					t.Errorf("MetadataURL must contain Format and OnlineResource: %+v", metadata)
				}
			}
			for _, style := range layer.Style {
				if style.LegendURL != nil && (style.LegendURL.Format == "" || style.LegendURL.OnlineResource.Href == "") {
					t.Errorf("style %q has an incomplete LegendURL", style.Name)
				}
			}

			authorities := make(map[string]struct{}, len(layer.AuthorityURL))
			for _, authority := range layer.AuthorityURL {
				if authority.Name == "" || authority.OnlineResource.Href == "" {
					t.Errorf("incomplete AuthorityURL: %+v", authority)
					continue
				}
				if _, duplicate := authorities[authority.Name]; duplicate {
					t.Errorf("duplicate AuthorityURL name %q", authority.Name)
				}
				authorities[authority.Name] = struct{}{}
			}
			for _, identifier := range layer.Identifier {
				if identifier.Value == "" {
					t.Errorf("empty Identifier for authority %q", identifier.Authority)
				}
				if _, ok := authorities[identifier.Authority]; !ok {
					t.Errorf("Identifier authority %q has no matching AuthorityURL", identifier.Authority)
				}
			}
		})
	}
}
