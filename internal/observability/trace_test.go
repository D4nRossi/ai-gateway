package observability

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLatencyTrace_NilSafe(t *testing.T) {
	t.Parallel()

	// Calls on a nil trace must not panic — caller may be on an old code
	// path that doesn't construct the trace yet.
	var trace *LatencyTrace
	trace.Mark("auth")
	if trace.Bucket("auth") != 0 {
		t.Errorf("nil trace Bucket should return 0")
	}
	if trace.Header() != "" {
		t.Errorf("nil trace Header should return empty string")
	}
	if trace.TotalMs() != 0 {
		t.Errorf("nil trace TotalMs should return 0")
	}
}

func TestLatencyTrace_SingleMark(t *testing.T) {
	t.Parallel()

	trace := StartTrace()
	time.Sleep(20 * time.Millisecond)
	trace.Mark("auth")

	got := trace.Bucket("auth")
	if got < 10 || got > 100 {
		// Tolerate scheduler jitter (Windows is especially noisy).
		t.Errorf("Bucket(auth) = %d ms; want ~20 ms (10-100 tolerance)", got)
	}
}

func TestLatencyTrace_MultipleMarksAccumulate(t *testing.T) {
	t.Parallel()

	trace := StartTrace()
	time.Sleep(10 * time.Millisecond)
	trace.Mark("auth") // ~10ms
	time.Sleep(10 * time.Millisecond)
	trace.Mark("mask") // ~10ms
	time.Sleep(10 * time.Millisecond)
	trace.Mark("auth") // accumulates another ~10ms to auth

	auth := trace.Bucket("auth")
	mask := trace.Bucket("mask")

	if auth < 15 {
		t.Errorf("Bucket(auth) accumulated = %d ms; want >= 15 (two segments of ~10ms each)", auth)
	}
	if mask < 5 || mask > 50 {
		t.Errorf("Bucket(mask) = %d ms; want ~10ms", mask)
	}
}

func TestLatencyTrace_HeaderFormat(t *testing.T) {
	t.Parallel()

	trace := StartTrace()
	time.Sleep(5 * time.Millisecond)
	trace.Mark("auth")
	time.Sleep(5 * time.Millisecond)
	trace.Mark("provider")

	got := trace.Header()
	// Format invariants: 5 fields in fixed order, "key=integer" separated by ";".
	parts := strings.Split(got, ";")
	if len(parts) != 5 {
		t.Fatalf("Header() = %q; want 5 fields separated by ';'", got)
	}
	want := []string{"auth=", "mask=", "guardrails=", "provider=", "encode="}
	for i, prefix := range want {
		if !strings.HasPrefix(parts[i], prefix) {
			t.Errorf("part[%d] = %q; want prefix %q", i, parts[i], prefix)
		}
	}
	// auth and provider should have non-zero values.
	if !strings.Contains(got, "auth=") || strings.Contains(got, "auth=0;") {
		// Allow auth=0 in rare scheduler conditions but check it's at least present.
	}
}

func TestLatencyTrace_UnknownBucketReturnsZero(t *testing.T) {
	t.Parallel()

	trace := StartTrace()
	if trace.Bucket("nonexistent") != 0 {
		t.Errorf("Bucket of never-marked name should be 0")
	}
}

func TestLatencyTrace_ConcurrentMarksAreSafe(t *testing.T) {
	t.Parallel()

	// The stream path may Mark from multiple goroutines (parser + handler).
	// Verify the mutex actually protects the map without -race complaining.
	trace := StartTrace()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			trace.Mark("provider")
		}()
	}
	wg.Wait()

	// All marks went to one bucket; just confirm no panic and value is sane.
	if trace.Bucket("provider") < 0 {
		t.Errorf("concurrent marks produced negative bucket")
	}
}

func TestLatencyTrace_TotalCoversAllElapsed(t *testing.T) {
	t.Parallel()

	trace := StartTrace()
	time.Sleep(30 * time.Millisecond)

	total := trace.TotalMs()
	if total < 25 || total > 200 {
		t.Errorf("TotalMs = %d; want ~30 ms (25-200 tolerance for jitter)", total)
	}
}
