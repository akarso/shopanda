package cart

import (
	"errors"
	"fmt"

	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/pkg/extapi"
)

// ValidationFailed indicates cart.validate found blocking issues during a mutation.
type ValidationFailed struct {
	Cart   *domainCart.Cart
	Issues []extapi.CartValidationIssue
}

func (e *ValidationFailed) Error() string {
	if e == nil {
		return "cart validation failed"
	}
	return fmt.Sprintf("cart validation failed (%d issue(s))", len(e.Issues))
}

// IsValidationFailed reports whether err is a cart ValidationFailed error.
func IsValidationFailed(err error) bool {
	var vf *ValidationFailed
	return errors.As(err, &vf)
}

// ValidationFailedFrom returns the ValidationFailed value when err wraps one.
func ValidationFailedFrom(err error) (*ValidationFailed, bool) {
	var vf *ValidationFailed
	if errors.As(err, &vf) {
		return vf, true
	}
	return nil, false
}
