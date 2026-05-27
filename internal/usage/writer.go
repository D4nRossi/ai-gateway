package usage

import (
	"context"
	"database/sql"
	"log/slog"
)

// Writer persists UsageEvents to the gogateway.usage_events table asynchronously.
// Events are queued to a buffered channel; a background goroutine drains it.
//
// References:
//   - SPEC.md §9.1 steps 11–12 — async emit
//   - ADR-0005 — async channel (buffer 10000) vs. synchronous write
//   - ADR-0022 — SQL Server (substitui pgxpool/PostgreSQL legacy)
type Writer struct {
	ch     chan UsageEvent
	db     *sql.DB
	logger *slog.Logger
}

// channelBuf is the channel buffer depth.
// Assumes ≤1000 req/s peak and a worker capable of >100 inserts/s,
// giving 10 s of burst capacity before back-pressure. See ADR-0005.
const channelBuf = 10_000

// NewWriter creates a Writer and starts the background drain goroutine.
// ctx controls the goroutine lifetime; closing ctx causes the worker to drain
// and exit after the current insert completes.
func NewWriter(ctx context.Context, db *sql.DB, logger *slog.Logger) *Writer {
	w := &Writer{
		ch:     make(chan UsageEvent, channelBuf),
		db:     db,
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
INSERT INTO gogateway.usage_events (
	request_id, application_name, tier, model, provider,
	input_tokens, output_tokens, total_tokens,
	latency_ms, status_code, estimated_cost_brl, created_at,
	lat_auth_ms, lat_mask_ms, lat_guardrails_ms, lat_provider_ms, lat_encode_ms
) VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9, @p10, @p11, @p12, @p13, @p14, @p15, @p16, @p17)`

// nullIfZero turns a 0 latency bucket into nil so the DB stores NULL.
// Reasoning: 0 in the struct can mean either "instrumented but sub-ms" or
// "not instrumented at all". We can't tell apart at the writer layer, so we
// adopt the convention: callers either populate all 5 buckets (where 0 is
// legit for unused buckets like guardrails in Tier 1) or none. Storing 0 as
// NULL only when ALL buckets are 0 keeps Tier 1 dashboards honest without
// invalidating Tier 2/3 measurements that happen to have a sub-ms bucket.
func (w *Writer) insert(e UsageEvent) {
	allZero := e.LatAuthMs == 0 && e.LatMaskMs == 0 &&
		e.LatGuardrailsMs == 0 && e.LatProviderMs == 0 && e.LatEncodeMs == 0

	var latAuth, latMask, latGuard, latProv, latEnc any
	if allZero {
		latAuth, latMask, latGuard, latProv, latEnc = nil, nil, nil, nil, nil
	} else {
		latAuth, latMask, latGuard, latProv, latEnc =
			e.LatAuthMs, e.LatMaskMs, e.LatGuardrailsMs, e.LatProviderMs, e.LatEncodeMs
	}

	_, err := w.db.ExecContext(
		context.Background(),
		insertSQL,
		e.RequestID, e.ApplicationName, e.Tier, e.Model, e.Provider,
		e.InputTokens, e.OutputTokens, e.TotalTokens,
		e.LatencyMs, e.StatusCode, e.EstimatedCostBRL, e.CreatedAt,
		latAuth, latMask, latGuard, latProv, latEnc,
	)
	if err != nil {
		w.logger.Error("inserting usage event",
			"err", err,
			"request_id", e.RequestID,
		)
	}
}
