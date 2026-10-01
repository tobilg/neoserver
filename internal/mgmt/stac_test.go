package mgmt

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSTACJSONRejectsOversizeTrailingData(t *testing.T) {
	// A limited reader must not turn an oversized body into a valid object
	// merely because the first object fits before its artificial EOF.
	for _, suffix := range []string{" ", `{}`} {
		body := `{}` + strings.Repeat(" ", (8<<20)-2) + suffix
		r := httptest.NewRequest("PUT", "/settings/stac", strings.NewReader(body))
		var input map[string]any
		if err := readSTACJSON(r, &input); err == nil {
			t.Fatal("oversized JSON body accepted")
		}
	}
}
