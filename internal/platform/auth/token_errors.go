package auth

import "errors"

// ErrNotCustomerToken means the bearer string is not a customer JWT (wrong
// shape — e.g. a plugin owner token). Middleware may fall back to the
// storefront session cookie or continue as guest. Do not use this for an
// expired, revoked, or tampered customer JWT.
var ErrNotCustomerToken = errors.New("auth: not a customer token")

// IsNotCustomerToken reports whether err is (or wraps) ErrNotCustomerToken.
func IsNotCustomerToken(err error) bool {
	return errors.Is(err, ErrNotCustomerToken)
}
