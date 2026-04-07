package cron

import (
	"testing"
	"time"
)

func TestParse_Valid(t *testing.T) {
	tests := []struct {
		spec string
	}{
		{"* * * * *"},
		{"*/5 * * * *"},
		{"0 0 * * *"},
		{"30 8 1 1 0"},
		{"0,15,30,45 * * * *"},
		{"0 9-17 * * 1-5"},
		{"0-30/5 * * * *"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			if _, err := parse(tt.spec); err != nil {
				t.Errorf("parse(%q) unexpected error: %v", tt.spec, err)
			}
		})
	}
}

func TestParse_Invalid(t *testing.T) {
	tests := []struct {
		spec string
	}{
		{""},
		{"* * *"},
		{"60 * * * *"},
		{"* 25 * * *"},
		{"* * 32 * *"},
		{"* * * 13 *"},
		{"* * * * 7"},
		{"abc * * * *"},
		{"*/0 * * * *"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			if _, err := parse(tt.spec); err == nil {
				t.Errorf("parse(%q) expected error, got nil", tt.spec)
			}
		})
	}
}

func TestCronExpr_Matches(t *testing.T) {
	expr, err := parse("*/5 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	yes := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
	if !expr.matches(yes) {
		t.Errorf("expected match at minute 0")
	}

	no := time.Date(2026, 4, 7, 10, 3, 0, 0, time.UTC)
	if expr.matches(no) {
		t.Errorf("expected no match at minute 3")
	}
}

func TestCronExpr_Matches_Ranges(t *testing.T) {
	expr, err := parse("0 9-17 * * 1-5")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !expr.matches(time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)) {
		t.Error("expected match on Tuesday 10:00")
	}

	if expr.matches(time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)) {
		t.Error("expected no match on Sunday")
	}

	if expr.matches(time.Date(2026, 4, 7, 20, 0, 0, 0, time.UTC)) {
		t.Error("expected no match at hour 20")
	}
}

func TestCronExpr_Matches_Lists(t *testing.T) {
	expr, err := parse("0,30 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !expr.matches(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) {
		t.Error("expected match at minute 0")
	}
	if !expr.matches(time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)) {
		t.Error("expected match at minute 30")
	}
	if expr.matches(time.Date(2026, 1, 1, 12, 15, 0, 0, time.UTC)) {
		t.Error("expected no match at minute 15")
	}
}

func TestCronExpr_Matches_RangeWithStep(t *testing.T) {
	expr, err := parse("0-30/10 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	for _, m := range []int{0, 10, 20, 30} {
		if !expr.matches(time.Date(2026, 1, 1, 0, m, 0, 0, time.UTC)) {
			t.Errorf("expected match at minute %d", m)
		}
	}
	for _, m := range []int{5, 15, 25, 31} {
		if expr.matches(time.Date(2026, 1, 1, 0, m, 0, 0, time.UTC)) {
			t.Errorf("expected no match at minute %d", m)
		}
	}
}
