package stac

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/duckdb/duckdb-go/v2"
	"github.com/tobilg/neoserver/internal/staccatalog"
)

func TestDecodeSearchRejectsMalformedParameters(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{"limit":0}`, `{"limit":null}`, `{"limit":1.5}`, `{"bbox":[]}`, `{"bbox":[0,0,1]}`, `{"bbox":[0,0,1,1],"intersects":{"type":"Point","coordinates":[0,0]}}`, `{"datetime":""}`, `{"ids":[]}`, `{"collections":null}`, `{"intersects":null}`, `{} {}`} {
		t.Run(body, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/search", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			if _, err := DecodeSearch(r); err == nil {
				t.Fatal("malformed search accepted")
			}
		})
	}
	for _, query := range []string{"limit=0", "limit=1&limit=2", "datetime=", "ids=", "bbox=0,0,NaN,1"} {
		r := httptest.NewRequest("GET", "/search?"+query, nil)
		if _, err := DecodeSearch(r); err == nil {
			t.Fatalf("malformed GET accepted: %s", query)
		}
	}
	r := httptest.NewRequest("POST", "/search", strings.NewReader(`{"limit":10000,"bbox":[170,-1,10,-170,1,20],"datetime":"2026-01-01T00:00:00.123456789Z/.."}`))
	r.Header.Set("Content-Type", "application/json")
	q, err := DecodeSearch(r)
	if err != nil || q.Limit != 1000 || len(q.BBox) != 6 {
		t.Fatalf("valid query rejected: %+v %v", q, err)
	}
}

func TestErrorStatusSeparatesClientAndServerFailures(t *testing.T) {
	r := httptest.NewRequest("GET", "/search", nil)
	for _, test := range []struct {
		err     error
		status  int
		message string
	}{
		{staccatalog.ErrNotFound, 404, staccatalog.ErrNotFound.Error()},
		{fmt.Errorf("wrapped: %w", staccatalog.ErrConflict), 409, "wrapped: " + staccatalog.ErrConflict.Error()},
		{errors.New("bbox must contain four or six coordinates"), 400, "bbox must contain four or six coordinates"},
		{fmt.Errorf("stage: %w", &duckdb.Error{Type: duckdb.ErrorTypeConstraint, Msg: "Constraint Error: duplicate key"}), 400, "the request was rejected as invalid"},
		{&duckdb.Error{Type: duckdb.ErrorTypeIO, Msg: "IO Error: disk full"}, 500, "fallback"},
		{fmt.Errorf("scan: %w", context.DeadlineExceeded), 500, "fallback"},
	} {
		status, message := Message(r, test.err, "fallback")
		if status != test.status || message != test.message {
			t.Errorf("%v: got %d %q, want %d %q", test.err, status, message, test.status, test.message)
		}
	}
}
