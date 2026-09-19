package datasource

import "testing"

func TestStableRenderFeatureID(t *testing.T) {
	if got := StableRenderFeatureID(int64(42), nil, nil); got != "42" {
		t.Fatalf("source ID = %q, want 42", got)
	}

	geometry := []byte{1, 2, 3}
	first := StableRenderFeatureID(nil, geometry, map[string]any{"name": "Main", "lanes": 2})
	second := StableRenderFeatureID(nil, geometry, map[string]any{"lanes": 2, "name": "Main"})
	if first == "" || first != second {
		t.Fatalf("fallback IDs are not stable: %q != %q", first, second)
	}
	if changed := StableRenderFeatureID(nil, geometry, map[string]any{"name": "Other", "lanes": 2}); changed == first {
		t.Fatalf("different feature content reused fallback ID %q", first)
	}
}
