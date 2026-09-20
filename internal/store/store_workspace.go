package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Workspace operations

func (s *DuckDBStore) CreateWorkspace(ctx context.Context, input CreateWorkspaceInput) (*Workspace, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workspaces (id, name, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, id, input.Name, input.Description, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "Duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	return &Workspace{
		ID:          id,
		Name:        input.Name,
		Description: input.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (s *DuckDBStore) GetWorkspace(ctx context.Context, id string) (*Workspace, error) {
	var ws Workspace
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM workspaces w WHERE id = ?
		AND NOT EXISTS (SELECT 1 FROM catalog_deletions d WHERE d.scope_kind='workspace'
			AND d.target_id=w.id AND d.status IN ('pending','running','failed'))
	`, id).Scan(&ws.ID, &ws.Name, &ws.Description, &ws.CreatedAt, &ws.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}
	return &ws, nil
}

func (s *DuckDBStore) GetWorkspaceByName(ctx context.Context, name string) (*Workspace, error) {
	var ws Workspace
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM workspaces w WHERE name = ?
		AND NOT EXISTS (SELECT 1 FROM catalog_deletions d WHERE d.scope_kind='workspace'
			AND d.target_id=w.id AND d.status IN ('pending','running','failed'))
	`, name).Scan(&ws.ID, &ws.Name, &ws.Description, &ws.CreatedAt, &ws.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}
	return &ws, nil
}

func (s *DuckDBStore) ListWorkspaces(ctx context.Context) ([]*Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM workspaces w
		WHERE NOT EXISTS (SELECT 1 FROM catalog_deletions d WHERE d.scope_kind='workspace'
			AND d.target_id=w.id AND d.status IN ('pending','running','failed'))
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []*Workspace
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.Description, &ws.CreatedAt, &ws.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan workspace: %w", err)
		}
		workspaces = append(workspaces, &ws)
	}
	return workspaces, rows.Err()
}

func (s *DuckDBStore) UpdateWorkspace(ctx context.Context, id string, input UpdateWorkspaceInput) (*Workspace, error) {
	ws, err := s.GetWorkspace(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		ws.Name = *input.Name
	}
	if input.Description != nil {
		ws.Description = *input.Description
	}
	ws.UpdatedAt = time.Now().UTC()

	result, err := s.db.ExecContext(ctx, `
		UPDATE workspaces SET capabilities_revision = capabilities_revision + 1, name = ?, description = ?, updated_at = ? WHERE id = ?
	`, ws.Name, ws.Description, ws.UpdatedAt, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "Duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return nil, ErrDuplicateKey
		}
		return nil, fmt.Errorf("failed to update workspace: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, ErrNotFound
	}

	return ws, nil
}

func (s *DuckDBStore) DeleteWorkspace(ctx context.Context, id string) error {
	plan, err := s.PlanWorkspaceDeletion(ctx, id)
	if err != nil {
		return err
	}
	if plan.HasDependencies() {
		return &DeletionConflictError{Plan: *plan}
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM workspaces WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete workspace: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM workspace_data_revisions WHERE workspace_id = ?", id); err != nil {
		return err
	}
	return nil
}
