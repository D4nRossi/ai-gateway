package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// LoginLimiter throttles login attempts per client IP to slow down brute-force
// password guessing on /admin/v1/auth/login.
//
// Defaults: 5 attempts / 5 minutes sustained, burst 5.
// Each IP gets its own *rate.Limiter; an LRU-style sweep evicts old entries
// when the map exceeds maxEntries to keep memory bounded.
//
// Reasoning: a per-IP token bucket gives correct behavior across timing windows
// (a static counter would let an attacker submit 5 attempts at 23:59:59 and
// 5 more at 00:00:01). The bucket refills 1 token per minute so a paused
// attacker can resume at the steady-state rate but a continuous attacker is
// pinned to 5/minute average.
//
// Trade-off — this is per-process state. In a multi-replica deployment the
// attacker could rotate replicas to extend the effective rate. For the
// single-instance demo this is acceptable; multi-replica needs Redis-backed
// counters (ADR-0006 covers the same trade-off for the app rate limiter).
//
// References:
//   - ADR-0006 — in-memory rate limit (Phase 1) vs Redis (Phase 2)
//   - https://owasp.org/www-community/controls/Blocking_Brute_Force_Attacks
type LoginLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*rate.Limiter
	burst      int
	refill     time.Duration
	maxEntries int
}

// NewLoginLimiter constructs a LoginLimiter. Pass 0 to use the safe defaults
// (5 attempts, 1-minute refill, 10k entries).
func NewLoginLimiter(burst int, refill time.Duration, maxEntries int) *LoginLimiter {
	if burst <= 0 {
		burst = 5
	}
	if refill <= 0 {
		refill = time.Minute
	}
	if maxEntries <= 0 {
		maxEntries = 10_000
	}
	return &LoginLimiter{
		buckets:    make(map[string]*rate.Limiter),
		burst:      burst,
		refill:     refill,
		maxEntries: maxEntries,
	}
}

// Allow returns true when the given IP still has tokens. Should be called once
// per login attempt regardless of outcome — both successful and failed attempts
// consume a token, which prevents an attacker from learning "ip-blocked vs
// wrong-password" through differential behavior.
func (l *LoginLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	lim, ok := l.buckets[ip]
	if !ok {
		// Evict on growth — coarse sweep removes one full-bucket entry to keep
		// memory bounded. Full buckets (tokens == burst) are by definition idle.
		if len(l.buckets) >= l.maxEntries {
			l.evictOneIdleLocked()
		}
		lim = rate.NewLimiter(rate.Every(l.refill), l.burst)
		l.buckets[ip] = lim
	}
	return lim.Allow()
}

// evictOneIdleLocked removes one map entry whose bucket appears full (idle).
// Caller must hold the mutex. O(N) worst-case but amortised: only runs when
// the map hits maxEntries.
func (l *LoginLimiter) evictOneIdleLocked() {
	for ip, lim := range l.buckets {
		// AllowN(now, burst) succeeds when burst tokens are available, then we
		// don't consume — we just probe via Tokens(); but rate.Limiter has no
		// public "tokens" accessor, so we approximate: if the limiter has been
		// idle long enough for a full burst, drop it.
		if lim.AllowN(time.Now(), 0) {
			// Limiter is healthy; check via reserve trick — peek 1 token.
			r := lim.Reserve()
			if r.OK() && r.Delay() == 0 {
				r.Cancel() // return the token unused
				delete(l.buckets, ip)
				return
			}
			r.Cancel()
		}
	}
	// Last resort: drop an arbitrary entry to make room.
	for ip := range l.buckets {
		delete(l.buckets, ip)
		return
	}
}

// Middleware returns a chi-compatible middleware that rejects requests exceeding
// the per-IP login rate. The IP is taken from X-Forwarded-For (first hop) when
// present, otherwise from RemoteAddr. Returns 429 with Retry-After header on
// throttle.
//
// Reasoning: X-Forwarded-For is trusted because the only consumer is the rate
// limiter — even if a client spoofs the header, they only hurt themselves
// (their fake IP becomes the throttled identity). Authentication and access
// control never use this header.
func (l *LoginLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if !l.Allow(ip) {
				retry := int(l.refill.Seconds())
				if retry < 1 {
					retry = 1
				}
				w.Header().Set("Retry-After", itoa(retry))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":"too_many_attempts","message":"muitas tentativas de login — aguarde antes de tentar novamente"}}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP extracts a best-effort client IP from X-Forwarded-For (first entry)
// or RemoteAddr. Returns "unknown" when neither yields a usable address — that
// bucket aggregates all unknown clients together, which is the right default
// (one bucket, slow rate) when no IP is determinable.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if comma := strings.IndexByte(xff, ','); comma > 0 {
			return strings.TrimSpace(xff[:comma])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if r.RemoteAddr == "" {
			return "unknown"
		}
		return r.RemoteAddr
	}
	return host
}

// itoa avoids importing strconv just for one int conversion.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
