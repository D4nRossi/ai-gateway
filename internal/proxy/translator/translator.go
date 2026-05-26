package translator

import (
	"errors"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
)

// Sentinel errors returned by translators. They are sentinel (not wrapped)
// so the caller can map them to HTTP status codes via errors.Is.
var (
	// ErrEndpointMisconfigured indicates the endpoint's provider_config is
	// missing required fields for its kind (e.g. Azure without api_version).
	// Caller should respond 500 — operator must fix the endpoint.
	ErrEndpointMisconfigured = errors.New("endpoint misconfigured for path translation")

	// ErrUnknownModel indicates the client's request named a model that is
	// not present in the endpoint's model_to_deployment mapping. Caller
	// should respond 400 with the list of available models.
	ErrUnknownModel = errors.New("model not configured for endpoint")

	// ErrUnsupportedOperation indicates the canonical path is not handled by
	// this translator (e.g. /embeddings on a translator that only handles chat).
	// Caller should respond 400.
	ErrUnsupportedOperation = errors.New("operation not supported by translator")
)

// Input is everything a PathTranslator needs to know about an inbound request
// to decide the outbound path.
type Input struct {
	// CanonicalPath is the request path AFTER the "/v1/proxy/{slug}" prefix
	// has been stripped. Examples:
	//   "/chat/completions"
	//   "/embeddings"
	//   "/openai/deployments/.../chat/completions"  (raw passthrough form)
	CanonicalPath string

	// RawQuery is the original query string (no leading "?").
	RawQuery string

	// Method is the HTTP verb. Translators that only handle POST may reject
	// other verbs with ErrUnsupportedOperation.
	Method string

	// Body is the raw request body. Translators that need to inspect it (to
	// extract `model`, for instance) read it here. Translators MUST NOT
	// mutate the slice — the proxy plane forwards the same bytes to upstream.
	Body []byte

	// Config is the endpoint's provider_config JSONB, already parsed.
	Config endpoint.ProviderConfig
}

// Output is the translator's decision for the outbound request.
type Output struct {
	// Path is the upstream path the proxy will set on pr.Out.URL.Path.
	Path string

	// RawQuery is the upstream query string (no leading "?"). It REPLACES the
	// original query — translators that want to preserve client query params
	// must merge them explicitly.
	RawQuery string
}

// PathTranslator is the contract for a per-kind path translation rule.
// Implementations are stateless and safe for concurrent use.
//
// Translate is called once per inbound proxy request, after auth + grant
// check, before the request is forwarded upstream. Returning an error aborts
// the request with the corresponding HTTP error (see sentinels above).
type PathTranslator interface {
	Translate(in Input) (Output, error)
}

// For returns the translator registered for the given provider kind.
// Returns (nil, false) for ProviderCustom (no translation) or any kind that
// doesn't have a translator yet — the caller treats both as passthrough.
//
// Reasoning: keeping the registry as a single function (vs. a map) makes the
// set of supported kinds visible at one glance, and lets each translator
// initialize its own closures lazily without an init() side-effect.
func For(kind endpoint.ProviderKind) (PathTranslator, bool) {
	switch kind {
	case endpoint.ProviderAzureOpenAI:
		return azureOpenAI{}, true
	default:
		// All other kinds (openai, anthropic, gemini, ..., custom) keep the
		// passthrough behavior until their translator lands.
		return nil, false
	}
}
