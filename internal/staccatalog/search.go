package staccatalog

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tobilg/neoserver/internal/stacmodel"
)

type cursor struct{ Workspace, Query, Collection, ID string }

func (c *Catalog) cursor(ws string, q Search, collection, id string) string {
	q.Token = ""
	hash := sha256.Sum256([]byte(jsonString(q)))
	raw, _ := json.Marshal(cursor{ws, fmt.Sprintf("%x", hash), collection, id})
	mac := hmac.New(sha256.New, c.key)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(append(mac.Sum(nil), raw...))
}
func (c *Catalog) decodeCursor(ws string, q Search) (cursor, error) {
	var value cursor
	raw, err := base64.RawURLEncoding.DecodeString(q.Token)
	if err != nil || len(raw) < 33 {
		return value, fmt.Errorf("invalid pagination token")
	}
	mac := hmac.New(sha256.New, c.key)
	mac.Write(raw[32:])
	if !hmac.Equal(raw[:32], mac.Sum(nil)) {
		return value, fmt.Errorf("invalid pagination token")
	}
	if err = json.Unmarshal(raw[32:], &value); err != nil {
		return value, fmt.Errorf("invalid pagination token")
	}
	q.Token = ""
	hash := sha256.Sum256([]byte(jsonString(q)))
	if value.Workspace != ws || value.Query != fmt.Sprintf("%x", hash) {
		return value, fmt.Errorf("pagination token belongs to another workspace or query")
	}
	return value, nil
}
func (q *Search) Validate() error {
	if q.Limit == 0 {
		q.Limit = 100
	}
	if q.Limit < 0 {
		return fmt.Errorf("limit must be positive")
	}
	if q.Limit > 1000 {
		q.Limit = 1000
	}
	if len(q.BBox) > 0 {
		if q.Intersects != nil {
			return fmt.Errorf("bbox and intersects are mutually exclusive")
		}
		if err := stacmodel.ValidateBBox(q.BBox); err != nil {
			return err
		}
	}
	if q.Intersects != nil {
		if _, err := stacmodel.GeometryBounds(q.Intersects); err != nil {
			return err
		}
	}
	if _, _, err := stacmodel.Interval(q.Datetime); err != nil {
		return err
	}
	if len(q.IDs) > 1000 || len(q.Collections) > 1000 {
		return fmt.Errorf("at most 1000 ids or collections may be specified")
	}
	for _, list := range [][]string{q.IDs, q.Collections} {
		for _, id := range list {
			if id == "" {
				return fmt.Errorf("empty identifier")
			}
		}
	}
	return nil
}
func (c *Catalog) Search(ctx context.Context, ws string, q Search, visible []string) (Page, error) {
	page := Page{Items: []stacmodel.Document{}}
	if err := q.Validate(); err != nil {
		return page, err
	}
	var after cursor
	if q.Token != "" {
		var err error
		after, err = c.decodeCursor(ws, q)
		if err != nil {
			return page, err
		}
	}
	if len(visible) == 0 {
		return page, nil
	}
	sql, args := searchStatement(ws, q, visible, after)
	rows, err := c.read.QueryContext(ctx, sql, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	var lastCollection, lastID string
	for rows.Next() {
		var raw, collection, id string
		if err = rows.Scan(&raw, &collection, &id); err != nil {
			return page, err
		}
		if len(page.Items) == q.Limit {
			page.Next = c.cursor(ws, q, lastCollection, lastID)
			break
		}
		doc, err := stacmodel.Decode([]byte(raw))
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, doc)
		lastCollection, lastID = collection, id
	}
	return page, rows.Err()
}
func bboxWKT(b [4]float64) string {
	if b[0] == b[2] && b[1] == b[3] {
		return fmt.Sprintf("POINT (%g %g)", b[0], b[1])
	}
	if b[0] == b[2] || b[1] == b[3] {
		return fmt.Sprintf("LINESTRING (%g %g,%g %g)", b[0], b[1], b[2], b[3])
	}
	return fmt.Sprintf("POLYGON ((%g %g,%g %g,%g %g,%g %g,%g %g))", b[0], b[1], b[2], b[1], b[2], b[3], b[0], b[3], b[0], b[1])
}

func searchStatement(ws string, q Search, visible []string, after cursor) (string, []any) {
	where := []string{"i.workspace_id=?"}
	args := []any{ws}
	addIn := func(col string, values []string) {
		if len(values) == 0 {
			return
		}
		where = append(where, col+" IN ("+strings.TrimRight(strings.Repeat("?,", len(values)), ",")+")")
		for _, v := range values {
			args = append(args, v)
		}
	}
	addIn("i.collection_id", visible)
	addIn("i.collection_id", q.Collections)
	addIn("i.id", q.IDs)
	if after.ID != "" {
		where = append(where, "(i.collection_id,i.id) > (?,?)")
		args = append(args, after.Collection, after.ID)
	}
	start, end, _ := stacmodel.Interval(q.Datetime)
	if start != "" {
		where = append(where, "i.end_time>=?")
		args = append(args, start)
	}
	if end != "" {
		where = append(where, "i.start_time<=?")
		args = append(args, end)
	}
	// A bbox also matches Items without a geometry (OGC Features
	// /req/core/fc-bbox-response). Keep that branch separate so the geometry
	// branch can still use the R-tree rather than an OR forcing a table scan.
	nonSpatialWhere := strings.Join(where, " AND ")
	nonSpatialArgs := append([]any(nil), args...)
	if q.Intersects != nil {
		where = append(where, "ST_Intersects(i.geom, ST_GeomFromGeoJSON(CAST(? AS VARCHAR)))")
		args = append(args, jsonString(q.Intersects))
	}
	if len(q.BBox) > 0 {
		b := q.BBox
		n := len(b) / 2
		minx, maxx := b[0], b[n]
		boxes := [][4]float64{{minx, b[1], maxx, b[n+1]}}
		if minx > maxx {
			boxes = [][4]float64{{minx, b[1], 180, b[n+1]}, {-180, b[1], maxx, b[n+1]}}
		}
		parts := []string{}
		for _, b := range boxes {
			parts = append(parts, "ST_Intersects(i.geom,ST_GeomFromText(CAST(? AS VARCHAR)))")
			args = append(args, bboxWKT(b))
		}
		where = append(where, "("+strings.Join(parts, " OR ")+")")
		if n == 3 {
			where = append(where, "i.max_z>=? AND i.min_z<=?")
			args = append(args, q.BBox[2], q.BBox[5])
		}
	}
	selectItems := `SELECT json_merge_patch(i.document,coalesce(o.patch,'{}'))::VARCHAR AS document,i.collection_id,i.id FROM stac_items i LEFT JOIN stac_item_overrides o ON o.workspace_id=i.workspace_id AND o.collection_id=i.collection_id AND o.item_id=i.id JOIN stac_collections c ON c.workspace_id=i.workspace_id AND c.id=i.collection_id AND c.generation=i.generation WHERE `
	sql := selectItems + strings.Join(where, " AND ")
	if len(q.BBox) > 0 {
		sql += " UNION ALL " + selectItems + nonSpatialWhere + " AND i.geom IS NULL"
		args = append(args, nonSpatialArgs...)
	}
	sql += " ORDER BY i.collection_id,i.id LIMIT ?"
	args = append(args, q.Limit+1)

	return sql, args
}
