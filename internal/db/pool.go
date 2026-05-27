// Package db provides SQL Server connection setup and migration running for
// the AI Gateway (ADR-0022).
//
// References:
//   - SPEC.md §16 steps 4–5 — bootstrap: pool + migrations
//   - ADR-0022 — troca PostgreSQL → SQL Server
//   - https://github.com/microsoft/go-mssqldb
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"

	// Registers the "sqlserver" driver with database/sql.
	_ "github.com/microsoft/go-mssqldb"

	"github.com/D4nRossi/ai-gateway/internal/config"
)

// NewMSSQL opens a SQL Server connection using database/sql and the
// microsoft/go-mssqldb driver. The connection is validated with a Ping before
// returning so the gateway exits at boot rather than at first request if the
// database is unreachable.
//
// References:
//   - SPEC.md §16 step 4
//   - ADR-0022 — driver e config estruturado
//   - https://pkg.go.dev/github.com/microsoft/go-mssqldb
func NewMSSQL(ctx context.Context, cfg config.DatabaseConfig) (*sql.DB, error) {
	connStr := ConnString(cfg)

	dbHandle, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, fmt.Errorf("opening sqlserver connection: %w", err)
	}

	// SetMaxOpenConns caps the total connections (active + idle).
	// SetMaxIdleConns caps the idle pool — there is no hard minimum on
	// database/sql, so MinConns from config becomes "max idle" here (closest
	// equivalent to pgxpool's MinConns).
	if cfg.MaxConns > 0 {
		dbHandle.SetMaxOpenConns(cfg.MaxConns)
	}
	if cfg.MinConns > 0 {
		dbHandle.SetMaxIdleConns(cfg.MinConns)
	}

	if err := dbHandle.PingContext(ctx); err != nil {
		_ = dbHandle.Close()
		return nil, fmt.Errorf("pinging sqlserver at %s:%d: %w", cfg.Host, effectivePort(cfg), err)
	}

	return dbHandle, nil
}

// ConnString builds the sqlserver:// URL the driver expects.
//
// Reasoning: assembling via net/url ensures the password (which can contain
// '@', ':', '/' coming from Key Vault) is properly percent-encoded. Manual
// string concatenation here would be a sharp edge.
//
// SECURITY: the returned string contains the database password — never log it.
//
// References:
//   - https://github.com/microsoft/go-mssqldb#connection-parameters-and-dsn
func ConnString(cfg config.DatabaseConfig) string {
	q := url.Values{}
	q.Set("database", cfg.Database)
	// Application name shows up in SQL Server traces (sys.dm_exec_sessions)
	// — useful for the DBA to identify gateway traffic.
	q.Set("app name", "ai-gateway")
	if cfg.Encrypt {
		q.Set("encrypt", "true")
	} else {
		q.Set("encrypt", "disable")
	}
	if cfg.TrustServerCertificate {
		q.Set("TrustServerCertificate", "true")
	}

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(effectivePort(cfg))),
		RawQuery: q.Encode(),
	}
	return u.String()
}

// effectivePort returns cfg.Port when explicitly set, otherwise the SQL Server
// default 1433.
func effectivePort(cfg config.DatabaseConfig) int {
	if cfg.Port > 0 {
		return cfg.Port
	}
	return 1433
}
