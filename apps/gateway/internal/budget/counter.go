package budget

import (
	"context"
	"database/sql"
	"log/slog"
)

// Recorder is the interface satisfied by Counter and any test stub.
// Decoupling the chat handler from the concrete async Counter enables
// unit testing without a live database connection.
//
// References:
//   - SPEC.md §12.3
//   - CLAUDE.md §14 — testability via interface injection
type Recorder interface {
	Record(UpdateEvent)
}

// UpdateEvent carries the data needed to update a budget counter row.
type UpdateEvent struct {
	ApplicationName  string
	TotalTokens      int
	EstimatedCostBRL float64
}

// Counter asynchronously upserts gogateway.budget_counters rows after each request.
//
// References:
//   - SPEC.md §12.3 — budget update async UPSERT
//   - ADR-0005 — async channel design
//   - ADR-0022 — SQL Server MERGE substitui ON CONFLICT DO UPDATE
type Counter struct {
	ch     chan UpdateEvent
	db     *sql.DB
	logger *slog.Logger
}

const counterChannelBuf = 10_000

// NewCounter creates a Counter and starts the background worker goroutine.
func NewCounter(ctx context.Context, db *sql.DB, logger *slog.Logger) *Counter {
	c := &Counter{
		ch:     make(chan UpdateEvent, counterChannelBuf),
		db:     db,
		logger: logger,
	}
	go c.run(ctx)
	return c
}

// Record enqueues a budget update. Non-blocking; drops and warns if channel is full.
func (c *Counter) Record(e UpdateEvent) {
	select {
	case c.ch <- e:
	default:
		c.logger.Warn("budget counter channel full, update dropped",
			"application_name", e.ApplicationName,
			"event_type", "budget_update_dropped",
		)
	}
}

func (c *Counter) run(ctx context.Context) {
	for {
		select {
		case e := <-c.ch:
			c.upsert(e)
		case <-ctx.Done():
			for {
				select {
				case e := <-c.ch:
					c.upsert(e)
				default:
					return
				}
			}
		}
	}
}

// upsertSQL inserts or increments the budget counter row for an (app, period) pair.
//
// SQL Server's idiom for UPSERT is MERGE (PG's INSERT ... ON CONFLICT DO UPDATE
// has no direct equivalent). The USING clause builds a 1-row virtual table from
// the bound parameters, the ON predicate locates the existing row (if any), and
// the MATCHED/NOT MATCHED branches handle update-vs-insert.
//
// Trailing semicolon is required — SQL Server's MERGE statement spec mandates it.
//
// References:
//   - SPEC.md §12.3
//   - https://learn.microsoft.com/en-us/sql/t-sql/statements/merge-transact-sql
const upsertSQL = `
MERGE gogateway.budget_counters WITH (HOLDLOCK) AS target
USING (VALUES (@p1, @p2, @p3, @p4)) AS source (application_name, period_yyyymm, total_tokens, estimated_cost_brl)
   ON target.application_name = source.application_name
  AND target.period_yyyymm    = source.period_yyyymm
WHEN MATCHED THEN UPDATE SET
    total_requests     = target.total_requests + 1,
    total_tokens       = target.total_tokens + source.total_tokens,
    estimated_cost_brl = target.estimated_cost_brl + source.estimated_cost_brl,
    updated_at         = SYSUTCDATETIME()
WHEN NOT MATCHED THEN INSERT (
    application_name, period_yyyymm, total_requests, total_tokens, estimated_cost_brl, updated_at
) VALUES (
    source.application_name, source.period_yyyymm, 1, source.total_tokens, source.estimated_cost_brl, SYSUTCDATETIME()
);`

func (c *Counter) upsert(e UpdateEvent) {
	_, err := c.db.ExecContext(
		context.Background(),
		upsertSQL,
		e.ApplicationName, currentPeriod(), e.TotalTokens, e.EstimatedCostBRL,
	)
	if err != nil {
		c.logger.Error("upserting budget counter",
			"err", err,
			"application_name", e.ApplicationName,
		)
	}
}
