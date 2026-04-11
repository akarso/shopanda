package promotion_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/platform/id"
)

// --- PromotionType ---

func TestPromotionType_IsValid(t *testing.T) {
	tests := []struct {
		typ  promotion.PromotionType
		want bool
	}{
		{promotion.TypeCatalog, true},
		{promotion.TypeCart, true},
		{"bogus", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.typ.IsValid(); got != tt.want {
			t.Errorf("PromotionType(%q).IsValid() = %v, want %v", tt.typ, got, tt.want)
		}
	}
}

// --- NewPromotion ---

func TestNewPromotion_Valid(t *testing.T) {
	pid := id.New()
	p, err := promotion.NewPromotion(pid, "Summer Sale", promotion.TypeCatalog)
	if err != nil {
		t.Fatalf("NewPromotion: %v", err)
	}
	if p.ID != pid {
		t.Errorf("ID = %q, want %q", p.ID, pid)
	}
	if p.Name != "Summer Sale" {
		t.Errorf("Name = %q, want %q", p.Name, "Summer Sale")
	}
	if p.Type != promotion.TypeCatalog {
		t.Errorf("Type = %q, want %q", p.Type, promotion.TypeCatalog)
	}
	if !p.Active {
		t.Error("expected Active=true by default")
	}
	if p.StartAt != nil {
		t.Error("expected StartAt=nil by default")
	}
	if p.EndAt != nil {
		t.Error("expected EndAt=nil by default")
	}
}

func TestNewPromotion_EmptyID(t *testing.T) {
	_, err := promotion.NewPromotion("", "Sale", promotion.TypeCatalog)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewPromotion_InvalidID(t *testing.T) {
	_, err := promotion.NewPromotion("not-a-uuid", "Sale", promotion.TypeCatalog)
	if err == nil {
		t.Fatal("expected error for invalid id")
	}
}

func TestNewPromotion_EmptyName(t *testing.T) {
	_, err := promotion.NewPromotion(id.New(), "", promotion.TypeCatalog)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestNewPromotion_InvalidType(t *testing.T) {
	_, err := promotion.NewPromotion(id.New(), "Sale", "bogus")
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

// --- IsEligible ---

func TestPromotion_IsEligible_ActiveNoWindow(t *testing.T) {
	p, _ := promotion.NewPromotion(id.New(), "Sale", promotion.TypeCatalog)
	if !p.IsEligible(time.Now()) {
		t.Error("expected eligible: active, no time window")
	}
}

func TestPromotion_IsEligible_Inactive(t *testing.T) {
	p, _ := promotion.NewPromotion(id.New(), "Sale", promotion.TypeCatalog)
	p.Active = false
	if p.IsEligible(time.Now()) {
		t.Error("expected not eligible: inactive")
	}
}

func TestPromotion_IsEligible_BeforeStart(t *testing.T) {
	p, _ := promotion.NewPromotion(id.New(), "Sale", promotion.TypeCatalog)
	future := time.Now().Add(24 * time.Hour)
	p.StartAt = &future
	if p.IsEligible(time.Now()) {
		t.Error("expected not eligible: before start")
	}
}

func TestPromotion_IsEligible_AfterEnd(t *testing.T) {
	p, _ := promotion.NewPromotion(id.New(), "Sale", promotion.TypeCatalog)
	past := time.Now().Add(-24 * time.Hour)
	p.EndAt = &past
	if p.IsEligible(time.Now()) {
		t.Error("expected not eligible: after end")
	}
}

func TestPromotion_IsEligible_WithinWindow(t *testing.T) {
	p, _ := promotion.NewPromotion(id.New(), "Sale", promotion.TypeCatalog)
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now().Add(1 * time.Hour)
	p.StartAt = &start
	p.EndAt = &end
	if !p.IsEligible(time.Now()) {
		t.Error("expected eligible: within time window")
	}
}
