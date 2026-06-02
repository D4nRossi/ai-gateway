package middleware

import "net/http"

// SecurityHeaders applies a baseline of hardening headers to every response.
//
// Set on ALL responses (API + SPA + healthz):
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: DENY                   (anti-clickjacking, legacy)
//   - Referrer-Policy: strict-origin-when-cross-origin
//   - Permissions-Policy: locked down (no camera/microphone/geolocation/etc.)
//
// Set only when the request arrived over TLS or via a TLS-terminating proxy:
//   - Strict-Transport-Security: max-age=15552000; includeSubDomains
//
// CSP is NOT applied here. The admin SPA needs a different CSP than the JSON
// API (which has no document, no scripts, no styles). The SPA handler in
// web/embed.go owns its own CSP; the API responses go without one, which is
// correct — CSP only applies to documents the browser will render.
//
// Reasoning: applying these at the outermost middleware layer guarantees they
// reach even error responses (401, 429, 5xx) that exit early without touching
// handlers. Adding HSTS conditionally avoids breaking local HTTP dev.
//
// References:
//   - https://owasp.org/www-project-secure-headers/
//   - https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Strict-Transport-Security
//   - https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Permissions-Policy
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Locked-down Permissions-Policy: deny features the admin console never uses.
		h.Set("Permissions-Policy",
			"accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")

		// HSTS only when we know we're behind TLS. r.TLS is set for direct TLS;
		// X-Forwarded-Proto=https indicates a TLS-terminating proxy in front.
		// Sending HSTS over plain HTTP is a no-op per RFC 6797 §7.2 but ugly.
		if isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=15552000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}

func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		return true
	}
	return false
}
