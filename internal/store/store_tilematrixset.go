package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type TileMatrixSetRecord struct {
	ID         string          `json:"id"`
	Definition json.RawMessage `json:"definition"`
	Revision   int64           `json:"revision"`
	Digest     string          `json:"digest"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type TileMatrixSetStore interface {
	ListTileMatrixSets(context.Context) ([]*TileMatrixSetRecord, error)
	GetTileMatrixSet(context.Context, string) (*TileMatrixSetRecord, error)
	UpsertTileMatrixSet(context.Context, string, json.RawMessage, string) (*TileMatrixSetRecord, error)
	DeleteTileMatrixSet(context.Context, string) error
}

func scanTileMatrixSet(row interface{ Scan(...any) error }) (*TileMatrixSetRecord, error) {
	var item TileMatrixSetRecord
	var definition any
	if err := row.Scan(&item.ID, &definition, &item.Revision, &item.Digest, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if err := decodeJSONValue(definition, &item.Definition); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *DuckDBStore) ListTileMatrixSets(ctx context.Context) ([]*TileMatrixSetRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,definition_json,revision,digest,created_at,updated_at FROM tile_matrix_sets ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list tile matrix sets: %w", err)
	}
	defer rows.Close()
	var result []*TileMatrixSetRecord
	for rows.Next() {
		item, scanErr := scanTileMatrixSet(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *DuckDBStore) GetTileMatrixSet(ctx context.Context, id string) (*TileMatrixSetRecord, error) {
	item, err := scanTileMatrixSet(s.db.QueryRowContext(ctx, `SELECT id,definition_json,revision,digest,created_at,updated_at FROM tile_matrix_sets WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return item, err
}

func (s *DuckDBStore) UpsertTileMatrixSet(ctx context.Context, id string, definition json.RawMessage, digest string) (*TileMatrixSetRecord, error) {
	now := time.Now().UTC()
	_, err := s.execCapabilitiesMutation(ctx, "", nil, `INSERT INTO tile_matrix_sets(id,definition_json,revision,digest,created_at,updated_at)
		VALUES(?,?,1,?,?,?) ON CONFLICT(id) DO UPDATE SET definition_json=excluded.definition_json,
		revision=tile_matrix_sets.revision+1,digest=excluded.digest,updated_at=excluded.updated_at`, id, string(definition), digest, now, now)
	if err != nil {
		return nil, fmt.Errorf("upsert tile matrix set: %w", err)
	}
	return s.GetTileMatrixSet(ctx, id)
}

func (s *DuckDBStore) DeleteTileMatrixSet(ctx context.Context, id string) error {
	result, err := s.execCapabilitiesMutation(ctx, "", nil, `DELETE FROM tile_matrix_sets WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete tile matrix set: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}
