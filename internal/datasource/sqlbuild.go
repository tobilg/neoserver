package datasource

import (
	"fmt"
	"strings"

	"github.com/tobilg/neoserver/internal/sqlutil"
)

// Clause builders shared by the PostGIS and DuckDB-family feature queries.
// Values are always bound; only quoted identifiers and SRID integers are
// interpolated.

// WhereClause joins predicates with AND, or returns "" when there are none.
func WhereClause(predicates []string) string {
	if len(predicates) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(predicates, " AND ")
}

// OrderByClause orders by the requested sort fields on alias, skipping fields
// that sortable rejects (nil accepts every field). Without requested fields it
// orders by idColumn, when there is one, so paging is stable.
func OrderByClause(alias string, sortBy []SortField, sortable func(string) bool, idColumn string) string {
	if len(sortBy) == 0 {
		if idColumn == "" {
			return ""
		}
		return fmt.Sprintf("ORDER BY %s.%s", alias, sqlutil.QuoteIdent(idColumn))
	}
	var items []string
	for _, field := range sortBy {
		if field.Name == "" || (sortable != nil && !sortable(field.Name)) {
			continue
		}
		direction := "ASC"
		if field.Desc {
			direction = "DESC"
		}
		items = append(items, fmt.Sprintf("%s.%s %s", alias, sqlutil.QuoteIdent(field.Name), direction))
	}
	if len(items) == 0 {
		return ""
	}
	return "ORDER BY " + strings.Join(items, ", ")
}

// LayerSortable accepts the layer's published columns and its ID column.
func LayerSortable(info *LayerInfo) func(string) bool {
	return func(name string) bool {
		_, ok := info.PGTypes[name]
		return ok || name == info.IDColumn
	}
}

// LimitOffsetClause binds limit and offset at argPos and argPos+1.
func LimitOffsetClause(args *[]any, argPos, limit, offset int) string {
	*args = append(*args, limit, offset)
	return fmt.Sprintf("LIMIT $%d OFFSET $%d", argPos, argPos+1)
}

// DuckDBBBoxPredicate intersects geomExpr with each part of bbox (two parts
// when it crosses the antimeridian), binding the coordinates from argPos. A
// bbox SRID of zero means EPSG:4326. The envelope is transformed into
// sourceSRID unless that is zero (unknown) or already the bbox CRS.
func DuckDBBBoxPredicate(geomExpr string, sourceSRID, bboxSRID, argPos int, bbox *BBox) (string, []any, int) {
	if bboxSRID == 0 {
		bboxSRID = 4326
	}
	var clauses []string
	var args []any
	for _, part := range bbox.Parts(bboxSRID) {
		envelope := fmt.Sprintf("ST_MakeEnvelope($%d, $%d, $%d, $%d)", argPos, argPos+1, argPos+2, argPos+3)
		if sourceSRID != 0 && sourceSRID != bboxSRID {
			envelope = fmt.Sprintf("ST_Transform(%s, 'EPSG:%d', 'EPSG:%d', always_xy := true)", envelope, bboxSRID, sourceSRID)
		}
		clauses = append(clauses, fmt.Sprintf("ST_Intersects(%s, %s)", geomExpr, envelope))
		args = append(args, part.MinX, part.MinY, part.MaxX, part.MaxY)
		argPos += 4
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args, argPos
}
