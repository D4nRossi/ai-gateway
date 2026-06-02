// Package web embeds the compiled admin SPA into the Go binary via go:embed.
//
// At build time, the React app under web/src is compiled by Vite into
// web/dist/. The Go toolchain then embeds that directory into the binary, so
// shipping the gateway is a single artifact deploy — no separate frontend host,
// no static asset CDN.
//
// Build chain:
//
//  1. cd web && pnpm install && pnpm build     → produces web/dist/*
//  2. go build ./cmd/gateway                    → embeds web/dist via this package
//
// Runtime mount: api/router.go mounts Handler() at /ui. SPA history-API routes
// (e.g. /ui/applications) all serve index.html so React Router can take over.
//
// References:
//   - ADR-0014 — frontend embedded in Go binary
//   - https://pkg.go.dev/embed
package web
