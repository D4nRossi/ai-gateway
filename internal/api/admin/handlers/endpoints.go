package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/D4nRossi/ai-gateway/internal/app/adminservice"
	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// targetAuthRequest is the JSON representation of target credentials in API requests.
// Only fields relevant to the chosen type need to be provided.
type targetAuthRequest struct {
	Type     string `json:"type"`
	Token    string `json:"token,omitempty"`
	Header   string `json:"header,omitempty"`
	Value    string `json:"value,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// targetResponse is the JSON representation of a Target.
// Auth credentials are NOT included in list/get responses — they are encrypted at rest
// and never returned to the caller after initial creation (ADR-0012).
type targetResponse struct {
	ID         int64  `json:"id"`
	EndpointID int64  `json:"endpoint_id"`
	URL        string `json:"url"`
	Weight     int    `json:"weight"`
	AuthType   string `json:"auth_type"`
	Active     bool   `json:"active"`
	CreatedAt  string `json:"created_at"`
}

// endpointResponse is the JSON representation of a ProxyEndpoint.
type endpointResponse struct {
	ID                 int64            `json:"id"`
	Slug               string           `json:"slug"`
	Name               string           `json:"name"`
	LBStrategy         string           `json:"lb_strategy"`
	MaxRPS             int              `json:"max_rps"`
	MaxMonthlyRequests int64            `json:"max_monthly_requests"`
	Active             bool             `json:"active"`
	Targets            []targetResponse `json:"targets"`
	CreatedAt          string           `json:"created_at"`
	UpdatedAt          string           `json:"updated_at"`
}

func toTargetResponse(t endpoint.Target) targetResponse {
	return targetResponse{
		ID:         t.ID,
		EndpointID: t.EndpointID,
		URL:        t.URL,
		Weight:     t.Weight,
		AuthType:   string(t.Auth.Type),
		Active:     t.Active,
		CreatedAt:  t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func toEndpointResponse(ep endpoint.ProxyEndpoint) endpointResponse {
	targets := make([]targetResponse, len(ep.Targets))
	for i, t := range ep.Targets {
		targets[i] = toTargetResponse(t)
	}
	return endpointResponse{
		ID:                 ep.ID,
		Slug:               ep.Slug,
		Name:               ep.Name,
		LBStrategy:         string(ep.LBStrategy),
		MaxRPS:             ep.MaxRPS,
		MaxMonthlyRequests: ep.MaxMonthlyRequests,
		Active:             ep.Active,
		Targets:            targets,
		CreatedAt:          ep.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:          ep.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func authFromRequest(a targetAuthRequest) endpoint.TargetAuth {
	return endpoint.TargetAuth{
		Type:     endpoint.AuthType(a.Type),
		Token:    a.Token,
		Header:   a.Header,
		Value:    a.Value,
		Username: a.Username,
		Password: a.Password,
	}
}

// createEndpointRequest is the JSON body for POST /admin/v1/endpoints.
type createEndpointRequest struct {
	Slug               string `json:"slug"`
	Name               string `json:"name"`
	LBStrategy         string `json:"lb_strategy"`
	MaxRPS             int    `json:"max_rps"`
	MaxMonthlyRequests int64  `json:"max_monthly_requests"`
}

// updateEndpointRequest is the JSON body for PUT /admin/v1/endpoints/{id}.
type updateEndpointRequest struct {
	Slug               string `json:"slug"`
	Name               string `json:"name"`
	LBStrategy         string `json:"lb_strategy"`
	MaxRPS             int    `json:"max_rps"`
	MaxMonthlyRequests int64  `json:"max_monthly_requests"`
	Active             bool   `json:"active"`
}

// addTargetRequest is the JSON body for POST /admin/v1/endpoints/{id}/targets.
type addTargetRequest struct {
	URL    string            `json:"url"`
	Weight int               `json:"weight"`
	Auth   targetAuthRequest `json:"auth"`
}

// updateTargetRequest is the JSON body for PUT /admin/v1/endpoints/{id}/targets/{targetID}.
type updateTargetRequest struct {
	URL    string            `json:"url"`
	Weight int               `json:"weight"`
	Auth   targetAuthRequest `json:"auth"`
	Active bool              `json:"active"`
}

// ListEndpoints handles GET /admin/v1/endpoints.
// Returns all endpoints (active and inactive) without their target lists.
func ListEndpoints(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		eps, err := svc.ListEndpoints(r.Context())
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to list endpoints")
			return
		}

		resp := make([]endpointResponse, len(eps))
		for i, ep := range eps {
			resp[i] = toEndpointResponse(ep)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// CreateEndpoint handles POST /admin/v1/endpoints.
// Creates a new proxy endpoint without targets. Add targets via POST /endpoints/{id}/targets.
func CreateEndpoint(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createEndpointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}
		if req.Slug == "" || req.Name == "" {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "slug and name are required")
			return
		}

		ep := endpoint.ProxyEndpoint{
			Slug:               req.Slug,
			Name:               req.Name,
			LBStrategy:         endpoint.LBStrategy(req.LBStrategy),
			MaxRPS:             req.MaxRPS,
			MaxMonthlyRequests: req.MaxMonthlyRequests,
		}

		created, err := svc.CreateEndpoint(r.Context(), ep)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to create endpoint")
			return
		}

		writeJSON(w, http.StatusCreated, toEndpointResponse(created))
	}
}

// GetEndpoint handles GET /admin/v1/endpoints/{id}.
// Returns the endpoint with its active targets.
func GetEndpoint(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r, "id")
		if !ok {
			return
		}

		ep, err := svc.GetEndpoint(r.Context(), id)
		if err != nil {
			if errors.Is(err, endpoint.ErrNotFound) {
				writeAdminError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to get endpoint")
			return
		}

		writeJSON(w, http.StatusOK, toEndpointResponse(ep))
	}
}

// UpdateEndpoint handles PUT /admin/v1/endpoints/{id}.
func UpdateEndpoint(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r, "id")
		if !ok {
			return
		}

		var req updateEndpointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}

		ep := endpoint.ProxyEndpoint{
			ID:                 id,
			Slug:               req.Slug,
			Name:               req.Name,
			LBStrategy:         endpoint.LBStrategy(req.LBStrategy),
			MaxRPS:             req.MaxRPS,
			MaxMonthlyRequests: req.MaxMonthlyRequests,
			Active:             req.Active,
		}

		updated, err := svc.UpdateEndpoint(r.Context(), ep)
		if err != nil {
			if errors.Is(err, endpoint.ErrNotFound) {
				writeAdminError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to update endpoint")
			return
		}

		writeJSON(w, http.StatusOK, toEndpointResponse(updated))
	}
}

// DeleteEndpoint handles DELETE /admin/v1/endpoints/{id}.
// Soft-deletes the endpoint (active=false). Returns 204 No Content.
func DeleteEndpoint(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(w, r, "id")
		if !ok {
			return
		}

		if err := svc.DeleteEndpoint(r.Context(), id); err != nil {
			if errors.Is(err, endpoint.ErrNotFound) {
				writeAdminError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to delete endpoint")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// AddTarget handles POST /admin/v1/endpoints/{id}/targets.
// Adds a new upstream target. Auth credentials are encrypted by the repository (ADR-0012).
func AddTarget(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		epID, ok := parseID(w, r, "id")
		if !ok {
			return
		}

		var req addTargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}
		if req.URL == "" {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "url is required")
			return
		}

		t := endpoint.Target{
			EndpointID: epID,
			URL:        req.URL,
			Weight:     req.Weight,
			Auth:       authFromRequest(req.Auth),
		}

		created, err := svc.AddTarget(r.Context(), t)
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to add target")
			return
		}

		writeJSON(w, http.StatusCreated, toTargetResponse(created))
	}
}

// UpdateTarget handles PUT /admin/v1/endpoints/{id}/targets/{targetID}.
// Replaces the target's URL, weight, and auth credentials. Credentials are re-encrypted (ADR-0012).
func UpdateTarget(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := parseID(w, r, "id") // endpoint ID — validated for URL consistency
		if !ok {
			return
		}
		targetID, ok := parseID(w, r, "targetID")
		if !ok {
			return
		}

		var req updateTargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAdminError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}

		t := endpoint.Target{
			ID:     targetID,
			URL:    req.URL,
			Weight: req.Weight,
			Auth:   authFromRequest(req.Auth),
			Active: req.Active,
		}

		updated, err := svc.UpdateTarget(r.Context(), t)
		if err != nil {
			if errors.Is(err, endpoint.ErrNotFound) {
				writeAdminError(w, http.StatusNotFound, "not_found", "target not found")
				return
			}
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to update target")
			return
		}

		writeJSON(w, http.StatusOK, toTargetResponse(updated))
	}
}

// RemoveTarget handles DELETE /admin/v1/endpoints/{id}/targets/{targetID}.
// Soft-deletes the target. Returns 204 No Content.
func RemoveTarget(svc *adminservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := parseID(w, r, "id")
		if !ok {
			return
		}
		targetID, ok := parseID(w, r, "targetID")
		if !ok {
			return
		}

		if err := svc.RemoveTarget(r.Context(), targetID); err != nil {
			if errors.Is(err, endpoint.ErrNotFound) {
				writeAdminError(w, http.StatusNotFound, "not_found", "target not found")
				return
			}
			writeAdminError(w, http.StatusInternalServerError, "internal", "failed to remove target")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
