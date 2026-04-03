package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/jwt"
)

// genCacheEntry holds a cached token generation value with expiry.
type genCacheEntry struct {
	gen    int64
	expiry time.Time
}

// ValidatingTokenParser implements auth.TokenParser by verifying the JWT
// signature and checking the token generation against the stored customer.
// It holds a short-lived in-memory cache to reduce DB hits.
type ValidatingTokenParser struct {
	issuer    *jwt.Issuer
	customers customer.CustomerRepository
	cacheTTL  time.Duration

	mu    sync.RWMutex
	cache map[string]genCacheEntry
}

// NewValidatingTokenParser creates a token parser that validates JWT
// signature and checks token generation against the customer record.
// cacheTTL controls how long a customer's generation is cached (0 disables caching).
func NewValidatingTokenParser(issuer *jwt.Issuer, customers customer.CustomerRepository, cacheTTL time.Duration) *ValidatingTokenParser {
	return &ValidatingTokenParser{
		issuer:    issuer,
		customers: customers,
		cacheTTL:  cacheTTL,
		cache:     make(map[string]genCacheEntry),
	}
}

// Parse extracts an Identity from a JWT bearer token, verifying that the
// token generation matches the customer's current generation.
func (p *ValidatingTokenParser) Parse(ctx context.Context, token string) (identity.Identity, error) {
	claims, err := p.issuer.Parse(token)
	if err != nil {
		return identity.Identity{}, fmt.Errorf("validating parser: %w", err)
	}

	gen, ok := p.getCached(claims.Sub)
	if !ok {
		c, err := p.customers.FindByID(ctx, claims.Sub)
		if err != nil {
			return identity.Identity{}, fmt.Errorf("validating parser: %w", err)
		}
		if c == nil {
			return identity.Identity{}, apperror.Unauthorized("invalid token")
		}
		gen = c.TokenGeneration
		p.putCached(claims.Sub, gen)
	}

	if gen != claims.Gen {
		return identity.Identity{}, apperror.Unauthorized("token revoked")
	}

	role := identity.Role(claims.Role)
	id, err := identity.NewIdentity(claims.Sub, role)
	if err != nil {
		return identity.Identity{}, fmt.Errorf("validating parser: %w", err)
	}
	return id, nil
}

// getCached returns the cached generation for a customer, if present and not expired.
func (p *ValidatingTokenParser) getCached(customerID string) (int64, bool) {
	if p.cacheTTL <= 0 {
		return 0, false
	}
	p.mu.RLock()
	entry, ok := p.cache[customerID]
	p.mu.RUnlock()
	if !ok || time.Now().After(entry.expiry) {
		return 0, false
	}
	return entry.gen, true
}

// putCached stores a customer's generation in the cache.
func (p *ValidatingTokenParser) putCached(customerID string, gen int64) {
	if p.cacheTTL <= 0 {
		return
	}
	p.mu.Lock()
	p.cache[customerID] = genCacheEntry{gen: gen, expiry: time.Now().Add(p.cacheTTL)}
	p.mu.Unlock()
}
