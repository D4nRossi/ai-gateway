package proxy

import (
	"context"

	"github.com/D4nRossi/ai-gateway/internal/domain/application"
)

// ctxKey is an unexported type for the proxy package's context keys, preventing
// accidental collisions with keys defined by other packages.
type ctxKey int

const (
	applicationKey ctxKey = iota
)

// ApplicationFromCtx retrieves the Application injected by Auth.
// Returns the zero value and false when called outside an Auth-protected handler.
func ApplicationFromCtx(ctx context.Context) (application.Application, bool) {
	a, ok := ctx.Value(applicationKey).(application.Application)
	return a, ok
}

// withApplication is the internal helper used by Auth to inject the Application
// into a request context. It is unexported because only Auth should populate it.
func withApplication(ctx context.Context, app application.Application) context.Context {
	return context.WithValue(ctx, applicationKey, app)
}
