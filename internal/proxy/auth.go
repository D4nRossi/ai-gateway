package proxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/D4nRossi/ai-gateway/internal/auth"
	"github.com/D4nRossi/ai-gateway/internal/domain/application"
)

// Auth is the proxy-plane Bearer-token middleware. It looks up the calling
// application in the DB by key prefix, verifies the SHA-256 hash with a
// constant-time comparison (CLAUDE.md §5.6), and injects the matched Application
// into the request context.
//
// Returns 401 Unauthorized for:
//   - missing/malformed Authorization header
//   - tokens that do not start with "gwk_"
//   - unknown key prefixes
//   - hash mismatches
//   - inactive applications
//
// Reasoning: the Phase 1 middleware in api/middleware/auth.go reads from a YAML
// PolicyStore; the V2 proxy plane authenticates against the DB so admin-created
// applications work without a restart. The two middlewares coexist — they apply
// to different route trees (/v1/chat/completions vs /v1/proxy/*).
//
// References:
//   - ADR-0009 — DB-backed admin plane
//   - CLAUDE.md §1.4 — never log the full token, only key_prefix
//   - CLAUDE.md §5.6 — constant-time comparison for credentials
func Auth(apps application.Repository, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := extractBearer(r)
			if tok == "" || !strings.HasPrefix(tok, "gwk_") {
				writeProxyError(w, http.StatusUnauthorized, "unauthorized", "missing or malformed bearer token")
				return
			}

			prefix := auth.ExtractPrefix(tok)

			key, err := apps.GetAPIKeyByPrefix(r.Context(), prefix)
			if err != nil {
				if errors.Is(err, application.ErrNotFound) {
					writeProxyError(w, http.StatusUnauthorized, "unauthorized", "unknown key prefix")
					return
				}
				logger.Error("proxy auth: api key lookup failed", "err", err, "key_prefix", prefix)
				writeProxyError(w, http.StatusInternalServerError, "internal", "authentication error")
				return
			}

			sum := sha256.Sum256([]byte(tok))
			want, err := hex.DecodeString(key.KeyHash)
			if err != nil || len(want) != sha256.Size {
				logger.Error("proxy auth: malformed stored key_hash",
					"key_prefix", prefix,
					"application_id", key.ApplicationID,
				)
				writeProxyError(w, http.StatusUnauthorized, "unauthorized", "credential configuration error")
				return
			}
			if subtle.ConstantTimeCompare(sum[:], want) != 1 {
				writeProxyError(w, http.StatusUnauthorized, "unauthorized", "token mismatch")
				return
			}

			app, err := apps.Get(r.Context(), key.ApplicationID)
			if err != nil {
				logger.Error("proxy auth: application load failed",
					"err", err,
					"application_id", key.ApplicationID,
				)
				writeProxyError(w, http.StatusInternalServerError, "internal", "authentication error")
				return
			}
			if !app.Active {
				writeProxyError(w, http.StatusUnauthorized, "unauthorized", "application is inactive")
				return
			}

			next.ServeHTTP(w, r.WithContext(withApplication(r.Context(), app)))
		})
	}
}

// extractBearer parses "Authorization: Bearer <tok>" and returns the token,
// or "" when the header is absent or uses a different scheme.
func extractBearer(r *http.Request) string {
	hdr := r.Header.Get("Authorization")
	tok, found := strings.CutPrefix(hdr, "Bearer ")
	if !found || tok == "" {
		return ""
	}
	return tok
}

// writeProxyError writes a small JSON error envelope. It deliberately avoids
// importing internal/api/admin/handlers to prevent a layering cycle.
func writeProxyError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Both code and message are package-level constants; no escaping needed.
	_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}`))
}
