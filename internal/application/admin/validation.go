package admin

import (
	"context"
	"errors"

	"github.com/akarso/shopanda/internal/platform/apperror"
)

// OrderValidator provides validation helpers for admin order operations.
type OrderValidator struct{}

// NewOrderValidator creates an OrderValidator.
func NewOrderValidator() OrderValidator {
	return OrderValidator{}
}

// ValidateOrderID validates that an order ID is not empty and meets basic constraints.
func (v OrderValidator) ValidateOrderID(id string) error {
	if id == "" {
		return apperror.Validation("order id is required")
	}
	// Prevent excessively large IDs (potential injection or resource exhaustion)
	if len(id) > 255 {
		return apperror.Validation("order id is too long")
	}
	return nil
}

// ValidatePagination ensures pagination parameters are sane and safe.
// This prevents resource exhaustion and SQL injection via limit/offset manipulation.
func (v OrderValidator) ValidatePagination(offset, limit int) error {
	const (
		maxLimit  = 1000 // Max 1000 items per page
		minLimit  = 1
		minOffset = 0
		maxOffset = 1000000 // Prevent scanning to absurd depths
	)

	if offset < minOffset {
		return apperror.Validation("offset must be >= 0")
	}
	if offset > maxOffset {
		return apperror.Validation("offset is too large")
	}
	if limit < minLimit {
		return apperror.Validation("limit must be >= 1")
	}
	if limit > maxLimit {
		return apperror.Validation("limit must be <= 1000")
	}
	return nil
}

// AdminContext represents the context of an admin request.
type AdminContext struct {
	AdminID     string   // The authenticated admin user ID
	AdminEmail  string   // The admin's email for audit purposes
	Permissions []string // The admin's permissions
	StoreID     string   // The store scope for this request
	Language    string   // The language scope for this request
	Currency    string   // The currency scope for this request
}

// AdminContextKey is the context key for AdminContext.
type AdminContextKey struct{}

// FromContext extracts the AdminContext from a request context.
func FromContext(ctx context.Context) (*AdminContext, error) {
	v := ctx.Value(AdminContextKey{})
	if v == nil {
		return nil, errors.New("admin context not found in request")
	}
	adminCtx, ok := v.(*AdminContext)
	if !ok {
		return nil, errors.New("admin context has wrong type")
	}
	return adminCtx, nil
}

// WithContext returns a new context with the AdminContext.
func (ac *AdminContext) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, AdminContextKey{}, ac)
}

// HasPermission checks if the admin has a specific permission.
func (ac *AdminContext) HasPermission(permission string) bool {
	for _, p := range ac.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}
