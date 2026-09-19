package datasource

import (
	"strings"
	"testing"
	"time"
)

func TestQueryParamsWithDateTimeFilter(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	params := QueryParams{
		Filter: `status = 'active'`,
		DateTime: &DateTimeFilter{
			SourceProperty: "valid_from", EndProperty: "valid_to", Start: &start, End: &end,
		},
	}
	combined := params.WithDateTimeFilter()
	for _, fragment := range []string{`status = 'active'`, `"valid_from" IS NULL`, `"valid_from" <= 2020-01-02T00:00:00Z`, `"valid_to" >= 2020-01-01T00:00:00Z`, `"valid_to" IS NULL`} {
		if !strings.Contains(combined.Filter, fragment) {
			t.Errorf("combined filter %q missing %q", combined.Filter, fragment)
		}
	}
	if params.Filter != `status = 'active'` {
		t.Fatal("WithDateTimeFilter must not mutate the original value")
	}
}

func TestDateTimeFilterInstantWithoutEndProperty(t *testing.T) {
	instant := time.Date(2020, 1, 1, 12, 30, 0, 0, time.FixedZone("offset", 2*60*60))
	filter := (&DateTimeFilter{SourceProperty: "observed", Start: &instant, End: &instant, Instant: true}).CQL2()
	if !strings.Contains(filter, `"observed" = 2020-01-01T10:30:00Z`) {
		t.Fatalf("unexpected instant filter: %s", filter)
	}
}

func TestStableSortAddsIDTieBreaker(t *testing.T) {
	input := []SortField{{Name: "name", Desc: true}}
	got := StableSort(input, "id")
	if len(got) != 2 || got[1].Name != "id" || got[1].Desc {
		t.Fatalf("stable sort = %+v", got)
	}
	if len(input) != 1 {
		t.Fatal("StableSort mutated its input")
	}
	if got = StableSort([]SortField{{Name: "id", Desc: true}}, "id"); len(got) != 1 {
		t.Fatalf("duplicate ID tie-breaker: %+v", got)
	}
}
