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

func TestCronExpr_Matches_DomDowOr(t *testing.T) {
	// "30 8 1 * 0" — DOM=1 wild DOW, only DOM restricted → AND semantics.
	domOnly, _ := parse("30 8 1 * 0")
	// Jan 1 2026 = Thursday (weekday=4). DOM matches, DOW=0 is wild → AND → matches.
	if !domOnly.matches(time.Date(2026, 1, 4, 8, 30, 0, 0, time.UTC)) {
		t.Error("DOM-only: expected match on Sunday the 4th (DOW matches)")
	}

	// "30 8 1 1 0" — both DOM=1 and DOW=0(Sun) restricted → OR semantics.
	both, _ := parse("30 8 1 1 0")
	// Jan 1 2026 = Thursday. DOM=1 matches → true via OR even though DOW=4 != 0.
	if !both.matches(time.Date(2026, 1, 1, 8, 30, 0, 0, time.UTC)) {
		t.Error("both restricted: expected match on Jan 1 (DOM matches via OR)")
	}
	// Jan 4 2026 = Sunday. DOW=0 matches → true via OR even though DOM=4 != 1.
	if !both.matches(time.Date(2026, 1, 4, 8, 30, 0, 0, time.UTC)) {
		t.Error("both restricted: expected match on Sunday (DOW matches via OR)")
	}
	// Jan 2 2026 = Friday. Neither DOM=2 nor DOW=5 matches → false.
	if both.matches(time.Date(2026, 1, 2, 8, 30, 0, 0, time.UTC)) {
		t.Error("both restricted: expected no match on Jan 2 Friday")
	}
}
