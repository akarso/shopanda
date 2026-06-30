package admin_test

import (
	"context"
	"math"
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

func TestRetentionDays_ParsesSupportedEncodings(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  int
	}{
		{name: "int", value: 90, want: 90},
		{name: "int64", value: int64(45), want: 45},
		{name: "float64", value: float64(30), want: 30},
		{name: "string", value: "120", want: 120},
		{name: "zero int", value: 0, want: 0},
		{name: "zero string", value: "0", want: 0},
		{name: "empty string", value: "", want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			days, err := admin.RetentionDays(context.Background(), &stubConfigRepo{
				values: map[string]interface{}{admin.RetentionDaysConfigKey: tc.value},
			})
			if err != nil {
				t.Fatalf("RetentionDays: %v", err)
			}
			if days != tc.want {
				t.Fatalf("days = %d, want %d", days, tc.want)
			}
		})
	}
}

func TestRetentionDays_RejectsNegative(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
	}{
		{name: "int", value: -1},
		{name: "int64", value: int64(-1)},
		{name: "float64", value: float64(-1)},
		{name: "string", value: "-5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := admin.RetentionDays(context.Background(), &stubConfigRepo{
				values: map[string]interface{}{admin.RetentionDaysConfigKey: tc.value},
			})
			if err == nil {
				t.Fatal("expected error for negative retention")
			}
		})
	}
}

func TestRetentionDays_RejectsOversizedValues(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
	}{
		{name: "float64", value: math.Nextafter(float64(math.MaxInt), math.Inf(1))},
		{name: "string", value: "999999999999999999999"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := admin.RetentionDays(context.Background(), &stubConfigRepo{
				values: map[string]interface{}{admin.RetentionDaysConfigKey: tc.value},
			})
			if err == nil {
				t.Fatal("expected error for oversized retention")
			}
		})
	}
}

func TestRetentionDays_RejectsOversizedInt64(t *testing.T) {
	if math.MaxInt64 <= int64(math.MaxInt) {
		t.Skip("int64 cannot exceed platform int range on this arch")
	}
	_, err := admin.RetentionDays(context.Background(), &stubConfigRepo{
		values: map[string]interface{}{admin.RetentionDaysConfigKey: int64(math.MaxInt64)},
	})
	if err == nil {
		t.Fatal("expected error for oversized int64 retention")
	}
}
