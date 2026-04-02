package auth

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/identity"
)

type ctxKey string

const identityKey ctxKey = "identity"

// WithIdentity stores an Identity in the context.
func WithIdentity(ctx context.Context, id identity.Identity) context.Context {
	return context.WithValue(ctx, identityKey, id)
}

// IdentityFrom extracts the Identity from the context.
// Returns a guest identity if none is present.
func IdentityFrom(ctx context.Context) identity.Identity {
	if v, ok := ctx.Value(identityKey).(identity.Identity); ok {
		return v
	}
	return identity.Guest()
}
