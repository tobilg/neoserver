package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AuditOutboxRecord is a bounded, encrypted catalog copy of an audit event
// awaiting delivery to the standalone audit database.
type AuditOutboxRecord struct {
	ID           string
	OccurredAt   time.Time
	Payload      []byte
	AttemptCount int
	LastError    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (s *DuckDBStore) EnqueueAuditEvent(ctx context.Context, id string, occurredAt time.Time, payload []byte) error {
	if id == "" || len(payload) == 0 {
		return errors.New("audit outbox event id and payload are required")
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_outbox(id,occurred_at,payload,created_at,updated_at)
		VALUES (?,?,?,current_timestamp,current_timestamp) ON CONFLICT(id) DO NOTHING`, id, occurredAt, string(payload))
	if err != nil {
		return fmt.Errorf("enqueue audit event: %w", err)
	}
	return nil
}

func (s *DuckDBStore) ListAuditOutbox(ctx context.Context, limit int) ([]AuditOutboxRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,occurred_at,CAST(payload AS VARCHAR),attempt_count,last_error,created_at,updated_at
		FROM audit_outbox ORDER BY occurred_at,id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit outbox: %w", err)
	}
	defer rows.Close()
	result := make([]AuditOutboxRecord, 0)
	for rows.Next() {
		var item AuditOutboxRecord
		var payload string
		if err := rows.Scan(&item.ID, &item.OccurredAt, &payload, &item.AttemptCount, &item.LastError, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Payload = []byte(payload)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *DuckDBStore) MarkAuditOutboxAttempt(ctx context.Context, id, lastError string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE audit_outbox SET attempt_count=attempt_count+1,last_error=?,updated_at=current_timestamp WHERE id=?`, lastError, id)
	if err != nil {
		return fmt.Errorf("mark audit outbox attempt: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *DuckDBStore) DeleteAuditOutboxEvent(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM audit_outbox WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete audit outbox event: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *DuckDBStore) CountAuditOutbox(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_outbox`).Scan(&count); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("count audit outbox: %w", err)
	}
	return count, nil
}
