package logkit

import "context"

type contextKey struct{}

// IntoContext stores the logger in ctx so it can be retrieved later with FromContext.
// Use in middleware to set a request-scoped logger; handlers then call FromContext(ctx) to get it.
// Panics if ctx is nil. The same ctx should be passed along the request chain.
func IntoContext(ctx context.Context, l Logger) context.Context {
	if ctx == nil {
		panic("logkit: nil context passed to IntoContext")
	}
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext returns the logger from ctx (set by IntoContext). Returns Noop() if ctx is nil or
// if no logger was stored—so callers can always use the result without a nil check.
func FromContext(ctx context.Context) Logger {
	if ctx == nil {
		return Noop()
	}
	if l, ok := ctx.Value(contextKey{}).(Logger); ok && l != nil {
		return l
	}
	return Noop()
}
