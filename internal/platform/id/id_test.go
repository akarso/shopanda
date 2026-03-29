package id_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/id"
)

func TestNew_ReturnsValidUUID(t *testing.T) {
	got := id.New()
	if !id.IsValid(got) {
		t.Fatalf("New() returned invalid UUID: %s", got)
	}
}

func TestNew_ReturnsUniqueValues(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		v := id.New()
		if seen[v] {
			t.Fatalf("duplicate UUID on iteration %d: %s", i, v)
		}
		seen[v] = true
	}
}

func TestNew_HasCorrectFormat(t *testing.T) {
	got := id.New()
	if len(got) != 36 {
		t.Fatalf("expected length 36, got %d: %s", len(got), got)
	}
	// Check dashes at correct positions.
	if got[8] != '-' || got[13] != '-' || got[18] != '-' || got[23] != '-' {
		t.Fatalf("unexpected dash positions: %s", got)
	}
	// Check version nibble.
	if got[14] != '4' {
		t.Fatalf("expected version 4 at position 14, got %c: %s", got[14], got)
	}
	// Check variant nibble (must be 8, 9, a, or b).
	v := got[19]
	if v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Fatalf("expected variant [89ab] at position 19, got %c: %s", v, got)
	}
}

func TestIsValid_AcceptsValidUUID(t *testing.T) {
	valid := "550e8400-e29b-41d4-a716-446655440000"
	if !id.IsValid(valid) {
		t.Fatalf("IsValid(%q) = false, want true", valid)
	}
}

func TestIsValid_RejectsInvalidStrings(t *testing.T) {
	cases := []string{
		"",
		"not-a-uuid",
		"550e8400-e29b-41d4-a716",
		"550e8400-e29b-31d4-a716-446655440000", // version 3
		"550e8400-e29b-41d4-c716-446655440000", // wrong variant
		"550e8400e29b41d4a716446655440000",      // no dashes
	}
	for _, c := range cases {
		if id.IsValid(c) {
			t.Errorf("IsValid(%q) = true, want false", c)
		}
	}
}
