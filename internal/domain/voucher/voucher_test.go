package voucher_test

import (
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/voucher"
)

func TestGenerateAndValidateCode(t *testing.T) {
	code, err := voucher.GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if strings.Count(code, "-") != 2 {
		t.Fatalf("code = %q, want two dashes", code)
	}
	if err := voucher.ValidateCode(code); err != nil {
		t.Fatalf("ValidateCode(%q): %v", code, err)
	}
	n := voucher.NormalizeCode(code)
	if voucher.FormatCode(n) != code {
		t.Fatalf("FormatCode round-trip = %q, want %q", voucher.FormatCode(n), code)
	}
}

func TestNewVoucherAndStatus(t *testing.T) {
	code, err := voucher.GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	v, err := voucher.NewVoucher("id-1", code, "ord-1", "ord-1/var-1", "prod-1", "var-1", "cust-1", 0, 2, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVoucher: %v", err)
	}
	if v.Status() != voucher.StatusIssued {
		t.Fatalf("status = %q, want issued", v.Status())
	}
	v.RedeemedCount = 1
	if v.Status() != voucher.StatusPartiallyRedeemed {
		t.Fatalf("status = %q, want partially_redeemed", v.Status())
	}
	v.RedeemedCount = 2
	if v.Status() != voucher.StatusRedeemed {
		t.Fatalf("status = %q, want redeemed", v.Status())
	}
	now := time.Now().UTC()
	v.VoidedAt = &now
	if v.Status() != voucher.StatusVoid {
		t.Fatalf("status = %q, want void", v.Status())
	}
}

func TestSettingsCodesForQuantity(t *testing.T) {
	s := voucher.DefaultSettings("prod-1")
	s.CodesPerUnit = 2
	n, err := s.CodesForQuantity(3)
	if err != nil {
		t.Fatalf("CodesForQuantity: %v", err)
	}
	if n != 6 {
		t.Fatalf("codes = %d, want 6", n)
	}
	s.Enabled = false
	if _, err := s.CodesForQuantity(1); err != voucher.ErrNotEnabled {
		t.Fatalf("err = %v, want ErrNotEnabled", err)
	}
}

func TestRedeemGates(t *testing.T) {
	code, _ := voucher.GenerateCode()
	v, err := voucher.NewVoucher("id-1", code, "ord-1", "ord-1/var-1", "prod-1", "var-1", "", 0, 1, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewVoucher: %v", err)
	}
	if err := v.CanRedeem(); err != nil {
		t.Fatalf("CanRedeem: %v", err)
	}
	if err := v.CanUnredeem(); err != voucher.ErrNothingToUnredeem {
		t.Fatalf("CanUnredeem = %v, want ErrNothingToUnredeem", err)
	}
	v.RedeemedCount = 1
	if err := v.CanRedeem(); err != voucher.ErrExhausted {
		t.Fatalf("CanRedeem = %v, want ErrExhausted", err)
	}
	if err := v.CanUnredeem(); err != nil {
		t.Fatalf("CanUnredeem: %v", err)
	}
}
