// Package budget provides synchronous budget pre-checking and asynchronous
// counter updates for the monthly spend cap feature.
//
// References:
//   - SPEC.md §12.2 — budget pre-check
//   - SPEC.md §12.3 — budget update (async)
package budget

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrBudgetExceeded is returned when the application's monthly spend cap is reached.
var ErrBudgetExceeded = errors.New("monthly budget exceeded")

// Checker performs synchronous budget pre-checks against the budget_counters table.
type Checker struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewChecker creates a Checker backed by the given connection pool.
func NewChecker(pool *pgxpool.Pool, logger *slog.Logger) *Checker {
	return &Checker{pool: pool, logger: logger}
}

// Check queries the current period's estimated spend for the application.
// Returns ErrBudgetExceeded if the spend has reached or exceeded the limit.
//
// The query has a 500 ms hard timeout; on timeout it fails-open (warn log,
// request continues) to prevent DB hiccups from blocking legitimate traffic.
//
// References:
//   - SPEC.md §12.2 — budget pre-check specification
func (c *Checker) Check(ctx context.Context, appName string, budgetBRL float64) error {
	// 500 ms deadline per SPEC §12.2.
	qCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	period := currentPeriod()

	var spent float64
	err := c.pool.QueryRow(
		qCtx,
		`SELECT estimated_cost_brl
		 FROM budget_counters
		 WHERE application_name = $1 AND period_yyyymm = $2`,
		appName, period,
	).Scan(&spent)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No spend recorded yet — allow the request.
			return nil
		}
		if errors.Is(qCtx.Err(), context.DeadlineExceeded) {
			// Fail-open: DB timeout should not block business traffic.
			c.logger.Warn("budget precheck timed out, failing open",
				"application_name", appName,
				"event_type", "budget_precheck_timeout",
			)
			return nil
		}
		// Other DB errors: fail-open with warn log.
		c.logger.Warn("budget precheck query failed, failing open",
			"err", err,
			"application_name", appName,
		)
		return nil
	}

	if spent >= budgetBRL {
		return fmt.Errorf("%w: %q has spent %.4f of %.4f BRL this month",
			ErrBudgetExceeded, appName, spent, budgetBRL)
	}
	return nil
}

// currentPeriod returns the current UTC month in YYYYMM format.
//
// References:
//   - SPEC.md §12.2 — "period format: YYYYMM UTC"
func currentPeriod() string {
	return time.Now().UTC().Format("200601")
}
