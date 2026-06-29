package admin_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/admin"
)

type stubConfigRepo struct {
	values map[string]interface{}
	err    error
}

func (s *stubConfigRepo) Get(_ context.Context, key string) (interface{}, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.values == nil {
		return nil, nil
	}
	return s.values[key], nil
}

func TestRetentionDays_MissingDefaultsDisabled(t *testing.T) {
	days, err := admin.RetentionDays(context.Background(), &stubConfigRepo{})
	if err != nil {
		t.Fatalf("RetentionDays: %v", err)
	}
	if days != 0 {
		t.Fatalf("days = %d, want 0", days)
	}
}

func TestRetentionDays_ParsesPositive(t *testing.T) {
	days, err := admin.RetentionDays(context.Background(), &stubConfigRepo{
		values: map[string]interface{}{admin.RetentionDaysConfigKey: 90},
	})
	if err != nil {
		t.Fatalf("RetentionDays: %v", err)
	}
	if days != 90 {
		t.Fatalf("days = %d, want 90", days)
	}
}

func TestRetentionDays_ZeroDisables(t *testing.T) {
	days, err := admin.RetentionDays(context.Background(), &stubConfigRepo{
		values: map[string]interface{}{admin.RetentionDaysConfigKey: 0},
	})
	if err != nil {
		t.Fatalf("RetentionDays: %v", err)
	}
	if days != 0 {
		t.Fatalf("days = %d, want 0", days)
	}
}

func TestRetentionDays_RejectsNegative(t *testing.T) {
	_, err := admin.RetentionDays(context.Background(), &stubConfigRepo{
		values: map[string]interface{}{admin.RetentionDaysConfigKey: -1},
	})
	if err == nil {
		t.Fatal("expected error for negative retention")
	}
}
