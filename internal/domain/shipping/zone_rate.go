package shipping

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// RateRequestItem describes a single line item for rate calculation.
type RateRequestItem struct {
	Weight   float64 // kg per unit
	Quantity int
}

// RateRequest holds the inputs for weight-based rate calculation.
type RateRequest struct {
	Country  string            // ISO 3166-1 alpha-2
	Currency string            // ISO 4217
	Items    []RateRequestItem // cart line items
}

// ZoneRateCalculator resolves shipping zones and matches weight-based tiers.
type ZoneRateCalculator struct {
	zones ZoneRepository
}

// NewZoneRateCalculator creates a calculator backed by the given repository.
func NewZoneRateCalculator(zones ZoneRepository) (*ZoneRateCalculator, error) {
	if zones == nil {
		return nil, errors.New("shipping: zone repository must not be nil")
	}
	return &ZoneRateCalculator{zones: zones}, nil
}

// CalculateRate resolves the zone for the given country, sums item weights,
// finds the matching rate tier, and returns a ShippingRate.
func (c *ZoneRateCalculator) CalculateRate(ctx context.Context, req RateRequest) (ShippingRate, error) {
	if req.Country == "" {
		return ShippingRate{}, errors.New("shipping: country is required")
	}
	if req.Currency == "" {
		return ShippingRate{}, errors.New("shipping: currency is required")
	}

	country := strings.ToUpper(req.Country)
	currency := strings.ToUpper(req.Currency)

	zones, err := c.zones.ListZones(ctx)
	if err != nil {
		return ShippingRate{}, fmt.Errorf("shipping: list zones: %w", err)
	}

	zone := resolveZone(zones, country)
	if zone == nil {
		return ShippingRate{}, fmt.Errorf("shipping: no zone available for country %s", country)
	}

	totalWeight := sumWeights(req.Items)

	tiers, err := c.zones.ListRateTiers(ctx, zone.ID)
	if err != nil {
		return ShippingRate{}, fmt.Errorf("shipping: list rate tiers: %w", err)
	}

	tier := matchTier(tiers, totalWeight)
	if tier == nil {
		return ShippingRate{}, fmt.Errorf("shipping: no rate tier for weight %.2f", totalWeight)
	}

	if !strings.EqualFold(tier.Price.Currency(), currency) {
		return ShippingRate{}, fmt.Errorf("shipping: rate currency %s does not match requested %s", tier.Price.Currency(), currency)
	}

	return ShippingRate{
		ProviderRef: "zone:" + zone.ID + ":tier:" + tier.ID,
		Cost:        tier.Price,
		Label:       zone.Name + " Shipping",
	}, nil
}

// resolveZone finds the highest-priority active zone whose countries include
// the given country code. Returns nil when no zone matches.
func resolveZone(zones []Zone, country string) *Zone {
	var best *Zone
	for i := range zones {
		z := &zones[i]
		if !z.Active {
			continue
		}
		if !zoneContainsCountry(z, country) {
			continue
		}
		if best == nil || z.Priority > best.Priority {
			best = z
		}
	}
	return best
}

func zoneContainsCountry(z *Zone, country string) bool {
	for _, c := range z.Countries {
		if c == country {
			return true
		}
	}
	return false
}

// matchTier finds a tier where min_weight <= weight AND (max_weight == 0 OR max_weight >= weight).
// When multiple tiers match, the tier with the highest min_weight wins (most specific).
func matchTier(tiers []RateTier, weight float64) *RateTier {
	var best *RateTier
	for i := range tiers {
		t := &tiers[i]
		if weight < t.MinWeight {
			continue
		}
		if t.MaxWeight > 0 && weight > t.MaxWeight {
			continue
		}
		if best == nil || t.MinWeight > best.MinWeight {
			best = t
		}
	}
	return best
}

// sumWeights computes total cart weight: sum(item.Weight * item.Quantity).
func sumWeights(items []RateRequestItem) float64 {
	var total float64
	for _, item := range items {
		total += item.Weight * float64(item.Quantity)
	}
	return total
}
