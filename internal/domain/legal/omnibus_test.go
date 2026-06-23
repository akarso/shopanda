package legal_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/legal"
)

type stubConfigGetter map[string]interface{}

func (s stubConfigGetter) Get(_ context.Context, key string) (interface{}, error) {
	if s == nil {
		return nil, nil
	}
	v, ok := s[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func TestOmnibusEnabled_DefaultTrueWhenMissing(t *testing.T) {
	ok, err := legal.OmnibusEnabled(context.Background(), stubConfigGetter{}, "store-1")
	if err != nil {
		t.Fatalf("OmnibusEnabled: %v", err)
	}
	if !ok {
		t.Fatal("expected default enabled")
	}
}

func TestOmnibusEnabled_StoreOverride(t *testing.T) {
	repo := stubConfigGetter{
		legal.ScopedConfigKey("store-eu", legal.OmnibusEnabledConfigKey): false,
	}
	ok, err := legal.OmnibusEnabled(context.Background(), repo, "store-eu")
	if err != nil {
		t.Fatalf("OmnibusEnabled: %v", err)
	}
	if ok {
		t.Fatal("expected store override disabled")
	}
}

func TestOmnibusEnabled_GlobalFallback(t *testing.T) {
	repo := stubConfigGetter{
		legal.OmnibusEnabledConfigKey: true,
	}
	ok, err := legal.OmnibusEnabled(context.Background(), repo, "store-1")
	if err != nil {
		t.Fatalf("OmnibusEnabled: %v", err)
	}
	if !ok {
		t.Fatal("expected global enabled")
	}
}

func TestOmnibusEnabled_NilRepo(t *testing.T) {
	ok, err := legal.OmnibusEnabled(context.Background(), nil, "")
	if err != nil || !ok {
		t.Fatalf("nil repo = enabled, got ok=%v err=%v", ok, err)
	}
}
