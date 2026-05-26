package proxy

import (
	"encoding/base64"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
	"github.com/D4nRossi/ai-gateway/internal/proxy/translator"
)

// rewriteRequest returns a Rewrite callback (used by httputil.ReverseProxy) that
// mutates the outbound request so it targets the chosen upstream:
//
//  1. Strips the "/v1/proxy/{slug}" prefix from the path.
//  2. Rewrites the URL to target.URL + (translation OR remainder) + query.
//  3. Removes the consumer's Authorization header.
//  4. Applies the target's auth (Bearer / API-key header / Basic).
//  5. Sets X-Forwarded-* via SetXForwarded so the upstream sees the original client.
//
// When `translation` is non-nil (provider_kind has a registered translator —
// ADR-0017), its Path + RawQuery REPLACE the canonical path the client sent.
// When `translation` is nil, behavior is unchanged from the pre-Onda-2 era:
// the client's path after "/v1/proxy/{slug}" is forwarded verbatim.
//
// The Rewrite callback (Go 1.20+) is preferred over the older Director field
// because it receives a httputil.ProxyRequest with both In and Out, letting us
// reuse net/http's safe X-Forwarded-* logic.
//
// Reasoning: removing the inbound Authorization is mandatory — keeping it would
// leak the consumer's gwk_* token to the upstream. The target's own credentials
// are injected fresh from the decrypted TargetAuth.
//
// References:
//   - ADR-0012 — credentials decrypted into TargetAuth in memory only
//   - ADR-0017 — path translation contract
//   - https://pkg.go.dev/net/http/httputil#ReverseProxy.Rewrite
func rewriteRequest(slug string, target endpoint.Target, translation *translator.Output) func(pr *httputil.ProxyRequest) {
	prefix := "/v1/proxy/" + slug
	return func(pr *httputil.ProxyRequest) {
		// Path rewriting: take everything after the "/v1/proxy/{slug}" prefix.
		// If the inbound path is exactly the prefix, forward to the target's root path.
		remainder := strings.TrimPrefix(pr.In.URL.Path, prefix)
		if remainder == "" {
			remainder = "/"
		}

		targetURL, err := url.Parse(target.URL)
		if err != nil {
			// Target URL was validated at admin-create time; reaching here implies
			// corruption. Rewrite cannot return an error, so we route to a sentinel
			// host that will fail with a clear 502 instead of forwarding garbage.
			pr.Out.URL.Scheme = "https"
			pr.Out.URL.Host = "invalid-target.local"
			pr.Out.URL.Path = remainder
			return
		}

		pr.Out.URL.Scheme = targetURL.Scheme
		pr.Out.URL.Host = targetURL.Host
		// Override Host header so upstreams routing on Host work correctly.
		pr.Out.Host = targetURL.Host

		// Combine the target base path with the translated path (or remainder),
		// avoiding "//".
		basePath := strings.TrimSuffix(targetURL.Path, "/")
		if translation != nil {
			pr.Out.URL.Path = basePath + translation.Path
			pr.Out.URL.RawQuery = translation.RawQuery
		} else {
			pr.Out.URL.Path = basePath + remainder
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery
		}

		// Strip the consumer's Authorization before injecting the target's credential.
		pr.Out.Header.Del("Authorization")

		applyTargetAuth(pr.Out, target.Auth)

		// X-Forwarded-For / X-Forwarded-Host / X-Forwarded-Proto.
		// ReverseProxy.Rewrite intentionally does NOT populate these automatically;
		// the Go docs recommend calling SetXForwarded explicitly.
		pr.SetXForwarded()
	}
}

// applyTargetAuth injects the target's credentials into the outbound request.
// TargetAuth is already decrypted by the repository layer (ADR-0012); this
// function performs no cryptography.
//
// Supported auth types:
//   - AuthNone:         no header set.
//   - AuthBearerToken:  "Authorization: Bearer <token>".
//   - AuthAPIKeyHeader: "<Header>: <Value>".
//   - AuthBasic:        "Authorization: Basic base64(<user>:<pass>)".
//
// Unknown types are a no-op — the DB CHECK constraint prevents them from
// reaching runtime; a defensive no-op is safer than panicking inside a handler.
func applyTargetAuth(out *http.Request, a endpoint.TargetAuth) {
	switch a.Type {
	case endpoint.AuthNone:
		return
	case endpoint.AuthBearerToken:
		out.Header.Set("Authorization", "Bearer "+a.Token)
	case endpoint.AuthAPIKeyHeader:
		if a.Header != "" {
			out.Header.Set(a.Header, a.Value)
		}
	case endpoint.AuthBasic:
		creds := a.Username + ":" + a.Password
		enc := base64.StdEncoding.EncodeToString([]byte(creds))
		out.Header.Set("Authorization", "Basic "+enc)
	}
}
