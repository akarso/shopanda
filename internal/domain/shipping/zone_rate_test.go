package shipping_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
)

// --- mock repository ---

type mockZoneRepo struct {
	listZonesFn     func(ctx context.Context) ([]shipping.Zone, error)
	listRateTiersFn func(ctx context.Context, zoneID string) ([]shipping.RateTier, error)
}

func (m *mockZoneRepo) ListZones(ctx context.Context) ([]shipping.Zone, error) {
	if m.listZonesFn != nil {
		return m.listZonesFn(ctx)
	}
	return nil, nil
}
func (m *mockZoneRepo) ListRateTiers(ctx context.Context, zoneID string) ([]shipping.RateTier, error) {
	if m.listRateTiersFn != nil {
		return m.listRateTiersFn(ctx, zoneID)
	}
	return nil, nil
}
func (m *mockZoneRepo) FindZoneByID(_ context.Context, _ string) (*shipping.Zone, error) {
	return nil, nil
}
func (m *mockZoneRepo) CreateZone(_ context.Context, _ *shipping.Zone) error { return nil }
func (m *mockZoneRepo) UpdateZone(_ context.Context, _ *shipping.Zone) error { return nil }
func (m *mockZoneRepo) DeleteZone(_ context.Context, _ string) error         { return nil }
func (m *mockZoneRepo) FindRateTierByID(_ context.Context, _ string) (*shipping.RateTier, error) {
	return nil, nil
}
func (m *mockZoneRepo) CreateRateTier(_ context.Context, _ *shipping.RateTier) error { return nil }
func (m *mockZoneRepo) UpdateRateTier(_ context.Context, _ *shipping.RateTier) error { return nil }
func (m *mockZoneRepo) DeleteRateTier(_ context.Context, _ string) error             { return nil }

// --- helpers ---

func mustZone(t *testing.T, id, name string, countries []string, priority int, active bool) shipping.Zone {
	t.Helper()
	z, err := shipping.NewZone(id, name, countries, priority)
	if err != nil {
		t.Fatalf("mustZone: %v", err)
	}
	z.Active = active
	return z
}

func mustTier(t *testing.T, id, zoneID string, min, max float64, amount int64) shipping.RateTier {
	t.Helper()
	price := shared.MustNewMoney(amount, "EUR")
	rt, err := shipping.NewRateTier(id, zoneID, min, max, price)
	if err != nil {
		t.Fatalf("mustTier: %v", err)
	}
	return rt
}

// --- CalculateRate tests ---

func TestZoneRateCalculator_HappyPath(t *testing.T) {
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return []shipping.Zone{
				mustZone(t, "z1", "Domestic", []string{"DE"}, 10, true),
			}, nil
		},
		listRateTiersFn: func(_ context.Context, _ string) ([]shipping.RateTier, error) {
			return []shipping.RateTier{
				mustTier(t, "t1", "z1", 0, 5, 500),
				mustTier(t, "t2", "z1", 5, 10, 800),
			}, nil
		},
	}
	calc := shipping.NewZoneRateCalculator(repo)

	rate, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Country:  "DE",
		Currency: "EUR",
		Items: []shipping.RateRequestItem{
			{Weight: 1.5, Quantity: 2}, // total 3 kg → first tier
		},
	})
	if err != nil {
		t.Fatalf("CalculateRate: %v", err)
	}
	if rate.Cost.Amount() != 500 {
		t.Errorf("cost = %d, want 500", rate.Cost.Amount())
	}
	if rate.Label != "Domestic Shipping" {
		t.Errorf("label = %q, want 'Domestic Shipping'", rate.Label)
	}
}

func TestZoneRateCalculator_HigherWeightTier(t *testing.T) {
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return []shipping.Zone{
				mustZone(t, "z1", "Domestic", []string{"DE"}, 10, true),
			}, nil
		},
		listRateTiersFn: func(_ context.Context, _ string) ([]shipping.RateTier, error) {
			return []shipping.RateTier{
				mustTier(t, "t1", "z1", 0, 5, 500),
				mustTier(t, "t2", "z1", 5, 10, 800),
			}, nil
		},
	}
	calc := shipping.NewZoneRateCalculator(repo)

	rate, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Country:  "DE",
		Currency: "EUR",
		Items: []shipping.RateRequestItem{
			{Weight: 3.0, Quantity: 2}, // total 6 kg → second tier
		},
	})
	if err != nil {
		t.Fatalf("CalculateRate: %v", err)
	}
	if rate.Cost.Amount() != 800 {
		t.Errorf("cost = %d, want 800", rate.Cost.Amount())
	}
}

func TestZoneRateCalculator_UnlimitedMaxWeight(t *testing.T) {
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return []shipping.Zone{
				mustZone(t, "z1", "EU", []string{"FR"}, 5, true),
			}, nil
		},
		listRateTiersFn: func(_ context.Context, _ string) ([]shipping.RateTier, error) {
			return []shipping.RateTier{
				mustTier(t, "t1", "z1", 10, 0, 1200), // max=0 means unlimited
			}, nil
		},
	}
	calc := shipping.NewZoneRateCalculator(repo)

	rate, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Country:  "FR",
		Currency: "EUR",
		Items: []shipping.RateRequestItem{
			{Weight: 50.0, Quantity: 1}, // 50 kg, unlimited tier catches it
		},
	})
	if err != nil {
		t.Fatalf("CalculateRate: %v", err)
	}
	if rate.Cost.Amount() != 1200 {
		t.Errorf("cost = %d, want 1200", rate.Cost.Amount())
	}
}

func TestZoneRateCalculator_MultiZonePriority(t *testing.T) {
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return []shipping.Zone{
				mustZone(t, "z-eu", "EU", []string{"DE", "FR", "IT"}, 5, true),
				mustZone(t, "z-de", "Domestic", []string{"DE"}, 10, true), // higher priority
			}, nil
		},
		listRateTiersFn: func(_ context.Context, zoneID string) ([]shipping.RateTier, error) {
			if zoneID == "z-de" {
				return []shipping.RateTier{
					mustTier(t, "t-de", "z-de", 0, 0, 300),
				}, nil
			}
			return []shipping.RateTier{
				mustTier(t, "t-eu", "z-eu", 0, 0, 900),
			}, nil
		},
	}
	calc := shipping.NewZoneRateCalculator(repo)

	rate, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Country:  "DE",
		Currency: "EUR",
		Items:    []shipping.RateRequestItem{{Weight: 1.0, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("CalculateRate: %v", err)
	}
	// Domestic zone wins (priority 10 > 5)
	if rate.Cost.Amount() != 300 {
		t.Errorf("cost = %d, want 300 (domestic zone)", rate.Cost.Amount())
	}
}

func TestZoneRateCalculator_InactiveZoneSkipped(t *testing.T) {
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return []shipping.Zone{
				mustZone(t, "z1", "Disabled", []string{"DE"}, 100, false), // inactive
				mustZone(t, "z2", "Active", []string{"DE"}, 1, true),
			}, nil
		},
		listRateTiersFn: func(_ context.Context, zoneID string) ([]shipping.RateTier, error) {
			return []shipping.RateTier{
				mustTier(t, "t1", zoneID, 0, 0, 400),
			}, nil
		},
	}
	calc := shipping.NewZoneRateCalculator(repo)

	rate, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Country:  "DE",
		Currency: "EUR",
		Items:    []shipping.RateRequestItem{{Weight: 1.0, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("CalculateRate: %v", err)
	}
	// Active zone picked despite lower priority; inactive skipped
	if rate.Label != "Active Shipping" {
		t.Errorf("label = %q, want 'Active Shipping'", rate.Label)
	}
}

func TestZoneRateCalculator_NoZone(t *testing.T) {
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return []shipping.Zone{
				mustZone(t, "z1", "EU", []string{"FR"}, 5, true),
			}, nil
		},
	}
	calc := shipping.NewZoneRateCalculator(repo)

	_, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Country:  "US",
		Currency: "EUR",
		Items:    []shipping.RateRequestItem{{Weight: 1.0, Quantity: 1}},
	})
	if err == nil {
		t.Fatal("expected error for unmatched country")
	}
}

func TestZoneRateCalculator_NoMatchingTier(t *testing.T) {
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return []shipping.Zone{
				mustZone(t, "z1", "EU", []string{"DE"}, 5, true),
			}, nil
		},
		listRateTiersFn: func(_ context.Context, _ string) ([]shipping.RateTier, error) {
			return []shipping.RateTier{
				mustTier(t, "t1", "z1", 0, 5, 500), // max 5 kg
			}, nil
		},
	}
	calc := shipping.NewZoneRateCalculator(repo)

	_, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Country:  "DE",
		Currency: "EUR",
		Items:    []shipping.RateRequestItem{{Weight: 10.0, Quantity: 1}}, // 10 kg > max 5
	})
	if err == nil {
		t.Fatal("expected error for weight exceeding all tiers")
	}
}

func TestZoneRateCalculator_EmptyItems(t *testing.T) {
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return []shipping.Zone{
				mustZone(t, "z1", "EU", []string{"DE"}, 5, true),
			}, nil
		},
		listRateTiersFn: func(_ context.Context, _ string) ([]shipping.RateTier, error) {
			return []shipping.RateTier{
				mustTier(t, "t1", "z1", 0, 0, 500), // unlimited, covers 0 weight
			}, nil
		},
	}
	calc := shipping.NewZoneRateCalculator(repo)

	rate, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Country:  "DE",
		Currency: "EUR",
		Items:    nil, // empty cart → weight 0
	})
	if err != nil {
		t.Fatalf("CalculateRate: %v", err)
	}
	if rate.Cost.Amount() != 500 {
		t.Errorf("cost = %d, want 500", rate.Cost.Amount())
	}
}

func TestZoneRateCalculator_CurrencyMismatch(t *testing.T) {
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return []shipping.Zone{
				mustZone(t, "z1", "EU", []string{"DE"}, 5, true),
			}, nil
		},
		listRateTiersFn: func(_ context.Context, _ string) ([]shipping.RateTier, error) {
			return []shipping.RateTier{
				mustTier(t, "t1", "z1", 0, 0, 500), // EUR tier
			}, nil
		},
	}
	calc := shipping.NewZoneRateCalculator(repo)

	_, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Country:  "DE",
		Currency: "USD", // mismatch with EUR tier
		Items:    []shipping.RateRequestItem{{Weight: 1.0, Quantity: 1}},
	})
	if err == nil {
		t.Fatal("expected error for currency mismatch")
	}
}

func TestZoneRateCalculator_MissingCountry(t *testing.T) {
	calc := shipping.NewZoneRateCalculator(&mockZoneRepo{})
	_, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Currency: "EUR",
	})
	if err == nil {
		t.Fatal("expected error for missing country")
	}
}

func TestZoneRateCalculator_MissingCurrency(t *testing.T) {
	calc := shipping.NewZoneRateCalculator(&mockZoneRepo{})
	_, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Country: "DE",
	})
	if err == nil {
		t.Fatal("expected error for missing currency")
	}
}

func TestZoneRateCalculator_MostSpecificTier(t *testing.T) {
	repo := &mockZoneRepo{
		listZonesFn: func(_ context.Context) ([]shipping.Zone, error) {
			return []shipping.Zone{
				mustZone(t, "z1", "EU", []string{"DE"}, 5, true),
			}, nil
		},
		listRateTiersFn: func(_ context.Context, _ string) ([]shipping.RateTier, error) {
			return []shipping.RateTier{
				mustTier(t, "t1", "z1", 0, 0, 500),  // catch-all
				mustTier(t, "t2", "z1", 5, 10, 800), // specific 5-10 kg
			}, nil
		},
	}
	calc := shipping.NewZoneRateCalculator(repo)

	rate, err := calc.CalculateRate(context.Background(), shipping.RateRequest{
		Country:  "DE",
		Currency: "EUR",
		Items:    []shipping.RateRequestItem{{Weight: 7.0, Quantity: 1}}, // 7 kg
	})
	if err != nil {
		t.Fatalf("CalculateRate: %v", err)
	}
	// Both tiers match (t1: min=0, t2: min=5), but t2 is more specific (higher min)
	if rate.Cost.Amount() != 800 {
		t.Errorf("cost = %d, want 800 (most specific tier)", rate.Cost.Amount())
	}
}
