package legal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/domain/legal"
)

func TestOssEnabled_DefaultFalse(t *testing.T) {
	ok, err := legal.OssEnabled(context.Background(), stubConfigGetter{}, "store-1")
	if err != nil {
		t.Fatalf("OssEnabled: %v", err)
	}
	if ok {
		t.Fatal("expected default false")
	}
}

func TestOssEnabled_StoreScoped(t *testing.T) {
	repo := stubConfigGetter{
		legal.ScopedConfigKey("store-de", legal.OssEnabledConfigKey): true,
	}
	ok, err := legal.OssEnabled(context.Background(), repo, "store-de")
	if err != nil {
		t.Fatalf("OssEnabled: %v", err)
	}
	if !ok {
		t.Fatal("expected store-scoped true")
	}
}

func TestNormalizeCountryCode(t *testing.T) {
	if got := legal.NormalizeCountryCode(" de "); got != "DE" {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureOssExportEnabled(t *testing.T) {
	err := legal.EnsureOssExportEnabled(context.Background(), stubConfigGetter{}, "store-1")
	if !errors.Is(err, legal.ErrOssExportDisabled) {
		t.Fatalf("err = %v, want ErrOssExportDisabled", err)
	}
}
