package store

import "context"

// IdentityRepository persists store identity, addresses, and tax identifier values.
type IdentityRepository interface {
	// GetIdentity returns the identity row, or (nil, nil) when missing.
	GetIdentity(ctx context.Context, storeID string) (*Identity, error)
	// SaveIdentity upserts the identity row.
	SaveIdentity(ctx context.Context, id Identity) error
	// MutateIdentity locks the identity row (FOR UPDATE), applies fn to the
	// current value (nil when missing), and saves the result in one transaction.
	MutateIdentity(ctx context.Context, storeID string, fn func(current *Identity) (Identity, error)) (Identity, error)

	// GetAddress returns one role address, or (nil, nil) when missing.
	GetAddress(ctx context.Context, storeID string, role AddressRole) (*Address, error)
	// ListAddresses returns persisted addresses only (no placeholders).
	ListAddresses(ctx context.Context, storeID string) ([]Address, error)
	// SaveAddress upserts one role address.
	SaveAddress(ctx context.Context, addr Address) error
	// MutateAddress locks the address row for role (FOR UPDATE), applies fn to
	// the current value (nil when missing), and saves in one transaction.
	MutateAddress(ctx context.Context, storeID string, role AddressRole, fn func(current *Address) (Address, error)) (Address, error)

	// ListTaxIdentifiers returns all tax IDs for a store.
	ListTaxIdentifiers(ctx context.Context, storeID string) ([]TaxIdentifier, error)
	// UpsertTaxIdentifier inserts or updates by (store_id, type).
	UpsertTaxIdentifier(ctx context.Context, ti TaxIdentifier) error
	// DeleteTaxIdentifier removes a type for the store.
	// Returns apperror.NotFound when no row matched.
	DeleteTaxIdentifier(ctx context.Context, storeID, typ string) error
}
