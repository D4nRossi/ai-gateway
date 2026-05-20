package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/D4nRossi/ai-gateway/internal/api/middleware"
	"github.com/D4nRossi/ai-gateway/internal/config"
)

// modelItem is a single entry in the GET /v1/models response.
type modelItem struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// Models handles GET /v1/models.
// Returns the subset of configured models that the caller's application may use.
//
// References:
//   - SPEC.md §6.5
func Models(cfg *config.Config) http.HandlerFunc {
	// Build a lookup map from public name to provider for O(1) access per request.
	modelProvider := make(map[string]string, len(cfg.Models))
	for _, m := range cfg.Models {
		modelProvider[m.PublicName] = m.Provider
	}

	return func(w http.ResponseWriter, r *http.Request) {
		policy, ok := middleware.PolicyFrom(r.Context())
		if !ok {
			http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
			return
		}

		items := make([]modelItem, 0, len(policy.AllowedModels))
		for _, name := range policy.AllowedModels {
			provider, exists := modelProvider[name]
			if !exists {
				provider = "azure"
			}
			items = append(items, modelItem{
				ID:      name,
				Object:  "model",
				OwnedBy: provider,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   items,
		})
	}
}
