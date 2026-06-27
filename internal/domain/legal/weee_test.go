package legal_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/legal"
)

func TestWeeeEnabled_DefaultFalseWhenMissing(t *testing.T) {
	ok, err := legal.WeeeEnabled(context.Background(), stubConfigGetter{}, "store-1")
	if err != nil {
		t.Fatalf("WeeeEnabled: %v", err)
	}
	if ok {
		t.Fatal("expected default disabled")
	}
}

func TestWeeeEnabled_StoreOverride(t *testing.T) {
	repo := stubConfigGetter{
		legal.ScopedConfigKey("store-eu", legal.WeeeEnabledConfigKey): true,
	}
	ok, err := legal.WeeeEnabled(context.Background(), repo, "store-eu")
	if err != nil {
		t.Fatalf("WeeeEnabled: %v", err)
	}
	if !ok {
		t.Fatal("expected store override enabled")
	}
}

func TestWeeeEnabled_NilRepo(t *testing.T) {
	ok, err := legal.WeeeEnabled(context.Background(), nil, "")
	if err != nil || ok {
		t.Fatalf("nil repo = disabled, got ok=%v err=%v", ok, err)
	}
}

func TestStoreProducerRegistration_StoreScope(t *testing.T) {
	repo := stubConfigGetter{
		legal.ScopedConfigKey("store-eu", legal.WeeeProducerRegistrationConfigKey): "PL-WEEE-12345",
	}
	got, err := legal.StoreProducerRegistration(context.Background(), repo, "store-eu")
	if err != nil {
		t.Fatalf("StoreProducerRegistration: %v", err)
	}
	if got != "PL-WEEE-12345" {
		t.Fatalf("got %q", got)
	}
}

func TestParseWeeeFromProduct(t *testing.T) {
	info := legal.ParseWeeeFromProduct(map[string]interface{}{
		legal.AttrWeeeCategory:             "small_it_telecom",
		legal.AttrWeeeProducerRegistration: "DE-123456",
		legal.AttrWeeeTakeBackInfo:         "Return to retailer.",
		legal.AttrWeeeSymbolVisible:        true,
	})
	if info.CategoryLabel != "Small IT and telecommunications equipment" {
		t.Fatalf("category label = %q", info.CategoryLabel)
	}
	if info.ProducerRegistration != "DE-123456" {
		t.Fatalf("registration = %q", info.ProducerRegistration)
	}
	if !info.HasDisclosure() {
		t.Fatal("expected disclosure")
	}
}

func TestWeeeInfo_WithStoreRegistration(t *testing.T) {
	info := legal.ParseWeeeFromProduct(map[string]interface{}{
		legal.AttrWeeeCategory: "lighting",
	}).WithStoreRegistration("PL-STORE-99")
	if info.ProducerRegistration != "PL-STORE-99" {
		t.Fatalf("registration = %q", info.ProducerRegistration)
	}
}

func TestWeeeInfo_HasDisclosure_SymbolOnlyWithoutRegistration(t *testing.T) {
	info := legal.WeeeInfo{SymbolVisible: true}
	if info.HasDisclosure() {
		t.Fatal("symbol alone without registration should not disclose")
	}
}
