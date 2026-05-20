package audit

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Writer persists AuditEvents to the audit_events table asynchronously.
//
// References:
//   - SPEC.md §5.4
//   - ADR-0005 — async channel design
type Writer struct {
	ch     chan AuditEvent
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// auditChannelBuf mirrors the buffer size rationale in usage.Writer.
// See ADR-0005.
const auditChannelBuf = 10_000

// NewWriter creates a Writer and starts the background drain goroutine.
func NewWriter(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) *Writer {
	w := &Writer{
		ch:     make(chan AuditEvent, auditChannelBuf),
		pool:   pool,
		logger: logger,
	}
	go w.run(ctx)
	return w
}

// Emit enqueues an audit event for async persistence. Drops and warns if channel is full.
func (w *Writer) Emit(e AuditEvent) {
	select {
	case w.ch <- e:
	default:
		w.logger.Warn("audit channel full, event dropped",
			"request_id", e.RequestID,
			"event_type", "audit_dropped",
		)
	}
}

func (w *Writer) run(ctx context.Context) {
	for {
		select {
		case e := <-w.ch:
			w.insert(e)
		case <-ctx.Done():
			for {
				select {
				case e := <-w.ch:
					w.insert(e)
				default:
					return
				}
			}
		}
	}
}

const auditInsertSQL = `
INSERT INTO audit_events (
	request_id, application_name, event_type, severity, metadata, created_at
) VALUES ($1, $2, $3, $4, $5, $6)`

func (w *Writer) insert(e AuditEvent) {
	var metaJSON []byte
	if e.Metadata != nil {
		var err error
		metaJSON, err = json.Marshal(e.Metadata)
		if err != nil {
			w.logger.Error("marshalling audit metadata",
				"err", err,
				"request_id", e.RequestID,
			)
			return
		}
	}

	_, err := w.pool.Exec(
		context.Background(),
		auditInsertSQL,
		e.RequestID, e.ApplicationName, e.EventType, e.Severity, metaJSON, e.CreatedAt,
	)
	if err != nil {
		w.logger.Error("inserting audit event",
			"err", err,
			"request_id", e.RequestID,
			"event_type", e.EventType,
		)
	}
}
