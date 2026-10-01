package staccatalog

import (
	"context"
	"fmt"
	"github.com/tobilg/neoserver/internal/stacmodel"
	"strings"
)

// BindLocalAsset is called only after authorizing the whole file for the target
// publication. Paths remain private management metadata in the encrypted store.
func (c *Catalog) BindLocalAsset(ctx context.Context, ws, generation string, a LocalAsset) error {
	if a.Key == "" || strings.ContainsAny(a.Key, "/\\\x00") || a.Key == "." || a.Key == ".." {
		return fmt.Errorf("asset key must be a URL path segment")
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM stac_collections WHERE workspace_id=? AND id=?", ws, a.CollectionID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrNotFound
	}
	if generation != "" {
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM stac_jobs WHERE workspace_id=? AND id=? AND status='running'", ws, generation).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrConflict
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO stac_local_assets VALUES(?,?,?,?,?,?,?) ON CONFLICT(workspace_id,collection_id,item_id,asset_key,generation) DO UPDATE SET path=excluded.path,media_type=excluded.media_type`, ws, a.CollectionID, a.ItemID, a.Key, a.Path, a.MediaType, generation)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (c *Catalog) LocalAssets(ctx context.Context, ws, collection, item string) ([]LocalAsset, error) {
	rows, err := c.read.QueryContext(ctx, `SELECT a.asset_key,a.path,a.media_type FROM stac_local_assets a JOIN stac_collections c ON c.workspace_id=a.workspace_id AND c.id=a.collection_id WHERE a.workspace_id=? AND a.collection_id=? AND a.item_id=? AND (a.generation='' OR a.generation=c.generation) ORDER BY a.asset_key`, ws, collection, item)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LocalAsset{}
	for rows.Next() {
		a := LocalAsset{CollectionID: collection, ItemID: item}
		if err = rows.Scan(&a.Key, &a.Path, &a.MediaType); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (c *Catalog) LocalAsset(ctx context.Context, ws, collection, item, key string) (*LocalAsset, error) {
	assets, err := c.LocalAssets(ctx, ws, collection, item)
	if err != nil {
		return nil, err
	}
	for _, a := range assets {
		if a.Key == key {
			return &a, nil
		}
	}
	return nil, ErrNotFound
}
func (c *Catalog) DeleteLocalAsset(ctx context.Context, ws, collection, item, key string) error {
	_, err := c.db.ExecContext(ctx, "DELETE FROM stac_local_assets WHERE workspace_id=? AND collection_id=? AND item_id=? AND asset_key=? AND generation=''", ws, collection, item, key)
	return err
}

func (c *Catalog) DeleteWorkspace(ctx context.Context, ws string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"stac_items", "stac_jobs", "stac_local_assets", "stac_item_overrides", "stac_collections"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE workspace_id=?", ws); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (c *Catalog) WorkspaceIDs(ctx context.Context) ([]string, error) {
	rows, err := c.read.QueryContext(ctx, "SELECT DISTINCT workspace_id FROM stac_collections")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var ws string
		if err = rows.Scan(&ws); err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}
func (c *Catalog) Cancel(ctx context.Context, ws, id string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, err := scanJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM stac_jobs WHERE workspace_id=? AND id=?", ws, id))
	if err != nil {
		return err
	}
	if j.Status == "succeeded" {
		return fmt.Errorf("published jobs cannot be cancelled")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE stac_jobs SET status='cancelled',updated_at=current_timestamp WHERE workspace_id=? AND id=?", ws, id); err != nil {
		return err
	}
	for _, table := range []string{"stac_items", "stac_local_assets"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE workspace_id=? AND generation=?", ws, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (c *Catalog) Retry(ctx context.Context, ws, id string) (*Job, error) {
	j, err := c.GetJob(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	if j.Status != "failed" {
		return nil, fmt.Errorf("only failed jobs can be retried")
	}
	if j.Kind == "import" {
		return nil, fmt.Errorf("retry the metadata upload; staged validation failures cannot be published")
	}
	return c.CreateJob(ctx, ws, j.CollectionID, j.Kind, "queued", j.Request)
}

// LocalAssetsForDocuments fetches an entire response page with one query.
func (c *Catalog) LocalAssetsForDocuments(ctx context.Context, ws string, docs []stacmodel.Document) (map[string][]LocalAsset, error) {
	out := map[string][]LocalAsset{}
	if len(docs) == 0 {
		return out, nil
	}
	args := []any{ws}
	pairs := make([]string, 0, len(docs))
	for _, d := range docs {
		collection, item := d.String("id"), ""
		if d.String("type") == "Feature" {
			collection, item = d.String("collection"), d.String("id")
		}
		pairs = append(pairs, "(?,?)")
		args = append(args, collection, item)
	}
	rows, err := c.read.QueryContext(ctx, `SELECT a.collection_id,a.item_id,a.asset_key,a.path,a.media_type FROM stac_local_assets a JOIN stac_collections c ON c.workspace_id=a.workspace_id AND c.id=a.collection_id WHERE a.workspace_id=? AND (a.collection_id,a.item_id) IN (`+strings.Join(pairs, ",")+`) AND (a.generation='' OR a.generation=c.generation) ORDER BY a.generation DESC,a.asset_key`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a LocalAsset
		if err = rows.Scan(&a.CollectionID, &a.ItemID, &a.Key, &a.Path, &a.MediaType); err != nil {
			return nil, err
		}
		key := a.CollectionID + "\x00" + a.ItemID
		out[key] = append(out[key], a)
	}
	return out, rows.Err()
}
