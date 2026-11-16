package auth

import "github.com/akarso/shopanda/internal/domain/identity"

// StorefrontActorID returns the authenticated account id for storefront cart,
// account, and checkout, or "" for guests. Staff roles (admin/manager/…) are
// valid storefront actors when that account is the session identity.
func StorefrontActorID(id identity.Identity) string {
	if id.IsGuest() {
		return ""
	}
	return id.UserID
}
