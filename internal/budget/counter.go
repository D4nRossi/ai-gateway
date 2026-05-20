package budget

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UpdateEvent carries the data needed to update a budget counter row.
type UpdateEvent struct {
	ApplicationName string
	TotalTokens     int
	EstimatedCostBRL float64
}

// Counter asynchronously upserts budget_counters rows after each request.
//
// References:
//   - SPEC.md §12.3 — budget update async UPSERT
//   - ADR-0005 — async channel design
type Counter struct {
	ch     chan UpdateEvent
	pool   *pgxpool.Pool
	logger *slog.Logger
}

const counterChannelBuf = 10_000

// NewCounter creates a Counter and starts the background worker goroutine.
func NewCounter(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) *Counter {
	c := &Counter{
		ch:     make(chan UpdateEvent, counterChannelBuf),
		pool:   pool,
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
// References:
//   - SPEC.md §12.3 — UPSERT specification
const upsertSQL = `
INSERT INTO budget_counters
	(application_name, period_yyyymm, total_requests, total_tokens, estimated_cost_brl, updated_at)
VALUES ($1, $2, 1, $3, $4, NOW())
ON CONFLICT (application_name, period_yyyymm) DO UPDATE SET
	total_requests    = budget_counters.total_requests + 1,
	total_tokens      = budget_counters.total_tokens + EXCLUDED.total_tokens,
	estimated_cost_brl = budget_counters.estimated_cost_brl + EXCLUDED.estimated_cost_brl,
	updated_at        = NOW()`

func (c *Counter) upsert(e UpdateEvent) {
	_, err := c.pool.Exec(
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
