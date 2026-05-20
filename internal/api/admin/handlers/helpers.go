// Package handlers implements the HTTP handlers for the Admin API.
//
// Handlers are thin: they decode request bodies, call adminservice methods, and
// serialize responses. No business logic lives here (ADR-0015).
//
// All handlers use writeJSON/writeAdminError for consistent JSON envelopes, and
// parseID for chi URL parameter parsing. Errors are mapped to HTTP status codes
// using errors.Is against domain-package sentinels.
//
// References:
//   - ADR-0009 — DB-backed admin plane
//   - ADR-0015 — handlers are thin; business logic lives in adminservice
package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// apiError is the standard JSON error envelope for all Admin API error responses.
type apiError struct {
	Error apiErrorDetail `json:"error"`
}

// apiErrorDetail carries a machine-readable code and a human-readable message.
type apiErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON serialises v as JSON and writes it with the given HTTP status code.
// On marshal failure it falls back to a 500 internal-error response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal","message":"response serialization failed"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeAdminError writes a standardised JSON error response.
func writeAdminError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiError{Error: apiErrorDetail{Code: code, Message: message}})
}

// parseID reads the named chi URL parameter and parses it as int64.
// On success it returns the value and true. On failure it writes a 400 response
// and returns 0, false so the caller can return immediately.
func parseID(w http.ResponseWriter, r *http.Request, param string) (int64, bool) {
	s := chi.URLParam(r, param)
	if s == "" {
		writeAdminError(w, http.StatusBadRequest, "invalid_param", param+" is required")
		return 0, false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		writeAdminError(w, http.StatusBadRequest, "invalid_param", param+" must be a positive integer")
		return 0, false
	}
	return id, true
}
