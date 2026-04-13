package store

import "context"

type ctxKey string

const storeKey ctxKey = "store"

// WithStore stores a Store in the context.
func WithStore(ctx context.Context, s *Store) context.Context {
	return context.WithValue(ctx, storeKey, s)
}

// FromContext extracts the Store from the context.
// Returns nil if no store is present.
func FromContext(ctx context.Context) *Store {
	if v, ok := ctx.Value(storeKey).(*Store); ok {
		return v
	}
	return nil
}
