package voucher_test

import (
	"context"
	"testing"
	"time"

	voucherapp "github.com/akarso/shopanda/internal/application/voucher"
	domain "github.com/akarso/shopanda/internal/domain/voucher"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type memRedemptions struct {
	rows []domain.Redemption
}

func (m *memRedemptions) Insert(_ context.Context, r *domain.Redemption) error {
	m.rows = append(m.rows, *r)
	return nil
}
func (m *memRedemptions) ListByVoucherID(_ context.Context, voucherID string) ([]domain.Redemption, error) {
	var out []domain.Redemption
	for _, r := range m.rows {
		if r.VoucherID == voucherID {
			out = append(out, r)
		}
	}
	return out, nil
}

func TestService_UnredeemRequiresNote(t *testing.T) {
	code, err := domain.GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	v, err := domain.NewVoucher(id.New(), code, id.New(), "ord/var", id.New(), "var-1", "cust", 0, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVoucher: %v", err)
	}
	repo := newMemVoucherRepo()
	if err := repo.Insert(context.Background(), v); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	r := &countingRepo{memVoucherRepo: repo}
	svc := voucherapp.NewService(r, event.NewBus(logger.New("error")), nil).WithRedemptions(&memRedemptions{})
	_, err = svc.Unredeem(context.Background(), v.ID, "admin-1", "   ")
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Unredeem empty note = %v, want validation", err)
	}
}

func TestService_RedeemUnredeem(t *testing.T) {
	code, err := domain.GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	v, err := domain.NewVoucher(id.New(), code, id.New(), "ord/var", id.New(), "var-1", "cust", 0, 2, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVoucher: %v", err)
	}
	repo := newMemVoucherRepo()
	if err := repo.Insert(context.Background(), v); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Patch UpdateCounts on mem repo — use a wrapping type.
	r := &countingRepo{memVoucherRepo: repo}
	red := &memRedemptions{}
	svc := voucherapp.NewService(r, event.NewBus(logger.New("error")), nil).WithRedemptions(red)

	got, err := svc.Redeem(context.Background(), v.ID, "admin-1", "door")
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if got.RedeemedCount != 1 || got.Status() != domain.StatusPartiallyRedeemed {
		t.Fatalf("after redeem: count=%d status=%s", got.RedeemedCount, got.Status())
	}
	got, err = svc.Unredeem(context.Background(), v.ID, "admin-1", "mistake")
	if err != nil {
		t.Fatalf("Unredeem: %v", err)
	}
	if got.RedeemedCount != 0 || got.Status() != domain.StatusIssued {
		t.Fatalf("after unredeem: count=%d status=%s", got.RedeemedCount, got.Status())
	}
	if len(red.rows) != 2 {
		t.Fatalf("redemptions = %d, want 2", len(red.rows))
	}
}

type countingRepo struct {
	*memVoucherRepo
}

func (c *countingRepo) UpdateCounts(_ context.Context, id string, redeemedCount, expectedCount int) error {
	v := c.byID[id]
	if v == nil || v.RedeemedCount != expectedCount {
		return apperror.Conflict("voucher redeem count conflict")
	}
	v.RedeemedCount = redeemedCount
	return nil
}
