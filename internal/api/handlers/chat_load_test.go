package handlers

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4nRossi/ai-gateway/internal/auth"
	"github.com/D4nRossi/ai-gateway/internal/providers/mock"
)

// ── Load & concurrency tests ──────────────────────────────────────────────────

// TestChat_ConcurrentLoad fires N parallel requests and verifies all complete
// successfully without races or panics.
//
// Run with: go test -race ./internal/api/handlers/... -run TestChat_ConcurrentLoad
func TestChat_ConcurrentLoad(t *testing.T) {
	t.Parallel()
	const concurrency = 50
	const requestsPerWorker = 10

	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	var (
		wg      sync.WaitGroup
		success atomic.Int64
		failed  atomic.Int64
	)

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < requestsPerWorker; i++ {
				body := jsonBody(t, map[string]any{
					"model":    "gpt-4.1-nano",
					"messages": []map[string]string{{"role": "user", "content": "hello"}},
				})
				req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code == http.StatusOK {
					success.Add(1)
				} else {
					failed.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	total := success.Load() + failed.Load()
	expected := int64(concurrency * requestsPerWorker)
	if total != expected {
		t.Errorf("total requests processed = %d; want %d", total, expected)
	}
	if failed.Load() > 0 {
		t.Errorf("%d requests failed (want 0 failures with mock provider)", failed.Load())
	}
}

// TestChat_LatencyDistribution measures P50/P95/P99 latency of the mock handler
// over many sequential requests.
//
// This is a correctness + performance characterisation test, not a hard SLA.
// It fails only if the median exceeds a generous 50 ms threshold
// (mock provider has zero I/O, so latency is pure Go overhead).
func TestChat_LatencyDistribution(t *testing.T) {
	t.Parallel()
	const iterations = 200

	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	latencies := make([]time.Duration, 0, iterations)
	for i := 0; i < iterations; i++ {
		body := jsonBody(t, map[string]any{
			"model":    "gpt-4.1-nano",
			"messages": []map[string]string{{"role": "user", "content": "latency test"}},
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		start := time.Now()
		handler.ServeHTTP(rec, req)
		elapsed := time.Since(start)

		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status %d at iteration %d", rec.Code, i)
		}
		latencies = append(latencies, elapsed)
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50 := latencies[int(math.Round(float64(iterations)*0.50))-1]
	p95 := latencies[int(math.Round(float64(iterations)*0.95))-1]
	p99 := latencies[int(math.Round(float64(iterations)*0.99))-1]

	t.Logf("Latency distribution over %d requests:", iterations)
	t.Logf("  P50 = %v", p50)
	t.Logf("  P95 = %v", p95)
	t.Logf("  P99 = %v", p99)

	// Guard: mock provider should complete well within 50 ms at P50.
	const p50Limit = 50 * time.Millisecond
	if p50 > p50Limit {
		t.Errorf("P50 latency %v exceeds limit %v — possible performance regression", p50, p50Limit)
	}
}

// TestChat_TierPipeline_Latency measures handler latency across all three tiers.
// Higher tiers run more guards; the test logs the delta so regressions are visible.
func TestChat_TierPipeline_Latency(t *testing.T) {
	t.Parallel()
	const iterations = 100

	tiers := []struct {
		tier   string
		models []string
	}{
		{"tier_1", []string{"gpt-4.1-nano"}},
		{"tier_2", []string{"gpt-4.1-nano"}},
		{"tier_3", []string{"gpt-4.1-nano"}},
	}

	// Content with PII so the masker actually works.
	content := "Meu CPF é 529.982.247-25 e preciso de ajuda."

	for _, tc := range tiers {
		tc := tc
		t.Run(tc.tier, func(t *testing.T) {
			t.Parallel()
			policy := auth.AppPolicy{
				Name:             "LatencyApp",
				KeyPrefix:        "gwk_lat",
				Tier:             tc.tier,
				AllowedModels:    tc.models,
				StreamingAllowed: false,
				MaxRPM:           9999,
				MonthlyBudgetBRL: 9999,
			}
			h := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

			latencies := make([]time.Duration, 0, iterations)
			for i := 0; i < iterations; i++ {
				body := jsonBody(t, map[string]any{
					"model":    tc.models[0],
					"messages": []map[string]string{{"role": "user", "content": content}},
				})
				req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()

				start := time.Now()
				h.ServeHTTP(rec, req)
				latencies = append(latencies, time.Since(start))
			}

			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			p50 := latencies[iterations/2]
			p99 := latencies[int(float64(iterations)*0.99)]
			t.Logf("%s: P50=%v P99=%v", tc.tier, p50, p99)
		})
	}
}

// ── Error path load tests ─────────────────────────────────────────────────────

// TestChat_PolicyErrors_Concurrent fires policy-failing requests in parallel
// to ensure error paths are goroutine-safe.
func TestChat_PolicyErrors_Concurrent(t *testing.T) {
	t.Parallel()
	const goroutines = 30

	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	var wg sync.WaitGroup
	var got403 atomic.Int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := jsonBody(t, map[string]any{
				"model":    "gpt-4.1-mini", // not in allowlist
				"messages": []map[string]string{{"role": "user", "content": "hi"}},
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusForbidden {
				got403.Add(1)
			}
		}()
	}
	wg.Wait()

	if got403.Load() != goroutines {
		t.Errorf("got %d 403s; want %d", got403.Load(), goroutines)
	}
}

// TestChat_BudgetDenial_Concurrent fires budget-denied requests in parallel.
func TestChat_BudgetDenial_Concurrent(t *testing.T) {
	t.Parallel()
	const goroutines = 30

	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(denyBudget{}, mock.New()))

	var wg sync.WaitGroup
	var got429 atomic.Int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := jsonBody(t, map[string]any{
				"model":    "gpt-4.1-nano",
				"messages": []map[string]string{{"role": "user", "content": "hi"}},
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusTooManyRequests {
				got429.Add(1)
			}
		}()
	}
	wg.Wait()

	if got429.Load() != goroutines {
		t.Errorf("got %d 429s; want %d", got429.Load(), goroutines)
	}
}

// ── Benchmarks ────────────────────────────────────────────────────────────────

func BenchmarkChat_NonStream_Tier1(b *testing.B) {
	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			body := jsonBodyB(map[string]any{
				"model":    "gpt-4.1-nano",
				"messages": []map[string]string{{"role": "user", "content": "hello"}},
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}
	})
}

func BenchmarkChat_NonStream_Tier2(b *testing.B) {
	policy := testPolicy("tier_2", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			body := jsonBodyB(map[string]any{
				"model":    "gpt-4.1-nano",
				"messages": []map[string]string{{"role": "user", "content": "hello"}},
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}
	})
}

func BenchmarkChat_NonStream_Tier3(b *testing.B) {
	policy := testPolicy("tier_3", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			body := jsonBodyB(map[string]any{
				"model":    "gpt-4.1-nano",
				"messages": []map[string]string{{"role": "user", "content": "hello"}},
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}
	})
}

func BenchmarkChat_WithPII_Tier1(b *testing.B) {
	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			body := jsonBodyB(map[string]any{
				"model": "gpt-4.1-nano",
				"messages": []map[string]string{{
					"role":    "user",
					"content": "Meu CPF é 529.982.247-25 e cartão 4111 1111 1111 1111.",
				}},
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}
	})
}

func BenchmarkChat_PolicyDenial(b *testing.B) {
	policy := testPolicy("tier_1", []string{"gpt-4.1-nano"}, false)
	handler := injectPolicy(policy, chatHandler(allowBudget{}, mock.New()))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := jsonBodyB(map[string]any{
			"model":    "gpt-4.1-mini", // not in allowlist
			"messages": []map[string]string{{"role": "user", "content": "hi"}},
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// jsonBodyB is the benchmark variant of jsonBody — does not use testing.T.
func jsonBodyB(v any) *readCloser {
	b, _ := json.Marshal(v)
	return &readCloser{data: b, pos: 0}
}

// readCloser is a minimal io.ReadCloser backed by a byte slice.
type readCloser struct {
	data []byte
	pos  int
}

func (r *readCloser) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *readCloser) Close() error { return nil }
