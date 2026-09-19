package cache

import (
	"net/http/httptest"
	"testing"
)

func TestRequestCachePolicy(t *testing.T) {
	for _, tc := range []struct {
		header      string
		read, write bool
	}{
		{"", true, true}, {"max-age=10", true, true}, {"no-cache", false, true},
		{"max-age=0", false, true}, {`public, MAX-AGE = "0"`, false, true},
		{"no-store", false, false}, {"no-cache, no-store", false, false},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Cache-Control", tc.header)
		read, write := RequestCachePolicy(r)
		if read != tc.read || write != tc.write {
			t.Fatalf("%q: got %v,%v", tc.header, read, write)
		}
	}
}
