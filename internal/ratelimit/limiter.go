// Package ratelimit provides per-application token bucket rate limiting.
//
// One *rate.Limiter is allocated per application on first use and cached for
// the lifetime of the process. The in-memory approach is intentional for Phase 1;
// a Redis-backed distributed limiter is planned for Phase 2 (see ADR-0006).
//
// References:
//   - SPEC.md §12.1 — rate limit specification
//   - ADR-0006 — in-memory rate limit (Phase 1) vs. Redis (Phase 2)
//   - https://pkg.go.dev/golang.org/x/time/rate
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter is the interface satisfied by Manager and any test stub.
// Decoupling the RateLimit middleware from the concrete in-memory Manager
// enables unit testing and future Redis-backed implementations without
// changing call sites (see ADR-0006).
//
// References:
//   - SPEC.md §12.1
//   - CLAUDE.md §14 — testability via interface injection
type Limiter interface {
	Allow(appName string) bool
}

// Manager holds a per-application rate limiter map.
// Safe for concurrent use.
type Manager struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	configs  map[string]appCfg
}

type appCfg struct {
	rpm int
}

// NewManager creates an empty Manager. Limiters are created on first Allow call.
func NewManager() *Manager {
	return &Manager{
		limiters: make(map[string]*rate.Limiter),
		configs:  make(map[string]appCfg),
	}
}

// Register pre-registers an application and its RPM limit.
// Must be called at bootstrap before the server starts accepting traffic.
func (m *Manager) Register(appName string, maxRPM int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[appName] = appCfg{rpm: maxRPM}
	m.limiters[appName] = newLimiter(maxRPM)
}

// Allow reports whether the application identified by appName is within its
// rate limit. Returns false if the request should be rejected with 429.
//
// References:
//   - SPEC.md §12.1 — rate.Limiter usage
func (m *Manager) Allow(appName string) bool {
	m.mu.Lock()
	l, ok := m.limiters[appName]
	if !ok {
		// Unknown application: deny by default (should not happen after Register).
		m.mu.Unlock()
		return false
	}
	m.mu.Unlock()
	return l.Allow()
}

// newLimiter constructs a token bucket limiter for maxRPM requests per minute.
//
// Constructor:  rate.NewLimiter(rate.Every(minute/rpm), burst)
// Burst:        max(1, rpm/10) — allows short bursts up to 10% of the per-minute limit.
//
// Reasoning: burst absorbs momentary spikes without permanently exceeding the
// average rate. 10% of RPM is conservative enough for most workloads.
//
// References:
//   - SPEC.md §12.1
func newLimiter(rpm int) *rate.Limiter {
	if rpm <= 0 {
		// Fallback: deny all (effectively disabled app).
		return rate.NewLimiter(0, 0)
	}
	interval := time.Minute / time.Duration(rpm)
	burst := rpm / 10
	if burst < 1 {
		burst = 1
	}
	return rate.NewLimiter(rate.Every(interval), burst)
}
