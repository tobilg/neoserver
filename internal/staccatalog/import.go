package staccatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"

	"github.com/tobilg/neoserver/internal/stacmodel"
)

const maxRecordBytes = 8 << 20

type recordReader struct {
	io.Reader
	remaining int64
}

func (r *recordReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, fmt.Errorf("metadata record exceeds 8 MiB")
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.Reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}

// Import streams Item/Collection JSON, FeatureCollections, and NDJSON into an
// unpublished generation. It never fetches links or extension schema URLs.
func (c *Catalog) Import(ctx context.Context, j *Job, reader io.Reader, baseURL string) error {
	limited := &recordReader{Reader: reader, remaining: maxRecordBytes}
	dec := json.NewDecoder(limited)
	dec.UseNumber()
	batch := make([]stacmodel.Document, 0, 200)
	flush := func() error {
		if err := c.Stage(ctx, j, batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	emit := func(d stacmodel.Document) error {
		if d.String("collection") == "" {
			d["collection"] = j.CollectionID
		}
		if err := resolveAssets(d, baseURL); err != nil {
			return err
		}
		// API publication owns the collection link; imported navigation is not used
		// to crawl remote catalogs or infer ownership.
		links, ok := d["links"].([]any)
		if !ok {
			links = []any{}
		}
		has := false
		for _, v := range links {
			if l, ok := v.(map[string]any); ok && l["rel"] == "collection" {
				has = true
			}
		}
		if !has {
			links = append(links, stacmodel.Document{"rel": "collection", "href": "https://neoserver.invalid/collections/" + url.PathEscape(j.CollectionID)})
		}
		d["links"] = links
		if err := stacmodel.Validate(d, "item"); err != nil {
			return fmt.Errorf("record %d: %w", j.Processed+int64(len(batch))+1, err)
		}
		batch = append(batch, d)
		if len(batch) == cap(batch) {
			return flush()
		}
		return nil
	}
	objects := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		limited.remaining = maxRecordBytes
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if token != json.Delim('{') {
			return fmt.Errorf("expected STAC object or NDJSON record")
		}
		d := stacmodel.Document{}
		featureCollection := false
		metadataBytes := int64(0)
		seen := map[string]bool{}
		for dec.More() {
			limited.remaining = maxRecordBytes
			fieldStart := dec.InputOffset()
			key, err := dec.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("expected object field")
			}
			if seen[name] {
				return fmt.Errorf("duplicate metadata field %q", name)
			}
			seen[name] = true
			if name == "features" {
				featureCollection = true
				open, err := dec.Token()
				if err != nil || open != json.Delim('[') {
					return fmt.Errorf("features must be an array")
				}
				for dec.More() {
					limited.remaining = maxRecordBytes
					var raw json.RawMessage
					if err = dec.Decode(&raw); err != nil {
						return err
					}
					if len(raw) > maxRecordBytes {
						return fmt.Errorf("metadata record exceeds 8 MiB")
					}
					item, err := stacmodel.Decode(raw)
					if err != nil {
						return err
					}
					if err = emit(item); err != nil {
						return err
					}
				}
				if _, err = dec.Token(); err != nil {
					return err
				}
			} else {
				var v any
				if err = dec.Decode(&v); err != nil {
					return err
				}
				metadataBytes += dec.InputOffset() - fieldStart
				if metadataBytes > maxRecordBytes {
					return fmt.Errorf("metadata record exceeds 8 MiB")
				}
				d[name] = v
			}
		}
		if _, err = dec.Token(); err != nil {
			return err
		}
		objects++
		if featureCollection {
			if d.String("type") != "FeatureCollection" {
				return fmt.Errorf("features require a FeatureCollection")
			}
		} else if d.String("type") == "Collection" {
			if d.String("id") != j.CollectionID {
				return fmt.Errorf("uploaded Collection ID does not match target")
			}
			if err = resolveAssets(d, baseURL); err != nil {
				return err
			}
			if err = stacmodel.Validate(d, "collection"); err != nil {
				return err
			}
			if j.Request == nil {
				j.Request = stacmodel.Document{}
			}
			j.Request["collection_document"] = d
		} else {
			if err = emit(d); err != nil {
				return err
			}
		}
	}
	if objects == 0 {
		return fmt.Errorf("empty metadata upload")
	}
	if err := flush(); err != nil {
		return err
	}
	_, err := c.db.ExecContext(ctx, "UPDATE stac_jobs SET request=?,status='ready',updated_at=current_timestamp WHERE workspace_id=? AND id=? AND status='uploading'", jsonString(j.Request), j.WorkspaceID, j.ID)
	return err
}
func resolveAssets(d stacmodel.Document, base string) error {
	for _, v := range d.Object("assets") {
		a, ok := v.(map[string]any)
		if !ok {
			continue
		}
		href, _ := a["href"].(string)
		u, err := url.Parse(href)
		if err != nil {
			return fmt.Errorf("invalid asset URL")
		}
		if !u.IsAbs() {
			b, err := url.Parse(base)
			if err != nil || b == nil || !b.IsAbs() {
				return fmt.Errorf("relative asset href requires an explicit base_url")
			}
			a["href"] = b.ResolveReference(u).String()
		}
	}
	return nil
}
func (c *Catalog) PreviewJob(ctx context.Context, ws, id string) ([]stacmodel.Document, error) {
	if _, err := c.GetJob(ctx, ws, id); err != nil {
		return nil, err
	}
	rows, err := c.read.QueryContext(ctx, "SELECT document FROM stac_items WHERE workspace_id=? AND generation=? ORDER BY collection_id,id LIMIT 10", ws, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []stacmodel.Document{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		d, err := stacmodel.Decode([]byte(raw))
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
