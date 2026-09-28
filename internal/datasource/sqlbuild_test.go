package datasource

import (
	"strings"
	"testing"
)

func TestOrderByClause(t *testing.T) {
	info := &LayerInfo{IDColumn: "fid", PGTypes: map[string]string{"name": "text"}}
	for _, tc := range []struct {
		name   string
		sortBy []SortField
		want   string
	}{
		{"default id order", nil, `ORDER BY t."fid"`},
		{"requested fields", []SortField{{Name: "name", Desc: true}, {Name: "fid"}}, `ORDER BY t."name" DESC, t."fid" ASC`},
		{"unknown fields dropped without id fallback", []SortField{{Name: `x"; DROP TABLE t; --`}}, ``},
	} {
		if got := OrderByClause("t", tc.sortBy, LayerSortable(info), info.IDColumn); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := OrderByClause("v", []SortField{{Name: `a"b`}}, nil, ""); got != `ORDER BY v."a""b" ASC` {
		t.Errorf("unfiltered sort: %q", got)
	}
}

func TestWhereAndLimitClauses(t *testing.T) {
	if got := WhereClause(nil); got != "" {
		t.Errorf("empty where: %q", got)
	}
	if got := WhereClause([]string{"a", "b"}); got != "WHERE a AND b" {
		t.Errorf("where: %q", got)
	}
	args := []any{"x"}
	if got := LimitOffsetClause(&args, 2, 10, 20); got != "LIMIT $2 OFFSET $3" || len(args) != 3 || args[1] != 10 || args[2] != 20 {
		t.Errorf("limit: %q %v", got, args)
	}
}

func TestDuckDBBBoxPredicate(t *testing.T) {
	bbox := &BBox{MinX: 1, MinY: 2, MaxX: 3, MaxY: 4}
	predicate, args, next := DuckDBBBoxPredicate("t.geom", 0, 0, 3, bbox)
	if strings.Contains(predicate, "ST_Transform") || next != 7 || len(args) != 4 || !strings.Contains(predicate, "$3, $4, $5, $6") {
		t.Fatalf("unknown source SRID must not transform: %s %v %d", predicate, args, next)
	}
	predicate, _, _ = DuckDBBBoxPredicate("t.geom", 3857, 4326, 1, bbox)
	if !strings.Contains(predicate, "'EPSG:4326', 'EPSG:3857'") {
		t.Fatalf("expected a transform into the source CRS: %s", predicate)
	}
	// An antimeridian-crossing box becomes two bound envelopes.
	predicate, args, next = DuckDBBBoxPredicate("t.geom", 4326, 4326, 1, &BBox{MinX: 170, MinY: -10, MaxX: -170, MaxY: 10})
	if strings.Count(predicate, "ST_Intersects") != 2 || len(args) != 8 || next != 9 {
		t.Fatalf("antimeridian split: %s %v %d", predicate, args, next)
	}
}
