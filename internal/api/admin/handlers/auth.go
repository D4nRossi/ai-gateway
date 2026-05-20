package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	adminmw "github.com/D4nRossi/ai-gateway/internal/api/admin/middleware"
	"github.com/D4nRossi/ai-gateway/internal/app/adminservice"
)

// loginRequest is the JSON body for POST /admin/v1/auth/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginResponse is the JSON body returned on successful login.
// Token is the raw session token and is shown exactly once — the client must store it.
type loginResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	Role      string `json:"role"`
}

// Login handles POST /admin/v1/auth/login.
//
// On success it returns 200 with the raw session token (shown once), the session
// expiry, and the user's role. The token must be sent as "Authorization: Bearer <token>"
// on subsequent admin requests.
//
// Returns 401 for unknown usernames and wrong passwords (no enumeration via ADR-0011).
//
// References:
//   - ADR-0011 — opaque session token, raw token returned once
func Login(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}
		if req.Username == "" || req.Password == "" {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "username and password are required")
			return
		}

		rawToken, session, user, err := svc.Login(r.Context(), req.Username, req.Password)
		if err != nil {
			if errors.Is(err, adminservice.ErrInvalidCredentials) {
				writeAdminError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
				return
			}
			writeAdminError(w, http.StatusInternalServerError, "internal", "login failed")
			return
		}

		writeJSON(w, http.StatusOK, loginResponse{
			Token:     rawToken,
			ExpiresAt: session.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
			Role:      string(user.Role),
		})
	}
}

// Logout handles DELETE /admin/v1/auth/logout.
//
// Revokes the current session identified by the Bearer token in the request.
// The SessionAuth middleware must run before this handler.
//
// Returns 204 No Content on success.
func Logout(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := adminmw.SessionFromCtx(r.Context())
		if !ok {
			writeAdminError(w, http.StatusUnauthorized, "unauthorized", "no active session")
			return
		}

		if err := svc.Logout(r.Context(), session.ID); err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "logout failed")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
