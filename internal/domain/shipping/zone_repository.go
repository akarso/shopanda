package shipping

import "context"

// ZoneRepository defines persistence operations for shipping zones and rate tiers.
type ZoneRepository interface {
	// ListZones returns all shipping zones ordered by priority descending.
	ListZones(ctx context.Context) ([]Zone, error)

	// FindZoneByID returns a zone by its ID. Returns (nil, nil) when not found.
	FindZoneByID(ctx context.Context, id string) (*Zone, error)

	// CreateZone persists a new shipping zone.
	CreateZone(ctx context.Context, z *Zone) error

	// UpdateZone updates a shipping zone's mutable fields.
	UpdateZone(ctx context.Context, z *Zone) error

	// DeleteZone removes a zone and its rate tiers.
	DeleteZone(ctx context.Context, id string) error

	// ListRateTiers returns all rate tiers for a zone ordered by min_weight.
	ListRateTiers(ctx context.Context, zoneID string) ([]RateTier, error)

	// CreateRateTier persists a new rate tier.
	CreateRateTier(ctx context.Context, rt *RateTier) error

	// UpdateRateTier updates a rate tier's fields.
	UpdateRateTier(ctx context.Context, rt *RateTier) error

	// FindRateTierByID returns a rate tier by its ID. Returns (nil, nil) when not found.
	FindRateTierByID(ctx context.Context, id string) (*RateTier, error)

	// DeleteRateTier removes a rate tier by ID.
	DeleteRateTier(ctx context.Context, id string) error
}
