package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
)

// Writer persists AuditEvents to the gogateway.audit_events table asynchronously.
//
// References:
//   - SPEC.md §5.4
//   - ADR-0005 — async channel design
//   - ADR-0022 — SQL Server (substitui pgxpool/PostgreSQL legacy)
type Writer struct {
	ch     chan AuditEvent
	db     *sql.DB
	logger *slog.Logger
}

// auditChannelBuf mirrors the buffer size rationale in usage.Writer. See ADR-0005.
const auditChannelBuf = 10_000

// NewWriter creates a Writer and starts the background drain goroutine.
func NewWriter(ctx context.Context, db *sql.DB, logger *slog.Logger) *Writer {
	w := &Writer{
		ch:     make(chan AuditEvent, auditChannelBuf),
		db:     db,
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
INSERT INTO gogateway.audit_events (
	request_id, application_name, event_type, severity, metadata, created_at
) VALUES (@p1, @p2, @p3, @p4, @p5, @p6)`

func (w *Writer) insert(e AuditEvent) {
	var metaJSON any
	if e.Metadata != nil {
		b, err := json.Marshal(e.Metadata)
		if err != nil {
			w.logger.Error("marshalling audit metadata",
				"err", err,
				"request_id", e.RequestID,
			)
			return
		}
		// Pass as string so the driver stores it in NVARCHAR(MAX) without
		// going through VARBINARY conversion.
		metaJSON = string(b)
	}

	_, err := w.db.ExecContext(
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
