package usage

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Writer persists UsageEvents to the usage_events table asynchronously.
// Events are queued to a buffered channel; a background goroutine drains it.
//
// References:
//   - SPEC.md §9.1 steps 11–12 — async emit
//   - ADR-0005 — async channel (buffer 10000) vs. synchronous write
type Writer struct {
	ch     chan UsageEvent
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// channelBuf is the channel buffer depth.
// Assumes ≤1000 req/s peak and a worker capable of >100 inserts/s,
// giving 10 s of burst capacity before back-pressure. See ADR-0005.
const channelBuf = 10_000

// NewWriter creates a Writer and starts the background drain goroutine.
// ctx controls the goroutine lifetime; closing ctx causes the worker to drain
// and exit after the current insert completes.
func NewWriter(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) *Writer {
	w := &Writer{
		ch:     make(chan UsageEvent, channelBuf),
		pool:   pool,
		logger: logger,
	}
	go w.run(ctx)
	return w
}

// Emit enqueues an event for async persistence. If the channel is full the event
// is dropped and a warning is logged. Emit never blocks.
//
// References:
//   - SPEC.md §9.1 step 12 — "non-blocking on full channel; warn if dropped"
func (w *Writer) Emit(e UsageEvent) {
	select {
	case w.ch <- e:
	default:
		w.logger.Warn("usage channel full, event dropped",
			"request_id", e.RequestID,
			"event_type", "usage_dropped",
		)
	}
}

// run is the background worker. It exits when ctx is cancelled, after draining
// any remaining events.
func (w *Writer) run(ctx context.Context) {
	for {
		select {
		case e := <-w.ch:
			w.insert(e)
		case <-ctx.Done():
			// Drain remaining events before exiting.
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

const insertSQL = `
INSERT INTO usage_events (
	request_id, application_name, tier, model, provider,
	input_tokens, output_tokens, total_tokens,
	latency_ms, status_code, estimated_cost_brl, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

func (w *Writer) insert(e UsageEvent) {
	_, err := w.pool.Exec(
		context.Background(),
		insertSQL,
		e.RequestID, e.ApplicationName, e.Tier, e.Model, e.Provider,
		e.InputTokens, e.OutputTokens, e.TotalTokens,
		e.LatencyMs, e.StatusCode, e.EstimatedCostBRL, e.CreatedAt,
	)
	if err != nil {
		w.logger.Error("inserting usage event",
			"err", err,
			"request_id", e.RequestID,
		)
	}
}
