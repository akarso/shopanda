package routing

import "context"

// RewriteRepository persists and retrieves URL rewrites.
type RewriteRepository interface {
	FindByPath(ctx context.Context, path string) (*URLRewrite, error)
	Save(ctx context.Context, rw *URLRewrite) error
	Delete(ctx context.Context, path string) error
}
