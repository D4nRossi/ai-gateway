package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Health handles GET /healthz — liveness probe.
// Always returns 200 if the process is running.
//
// References:
//   - SPEC.md §6.1, §13.3
func Health() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// Ready handles GET /readyz — readiness probe.
// Returns 200 if both the PostgreSQL pool and the Azure endpoint are reachable.
// Returns 503 with a body listing failed checks otherwise.
//
// DB check uses a 1-second timeout (local network). Azure check uses 5 seconds
// because the Azure cognitive-services endpoint can take 1-2 s on cold-start
// (measured: ~1.2 s from West Europe). Both checks run concurrently.
//
// References:
//   - SPEC.md §6.1, §13.3
func Ready(pool *pgxpool.Pool, azureEndpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type result struct {
			name string
			err  string
		}

		dbCh := make(chan result, 1)
		azureCh := make(chan result, 1)

		go func() {
			ctx, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			if err := pool.Ping(ctx); err != nil {
				dbCh <- result{name: "postgres", err: "database unreachable"}
				return
			}
			dbCh <- result{name: "postgres"}
		}()

		go func() {
			// 5s: Azure cognitive-services base endpoint can take ~1.2s on cold-start.
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodHead, azureEndpoint, nil)
			if err != nil {
				azureCh <- result{name: "azure", err: "azure endpoint url invalid"}
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				azureCh <- result{name: "azure", err: "azure endpoint unreachable"}
				return
			}
			resp.Body.Close()
			azureCh <- result{name: "azure"}
		}()

		dbRes := <-dbCh
		azureRes := <-azureCh

		checks := map[string]string{}
		allOK := true

		if dbRes.err != "" {
			checks[dbRes.name] = dbRes.err
			allOK = false
		}
		if azureRes.err != "" {
			checks[azureRes.name] = azureRes.err
			allOK = false
		}

		if allOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "not ready",
			"checks": checks,
		})
	}
}
