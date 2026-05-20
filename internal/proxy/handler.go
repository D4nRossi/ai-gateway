package proxy

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/D4nRossi/ai-gateway/internal/app/proxyservice"
	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
	"github.com/D4nRossi/ai-gateway/internal/proxy/loadbalancer"
)

// Handler returns an http.Handler that serves /v1/proxy/{slug}/* requests.
//
// The Auth middleware must run before this handler so ApplicationFromCtx returns
// a valid Application. The handler:
//
//  1. Reads {slug} from chi URL parameters.
//  2. Calls proxyservice.Resolve to load endpoint, verify grant, select target.
//  3. Notifies the Balancer of the request start (least-connections counter).
//  4. Forwards the request via httputil.ReverseProxy with our rewriteRequest.
//  5. On response (or error), notifies the Balancer of the request end via a
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
func Handler(svc *proxyservice.Service, transport http.RoundTripper, logger *slog.Logger) http.Handler {
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
			Rewrite:   rewriteRequest(slug, res.Target),
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
