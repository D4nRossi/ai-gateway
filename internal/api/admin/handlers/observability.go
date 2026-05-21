package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// usageEventRow is the JSON representation of a usage_events row.
type usageEventRow struct {
	ID               int64    `json:"id"`
	RequestID        string   `json:"request_id"`
	ApplicationName  string   `json:"application_name"`
	Tier             string   `json:"tier"`
	Model            string   `json:"model"`
	Provider         string   `json:"provider"`
	InputTokens      *int32   `json:"input_tokens"`
	OutputTokens     *int32   `json:"output_tokens"`
	TotalTokens      *int32   `json:"total_tokens"`
	LatencyMS        int32    `json:"latency_ms"`
	StatusCode       int32    `json:"status_code"`
	EstimatedCostBRL *float64 `json:"estimated_cost_brl"`
	CreatedAt        string   `json:"created_at"`
}

// auditEventRow is the JSON representation of an audit_events row.
type auditEventRow struct {
	ID              int64   `json:"id"`
	RequestID       string  `json:"request_id"`
	ApplicationName string  `json:"application_name"`
	EventType       string  `json:"event_type"`
	Severity        string  `json:"severity"`
	Metadata        *string `json:"metadata"`
	CreatedAt       string  `json:"created_at"`
}

// budgetRow is the JSON representation of a budget_counters row.
type budgetRow struct {
	ApplicationName  string   `json:"application_name"`
	PeriodYYYYMM     string   `json:"period"`
	TotalRequests    int64    `json:"total_requests"`
	TotalTokens      int64    `json:"total_tokens"`
	EstimatedCostBRL float64  `json:"estimated_cost_brl"`
	UpdatedAt        string   `json:"updated_at"`
}

// ListUsageEvents handles GET /admin/v1/usage.
//
// Query parameters:
//   - from (RFC3339): start of time range (default: 24 hours ago)
//   - to   (RFC3339): end of time range (default: now)
//   - application: filter by application name
//   - limit: max rows (default 100, max 1000)
//
// Reasoning: the observability handler queries usage_events directly via the pool
// because these are read-only reporting queries with no business logic. Routing them
// through adminservice would add a dependency between the service and pgxpool without
// any business-rule benefit at this stage. A dedicated ObservabilityRepository can be
// extracted if reporting grows in complexity (ADR-0015 allows this).
func ListUsageEvents(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, limit, ok := parseTimeRange(w, r)
		if !ok {
			return
		}
		appFilter := r.URL.Query().Get("application")

		rows, err := queryUsageEvents(r.Context(), pool, from, to, appFilter, limit)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to query usage events")
			return
		}

		writeJSON(w, http.StatusOK, rows)
	}
}

// ListAuditEvents handles GET /admin/v1/audit.
//
// Query parameters:
//   - from, to, application, limit (same as ListUsageEvents)
//   - event_type: filter by event_type column
func ListAuditEvents(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, limit, ok := parseTimeRange(w, r)
		if !ok {
			return
		}
		appFilter := r.URL.Query().Get("application")
		eventType := r.URL.Query().Get("event_type")

		rows, err := queryAuditEvents(r.Context(), pool, from, to, appFilter, eventType, limit)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to query audit events")
			return
		}

		writeJSON(w, http.StatusOK, rows)
	}
}

// ListBudget handles GET /admin/v1/budget.
//
// Query parameters:
//   - period: YYYYMM (default: current month)
//   - application: filter by application name
func ListBudget(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		period := r.URL.Query().Get("period")
		if period == "" {
			period = time.Now().UTC().Format("200601")
		}
		appFilter := r.URL.Query().Get("application")

		rows, err := queryBudget(r.Context(), pool, period, appFilter)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to query budget")
			return
		}

		writeJSON(w, http.StatusOK, rows)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// parseTimeRange extracts and validates the from/to/limit query parameters.
func parseTimeRange(w http.ResponseWriter, r *http.Request) (from, to time.Time, limit int, ok bool) {
	now := time.Now().UTC()
	from = now.Add(-24 * time.Hour)
	to = now
	limit = 100

	if s := r.URL.Query().Get("from"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeAdminError(w, http.StatusBadRequest, "invalid_param", "from must be RFC3339 (e.g. 2006-01-02T15:04:05Z)")
			return time.Time{}, time.Time{}, 0, false
		}
		from = t
	}
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeAdminError(w, http.StatusBadRequest, "invalid_param", "to must be RFC3339 (e.g. 2006-01-02T15:04:05Z)")
			return time.Time{}, time.Time{}, 0, false
		}
		to = t
	}
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 1000 {
			writeAdminError(w, http.StatusBadRequest, "invalid_param", "limit must be 1–1000")
			return time.Time{}, time.Time{}, 0, false
		}
		limit = n
	}

	return from, to, limit, true
}

func queryUsageEvents(ctx context.Context, pool *pgxpool.Pool, from, to time.Time, app string, limit int) ([]usageEventRow, error) {
	q := `
		SELECT id, request_id, application_name, tier, model, provider,
		       input_tokens, output_tokens, total_tokens, latency_ms, status_code,
		       estimated_cost_brl, created_at
		FROM usage_events
		WHERE created_at BETWEEN $1 AND $2`

	args := []any{from, to}
	if app != "" {
		args = append(args, app)
		q += fmt.Sprintf(" AND application_name = $%d", len(args))
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("querying usage events: %w", err)
	}
	defer rows.Close()

	result := []usageEventRow{}
	for rows.Next() {
		var row usageEventRow
		var createdAt time.Time
		if err := rows.Scan(
			&row.ID, &row.RequestID, &row.ApplicationName, &row.Tier,
			&row.Model, &row.Provider,
			&row.InputTokens, &row.OutputTokens, &row.TotalTokens,
			&row.LatencyMS, &row.StatusCode,
			&row.EstimatedCostBRL, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scanning usage event row: %w", err)
		}
		row.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating usage events: %w", err)
	}
	return result, nil
}

func queryAuditEvents(ctx context.Context, pool *pgxpool.Pool, from, to time.Time, app, eventType string, limit int) ([]auditEventRow, error) {
	q := `
		SELECT id, request_id, application_name, event_type, severity,
		       metadata::TEXT, created_at
		FROM audit_events
		WHERE created_at BETWEEN $1 AND $2`

	args := []any{from, to}
	if app != "" {
		args = append(args, app)
		q += fmt.Sprintf(" AND application_name = $%d", len(args))
	}
	if eventType != "" {
		args = append(args, eventType)
		q += fmt.Sprintf(" AND event_type = $%d", len(args))
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("querying audit events: %w", err)
	}
	defer rows.Close()

	result := []auditEventRow{}
	for rows.Next() {
		var row auditEventRow
		var createdAt time.Time
		if err := rows.Scan(
			&row.ID, &row.RequestID, &row.ApplicationName,
			&row.EventType, &row.Severity, &row.Metadata, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scanning audit event row: %w", err)
		}
		row.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating audit events: %w", err)
	}
	return result, nil
}

func queryBudget(ctx context.Context, pool *pgxpool.Pool, period, app string) ([]budgetRow, error) {
	q := `
		SELECT application_name, period_yyyymm, total_requests, total_tokens,
		       estimated_cost_brl, updated_at
		FROM budget_counters
		WHERE period_yyyymm = $1`

	args := []any{period}
	if app != "" {
		args = append(args, app)
		q += fmt.Sprintf(" AND application_name = $%d", len(args))
	}
	q += " ORDER BY application_name"

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("querying budget: %w", err)
	}
	defer rows.Close()

	result := []budgetRow{}
	for rows.Next() {
		var row budgetRow
		var updatedAt time.Time
		if err := rows.Scan(
			&row.ApplicationName, &row.PeriodYYYYMM,
			&row.TotalRequests, &row.TotalTokens,
			&row.EstimatedCostBRL, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning budget row: %w", err)
		}
		row.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating budget rows: %w", err)
	}
	return result, nil
}
