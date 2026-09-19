package postgis

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tobilg/neoserver/internal/datasource"
)

type contractPredicate struct {
	t     *testing.T
	alias string
}

func (p contractPredicate) CompilePredicate(o datasource.PredicateOptions) (string, []any, int, error) {
	if o.Dialect != datasource.SQLPostGIS || o.TableAlias != p.alias || !strings.Contains(o.GeometryExpression, `"geom"`) {
		p.t.Fatalf("wrong adapter context: %+v", o)
	}
	if o.StartParamIndex <= 1 {
		p.t.Fatal("bbox/ID parameter offset was lost")
	}
	return fmt.Sprintf(`%s."name" = $%d`, o.TableAlias, o.StartParamIndex), []any{"sentinel"}, o.StartParamIndex + 1, nil
}

func TestPostGISPredicateContractAcrossQueryBuilders(t *testing.T) {
	ds := &DataSource{}
	info := &datasource.LayerInfo{Name: "public.points", Schema: "public", GeometryColumn: "geom", IDColumn: "id", SRID: 4326}
	view := &datasource.SQLViewConfig{SQL: "SELECT id, name, geom FROM points", GeometryColumn: "geom", IDColumn: "id", SRID: 4326}
	for name, builder := range map[string]func(datasource.QueryParams) (string, []any, error){
		"list":        func(p datasource.QueryParams) (string, []any, error) { return ds.buildListSQL(info, p) },
		"count":       func(p datasource.QueryParams) (string, []any, error) { return ds.buildCountSQL(info, p) },
		"render":      func(p datasource.QueryParams) (string, []any, error) { return ds.buildWKBSQL(info, p) },
		"view/list":   func(p datasource.QueryParams) (string, []any, error) { return ds.buildSQLViewListSQL(view, p) },
		"view/count":  func(p datasource.QueryParams) (string, []any, error) { return ds.buildSQLViewCountSQL(view, p) },
		"view/render": func(p datasource.QueryParams) (string, []any, error) { return ds.buildSQLViewWKBSQL(view, p) },
	} {
		t.Run(name, func(t *testing.T) {
			alias := "t"
			p := datasource.QueryParams{BBox: &datasource.BBox{MinX: 6, MinY: 50, MaxX: 8, MaxY: 52}, BBoxSRID: 4326, Limit: 10}
			if strings.HasPrefix(name, "view/") {
				alias = "v"
				p.FeatureIDs = []string{"1"}
			}
			p.Predicate = contractPredicate{t, alias}
			sql, args, err := builder(p)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for i, arg := range args {
				if arg == "sentinel" {
					found = true
					if !strings.Contains(sql, fmt.Sprintf(`%s."name" = $%d`, alias, i+1)) {
						t.Fatalf("misbound predicate: %s, %v", sql, args)
					}
				}
			}
			if !found {
				t.Fatal("predicate dropped")
			}
		})
	}
}
