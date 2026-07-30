package promodemo

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
)

func TestEvalMinLineTotal(t *testing.T) {
	ok, err := evalMinLineTotal(context.Background(), []byte(`{"type":"min_line_total","value":5000}`), &extapi.PromotionPricingItem{TotalAmount: 6000})
	if err != nil || !ok {
		t.Fatalf("match high total: ok=%v err=%v", ok, err)
	}
	ok, err = evalMinLineTotal(context.Background(), []byte(`{"type":"min_line_total","value":5000}`), &extapi.PromotionPricingItem{TotalAmount: 4000})
	if err != nil || ok {
		t.Fatalf("reject low total: ok=%v err=%v", ok, err)
	}
}

func TestEvalLineBonusPercent(t *testing.T) {
	got, err := evalLineBonusPercent(context.Background(), []byte(`{"type":"line_bonus_percent","percentage":10}`), &extapi.PromotionPricingItem{TotalAmount: 2000})
	if err != nil || got != 200 {
		t.Fatalf("discount = %d err=%v", got, err)
	}
}
