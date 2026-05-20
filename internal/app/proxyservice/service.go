package proxyservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/D4nRossi/ai-gateway/internal/domain/endpoint"
	"github.com/D4nRossi/ai-gateway/internal/proxy/loadbalancer"
)

// ErrAccessDenied is returned by Resolve when the calling application has not
// been granted access to the requested endpoint. Map to HTTP 403 Forbidden.
var ErrAccessDenied = errors.New("application has no grant for this endpoint")

// Service is the application-layer service that powers /v1/proxy/{slug} requests.
// It is safe for concurrent use.
type Service struct {
	endpoints endpoint.Repository
	balancers *loadbalancer.Registry
	logger    *slog.Logger
}

// New constructs a Service backed by the given endpoint repository and balancer
// registry. The registry is owned externally so it can survive Service lifecycle
// changes (e.g. dependency rewiring in tests).
func New(endpoints endpoint.Repository, balancers *loadbalancer.Registry, logger *slog.Logger) *Service {
	return &Service{endpoints: endpoints, balancers: balancers, logger: logger}
}

// Resolution carries the result of Resolve: the loaded endpoint, the selected
// target, and the Balancer instance that picked it. The caller passes the
// Balancer to OnRequestStart / OnRequestEnd so least-connections counters stay
// consistent across the request lifecycle.
type Resolution struct {
	Endpoint endpoint.ProxyEndpoint
	Target   endpoint.Target
	Balancer loadbalancer.Balancer
}

// Resolve loads the endpoint identified by slug, verifies that applicationID has
// been granted access to it, and selects a target via the endpoint's configured
// load-balancing strategy.
//
// Error mapping:
//   - endpoint.ErrNotFound  → 404 (unknown slug)
//   - ErrAccessDenied       → 403 (app not granted)
//   - loadbalancer.ErrNoTargets → 503 (endpoint has no active targets)
//   - any other error       → 500
//
// Reasoning: keeping all three checks here means the HTTP handler only needs to
// pattern-match errors with errors.Is; it never duplicates the lookup logic.
func (s *Service) Resolve(ctx context.Context, slug string, applicationID int64, clientIP string) (Resolution, error) {
	ep, err := s.endpoints.GetBySlug(ctx, slug)
	if err != nil {
		return Resolution{}, fmt.Errorf("loading endpoint %q: %w", slug, err)
	}

	granted, err := s.endpoints.HasGrant(ctx, applicationID, ep.ID)
	if err != nil {
		return Resolution{}, fmt.Errorf("checking grant app=%d ep=%d: %w", applicationID, ep.ID, err)
	}
	if !granted {
		return Resolution{}, fmt.Errorf("app=%d slug=%q: %w", applicationID, slug, ErrAccessDenied)
	}

	bal, err := s.balancers.For(ep)
	if err != nil {
		return Resolution{}, fmt.Errorf("building balancer for endpoint %q: %w", slug, err)
	}

	target, err := bal.Select(ep.Targets, clientIP)
	if err != nil {
		return Resolution{}, fmt.Errorf("selecting target for endpoint %q: %w", slug, err)
	}

	return Resolution{Endpoint: ep, Target: target, Balancer: bal}, nil
}
