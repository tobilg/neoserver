package mgmt

import (
	"testing"

	"github.com/tobilg/neoserver/internal/store"
)

func TestValidateWCS20CoverageSubtype(t *testing.T) {
	for _, test := range []struct {
		name       string
		value      string
		allowEmpty bool
		wantError  bool
	}{
		{name: "create default", allowEmpty: true},
		{name: "rectified", value: store.WCS20CoverageSubtypeRectifiedGrid},
		{name: "grid", value: store.WCS20CoverageSubtypeGrid},
		{name: "update empty", allowEmpty: false, wantError: true},
		{name: "wrong case", value: "gridcoverage", wantError: true},
		{name: "unsupported", value: "ReferenceableGridCoverage", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateWCS20CoverageSubtype(test.value, test.allowEmpty)
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v wantError=%v", err, test.wantError)
			}
		})
	}
}
