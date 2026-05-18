package analyzer

import "context"

type sessionContextKey struct{}

// WithSessionContext attaches a SessionContext to a context.
func WithSessionContext(ctx context.Context, sc *SessionContext) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, sc)
}

// GetSessionContext retrieves the SessionContext from a context.
func GetSessionContext(ctx context.Context) *SessionContext {
	sc, ok := ctx.Value(sessionContextKey{}).(*SessionContext)
	if !ok {
		return &SessionContext{}
	}
	return sc
}
