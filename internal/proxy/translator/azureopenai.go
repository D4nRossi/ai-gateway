package translator

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// azureOpenAI implements PathTranslator for ProviderAzureOpenAI.
//
// Expected provider_config shape:
//
//	{
//	  "api_version": "2025-01-01-preview",
//	  "model_to_deployment": {
//	    "gpt-4.1":      "gpt-4.1",
//	    "gpt-4.1-mini": "gpt-4.1-mini"
//	  }
//	}
//
// Translation rules (ADR-0017):
//
//   - Canonical "/chat/completions": extract `model` from body, map to deployment,
//     output "/openai/deployments/{deployment}/chat/completions?api-version=X".
//   - Legacy Azure-shaped "/openai/deployments/.../{op}": passthrough (transition
//     period — endpoints cadastrados antes da Onda 2 continuam funcionando até
//     migrarem para o formato canônico).
//   - Anything else: ErrUnsupportedOperation (forces a 400 with clear message).
type azureOpenAI struct{}

const (
	// canonChatPath is the OpenAI-style chat path clients send.
	canonChatPath = "/chat/completions"

	// legacyAzurePathPrefix is the Azure-native path some legacy clients still
	// send directly. Recognized as passthrough during the transition.
	legacyAzurePathPrefix = "/openai/deployments/"
)

func (azureOpenAI) Translate(in Input) (Output, error) {
	// Legacy passthrough — keep behavior of pre-Onda-2 clients.
	if strings.HasPrefix(in.CanonicalPath, legacyAzurePathPrefix) {
		return Output{Path: in.CanonicalPath, RawQuery: in.RawQuery}, nil
	}

	if in.CanonicalPath != canonChatPath {
		return Output{}, fmt.Errorf("%w: azure_openai accepts %q or %s* (got %q)",
			ErrUnsupportedOperation, canonChatPath, legacyAzurePathPrefix, in.CanonicalPath)
	}

	apiVersion, _ := in.Config["api_version"].(string)
	if apiVersion == "" {
		return Output{}, fmt.Errorf("%w: azure_openai requires api_version", ErrEndpointMisconfigured)
	}

	mapping, err := readModelMapping(in.Config)
	if err != nil {
		return Output{}, err
	}

	model, err := extractModel(in.Body)
	if err != nil {
		return Output{}, err
	}

	deployment, ok := mapping[model]
	if !ok {
		return Output{}, fmt.Errorf("%w: model %q (available: %s)",
			ErrUnknownModel, model, joinSorted(mapping))
	}

	// Merge upstream api-version into any client query params. Client values
	// for api-version are overwritten — endpoint config is the source of truth.
	q := url.Values{}
	if in.RawQuery != "" {
		parsed, parseErr := url.ParseQuery(in.RawQuery)
		if parseErr == nil {
			q = parsed
		}
	}
	q.Set("api-version", apiVersion)

	return Output{
		Path:     "/openai/deployments/" + url.PathEscape(deployment) + "/chat/completions",
		RawQuery: q.Encode(),
	}, nil
}

// readModelMapping extracts and validates the model_to_deployment map from
// the loose ProviderConfig. Empty map is treated as misconfiguration — a
// translator with no mappings can't route any request.
func readModelMapping(cfg endpoint.ProviderConfig) (map[string]string, error) {
	raw, ok := cfg["model_to_deployment"]
	if !ok {
		return nil, fmt.Errorf("%w: azure_openai requires model_to_deployment", ErrEndpointMisconfigured)
	}

	// JSON unmarshalled as map[string]any — coerce values to strings.
	mAny, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: model_to_deployment must be an object", ErrEndpointMisconfigured)
	}
	if len(mAny) == 0 {
		return nil, fmt.Errorf("%w: model_to_deployment is empty", ErrEndpointMisconfigured)
	}

	out := make(map[string]string, len(mAny))
	for k, v := range mAny {
		s, ok := v.(string)
		if !ok || s == "" {
			return nil, fmt.Errorf("%w: model_to_deployment[%q] must be a non-empty string", ErrEndpointMisconfigured, k)
		}
		out[k] = s
	}
	return out, nil
}

// extractModel reads just the "model" field from an OpenAI-style chat body.
// Uses a minimal struct to avoid allocating the full message slice.
func extractModel(body []byte) (string, error) {
	if len(body) == 0 {
		return "", fmt.Errorf("%w: request body is empty (need {\"model\":\"...\"})", ErrUnknownModel)
	}
	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return "", fmt.Errorf("%w: request body is not valid JSON: %v", ErrUnknownModel, err)
	}
	if probe.Model == "" {
		return "", fmt.Errorf("%w: request body has no \"model\" field", ErrUnknownModel)
	}
	return probe.Model, nil
}

// joinSorted returns a deterministic comma-separated list of map keys.
// Used in error messages so the available-models list is reproducible.
func joinSorted(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
