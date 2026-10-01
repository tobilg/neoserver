package staccatalog

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"reflect"
)

// Item overrides contain descriptive/custom properties only. Identity,
// footprint, time and assets remain source-owned across every refresh.
func (c *Catalog) overrideItem(ctx context.Context, ws, collection string, d stacmodel.Document) error {
	old, err := c.GetItem(ctx, ws, collection, d.String("id"))
	if err != nil {
		return err
	}
	if err = stacmodel.Validate(d, "item"); err != nil {
		return err
	}
	for _, key := range []string{"type", "id", "collection", "stac_version", "stac_extensions", "geometry", "bbox", "assets", "links"} {
		if !reflect.DeepEqual(d[key], old[key]) {
			return fmt.Errorf("linked Item field %s is owned by its source", key)
		}
	}
	previous, next := old.Object("properties"), d.Object("properties")
	owned := map[string]bool{"datetime": true, "start_datetime": true, "end_datetime": true}
	for key := range owned {
		if !reflect.DeepEqual(previous[key], next[key]) {
			return fmt.Errorf("linked Item time is owned by its source")
		}
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM stac_collections WHERE workspace_id=? AND id=?", ws, collection).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	var raw string
	patch := stacmodel.Document{}
	err = tx.QueryRowContext(ctx, "SELECT patch FROM stac_item_overrides WHERE workspace_id=? AND collection_id=? AND item_id=?", ws, collection, d.String("id")).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		existing, decodeErr := stacmodel.Decode([]byte(raw))
		if decodeErr != nil {
			return decodeErr
		}
		if props := existing.Object("properties"); props != nil {
			patch = props
		}
	}
	// Persist only deliberate differences. Unedited mapped properties keep following
	// their source, and earlier user overrides survive subsequent edits.
	for key, value := range previous {
		if !owned[key] && !reflect.DeepEqual(value, next[key]) {
			patch[key] = next[key]
		}
	}
	for key, value := range next {
		if !owned[key] && !reflect.DeepEqual(value, previous[key]) {
			patch[key] = value
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO stac_item_overrides VALUES(?,?,?,?) ON CONFLICT(workspace_id,collection_id,item_id) DO UPDATE SET patch=excluded.patch`, ws, collection, d.String("id"), jsonString(stacmodel.Document{"properties": patch}))
	if err != nil {
		return err
	}
	return tx.Commit()
}
