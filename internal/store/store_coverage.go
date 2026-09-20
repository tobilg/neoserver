package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func marshalJSON(value any, fallback string) string {
	b, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(b)
}

func decodeJSONColumn(value any, target any) {
	if value == nil {
		return
	}
	b, err := json.Marshal(value)
	if err == nil {
		_ = json.Unmarshal(b, target)
	}
}

func scanCoverage(scanner interface{ Scan(...any) error }) (*Coverage, error) {
	var coverage Coverage
	var roles, fields, dimensions, styles, nativeExtent any
	err := scanner.Scan(
		&coverage.ID, &coverage.WorkspaceID, &coverage.ServiceID, &coverage.SourceCoverage, &coverage.PublicID,
		&coverage.Title, &coverage.Description, &coverage.Enabled, &coverage.Public,
		&roles, &fields, &dimensions, &coverage.DefaultStyle, &styles, &coverage.Resampling, &coverage.WCS20CoverageSubtype, &nativeExtent,
		&coverage.TileCacheQuotaBytes, &coverage.TileCacheGeneration,
		&coverage.CreatedAt, &coverage.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	decodeJSONColumn(roles, &coverage.AllowedRoles)
	decodeJSONColumn(fields, &coverage.RangeFields)
	decodeJSONColumn(dimensions, &coverage.Dimensions)
	decodeJSONColumn(styles, &coverage.Styles)
	decodeJSONColumn(nativeExtent, &coverage.NativeExtent)
	return &coverage, nil
}

const coverageColumns = `id, workspace_id, service_id, source_coverage, public_id, title, description,
 enabled, public, allowed_roles, range_fields, dimensions, default_style, styles, resampling, wcs20_coverage_subtype, native_extent,
 tile_cache_quota_bytes, tile_cache_generation, created_at, updated_at`

func (s *DuckDBStore) CreateCoverage(ctx context.Context, input CreateCoverageInput) (*Coverage, error) {
	s.roleMu.RLock()
	defer s.roleMu.RUnlock()
	if err := s.checkRoleAssignments(ctx, input.AllowedRoles); err != nil {
		return nil, err
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	workspaceID := input.WorkspaceID
	if workspaceID == "" {
		if err := s.db.QueryRowContext(ctx, "SELECT workspace_id FROM services WHERE id = ?", input.ServiceID).Scan(&workspaceID); err != nil {
			if err == sql.ErrNoRows {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("resolve coverage workspace: %w", err)
		}
	}
	_, err := s.execCapabilitiesMutation(ctx, "SELECT workspace_id FROM services WHERE id = ?", input.ServiceID, `
		INSERT INTO coverages (`+coverageColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, id, workspaceID, input.ServiceID, input.SourceCoverage, input.PublicID, input.Title,
		input.Description, input.Enabled, input.Public,
		marshalJSON(input.AllowedRoles, "[]"), marshalJSON(input.RangeFields, "[]"),
		marshalJSON(input.Dimensions, "[]"), input.DefaultStyle, marshalJSON(input.Styles, "[]"), effectiveResampling(input.Resampling),
		effectiveWCS20CoverageSubtype(input.WCS20CoverageSubtype), nullableJSON(input.NativeExtent), input.TileCacheQuotaBytes, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") || strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("create coverage: %w", err)
	}
	return &Coverage{
		ID: id, WorkspaceID: workspaceID, ServiceID: input.ServiceID, SourceCoverage: input.SourceCoverage,
		PublicID: input.PublicID, Title: input.Title, Description: input.Description,
		Enabled: input.Enabled, Public: input.Public, AllowedRoles: input.AllowedRoles,
		RangeFields: input.RangeFields, Dimensions: input.Dimensions, CreatedAt: now, UpdatedAt: now,
		DefaultStyle: input.DefaultStyle, Styles: input.Styles, Resampling: effectiveResampling(input.Resampling),
		WCS20CoverageSubtype: effectiveWCS20CoverageSubtype(input.WCS20CoverageSubtype),
		NativeExtent:         input.NativeExtent, TileCacheQuotaBytes: input.TileCacheQuotaBytes, TileCacheGeneration: 1,
	}, nil
}

func (s *DuckDBStore) GetCoverage(ctx context.Context, id string) (*Coverage, error) {
	coverage, err := scanCoverage(s.db.QueryRowContext(ctx,
		`SELECT `+coverageColumns+` FROM coverages WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get coverage: %w", err)
	}
	return coverage, nil
}

func (s *DuckDBStore) GetCoverageByPublicID(ctx context.Context, workspaceID, publicID string) (*Coverage, error) {
	coverage, err := scanCoverage(s.db.QueryRowContext(ctx,
		`SELECT `+coverageColumns+` FROM coverages WHERE workspace_id = ? AND public_id = ?`, workspaceID, publicID))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get coverage by public id: %w", err)
	}
	return coverage, nil
}

func (s *DuckDBStore) ListCoverages(ctx context.Context, serviceID string) ([]*Coverage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+coverageColumns+` FROM coverages WHERE service_id = ? ORDER BY public_id`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("list coverages: %w", err)
	}
	defer rows.Close()
	var result []*Coverage
	for rows.Next() {
		coverage, err := scanCoverage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan coverage: %w", err)
		}
		result = append(result, coverage)
	}
	return result, rows.Err()
}

func (s *DuckDBStore) UpdateCoverage(ctx context.Context, id string, input UpdateCoverageInput) (*Coverage, error) {
	s.roleMu.RLock()
	defer s.roleMu.RUnlock()
	if err := s.checkRoleAssignments(ctx, input.AllowedRoles); err != nil {
		return nil, err
	}
	coverage, err := s.GetCoverage(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Title != nil {
		coverage.Title = *input.Title
	}
	if input.PublicID != nil {
		coverage.PublicID = *input.PublicID
	}
	if input.Description != nil {
		coverage.Description = *input.Description
	}
	if input.Enabled != nil {
		coverage.Enabled = *input.Enabled
	}
	if input.Public != nil {
		coverage.Public = *input.Public
	}
	if input.AllowedRoles != nil {
		coverage.AllowedRoles = input.AllowedRoles
	}
	if input.RangeFields != nil {
		coverage.RangeFields = input.RangeFields
	}
	if input.Dimensions != nil {
		coverage.Dimensions = input.Dimensions
	}
	if input.DefaultStyle != nil {
		coverage.DefaultStyle = *input.DefaultStyle
	}
	if input.Styles != nil {
		coverage.Styles = input.Styles
	}
	if input.Resampling != nil {
		coverage.Resampling = effectiveResampling(*input.Resampling)
	}
	if input.WCS20CoverageSubtype != nil {
		coverage.WCS20CoverageSubtype = effectiveWCS20CoverageSubtype(*input.WCS20CoverageSubtype)
	}
	if input.NativeExtent != nil {
		coverage.NativeExtent = input.NativeExtent
	}
	if input.MarkExtentStale != nil && *input.MarkExtentStale && coverage.NativeExtent != nil {
		coverage.NativeExtent.Stale = true
	}
	if input.TileCacheQuotaBytes != nil {
		coverage.TileCacheQuotaBytes = *input.TileCacheQuotaBytes
	}
	coverage.TileCacheGeneration++
	coverage.UpdatedAt = time.Now().UTC()
	_, err = s.execCapabilitiesMutation(ctx, "SELECT id FROM workspaces WHERE id = ?", coverage.WorkspaceID, `
		UPDATE coverages SET public_id=?, title=?, description=?, enabled=?, public=?, allowed_roles=?,
		 range_fields=?, dimensions=?, default_style=?, styles=?, resampling=?, wcs20_coverage_subtype=?, native_extent=?, tile_cache_quota_bytes=?,
		 tile_cache_generation=?, updated_at=? WHERE id=?
	`, coverage.PublicID, coverage.Title, coverage.Description, coverage.Enabled, coverage.Public,
		marshalJSON(coverage.AllowedRoles, "[]"), marshalJSON(coverage.RangeFields, "[]"),
		marshalJSON(coverage.Dimensions, "[]"), coverage.DefaultStyle, marshalJSON(coverage.Styles, "[]"), coverage.Resampling,
		coverage.WCS20CoverageSubtype, nullableJSON(coverage.NativeExtent), coverage.TileCacheQuotaBytes, coverage.TileCacheGeneration,
		coverage.UpdatedAt, coverage.ID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") || strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("update coverage: %w", err)
	}
	return coverage, nil
}

func effectiveResampling(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "nearest", "bilinear", "cubic":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "bilinear"
	}
}

func effectiveWCS20CoverageSubtype(value string) string {
	if value == WCS20CoverageSubtypeGrid {
		return WCS20CoverageSubtypeGrid
	}
	return WCS20CoverageSubtypeRectifiedGrid
}

func (s *DuckDBStore) DeleteCoverage(ctx context.Context, id string) error {
	result, err := s.execCapabilitiesMutation(ctx, "SELECT workspace_id FROM coverages WHERE id = ?", id, "DELETE FROM coverages WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete coverage: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *DuckDBStore) GetWCSSettings(ctx context.Context, workspaceID string) (*WCSSettings, error) {
	var value any
	err := s.db.QueryRowContext(ctx, "SELECT wcs_settings FROM workspaces WHERE id = ?", workspaceID).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get WCS settings: %w", err)
	}
	var settings WCSSettings
	decodeJSONColumn(value, &settings)
	return &settings, nil
}

func (s *DuckDBStore) UpdateWCSSettings(ctx context.Context, workspaceID string, settings WCSSettings) error {
	value, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal WCS settings: %w", err)
	}
	result, err := s.db.ExecContext(ctx,
		"UPDATE workspaces SET wcs_settings = ?, updated_at = ? WHERE id = ?",
		value, time.Now().UTC(), workspaceID)
	if err != nil {
		return fmt.Errorf("update WCS settings: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
