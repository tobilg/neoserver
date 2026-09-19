package audit

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/tobilg/neoserver/internal/conf"
	"github.com/tobilg/neoserver/internal/identity"
	"github.com/tobilg/neoserver/internal/protocolrequest"
	"github.com/tobilg/neoserver/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Event struct {
	ID           string    `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	RequestID    string    `json:"request_id,omitempty"`
	Principal    string    `json:"principal,omitempty"`
	CredentialID string    `json:"credential_id,omitempty"`
	AuthMethod   string    `json:"auth_method,omitempty"`
	Workspace    string    `json:"workspace,omitempty"`
	Protocol     string    `json:"protocol,omitempty"`
	Operation    string    `json:"operation,omitempty"`
	Action       string    `json:"action"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	Status       int       `json:"status"`
	DurationMS   int64     `json:"duration_ms"`
	Security     bool      `json:"security_event"`
}

type Query struct {
	Cursor       string
	Search       string
	extra        bool
	Workspace    string
	Principal    string
	CredentialID string
	Since        *time.Time
	// Outcome narrows by response class: "failed" (status >= 400) or
	// "succeeded" (status < 400). Empty keeps every event.
	Outcome string
	Limit   int
}

// Outcome filter values accepted by Query.Outcome.
const (
	OutcomeFailed    = "failed"
	OutcomeSucceeded = "succeeded"
)

type Outbox interface {
	EnqueueAuditEvent(context.Context, string, time.Time, []byte) error
	ListAuditOutbox(context.Context, int) ([]store.AuditOutboxRecord, error)
	MarkAuditOutboxAttempt(context.Context, string, string) error
	DeleteAuditOutboxEvent(context.Context, string) error
	CountAuditOutbox(context.Context) (int64, error)
}

type Manager struct {
	cfg       conf.Audit
	db        *sql.DB
	outbox    Outbox
	logger    *slog.Logger
	mu        sync.Mutex
	drainMu   sync.Mutex
	hookMu    sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	wake      chan struct{}
	pending   atomic.Int64
	writeBad  atomic.Bool
	retainBad atomic.Bool
	lost      atomic.Bool

	writeEvent func(context.Context, Event) error
	purgeOld   func(context.Context) error

	writeFailures         metric.Int64Counter
	outboxEnqueueFailures metric.Int64Counter
	retryDeliveries       metric.Int64Counter
	retentionFailures     metric.Int64Counter
	eventsLost            metric.Int64Counter
	metricRegistration    metric.Registration
}

func Open(ctx context.Context, cfg conf.Audit, encryptionKey string, logger *slog.Logger, outboxes ...Outbox) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0700); err != nil {
		return nil, fmt.Errorf("create audit directory: %w", err)
	}
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	_, _ = db.Exec("INSTALL httpfs; LOAD httpfs")
	attach := fmt.Sprintf("ATTACH '%s' AS audit", sqlLiteral(cfg.DatabasePath))
	if encryptionKey != "" {
		attach += fmt.Sprintf(" (ENCRYPTION_KEY '%s')", sqlLiteral(encryptionKey))
	}
	if _, err = db.Exec(attach + "; USE audit"); err != nil {
		db.Close()
		return nil, fmt.Errorf("open encrypted audit database: %w", err)
	}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS audit_events (
		id VARCHAR PRIMARY KEY, occurred_at TIMESTAMP NOT NULL, request_id VARCHAR, principal VARCHAR,
		auth_method VARCHAR, workspace VARCHAR, protocol VARCHAR, operation VARCHAR, action VARCHAR,
		method VARCHAR, path VARCHAR, status INTEGER, duration_ms BIGINT, security_event BOOLEAN
	); CREATE INDEX IF NOT EXISTS audit_events_time ON audit_events(occurred_at);
	CREATE INDEX IF NOT EXISTS audit_events_workspace ON audit_events(workspace,occurred_at);`); err != nil {
		db.Close()
		return nil, err
	}
	// Additive migration preserves old events and queued pre-upgrade payloads.
	if _, err = db.Exec(`ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS credential_id VARCHAR DEFAULT ''`); err != nil {
		db.Close()
		return nil, err
	}
	managerCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	m := &Manager{cfg: cfg, db: db, logger: logger, ctx: managerCtx, cancel: cancel, wake: make(chan struct{}, 1)}
	if len(outboxes) > 0 {
		m.outbox = outboxes[0]
	}
	m.writeEvent = m.insertEvent
	m.purgeOld = m.purgeEvents
	if err := m.initMetrics(); err != nil {
		cancel()
		db.Close()
		return nil, fmt.Errorf("initialize audit metrics: %w", err)
	}
	if m.outbox != nil {
		pending, countErr := m.outbox.CountAuditOutbox(ctx)
		if countErr != nil {
			cancel()
			if m.metricRegistration != nil {
				_ = m.metricRegistration.Unregister()
			}
			db.Close()
			return nil, fmt.Errorf("inspect audit outbox: %w", countErr)
		}
		m.pending.Store(pending)
		m.writeBad.Store(pending > 0)
	}
	m.wg.Add(1)
	go m.maintenance()
	if m.outbox != nil {
		m.wg.Add(1)
		go m.retryLoop()
		if m.pending.Load() > 0 {
			m.signalRetry()
		}
	}
	return m, nil
}

func (m *Manager) maintenance() {
	defer m.wg.Done()
	interval := time.Duration(m.cfg.CleanupIntervalSec) * time.Second
	retry := m.retryDuration()
	for {
		err := m.Purge(context.Background())
		wait := interval
		if err != nil {
			wait = retry
		}
		timer := time.NewTimer(wait)
		select {
		case <-m.ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func (m *Manager) Health(ctx context.Context) error {
	if err := m.ctx.Err(); err != nil {
		return fmt.Errorf("audit coordinator stopped: %w", err)
	}
	if err := m.db.PingContext(ctx); err != nil {
		return err
	}
	if m.lost.Load() {
		return errors.New("audit event handoff failed")
	}
	if m.retainBad.Load() {
		return errors.New("audit retention is degraded")
	}
	if pending := m.pending.Load(); pending > 0 || m.writeBad.Load() {
		return fmt.Errorf("audit delivery backlog contains %d events", pending)
	}
	return nil
}

func (m *Manager) Close(ctx context.Context) error {
	var drainErr error
	for m.outbox != nil && m.pending.Load() > 0 {
		if err := m.drainBatch(ctx); err != nil {
			drainErr = err
			timer := time.NewTimer(m.retryDuration())
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				drainErr = errors.Join(drainErr, fmt.Errorf("audit shutdown left %d pending events: %w", m.pending.Load(), ctx.Err()))
				goto stop
			case <-timer.C:
			}
		}
	}

stop:
	m.cancel()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		drainErr = errors.Join(drainErr, ctx.Err())
	}
	if m.metricRegistration != nil {
		_ = m.metricRegistration.Unregister()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return errors.Join(drainErr, m.db.Close())
}

func (m *Manager) Record(ctx context.Context, event Event) error {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	event.RequestID = m.field(event.RequestID)
	event.Principal = m.field(event.Principal)
	event.CredentialID = m.field(event.CredentialID)
	event.AuthMethod = m.field(event.AuthMethod)
	event.Workspace = m.field(event.Workspace)
	event.Protocol = m.field(event.Protocol)
	event.Operation = m.field(event.Operation)
	event.Path = m.field(event.Path)
	if m.writeBad.Load() || m.pending.Load() > 0 {
		return m.enqueue(ctx, event)
	}
	writeCtx, cancel := m.writeContext(context.WithoutCancel(ctx))
	err := m.callWriteEvent(writeCtx, event)
	cancel()
	if err == nil {
		return nil
	}
	m.writeFailures.Add(context.Background(), 1, metric.WithAttributes(attribute.String("stage", "direct")))
	m.logger.Error("write audit event failed; queueing durable retry", "event_id", event.ID, "error", err)
	return m.enqueue(ctx, event)
}

func (m *Manager) insertEvent(ctx context.Context, event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.ExecContext(ctx, `INSERT INTO audit_events (id,occurred_at,request_id,principal,auth_method,workspace,protocol,operation,action,method,path,status,duration_ms,security_event,credential_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
		event.ID, event.Timestamp, event.RequestID, event.Principal, event.AuthMethod, event.Workspace,
		event.Protocol, event.Operation, event.Action, event.Method, event.Path, event.Status, event.DurationMS, event.Security, event.CredentialID)
	return err
}

func (m *Manager) enqueue(ctx context.Context, event Event) error {
	if m.outbox == nil {
		m.markLost(event.ID, errors.New("audit outbox is unavailable"))
		return errors.New("audit event could not be durably queued")
	}
	payload, err := json.Marshal(event)
	if err != nil {
		m.markLost(event.ID, err)
		return err
	}
	writeCtx, cancel := m.writeContext(context.WithoutCancel(ctx))
	err = m.outbox.EnqueueAuditEvent(writeCtx, event.ID, event.Timestamp, payload)
	cancel()
	if err != nil {
		m.outboxEnqueueFailures.Add(context.Background(), 1)
		m.markLost(event.ID, err)
		return fmt.Errorf("queue audit event: %w", err)
	}
	m.pending.Add(1)
	if !m.writeBad.Swap(true) {
		m.logger.Error("audit delivery degraded", "pending_events", m.pending.Load())
	}
	m.signalRetry()
	return nil
}

func (m *Manager) markLost(eventID string, cause error) {
	m.lost.Store(true)
	m.writeBad.Store(true)
	m.eventsLost.Add(context.Background(), 1)
	m.logger.Error("audit event handoff lost; readiness is latched failed", "event_id", eventID, "error", cause)
}

func (m *Manager) signalRetry() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) retryLoop() {
	defer m.wg.Done()
	interval := m.retryDuration()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		case <-ticker.C:
		}
		if m.pending.Load() == 0 {
			continue
		}
		if err := m.drainBatch(m.ctx); err != nil && m.ctx.Err() == nil {
			m.logger.Error("audit outbox delivery failed", "pending_events", m.pending.Load(), "error", err)
		}
	}
}

func (m *Manager) drainBatch(ctx context.Context) error {
	if m.outbox == nil {
		return nil
	}
	m.drainMu.Lock()
	defer m.drainMu.Unlock()
	items, err := m.outbox.ListAuditOutbox(ctx, m.cfg.RetryBatchSize)
	if err != nil {
		m.writeBad.Store(true)
		return err
	}
	for _, item := range items {
		var event Event
		if err = json.Unmarshal(item.Payload, &event); err != nil {
			message := boundedError(err)
			_ = m.outbox.MarkAuditOutboxAttempt(ctx, item.ID, message)
			m.writeBad.Store(true)
			return fmt.Errorf("decode queued audit event %s: %w", item.ID, err)
		}
		writeCtx, cancel := m.writeContext(ctx)
		err = m.callWriteEvent(writeCtx, event)
		cancel()
		if err != nil {
			m.writeFailures.Add(context.Background(), 1, metric.WithAttributes(attribute.String("stage", "retry")))
			_ = m.outbox.MarkAuditOutboxAttempt(ctx, item.ID, boundedError(err))
			m.writeBad.Store(true)
			return err
		}
		if err = m.outbox.DeleteAuditOutboxEvent(ctx, item.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			m.writeBad.Store(true)
			return err
		}
		m.pending.Add(-1)
		m.retryDeliveries.Add(context.Background(), 1)
	}
	count, err := m.outbox.CountAuditOutbox(ctx)
	if err != nil {
		m.writeBad.Store(true)
		return err
	}
	m.pending.Store(count)
	if count == 0 && !m.lost.Load() {
		if m.writeBad.Swap(false) {
			m.logger.Info("audit delivery recovered")
		}
	} else {
		m.writeBad.Store(true)
	}
	return nil
}

func (m *Manager) writeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := time.Duration(m.cfg.WriteTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}

func (m *Manager) retryDuration() time.Duration {
	interval := time.Duration(m.cfg.RetryIntervalSec) * time.Second
	if interval <= 0 {
		return 5 * time.Second
	}
	return interval
}

func (m *Manager) callWriteEvent(ctx context.Context, event Event) error {
	m.hookMu.RLock()
	write := m.writeEvent
	m.hookMu.RUnlock()
	return write(ctx, event)
}

func (m *Manager) setWriteEvent(write func(context.Context, Event) error) {
	m.hookMu.Lock()
	m.writeEvent = write
	m.hookMu.Unlock()
}

func (m *Manager) callPurgeOld(ctx context.Context) error {
	m.hookMu.RLock()
	purge := m.purgeOld
	m.hookMu.RUnlock()
	return purge(ctx)
}

func (m *Manager) setPurgeOld(purge func(context.Context) error) {
	m.hookMu.Lock()
	m.purgeOld = purge
	m.hookMu.Unlock()
}

func boundedError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 2048 {
		return message[:2048]
	}
	return message
}

func (m *Manager) initMetrics() error {
	meter := otel.Meter("github.com/tobilg/neoserver/audit")
	var err error
	if m.writeFailures, err = meter.Int64Counter("neoserver.audit.write_failures"); err != nil {
		return err
	}
	if m.outboxEnqueueFailures, err = meter.Int64Counter("neoserver.audit.outbox_enqueue_failures"); err != nil {
		return err
	}
	if m.retryDeliveries, err = meter.Int64Counter("neoserver.audit.retry_deliveries"); err != nil {
		return err
	}
	if m.retentionFailures, err = meter.Int64Counter("neoserver.audit.retention_failures"); err != nil {
		return err
	}
	if m.eventsLost, err = meter.Int64Counter("neoserver.audit.events_lost"); err != nil {
		return err
	}
	pending, err := meter.Int64ObservableGauge("neoserver.audit.pending_events")
	if err != nil {
		return err
	}
	degraded, err := meter.Int64ObservableGauge("neoserver.audit.degraded")
	if err != nil {
		return err
	}
	m.metricRegistration, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		observer.ObserveInt64(pending, m.pending.Load())
		var value int64
		if m.writeBad.Load() || m.retainBad.Load() || m.lost.Load() || m.pending.Load() > 0 {
			value = 1
		}
		observer.ObserveInt64(degraded, value)
		return nil
	}, pending, degraded)
	return err
}

func (m *Manager) field(value string) string {
	maximum := m.cfg.MaxFieldBytes
	if maximum > 0 && len(value) > maximum {
		return value[:maximum]
	}
	return value
}

func (m *Manager) List(ctx context.Context, query Query) ([]Event, error) {
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if query.extra {
		limit++
	}
	where := []string{"1=1"}
	args := []any{}
	if query.Workspace != "" {
		where, args = append(where, "workspace=?"), append(args, query.Workspace)
	}
	if query.Principal != "" {
		where, args = append(where, "principal=?"), append(args, query.Principal)
	}
	if query.CredentialID != "" {
		where, args = append(where, "credential_id=?"), append(args, query.CredentialID)
	}
	if query.Since != nil {
		where, args = append(where, "occurred_at>=?"), append(args, *query.Since)
	}
	switch query.Outcome {
	case OutcomeFailed:
		where = append(where, "status>=400")
	case OutcomeSucceeded:
		where = append(where, "status<400")
	}
	if query.Cursor != "" {
		cursor, err := decodeCursor(query.Cursor)
		if err != nil {
			return nil, err
		}
		where = append(where, "(occurred_at<? OR (occurred_at=? AND id<?))")
		args = append(args, cursor.Timestamp, cursor.Timestamp, cursor.ID)
	}
	if query.Search != "" {
		where = append(where, "contains(lower(concat_ws(' ',principal,credential_id,auth_method,workspace,protocol,operation,action,method,path,CAST(status AS VARCHAR))),lower(?))")
		args = append(args, query.Search)
	}
	args = append(args, limit)
	rows, err := m.db.QueryContext(ctx, `SELECT id,occurred_at,request_id,principal,auth_method,workspace,protocol,operation,
		action,method,path,status,duration_ms,security_event,COALESCE(credential_id,'') FROM audit_events WHERE `+strings.Join(where, " AND ")+` ORDER BY occurred_at DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Timestamp, &event.RequestID, &event.Principal, &event.AuthMethod, &event.Workspace,
			&event.Protocol, &event.Operation, &event.Action, &event.Method, &event.Path, &event.Status, &event.DurationMS, &event.Security, &event.CredentialID); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

type Page struct {
	Events     []Event `json:"events"`
	NextCursor string  `json:"next_cursor,omitempty"`
}
type eventCursor struct {
	Timestamp time.Time `json:"t"`
	ID        string    `json:"id"`
}

var ErrInvalidCursor = errors.New("invalid audit cursor")

func decodeCursor(value string) (eventCursor, error) {
	var cursor eventCursor
	if len(value) > 2048 {
		return cursor, ErrInvalidCursor
	}
	body, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(body, &cursor) != nil || cursor.Timestamp.IsZero() || cursor.ID == "" || len(cursor.ID) > 256 {
		return cursor, ErrInvalidCursor
	}
	return cursor, nil
}

func (m *Manager) ListPage(ctx context.Context, query Query) (Page, error) {
	if query.Limit <= 0 || query.Limit > 1000 {
		query.Limit = 100
	}
	query.extra = true
	events, err := m.List(ctx, query)
	if err != nil {
		return Page{}, err
	}
	page := Page{Events: events}
	if len(events) > query.Limit {
		page.Events = events[:query.Limit]
		last := page.Events[len(page.Events)-1]
		body, _ := json.Marshal(eventCursor{Timestamp: last.Timestamp, ID: last.ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(body)
	}
	if page.Events == nil {
		page.Events = []Event{}
	}
	return page, nil
}

func (m *Manager) Purge(ctx context.Context) error {
	writeCtx, cancel := m.writeContext(ctx)
	err := m.callPurgeOld(writeCtx)
	cancel()
	if err != nil {
		m.retentionFailures.Add(context.Background(), 1)
		if !m.retainBad.Swap(true) {
			m.logger.Error("audit retention cleanup degraded", "error", err)
		}
		return err
	}
	if m.retainBad.Swap(false) {
		m.logger.Info("audit retention cleanup recovered")
	}
	return nil
}

func (m *Manager) purgeEvents(ctx context.Context) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -m.cfg.RetentionDays)
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.db.ExecContext(ctx, `DELETE FROM audit_events WHERE occurred_at < ?`, cutoff)
	return err
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, attribution := identity.CaptureAuditPrincipal(r.Context())
		r = r.WithContext(ctx)
		if principal, ok := identity.FromContext(ctx); ok {
			identity.RecordAuditPrincipal(ctx, principal)
		}
		r = protocolrequest.Prepare(r, "")
		operation := protocolrequest.Get(r)
		started := time.Now()
		wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(wrapped, r)
		status := wrapped.Status()
		mutation := operation.Mutating(r.Method)
		authOperation := ""
		for _, name := range []string{"login", "refresh", "logout", "oidc/callback"} {
			if strings.HasSuffix(r.URL.Path, "/auth/"+name) {
				authOperation = name
				break
			}
		}
		securityEvent := status == http.StatusUnauthorized || status == http.StatusForbidden || authOperation != ""
		if !m.cfg.RecordReads && !mutation && !securityEvent {
			return
		}
		event := Event{Timestamp: time.Now().UTC(), RequestID: middleware.GetReqID(r.Context()), Method: r.Method,
			Path: r.URL.Path, Status: status, DurationMS: time.Since(started).Milliseconds(), Security: securityEvent}
		if securityEvent {
			event.Action = "security"
		} else if mutation {
			event.Action = "change"
		} else {
			event.Action = "read"
		}
		event.Principal, event.AuthMethod, event.CredentialID = attribution.Subject, attribution.Method, attribution.CredentialID
		event.Workspace, event.Protocol = operation.Workspace, operation.Service
		if event.Protocol == "" {
			event.Protocol = "management"
		}
		event.Operation = operation.Name
		if authOperation != "" {
			event.Operation = authOperation
		}
		if operation.Err != nil {
			event.Operation = "INVALID"
		}
		if err := m.Record(context.WithoutCancel(r.Context()), event); err != nil {
			m.logger.Error("write audit event failed", "error", err)
		}
	})
}

func sqlLiteral(value string) string { return strings.ReplaceAll(value, "'", "''") }

func ParseLimit(value string) int {
	limit, _ := strconv.Atoi(value)
	return limit
}
