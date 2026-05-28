package proxy

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/D4nRossi/ai-gateway/internal/app/proxyservice"
	"github.com/D4nRossi/ai-gateway/internal/config"
	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
	"github.com/D4nRossi/ai-gateway/internal/observability"
	"github.com/D4nRossi/ai-gateway/internal/proxy/loadbalancer"
	"github.com/D4nRossi/ai-gateway/internal/proxy/translator"
	"github.com/D4nRossi/ai-gateway/internal/usage"
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
//  4. Reads the request body once so both the path translator and the usage
//     extractor (ADR-0024) see the same bytes without re-reading.
//  5. Notifies the Balancer of the request start (least-connections counter).
//  6. Forwards the request via httputil.ReverseProxy with our rewriteRequest.
//  7. On response (or error), notifies the Balancer of the request end via a
//     ModifyResponse hook and ErrorHandler.
//  8. ModifyResponse emits a UsageEvent (ADR-0024) — completo pra azure_openai
//     / openai non-stream, minimal pra outros casos.
//
// Reasoning: the lifecycle hooks must fire exactly once per request, regardless
// of which path the request takes (successful response, upstream error, panic).
// ReverseProxy guarantees that exactly one of ModifyResponse OR ErrorHandler runs
// per request, so wiring OnRequestEnd in both is correct and complete.
//
// References:
//   - ADR-0010 — generic HTTP proxy engine
//   - ADR-0013 — load balancing lifecycle hooks
//   - ADR-0017 — path translation per provider_kind
//   - ADR-0020 — credential storage mode per target
//   - ADR-0024 — usage tracking no proxy plane
func Handler(
	svc *proxyservice.Service,
	resolver proxyservice.CredentialResolver,
	transport http.RoundTripper,
	usageEmitter usage.Emitter,
	models map[string]config.ModelConfig,
	logger *slog.Logger,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

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

		// Read the request body once. Both the path translator (ADR-0017) and
		// the usage extractor (ADR-0024) need it; reading twice would either
		// require duplicating the read+restore dance or buffering on the heap
		// twice. The body is restored on r.Body afterwards.
		requestBody, err := readAndRestoreRequestBody(r)
		if err != nil {
			logger.Error("proxy: failed to read request body",
				"slug", slug, "err", err)
			writeProxyError(w, http.StatusBadRequest, "bad_request", "failed to read request body")
			return
		}
		requestModel := extractModelFromRequestBody(requestBody)

		// Path translation per provider_kind (ADR-0017). The translator may
		// inspect the body to choose the upstream path (Azure: extract `model`
		// from the chat payload). When kind = custom or no translator exists,
		// the request stays passthrough.
		translation, err := applyTranslator(r, requestBody, slug, res.Endpoint)
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

		// usageState carries the cross-callback data needed to emit a UsageEvent
		// from ModifyResponse / ErrorHandler. Captured by closure.
		usageEmitted := false
		emitUsage := func(statusCode int, parsed extractedUsage, parsedOK bool) {
			if usageEmitted {
				return
			}
			usageEmitted = true
			event := usage.UsageEvent{
				RequestID:       requestIDFrom(r.Context()),
				ApplicationName: app.Name,
				Tier:            string(app.Tier),
				Model:           requestModel,
				Provider:        providerFromKind(res.Endpoint.ProviderKind),
				LatencyMs:       int(time.Since(start).Milliseconds()),
				StatusCode:      statusCode,
				CreatedAt:       time.Now().UTC(),
			}
			if parsedOK {
				event.InputTokens = parsed.InputTokens
				event.OutputTokens = parsed.OutputTokens
				event.TotalTokens = parsed.TotalTokens
				if mc, ok := models[requestModel]; ok {
					event.EstimatedCostBRL = computeCostBRL(mc, parsed.InputTokens, parsed.OutputTokens)
				}
			}
			usageEmitter.Emit(event)
		}

		rp := &httputil.ReverseProxy{
			Transport: transport,
			Rewrite:   rewriteRequest(slug, res.Target, translation),
			ModifyResponse: func(resp *http.Response) error {
				fireEnd()

				// Skip token extraction for streaming responses (V1 limitation,
				// ADR-0024). Emit the event with status + latency so dashboards
				// still count the request; tokens / cost stay 0.
				if isStreamResponse(resp) {
					emitUsage(resp.StatusCode, extractedUsage{}, false)
					return nil
				}

				// Only attempt extraction for 2xx JSON responses from IA-schema
				// providers. Everything else gets a minimal event.
				if resp.StatusCode < 200 || resp.StatusCode >= 300 ||
					!isIASchemaProvider(res.Endpoint.ProviderKind) ||
					!isJSONResponse(resp) {
					emitUsage(resp.StatusCode, extractedUsage{}, false)
					return nil
				}

				body, truncated, err := readCappedResponseBody(resp)
				if err != nil {
					logger.Warn("proxy: failed to read response body for usage extraction",
						"slug", slug, "err", err)
					emitUsage(resp.StatusCode, extractedUsage{}, false)
					return nil
				}
				if truncated {
					// Body too big — extraction is unreliable past the cap.
					logger.Warn("proxy: response body exceeded usage cap",
						"slug", slug, "cap_bytes", maxResponseBodyBytes)
					emitUsage(resp.StatusCode, extractedUsage{}, false)
					return nil
				}

				parsed, parsedOK := extractUsageFromResponseBody(body)
				if !parsedOK {
					logger.Warn("proxy: response body had no usable usage info",
						"event_type", "proxy_usage_extraction_failed",
						"slug", slug, "model", requestModel)
				}
				emitUsage(resp.StatusCode, parsed, parsedOK)
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
				emitUsage(http.StatusBadGateway, extractedUsage{}, false)
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
			"model", requestModel,
			"lb_strategy", string(res.Endpoint.LBStrategy),
		)

		rp.ServeHTTP(w, r)
	})
}

// requestIDFrom pulls the request_id from the context populated by the
// observability middleware. Returns "" when missing — the writer accepts
// empty IDs but dashboards correlate by it, so missing IDs hint at a
// middleware order regression.
func requestIDFrom(ctx interface{ Value(any) any }) string {
	if v, ok := ctx.Value(observability.RequestIDKey).(string); ok {
		return v
	}
	return ""
}

// applyTranslator runs the path translator for the endpoint's provider kind
// when one is registered. Returns (nil, nil) when no translator applies — the
// request stays passthrough (ADR-0017).
//
// `requestBody` is passed in pre-read (and already restored on r.Body) by the
// caller so the translator and the usage extractor share the same byte slice.
// ADR-0024 (usage tracking) made the body a cross-cutting concern.
func applyTranslator(r *http.Request, requestBody []byte, slug string, ep endpoint.ProxyEndpoint) (*translator.Output, error) {
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

	out, err := t.Translate(translator.Input{
		CanonicalPath: canonical,
		RawQuery:      r.URL.RawQuery,
		Method:        r.Method,
		Body:          requestBody,
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
