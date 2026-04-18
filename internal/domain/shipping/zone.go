package shipping

import (
	"errors"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/shared"
)

// Zone represents a geographic shipping zone with associated rate tiers.
type Zone struct {
	ID        string
	Name      string
	Countries []string // ISO 3166-1 alpha-2 codes
	Priority  int      // higher priority wins on overlap
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewZone creates a new active shipping zone.
func NewZone(id, name string, countries []string, priority int) (Zone, error) {
	if id == "" {
		return Zone{}, errors.New("shipping: zone id must not be empty")
	}
	if name == "" {
		return Zone{}, errors.New("shipping: zone name must not be empty")
	}
	if len(countries) == 0 {
		return Zone{}, errors.New("shipping: zone must have at least one country")
	}
	normalized := make([]string, 0, len(countries))
	for _, c := range countries {
		if len(c) != 2 {
			return Zone{}, errors.New("shipping: country code must be 2 characters: " + c)
		}
		normalized = append(normalized, strings.ToUpper(c))
	}

	now := time.Now().UTC()
	return Zone{
		ID:        id,
		Name:      name,
		Countries: normalized,
		Priority:  priority,
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// RateTier represents a weight-based shipping rate within a zone.
type RateTier struct {
	ID        string
	ZoneID    string
	MinWeight float64 // kg, 0 = no minimum
	MaxWeight float64 // kg, 0 = no maximum (unlimited)
	Price     shared.Money
}

// NewRateTier creates a new rate tier for a zone.
func NewRateTier(id, zoneID string, minWeight, maxWeight float64, price shared.Money) (RateTier, error) {
	if id == "" {
		return RateTier{}, errors.New("shipping: rate tier id must not be empty")
	}
	if zoneID == "" {
		return RateTier{}, errors.New("shipping: rate tier zone id must not be empty")
	}
	if minWeight < 0 {
		return RateTier{}, errors.New("shipping: min weight must not be negative")
	}
	if maxWeight < 0 {
		return RateTier{}, errors.New("shipping: max weight must not be negative")
	}
	if maxWeight > 0 && maxWeight < minWeight {
		return RateTier{}, errors.New("shipping: max weight must be >= min weight")
	}
	if price.Amount() < 0 {
		return RateTier{}, errors.New("shipping: price must not be negative")
	}

	return RateTier{
		ID:        id,
		ZoneID:    zoneID,
		MinWeight: minWeight,
		MaxWeight: maxWeight,
		Price:     price,
	}, nil
}
