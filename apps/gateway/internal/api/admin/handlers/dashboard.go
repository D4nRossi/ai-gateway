package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ── timeseries ────────────────────────────────────────────────────────────────

// timeseriesPoint is one bucket of aggregated usage data, suited for line/area
// charts in the Dashboard (§3.5 of roadmap).
//
// Latency aggregation: V1 exposes avg + max. Percentis (p50/p95/p99) ficam
// como follow-up — SQL Server suporta via PERCENTILE_CONT mas como window
// function isso requer subquery dedicada por percentil, e o ganho UX pra V1
// (Playground chamando proxy) é marginal vs avg+max.
type timeseriesPoint struct {
	BucketStart      string  `json:"bucket_start"`
	RequestCount     int64   `json:"request_count"`
	AvgLatencyMS     float64 `json:"avg_latency_ms"`
	MaxLatencyMS     int     `json:"max_latency_ms"`
	TotalTokens      int64   `json:"total_tokens"`
	TotalCostBRL     float64 `json:"total_cost_brl"`
	ErrorCount4xx    int64   `json:"error_count_4xx"`
	ErrorCount5xx    int64   `json:"error_count_5xx"`
}

// DashboardTimeseries handles GET /admin/v1/dashboard/timeseries.
//
// Query params:
//   - from (RFC3339, default: 24h ago)
//   - to   (RFC3339, default: now)
//   - bucket: "hour" | "day" (default: hour)
//
// Buckets vazios não são retornados — o frontend interpola gaps via recharts
// (data points ausentes ficam com valor zero implícito quando o eixo X é
// contínuo).
//
// References:
//   - roadmap §3.5 — Dashboard nativo com timeseries
//   - ADR-0024 — usage tracking inclui agora as chamadas via proxy plane
func DashboardTimeseries(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, _, ok := parseTimeRange(w, r)
		if !ok {
			return
		}
		bucket := strings.ToLower(r.URL.Query().Get("bucket"))
		if bucket == "" {
			bucket = "hour"
		}
		if bucket != "hour" && bucket != "day" {
			writeAdminError(w, http.StatusBadRequest, "invalid_param", "bucket must be 'hour' or 'day'")
			return
		}

		rows, err := queryTimeseries(r.Context(), db, from, to, bucket)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to query timeseries")
			return
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

func queryTimeseries(ctx context.Context, db *sql.DB, from, to time.Time, bucket string) ([]timeseriesPoint, error) {
	// T-SQL: DATEADD(<unit>, DATEDIFF(<unit>, 0, created_at), 0) trunca a
	// data ao início do bucket (epoch 0 + N units). Funciona pra hour e day
	// igualmente. Embebber o unit é seguro porque restringimos a duas opções
	// antes — sem injection.
	q := fmt.Sprintf(`
		SELECT
		    DATEADD(%[1]s, DATEDIFF(%[1]s, 0, created_at), 0) AS bucket_start,
		    COUNT(*) AS request_count,
		    AVG(CAST(latency_ms AS FLOAT)) AS avg_latency,
		    MAX(latency_ms) AS max_latency,
		    COALESCE(SUM(CAST(total_tokens AS BIGINT)), 0) AS total_tokens,
		    COALESCE(SUM(estimated_cost_brl), 0) AS total_cost,
		    SUM(CASE WHEN status_code BETWEEN 400 AND 499 THEN 1 ELSE 0 END) AS err_4xx,
		    SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END) AS err_5xx
		FROM gogateway.usage_events
		WHERE created_at BETWEEN @p1 AND @p2
		GROUP BY DATEADD(%[1]s, DATEDIFF(%[1]s, 0, created_at), 0)
		ORDER BY bucket_start ASC`, bucket)

	rows, err := db.QueryContext(ctx, q, from, to)
	if err != nil {
		return nil, fmt.Errorf("querying timeseries: %w", err)
	}
	defer rows.Close()

	result := []timeseriesPoint{}
	for rows.Next() {
		var (
			bucketStart time.Time
			p           timeseriesPoint
		)
		if err := rows.Scan(
			&bucketStart, &p.RequestCount, &p.AvgLatencyMS, &p.MaxLatencyMS,
			&p.TotalTokens, &p.TotalCostBRL, &p.ErrorCount4xx, &p.ErrorCount5xx,
		); err != nil {
			return nil, fmt.Errorf("scanning timeseries row: %w", err)
		}
		p.BucketStart = bucketStart.UTC().Format(time.RFC3339)
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating timeseries rows: %w", err)
	}
	return result, nil
}

// ── breakdown ─────────────────────────────────────────────────────────────────

// breakdownRow is a single category in a breakdown chart (pie / bar).
// Dimension determina a semântica do campo `key`.
type breakdownRow struct {
	Key          string  `json:"key"`
	RequestCount int64   `json:"request_count"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalCostBRL float64 `json:"total_cost_brl"`
}

// DashboardBreakdown handles GET /admin/v1/dashboard/breakdown.
//
// Query params:
//   - from, to (same defaults as timeseries)
//   - dimension: "tier" | "model" | "application" (default: application)
//   - limit: top N (default 10, max 50)
//
// Para "application", retorna ordenado por custo descendente — top spenders.
// Para "tier" / "model", retorna todos (sem limit) já que a cardinalidade é
// baixa.
func DashboardBreakdown(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, _, ok := parseTimeRange(w, r)
		if !ok {
			return
		}
		dimension := r.URL.Query().Get("dimension")
		if dimension == "" {
			dimension = "application"
		}
		column, ok := dimensionColumn(dimension)
		if !ok {
			writeAdminError(w, http.StatusBadRequest, "invalid_param",
				"dimension must be one of: application, tier, model")
			return
		}

		limit := parsePositiveIntDefault(r, "limit", 10, 50)
		// Para tier (cardinalidade ≤ 3) e model (provavelmente ≤ 20) o limit
		// não faz diferença prática mas mantemos o cap pra evitar payloads
		// gigantes em ambientes patológicos.

		rows, err := queryBreakdown(r.Context(), db, from, to, column, limit)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to query breakdown")
			return
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

// dimensionColumn resolves a dimension name to the SQL column. Allowlist
// evita SQL injection do parâmetro `dimension`.
func dimensionColumn(d string) (string, bool) {
	switch d {
	case "application":
		return "application_name", true
	case "tier":
		return "tier", true
	case "model":
		return "model", true
	default:
		return "", false
	}
}

func queryBreakdown(ctx context.Context, db *sql.DB, from, to time.Time, column string, limit int) ([]breakdownRow, error) {
	// column vem de dimensionColumn (allowlist) — seguro embeddar.
	q := fmt.Sprintf(`
		SELECT TOP (@p3)
		    %[1]s AS k,
		    COUNT(*) AS request_count,
		    COALESCE(SUM(CAST(total_tokens AS BIGINT)), 0) AS total_tokens,
		    COALESCE(SUM(estimated_cost_brl), 0) AS total_cost
		FROM gogateway.usage_events
		WHERE created_at BETWEEN @p1 AND @p2
		  AND %[1]s IS NOT NULL
		  AND %[1]s <> ''
		GROUP BY %[1]s
		ORDER BY total_cost DESC, request_count DESC`, column)

	rows, err := db.QueryContext(ctx, q, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("querying breakdown: %w", err)
	}
	defer rows.Close()

	result := []breakdownRow{}
	for rows.Next() {
		var row breakdownRow
		if err := rows.Scan(&row.Key, &row.RequestCount, &row.TotalTokens, &row.TotalCostBRL); err != nil {
			return nil, fmt.Errorf("scanning breakdown row: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating breakdown rows: %w", err)
	}
	return result, nil
}

// parsePositiveIntDefault reads a query parameter as a positive integer.
// Returns def when the param is missing, malformed, or outside [1, max].
func parsePositiveIntDefault(r *http.Request, key string, def, max int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	if n < 1 || n > max {
		return def
	}
	return n
}
