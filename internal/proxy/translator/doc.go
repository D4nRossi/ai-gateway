// Package translator implements path translation for the proxy plane.
//
// Each translator is selected by ProviderKind and rewrites a canonical
// OpenAI-style path (e.g. /chat/completions) into the upstream-native
// path (e.g. /openai/deployments/{deployment}/chat/completions?api-version=X).
//
// The motivation and contract are documented in ADR-0017. The proxy engine
// itself (internal/proxy/director.go) remains a generic httputil.ReverseProxy
// — the translator is a focused hook that runs inside Rewrite when the
// endpoint is not `custom`.
//
// Adding a new provider:
//
//  1. Implement PathTranslator (see azureopenai.go for a worked example).
//  2. Register it in For() so the director can find it by kind.
//  3. Document the expected provider_config shape on the new translator.
//
// References:
//   - ADR-0010 — generic HTTP proxy engine (unchanged)
//   - ADR-0016 — provider catalog (metadata that this package activates)
//   - ADR-0017 — path translation contract
package translator
