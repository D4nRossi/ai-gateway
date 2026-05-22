package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/D4nRossi/ai-gateway/internal/app/adminservice"
	"github.com/D4nRossi/ai-gateway/internal/domain/admin"
)

// adminUserResponse is the safe JSON representation of an AdminUser.
// PasswordHash is intentionally omitted.
type adminUserResponse struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toAdminUserResponse(u admin.AdminUser) adminUserResponse {
	return adminUserResponse{
		ID:        u.ID,
		Username:  u.Username,
		Role:      string(u.Role),
		Active:    u.Active,
		CreatedAt: u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt: u.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// createUserRequest is the JSON body for POST /admin/v1/users.
type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// ListUsers handles GET /admin/v1/users (admin role only).
// Returns all admin users (active and inactive).
func ListUsers(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := svc.ListAdminUsers(r.Context())
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to list users")
			return
		}

		resp := make([]adminUserResponse, len(users))
		for i, u := range users {
			resp[i] = toAdminUserResponse(u)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// CreateUser handles POST /admin/v1/users (admin role only).
// Creates a new admin user. The plaintext password is hashed by the service layer.
func CreateUser(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}
		if req.Username == "" || req.Password == "" || req.Role == "" {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "username, password, and role are required")
			return
		}

		role := admin.Role(req.Role)
		switch role {
		case admin.RoleAdmin, admin.RoleOperator, admin.RoleViewer:
		default:
			writeAdminError(w, http.StatusBadRequest, "invalid_role", "role must be admin, operator, or viewer")
			return
		}

		user, err := svc.CreateAdminUser(r.Context(), req.Username, req.Password, role)
		if err != nil {
			if status, code, msg, details, ok := translatePgError(err); ok {
				writeAdminErrorWithDetails(w, status, code, msg, details)
				return
			}
			writeAdminErrorWithDetails(
				w, http.StatusInternalServerError,
				"internal", "falha ao criar usuário", err.Error(),
			)
			return
		}

		writeJSON(w, http.StatusCreated, toAdminUserResponse(user))
	}
}

// DeactivateUser handles DELETE /admin/v1/users/{id} (admin role only).
// Soft-deletes the user (active=false) and revokes all their sessions.
func DeactivateUser(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r, "id")
		if !ok {
			return
		}

		if err := svc.DeactivateAdminUser(r.Context(), id); err != nil {
			if errors.Is(err, admin.ErrNotFound) {
				writeAdminError(w, http.StatusNotFound, "not_found", "admin user not found")
				return
			}
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to deactivate user")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
