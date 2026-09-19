package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const layerGroupColumns = `id, workspace_id, public_id, title, description, enabled, public,
 allowed_roles, members, default_style, styles, native_extent, tile_cache_quota_bytes,
 tile_cache_generation, created_at, updated_at`

func scanLayerGroup(scanner interface{ Scan(...any) error }) (*LayerGroup, error) {
	var value LayerGroup
	var roles, members, styles, extent any
	err := scanner.Scan(&value.ID, &value.WorkspaceID, &value.PublicID, &value.Title, &value.Description,
		&value.Enabled, &value.Public, &roles, &members, &value.DefaultStyle, &styles, &extent,
		&value.TileCacheQuotaBytes, &value.TileCacheGeneration, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return nil, err
	}
	decodeJSONColumn(roles, &value.AllowedRoles)
	decodeJSONColumn(members, &value.Members)
	decodeJSONColumn(styles, &value.Styles)
	decodeJSONColumn(extent, &value.NativeExtent)
	return &value, nil
}

func (s *DuckDBStore) CreateLayerGroup(ctx context.Context, input CreateLayerGroupInput) (*LayerGroup, error) {
	s.roleMu.RLock()
	defer s.roleMu.RUnlock()
	if err := s.checkRoleAssignments(ctx, input.AllowedRoles); err != nil {
		return nil, err
	}
	id, now := uuid.NewString(), time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO layer_groups (`+layerGroupColumns+`)
	 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`, id, input.WorkspaceID, input.PublicID,
		input.Title, input.Description, input.Enabled, input.Public, marshalJSON(input.AllowedRoles, "[]"),
		marshalJSON(input.Members, "[]"), input.DefaultStyle, marshalJSON(input.Styles, "[]"),
		nullableJSON(input.NativeExtent), input.TileCacheQuotaBytes, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("create layer group: %w", err)
	}
	return &LayerGroup{ID: id, WorkspaceID: input.WorkspaceID, PublicID: input.PublicID, Title: input.Title,
		Description: input.Description, Enabled: input.Enabled, Public: input.Public, AllowedRoles: input.AllowedRoles,
		Members: input.Members, DefaultStyle: input.DefaultStyle, Styles: input.Styles, NativeExtent: input.NativeExtent,
		TileCacheQuotaBytes: input.TileCacheQuotaBytes, TileCacheGeneration: 1, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *DuckDBStore) GetLayerGroup(ctx context.Context, id string) (*LayerGroup, error) {
	value, err := scanLayerGroup(s.db.QueryRowContext(ctx, `SELECT `+layerGroupColumns+` FROM layer_groups WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return value, err
}

func (s *DuckDBStore) GetLayerGroupByPublicID(ctx context.Context, workspaceID, publicID string) (*LayerGroup, error) {
	value, err := scanLayerGroup(s.db.QueryRowContext(ctx, `SELECT `+layerGroupColumns+` FROM layer_groups WHERE workspace_id=? AND public_id=?`, workspaceID, publicID))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return value, err
}

func (s *DuckDBStore) ListLayerGroups(ctx context.Context, workspaceID string) ([]*LayerGroup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+layerGroupColumns+` FROM layer_groups WHERE workspace_id=? ORDER BY public_id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*LayerGroup
	for rows.Next() {
		value, err := scanLayerGroup(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *DuckDBStore) UpdateLayerGroup(ctx context.Context, id string, input UpdateLayerGroupInput) (*LayerGroup, error) {
	s.roleMu.RLock()
	defer s.roleMu.RUnlock()
	if input.AllowedRoles != nil {
		if err := s.checkRoleAssignments(ctx, *input.AllowedRoles); err != nil {
			return nil, err
		}
	}
	value, err := s.GetLayerGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.PublicID != nil {
		value.PublicID = *input.PublicID
	}
	if input.Title != nil {
		value.Title = *input.Title
	}
	if input.Description != nil {
		value.Description = *input.Description
	}
	if input.Enabled != nil {
		value.Enabled = *input.Enabled
	}
	if input.Public != nil {
		value.Public = *input.Public
	}
	if input.AllowedRoles != nil {
		value.AllowedRoles = *input.AllowedRoles
	}
	if input.Members != nil {
		value.Members = *input.Members
	}
	if input.DefaultStyle != nil {
		value.DefaultStyle = *input.DefaultStyle
	}
	if input.Styles != nil {
		value.Styles = *input.Styles
	}
	if input.NativeExtent != nil {
		value.NativeExtent = *input.NativeExtent
	}
	if input.TileCacheQuotaBytes != nil {
		value.TileCacheQuotaBytes = *input.TileCacheQuotaBytes
	}
	value.TileCacheGeneration++
	value.UpdatedAt = time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `UPDATE layer_groups SET public_id=?, title=?, description=?, enabled=?, public=?,
	 allowed_roles=?, members=?, default_style=?, styles=?, native_extent=?, tile_cache_quota_bytes=?,
	 tile_cache_generation=?, updated_at=? WHERE id=?`, value.PublicID, value.Title, value.Description, value.Enabled,
		value.Public, marshalJSON(value.AllowedRoles, "[]"), marshalJSON(value.Members, "[]"), value.DefaultStyle,
		marshalJSON(value.Styles, "[]"), nullableJSON(value.NativeExtent), value.TileCacheQuotaBytes,
		value.TileCacheGeneration, value.UpdatedAt, id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, ErrDuplicateKey
		}
		return nil, err
	}
	return value, nil
}

func (s *DuckDBStore) DeleteLayerGroup(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM layer_groups WHERE id=?`, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
