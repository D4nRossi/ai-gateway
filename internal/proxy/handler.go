package proxy

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/D4nRossi/ai-gateway/internal/app/proxyservice"
	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
	"github.com/D4nRossi/ai-gateway/internal/proxy/loadbalancer"
	"github.com/D4nRossi/ai-gateway/internal/proxy/translator"
)

// maxProxyBodyBytes caps the in-memory buffer used by the path translator to
// inspect the request body (e.g. extract the "model" field for Azure OpenAI).
// Matches the limit in internal/api/handlers/chat.go for consistency.
const maxProxyBodyBytes = 1 << 20 // 1 MiB

// Handler returns an http.Handler that serves /v1/proxy/{slug}/* requests.
//
// The Auth middleware must run before this handler so ApplicationFromCtx returns
// a valid Application. The handler:
//
//  1. Reads {slug} from chi URL parameters.
//  2. Calls proxyservice.Resolve to load endpoint, verify grant, select target.
//  3. Resolves the target's credentials per its CredentialStorageMode
//     (ADR-0020) — AES in-memory for mode=aes, Key Vault with 200 ms timeout
//     for mode=kv|both.
//  4. Notifies the Balancer of the request start (least-connections counter).
//  5. Forwards the request via httputil.ReverseProxy with our rewriteRequest.
//  6. On response (or error), notifies the Balancer of the request end via a
//     ModifyResponse hook and ErrorHandler.
//
// Reasoning: the lifecycle hooks must fire exactly once per request, regardless
// of which path the request takes (successful response, upstream error, panic).
// ReverseProxy guarantees that exactly one of ModifyResponse OR ErrorHandler runs
// per request, so wiring OnRequestEnd in both is correct and complete.
//
// References:
//   - ADR-0010 — generic HTTP proxy engine
//   - ADR-0013 — load balancing lifecycle hooks
//   - ADR-0020 — credential storage mode per target
func Handler(svc *proxyservice.Service, resolver proxyservice.CredentialResolver, transport http.RoundTripper, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		if slug == "" {
			writeProxyError(w, http.StatusBadRequest, "bad_request", "missing slug")
			return
		}

		app, ok := ApplicationFromCtx(r.Context())
		if !ok {
			// Should be unreachable when Auth is properly mounted before this handler.
			writeProxyError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}

		clientIP := clientIPFrom(r)

		res, err := svc.Resolve(r.Context(), slug, app.ID, clientIP)
		if err != nil {
			handleResolveError(w, logger, slug, app.ID, err)
			return
		}

		// Credential resolution per target (ADR-0020). For mode=aes this returns
		// res.Target.Auth as-is; for mode=kv|both this consults Key Vault under
		// a 200 ms deadline and may fall back to the cached AES copy.
		resolvedAuth, err := resolver.Resolve(r.Context(), res.Target)
		if err != nil {
			handleCredentialResolveError(w, logger, slug, res.Target.ID, err)
			return
		}
		res.Target.Auth = resolvedAuth

		// Path translation per provider_kind (ADR-0017). The translator may
		// inspect the body to choose the upstream path (Azure: extract `model`
		// from the chat payload). When kind = custom or no translator exists,
		// the request stays passthrough.
		translation, err := applyTranslator(r, slug, res.Endpoint)
		if err != nil {
			handleTranslatorError(w, logger, slug, res.Endpoint.ID, err)
			return
		}

		// Lifecycle: notify the balancer once before dispatching the upstream call.
		// OnRequestEnd is registered to fire exactly once via ModifyResponse OR
		// ErrorHandler (ReverseProxy guarantees these are mutually exclusive).
		res.Balancer.OnRequestStart(res.Target.ID)
		endFired := false
		fireEnd := func() {
			if !endFired {
				res.Balancer.OnRequestEnd(res.Target.ID)
				endFired = true
			}
		}

		rp := &httputil.ReverseProxy{
			Transport: transport,
			Rewrite:   rewriteRequest(slug, res.Target, translation),
			ModifyResponse: func(_ *http.Response) error {
				fireEnd()
				return nil
			},
			ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, upstreamErr error) {
				fireEnd()
				logger.Error("proxy upstream error",
					"slug", slug,
					"target_id", res.Target.ID,
					"target_url", res.Target.URL,
					"err", upstreamErr,
				)
				writeProxyError(rw, http.StatusBadGateway, "bad_gateway", "upstream request failed")
			},
		}

		// Final safety net: if the client disconnects mid-stream, ReverseProxy's
		// ErrorHandler may not fire before we return. Defer ensures the counter
		// is released exactly once.
		defer fireEnd()

		logger.Info("proxy forwarding",
			"slug", slug,
			"application_id", app.ID,
			"application_name", app.Name,
			"target_id", res.Target.ID,
			"lb_strategy", string(res.Endpoint.LBStrategy),
		)

		rp.ServeHTTP(w, r)
	})
}

// applyTranslator runs the path translator for the endpoint's provider kind
// when one is registered. Returns (nil, nil) when no translator applies — the
// request stays passthrough (ADR-0017).
//
// When the translator needs to inspect the body, the body is read in full
// (capped at maxProxyBodyBytes) and replaced on the request with a fresh
// reader over the same bytes so the downstream ReverseProxy can re-read it.
func applyTranslator(r *http.Request, slug string, ep endpoint.ProxyEndpoint) (*translator.Output, error) {
	t, ok := translator.For(ep.ProviderKind)
	if !ok {
		return nil, nil
	}

	// Strip the "/v1/proxy/{slug}" prefix so the translator sees the canonical
	// path the client intended (e.g. "/chat/completions").
	prefix := "/v1/proxy/" + slug
	canonical := strings.TrimPrefix(r.URL.Path, prefix)
	if canonical == "" {
		canonical = "/"
	}

	var body []byte
	if r.Body != nil && r.ContentLength != 0 {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, maxProxyBodyBytes))
		if err != nil {
			return nil, err
		}
		// Restore body so ReverseProxy can stream it to upstream.
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
	}

	out, err := t.Translate(translator.Input{
		CanonicalPath: canonical,
		RawQuery:      r.URL.RawQuery,
		Method:        r.Method,
		Body:          body,
		Config:        ep.ProviderConfig,
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// handleTranslatorError maps translator sentinel errors to HTTP status codes.
// ErrEndpointMisconfigured is a server-side condition (admin must fix the
// endpoint), so it surfaces as 500 with a log entry. Other errors are client-
// addressable (wrong path, wrong model) and map to 400.
func handleTranslatorError(w http.ResponseWriter, logger *slog.Logger, slug string, endpointID int64, err error) {
	switch {
	case errors.Is(err, translator.ErrUnknownModel):
		writeProxyError(w, http.StatusBadRequest, "unknown_model", err.Error())
	case errors.Is(err, translator.ErrUnsupportedOperation):
		writeProxyError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, translator.ErrEndpointMisconfigured):
		logger.Error("proxy translator: endpoint misconfigured",
			"slug", slug,
			"endpoint_id", endpointID,
			"err", err,
		)
		writeProxyError(w, http.StatusInternalServerError, "endpoint_misconfigured", err.Error())
	default:
		logger.Error("proxy translator failed",
			"slug", slug,
			"endpoint_id", endpointID,
			"err", err,
		)
		writeProxyError(w, http.StatusBadRequest, "bad_request", "request translation failed")
	}
}

// handleCredentialResolveError maps CredentialResolver errors to HTTP status
// codes. ErrKVTimeout in mode=kv surfaces as 503 (transient); other failures
// (missing kv_secret_name, malformed payload in KV, network errors with no
// fallback) surface as 500.
func handleCredentialResolveError(w http.ResponseWriter, logger *slog.Logger, slug string, targetID int64, err error) {
	switch {
	case errors.Is(err, proxyservice.ErrKVTimeout):
		logger.Warn("proxy credential resolve: kv timeout",
			"slug", slug,
			"target_id", targetID,
			"err", err,
		)
		writeProxyError(w, http.StatusServiceUnavailable, "kv_timeout", "credential resolution timed out")
	default:
		logger.Error("proxy credential resolve failed",
			"slug", slug,
			"target_id", targetID,
			"err", err,
		)
		writeProxyError(w, http.StatusInternalServerError, "credential_unavailable", "failed to resolve target credentials")
	}
}

// handleResolveError maps proxyservice errors to HTTP status codes.
func handleResolveError(w http.ResponseWriter, logger *slog.Logger, slug string, appID int64, err error) {
	switch {
	case errors.Is(err, endpoint.ErrNotFound):
		writeProxyError(w, http.StatusNotFound, "not_found", "unknown proxy endpoint")
	case errors.Is(err, proxyservice.ErrAccessDenied):
		writeProxyError(w, http.StatusForbidden, "forbidden", "application has no grant for this endpoint")
	case errors.Is(err, loadbalancer.ErrNoTargets):
		writeProxyError(w, http.StatusServiceUnavailable, "no_targets", "endpoint has no active targets")
	default:
		logger.Error("proxy resolve failed",
			"slug", slug,
			"application_id", appID,
			"err", err,
		)
		writeProxyError(w, http.StatusInternalServerError, "internal", "failed to resolve proxy endpoint")
	}
}

// clientIPFrom returns the best-effort client IP for hash-based load balancing.
//
// Order of preference:
//  1. The first IP in X-Forwarded-For (set by an upstream proxy/LB).
//  2. RemoteAddr stripped of its port.
//
// Reasoning: when a load balancer (e.g. Azure App Gateway) fronts this service,
// RemoteAddr is the LB's IP, which would route every request to the same target.
// X-Forwarded-For carries the original client IP and is the standard remedy.
// Trusting it is acceptable because the only consumer is ip_hash bucketing
// (not authentication or rate limiting).
func clientIPFrom(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP — XFF can be a comma-separated chain.
		if comma := strings.IndexByte(xff, ','); comma > 0 {
			return strings.TrimSpace(xff[:comma])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
