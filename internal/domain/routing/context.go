package routing

import "context"

type ctxKey string

const rewriteKey ctxKey = "url_rewrite"

// WithRewrite stores a URLRewrite in the context.
func WithRewrite(ctx context.Context, rw *URLRewrite) context.Context {
	return context.WithValue(ctx, rewriteKey, rw)
}

// RewriteFrom extracts the URLRewrite from the context.
// Returns nil if none is present.
func RewriteFrom(ctx context.Context) *URLRewrite {
	rw, _ := ctx.Value(rewriteKey).(*URLRewrite)
	return rw
}
