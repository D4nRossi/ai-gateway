// Package observability provides the slog logger factory and context helpers
// used across all gateway packages.
//
// References:
//   - SPEC.md §13.1 — logging requirements
//   - ADR-0003 — slog as the official logger
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

type ctxKey string

const (
	// RequestIDKey is the context key under which the request UUID is stored.
	RequestIDKey ctxKey = "request_id"

	// LoggerKey is the context key under which the request-scoped logger is stored.
	LoggerKey ctxKey = "logger"
)

// New builds a *slog.Logger for the given level and output format.
// level must be one of: debug, info, warn, error.
// format must be one of: json, text (empty string defaults to json).
//
// Reasoning: logger is constructed once at boot from config values so that
// verbosity and output mode are controlled by the operator without code changes.
//
// References:
//   - SPEC.md §13.1
//   - ADR-0003
func New(level, format string) (*slog.Logger, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", level, err)
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	switch format {
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	case "json", "":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		return nil, fmt.Errorf("invalid log format %q; must be json or text", format)
	}
	return slog.New(handler), nil
}

// WithRequestID returns a child logger with the request_id field pre-attached,
// extracted from ctx if present.
//
// References:
//   - SPEC.md §13.1 — request_id required in all request lifecycle logs
func WithRequestID(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if v, ok := ctx.Value(RequestIDKey).(string); ok {
		return logger.With("request_id", v)
	}
	return logger
}

// LoggerFrom extracts the request-scoped logger stored in ctx by the logging
// middleware. If no logger is found, fallback is returned unchanged.
func LoggerFrom(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if l, ok := ctx.Value(LoggerKey).(*slog.Logger); ok {
		return l
	}
	return fallback
}
