// Package proxyservice implements the application-layer use cases for the generic
// HTTP proxy engine: endpoint lookup, grant verification, and target selection.
//
// The package owns no HTTP-specific concerns — that is the responsibility of the
// internal/proxy package. proxyservice imports only domain types, the load-balancer
// package, and Go stdlib, keeping it fully unit-testable without infrastructure.
//
// References:
//   - ADR-0010 — generic HTTP proxy engine
//   - ADR-0013 — load balancing strategies
//   - ADR-0015 — app layer use cases
package proxyservice
