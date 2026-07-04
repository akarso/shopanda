package cart_test

import (
	"context"
	"testing"
	"time"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	domainCfg "github.com/akarso/shopanda/internal/domain/config"
)

type stubRecoveryConfigRepo struct {
	values map[string]interface{}
}

func (s *stubRecoveryConfigRepo) Get(_ context.Context, key string) (interface{}, error) {
	if s.values == nil {
		return nil, nil
	}
	val, ok := s.values[key]
	if !ok {
		return nil, nil
	}
	return val, nil
}

func (s *stubRecoveryConfigRepo) Set(context.Context, string, interface{}) error { return nil }
func (s *stubRecoveryConfigRepo) SetMany(context.Context, map[string]interface{}) error {
	return nil
}
func (s *stubRecoveryConfigRepo) Delete(context.Context, string) error { return nil }
func (s *stubRecoveryConfigRepo) All(context.Context) ([]domainCfg.Entry, error) {
	return nil, nil
}

func TestLoadRecoverySettings_DefaultsWhenRepoNil(t *testing.T) {
	settings, err := cartApp.LoadRecoverySettings(context.Background(), nil, 24*time.Hour)
	if err != nil {
		t.Fatalf("LoadRecoverySettings: %v", err)
	}
	if !settings.Enabled {
		t.Fatal("expected enabled by default")
	}
	if settings.StaleAfter != 24*time.Hour {
		t.Fatalf("StaleAfter = %v, want 24h", settings.StaleAfter)
	}
}

func TestLoadRecoverySettings_ReadsConfig(t *testing.T) {
	repo := &stubRecoveryConfigRepo{values: map[string]interface{}{
		cartApp.ConfigKeyCartRecoveryEnabled:    false,
		cartApp.ConfigKeyCartRecoveryDelayHours: float64(48),
	}}
	settings, err := cartApp.LoadRecoverySettings(context.Background(), repo, cartApp.DefaultRecoveryStaleAfter)
	if err != nil {
		t.Fatalf("LoadRecoverySettings: %v", err)
	}
	if settings.Enabled {
		t.Fatal("expected disabled")
	}
	if settings.StaleAfter != 48*time.Hour {
		t.Fatalf("StaleAfter = %v, want 48h", settings.StaleAfter)
	}
}

func TestLoadRecoverySettings_RejectsInvalidDelay(t *testing.T) {
	repo := &stubRecoveryConfigRepo{values: map[string]interface{}{
		cartApp.ConfigKeyCartRecoveryDelayHours: float64(0),
	}}
	_, err := cartApp.LoadRecoverySettings(context.Background(), repo, cartApp.DefaultRecoveryStaleAfter)
	if err == nil {
		t.Fatal("expected error for invalid delay")
	}
}
