// Package proxy implements the HTTP layer of the generic proxy engine: a chi
// sub-router mounted at /v1/proxy that authenticates the calling application
// against the DB, resolves the slug to a target via proxyservice, and forwards
// the request using net/http/httputil.ReverseProxy.
//
// File layout:
//
//	context.go    — typed context keys + accessors for the authenticated Application
//	auth.go       — DB-backed Bearer-token middleware
//	transport.go  — tuned *http.Transport shared by all upstream calls
//	director.go   — request-rewriting callback used by ReverseProxy
//	handler.go    — top-level http.Handler that ties everything together
//
// Streaming responses are handled correctly because ReverseProxy uses
// io.Copy with FlushInterval=-1 (immediate flush) by default in Go 1.21+,
// and our tuned transport disables response-body buffering.
//
// References:
//   - ADR-0010 — generic HTTP proxy engine
//   - ADR-0012 — target credentials decrypted in memory only
//   - ADR-0013 — load balancing strategies
//   - https://pkg.go.dev/net/http/httputil#ReverseProxy
package proxy
