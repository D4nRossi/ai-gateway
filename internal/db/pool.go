// Package db provides PostgreSQL connection pool setup and migration running
// for the AI Gateway.
//
// References:
//   - SPEC.md §16 steps 4–5 — bootstrap: pool + migrations
//   - https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates and validates a pgxpool connection pool using the given
// database URL and connection count settings.
//
// The pool is validated with a Ping before returning; any connectivity failure
// is surfaced immediately so the gateway can exit on boot rather than at
// first request.
//
// References:
//   - SPEC.md §16 step 4
//   - https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool
func NewPool(ctx context.Context, url string, maxConns, minConns int) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parsing database URL: %w", err)
	}

	if maxConns > 0 {
		cfg.MaxConns = int32(maxConns)
	}
	if minConns > 0 {
		cfg.MinConns = int32(minConns)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	return pool, nil
}
