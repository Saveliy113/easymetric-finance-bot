package trace

import "context"

type ctxKey struct{}

// Extending context with trace id
func WithId(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// Extracts trace_id or returns empty string
func FromContext(ctx context.Context) string {
	if id, ok := ctx.Value(ctxKey{}).(string); ok {
		return id
	}
	return ""
}
