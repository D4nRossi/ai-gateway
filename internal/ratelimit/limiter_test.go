package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
)

// ── newLimiter internals ──────────────────────────────────────────────────────

func TestNewLimiter_ZeroRPM_DenyAll(t *testing.T) {
	t.Parallel()
	l := newLimiter(0)
	// rate.NewLimiter(0, 0) always denies.
	if l.Allow() {
		t.Error("newLimiter(0).Allow() = true; want false (zero-RPM denies all)")
	}
}

func TestNewLimiter_NegativeRPM_DenyAll(t *testing.T) {
	t.Parallel()
	l := newLimiter(-1)
	if l.Allow() {
		t.Error("newLimiter(-1).Allow() = true; want false")
	}
}

func TestNewLimiter_BurstFloor(t *testing.T) {
	t.Parallel()
	// rpm=5 → burst = 5/10 = 0, clamped to 1.
	// The first Allow() must succeed (burst=1).
	l := newLimiter(5)
	if !l.Allow() {
		t.Error("first Allow() on rpm=5 limiter returned false; want true (burst≥1)")
	}
}

func TestNewLimiter_BurstRPMDividedBy10(t *testing.T) {
	t.Parallel()
	// rpm=100 → burst = 10; consume 10 tokens quickly — all should pass.
	l := newLimiter(100)
	allowed := 0
	for i := 0; i < 10; i++ {
		if l.Allow() {
			allowed++
		}
	}
	if allowed < 10 {
		t.Errorf("expected 10 burst allows for rpm=100, got %d", allowed)
	}
	// 11th in the same instant must be denied.
	if l.Allow() {
		t.Error("11th immediate Allow() should be denied after burst exhausted")
	}
}

// ── Manager registration and lookup ──────────────────────────────────────────

func TestManager_RegisterAndAllow(t *testing.T) {
	t.Parallel()
	m := NewManager()
	m.Register("TestApp", 600)
	if !m.Allow("TestApp") {
		t.Error("Allow after Register returned false; want true")
	}
}

func TestManager_UnknownApp_Denied(t *testing.T) {
	t.Parallel()
	m := NewManager()
	// Never registered — must deny by default.
	if m.Allow("NonExistent") {
		t.Error("Allow for unregistered app returned true; want false")
	}
}

func TestManager_MultipleApps(t *testing.T) {
	t.Parallel()
	m := NewManager()
	m.Register("AppA", 600)
	m.Register("AppB", 60)

	if !m.Allow("AppA") {
		t.Error("AppA: want true")
	}
	if !m.Allow("AppB") {
		t.Error("AppB: want true")
	}
	// Unknown app still denied.
	if m.Allow("AppC") {
		t.Error("AppC (unknown): want false")
	}
}

func TestManager_RateLimitEnforced(t *testing.T) {
	t.Parallel()
	// Register with rpm=6 → burst = max(1, 6/10) = 1.
	// Only 1 instant token; second call should fail.
	m := NewManager()
	m.Register("LowRPM", 6)

	first := m.Allow("LowRPM")
	second := m.Allow("LowRPM")

	if !first {
		t.Error("first Allow should succeed")
	}
	if second {
		t.Error("second immediate Allow should fail (burst=1 exhausted)")
	}
}

// TestManager_RateLimitEnforcedMultiApp verifies independent rate limits per app.
// Each app's limiter is independent — exhausting one must not affect another.
func TestManager_RateLimitEnforcedMultiApp(t *testing.T) {
	t.Parallel()
	m := NewManager()
	// rpm=6 → burst=max(1, 6/10)=1 — each app gets exactly 1 instant token.
	m.Register("AppX", 6)
	m.Register("AppY", 6)

	// Exhaust AppX's burst.
	if !m.Allow("AppX") {
		t.Fatal("AppX first Allow should succeed")
	}
	if m.Allow("AppX") {
		t.Error("AppX second Allow should fail (burst=1 exhausted)")
	}

	// AppY must still be allowed (independent limiter).
	if !m.Allow("AppY") {
		t.Error("AppY should not be affected by AppX exhaustion")
	}
}

// TestManager_Concurrent ensures no data races under parallel Allow() calls.
// Run with: go test -race ./internal/ratelimit/...
func TestManager_Concurrent(t *testing.T) {
	t.Parallel()
	m := NewManager()
	m.Register("ConcApp", 6000) // high RPM to minimise denials

	var wg sync.WaitGroup
	var allowed, denied atomic.Int64
	const goroutines = 50
	const callsPerGoroutine = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				if m.Allow("ConcApp") {
					allowed.Add(1)
				} else {
					denied.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	total := allowed.Load() + denied.Load()
	if total != goroutines*callsPerGoroutine {
		t.Errorf("total calls = %d; want %d", total, goroutines*callsPerGoroutine)
	}
}

// ── Benchmarks ────────────────────────────────────────────────────────────────

func BenchmarkManager_Allow_Allowed(b *testing.B) {
	m := NewManager()
	m.Register("BenchApp", 1_000_000) // effectively unlimited
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.Allow("BenchApp")
		}
	})
}

func BenchmarkManager_Allow_Unknown(b *testing.B) {
	m := NewManager()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Allow("NoSuchApp")
	}
}

func BenchmarkNewLimiter(b *testing.B) {
	for i := 0; i < b.N; i++ {
		newLimiter(600)
	}
}
