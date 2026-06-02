package proxy

import (
	"net"
	"net/http"
	"time"
)

// NewTransport returns an *http.Transport tuned for outbound proxy calls.
//
// Settings rationale:
//   - DialContext: 5 s connect timeout — enough for transient routing hiccups
//     but fast enough to fail before user-visible timeouts on the inbound request.
//   - TLSHandshakeTimeout: 5 s — same logic as DialContext.
//   - ResponseHeaderTimeout: 30 s — first-byte budget; SSE streams may then take
//     arbitrarily long for subsequent chunks.
//   - ExpectContinueTimeout: 1 s — only matters when a client sends
//     "Expect: 100-continue" with a large body.
//   - MaxIdleConnsPerHost: 50 — keeps connection reuse high under fan-in across
//     multiple targets without exhausting upstream connection limits.
//   - MaxIdleConns: 200 — sum cap across all hosts.
//   - IdleConnTimeout: 90 s — Go default; closes stale TCP sessions.
//   - DisableCompression: false — let upstreams negotiate gzip; ReverseProxy
//     forwards the body verbatim, so we just relay the encoding header.
//
// Reasoning: a single shared transport across all targets keeps connection-pool
// reuse high. Per-target transports would waste TCP handshakes on warm targets.
//
// References:
//   - https://pkg.go.dev/net/http#Transport
//   - https://pkg.go.dev/net/http/httputil#ReverseProxy
func NewTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
