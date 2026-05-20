package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/D4nRossi/ai-gateway/internal/app/adminservice"
	"github.com/D4nRossi/ai-gateway/internal/domain/application"
)

// applicationResponse is the JSON representation of an Application.
// The APIKey (hash and prefix) are not included — use rotate-key to surface a new token.
type applicationResponse struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	Tier             string   `json:"tier"`
	AllowedModels    []string `json:"allowed_models"`
	StreamingAllowed bool     `json:"streaming_allowed"`
	MaxRPM           int      `json:"max_rpm"`
	MaxTPM           int      `json:"max_tpm"`
	MonthlyBudgetBRL float64  `json:"monthly_budget_brl"`
	Active           bool     `json:"active"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
}

// createApplicationResponse extends applicationResponse with the one-time raw token.
// The token is shown exactly once and never retrievable again (ADR-0009, ADR-0011).
type createApplicationResponse struct {
	applicationResponse
	Token     string `json:"token"`
	KeyPrefix string `json:"key_prefix"`
}

// rotateKeyResponse carries the new raw token returned after a key rotation.
type rotateKeyResponse struct {
	Token     string `json:"token"`
	KeyPrefix string `json:"key_prefix"`
}

func toApplicationResponse(app application.Application) applicationResponse {
	models := app.AllowedModels
	if models == nil {
		models = []string{}
	}
	return applicationResponse{
		ID:               app.ID,
		Name:             app.Name,
		Tier:             string(app.Tier),
		AllowedModels:    models,
		StreamingAllowed: app.StreamingAllowed,
		MaxRPM:           app.MaxRPM,
		MaxTPM:           app.MaxTPM,
		MonthlyBudgetBRL: app.MonthlyBudgetBRL,
		Active:           app.Active,
		CreatedAt:        app.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:        app.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// createApplicationRequest is the JSON body for POST /admin/v1/applications.
type createApplicationRequest struct {
	Name             string   `json:"name"`
	Tier             string   `json:"tier"`
	AllowedModels    []string `json:"allowed_models"`
	StreamingAllowed bool     `json:"streaming_allowed"`
	MaxRPM           int      `json:"max_rpm"`
	MaxTPM           int      `json:"max_tpm"`
	MonthlyBudgetBRL float64  `json:"monthly_budget_brl"`
}

// updateApplicationRequest is the JSON body for PUT /admin/v1/applications/{id}.
type updateApplicationRequest struct {
	Name             string   `json:"name"`
	Tier             string   `json:"tier"`
	AllowedModels    []string `json:"allowed_models"`
	StreamingAllowed bool     `json:"streaming_allowed"`
	MaxRPM           int      `json:"max_rpm"`
	MaxTPM           int      `json:"max_tpm"`
	MonthlyBudgetBRL float64  `json:"monthly_budget_brl"`
	Active           bool     `json:"active"`
}

// ListApplications handles GET /admin/v1/applications.
// Returns all applications (active and inactive), ordered by name.
func ListApplications(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apps, err := svc.ListApplications(r.Context())
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to list applications")
			return
		}

		resp := make([]applicationResponse, len(apps))
		for i, a := range apps {
			resp[i] = toApplicationResponse(a)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// CreateApplication handles POST /admin/v1/applications.
//
// Creates a new application with an initial API key. The raw token is returned in
// the response body and never stored — the caller must save it immediately.
//
// References:
//   - ADR-0009 — raw token shown once on create and rotate
func CreateApplication(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createApplicationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}
		if req.Name == "" {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "name is required")
			return
		}

		tier := application.TierLevel(req.Tier)
		switch tier {
		case application.Tier1, application.Tier2, application.Tier3:
		default:
			writeAdminError(w, http.StatusBadRequest, "invalid_tier", "tier must be tier_1, tier_2, or tier_3")
			return
		}

		app := application.Application{
			Name:             req.Name,
			Tier:             tier,
			AllowedModels:    req.AllowedModels,
			StreamingAllowed: req.StreamingAllowed,
			MaxRPM:           req.MaxRPM,
			MaxTPM:           req.MaxTPM,
			MonthlyBudgetBRL: req.MonthlyBudgetBRL,
		}

		created, rawToken, err := svc.CreateApplication(r.Context(), app)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to create application")
			return
		}

		// Derive the key prefix the same way the service does, for the response.
		// The service method returns just the full token; prefix = everything before the last "_"+secret.
		// Since prefix = "gwk_{name}" and full token = prefix + "_" + 64hexchars, we can extract it.
		prefix := rawToken[:len(rawToken)-65] // len("_" + 64hexchars) = 65

		writeJSON(w, http.StatusCreated, createApplicationResponse{
			applicationResponse: toApplicationResponse(created),
			Token:               rawToken,
			KeyPrefix:           prefix,
		})
	}
}

// GetApplication handles GET /admin/v1/applications/{id}.
func GetApplication(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r, "id")
		if !ok {
			return
		}

		app, err := svc.GetApplication(r.Context(), id)
		if err != nil {
			if errors.Is(err, application.ErrNotFound) {
				writeAdminError(w, http.StatusNotFound, "not_found", "application not found")
				return
			}
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to get application")
			return
		}

		writeJSON(w, http.StatusOK, toApplicationResponse(app))
	}
}

// UpdateApplication handles PUT /admin/v1/applications/{id}.
// The caller provides the full replacement set of fields.
func UpdateApplication(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r, "id")
		if !ok {
			return
		}

		var req updateApplicationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}

		tier := application.TierLevel(req.Tier)
		switch tier {
		case application.Tier1, application.Tier2, application.Tier3:
		default:
			writeAdminError(w, http.StatusBadRequest, "invalid_tier", "tier must be tier_1, tier_2, or tier_3")
			return
		}

		app := application.Application{
			ID:               id,
			Name:             req.Name,
			Tier:             tier,
			AllowedModels:    req.AllowedModels,
			StreamingAllowed: req.StreamingAllowed,
			MaxRPM:           req.MaxRPM,
			MaxTPM:           req.MaxTPM,
			MonthlyBudgetBRL: req.MonthlyBudgetBRL,
			Active:           req.Active,
		}

		updated, err := svc.UpdateApplication(r.Context(), app)
		if err != nil {
			if errors.Is(err, application.ErrNotFound) {
				writeAdminError(w, http.StatusNotFound, "not_found", "application not found")
				return
			}
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to update application")
			return
		}

		writeJSON(w, http.StatusOK, toApplicationResponse(updated))
	}
}

// DeleteApplication handles DELETE /admin/v1/applications/{id}.
// Soft-deletes the application (active=false). Returns 204 No Content.
func DeleteApplication(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r, "id")
		if !ok {
			return
		}

		if err := svc.DeleteApplication(r.Context(), id); err != nil {
			if errors.Is(err, application.ErrNotFound) {
				writeAdminError(w, http.StatusNotFound, "not_found", "application not found")
				return
			}
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to delete application")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// RotateAPIKey handles POST /admin/v1/applications/{id}/rotate-key.
//
// Atomically invalidates the current API key and issues a new one. The new raw
// token is returned once and never retrievable again (ADR-0009).
func RotateAPIKey(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r, "id")
		if !ok {
			return
		}

		rawToken, err := svc.RotateAPIKey(r.Context(), id)
		if err != nil {
			if errors.Is(err, application.ErrNotFound) {
				writeAdminError(w, http.StatusNotFound, "not_found", "application not found")
				return
			}
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to rotate API key")
			return
		}

		prefix := rawToken[:len(rawToken)-65]

		writeJSON(w, http.StatusOK, rotateKeyResponse{
			Token:     rawToken,
			KeyPrefix: prefix,
		})
	}
}

// GrantEndpointAccess handles POST /admin/v1/applications/{id}/grants/{endpointID}.
// Allows the application to call the specified proxy endpoint. Idempotent.
func GrantEndpointAccess(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appID, ok := parseID(w, r, "id")
		if !ok {
			return
		}
		epID, ok := parseID(w, r, "endpointID")
		if !ok {
			return
		}

		if err := svc.GrantAccess(r.Context(), appID, epID); err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to grant access")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// RevokeEndpointAccess handles DELETE /admin/v1/applications/{id}/grants/{endpointID}.
// Removes the application's access to the specified proxy endpoint.
func RevokeEndpointAccess(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appID, ok := parseID(w, r, "id")
		if !ok {
			return
		}
		epID, ok := parseID(w, r, "endpointID")
		if !ok {
			return
		}

		if err := svc.RevokeAccess(r.Context(), appID, epID); err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to revoke access")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
