// Package loadbalancer implements the five load-balancing strategies supported by
// the generic HTTP proxy engine (ADR-0013): round_robin, weighted_round_robin,
// random, least_connections, and ip_hash.
//
// The Balancer interface is intentionally minimal: Select returns one Target from
// the input list (using clientIP only for sticky strategies); OnRequestStart and
// OnRequestEnd let stateful strategies (least_connections) track in-flight counts.
// Stateless strategies implement the two hooks as no-ops.
//
// The Registry holds one Balancer per ProxyEndpoint ID. It detects strategy
// changes on the next lookup and replaces the stale Balancer accordingly, so a
// gateway operator can switch strategies via the Admin API without restart.
//
// All algorithms are O(N) in the number of targets per request; with the small
// fan-out typical of a gateway endpoint (≤10 targets) this is well within budget
// and avoids the complexity of skip-lists or priority queues.
//
// References:
//   - ADR-0013 — load balancing strategies
//   - ADR-0010 — generic HTTP proxy engine
//   - https://www.nginx.com/blog/test-drive-nginx-plus-load-balancing-algorithms/
package loadbalancer
