package conformance

import "testing"

func TestCatalogHasUniqueURIsAndIntegrationProfiles(t *testing.T) {
	seen := map[string]string{}
	for _, class := range All() {
		if class.Key == "" || class.URI == "" || class.IntegrationProfile == "" {
			t.Errorf("incomplete catalog class: %+v", class)
		}
		if previous := seen[class.URI]; previous != "" {
			t.Errorf("URI %q is assigned to both %q and %q", class.URI, previous, class.Key)
		}
		seen[class.URI] = class.Key
	}
}

func TestURIsRejectsUnknownClass(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected unknown class to panic")
		}
	}()
	_ = URIs("not-a-class")
}
