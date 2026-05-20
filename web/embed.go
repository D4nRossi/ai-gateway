package web

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// distFS holds the Vite build output. The `all:` prefix includes dotfiles so a
// placeholder dist/.keep file is enough to satisfy the embed when the JS build
// has not run yet — useful for go-only CI jobs that just check compilation.
//
//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves the embedded admin SPA.
//
// Behavior:
//   - Static assets (anything under /assets/ or matching a real file in dist/)
//     are served with long cache headers since Vite fingerprints filenames.
//   - Every other path falls back to index.html so React Router can handle
//     client-side routes (e.g. /ui/applications) on page refresh.
//   - All responses include a strict Content-Security-Policy and the usual
//     hardening headers (no-sniff, frame-deny, referrer-policy).
//
// The SPA itself, served by Vite with base="/ui/", expects to be mounted at
// /ui. When mounting under a different prefix, the Go router must rewrite the
// inbound request path before invoking this handler (chi's Mount does the
// right thing automatically).
//
// References:
//   - ADR-0014 — single-binary deploy
//   - https://content-security-policy.com/
func Handler() http.Handler {
	root, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Misconfiguration would surface at boot; fall back to 503 so a missing
		// dist directory does not crash the entire gateway.
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "admin UI not built — run `pnpm build` in web/", http.StatusServiceUnavailable)
		})
	}

	fileServer := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip leading slash for fs.FS lookups; e.g. "/assets/foo" → "assets/foo".
		// chi.Mount has already stripped the /ui prefix at this point.
		reqPath := strings.TrimPrefix(r.URL.Path, "/")
		if reqPath == "" {
			reqPath = "index.html"
		}

		setSecurityHeaders(w)

		// If the requested file does NOT exist in dist, this is a client-side
		// route (e.g. /applications). Serve index.html so React Router can pick
		// it up. We don't redirect — that would break direct deep-links.
		if _, err := fs.Stat(root, reqPath); err != nil {
			serveIndex(w, root)
			return
		}

		// Long-cache for hashed assets. Vite emits filenames like
		// `index-abc123.js` so cache invalidation happens automatically on
		// rebuild.
		if strings.HasPrefix(reqPath, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, root fs.FS) {
	f, err := root.Open("index.html")
	if err != nil {
		http.Error(w, "index.html not found in embedded dist", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.Copy(w, f)
}

// setSecurityHeaders applies a conservative baseline of hardening headers.
//
// CSP rationale:
//   - default-src 'self'         → block third-party origins
//   - script-src 'self'          → no inline scripts; Vite produces external chunks
//   - style-src 'self' 'unsafe-inline' → Radix/shadcn inject inline styles for animations
//   - img-src 'self' data:       → allow inline SVG / data URIs
//   - connect-src 'self'         → API calls only to same origin
//   - frame-ancestors 'none'     → prevents clickjacking; equivalent to X-Frame-Options DENY
//
// Reasoning: the SPA never loads scripts from CDNs and never makes cross-origin
// API calls, so a strict CSP is essentially free.
func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; "+
			"script-src 'self'; "+
			"style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data:; "+
			"font-src 'self' data:; "+
			"connect-src 'self'; "+
			"frame-ancestors 'none'; "+
			"base-uri 'self'; "+
			"form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("X-Frame-Options", "DENY")
}

