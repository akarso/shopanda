package extapi_test

import (
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
)

func TestIsValidCurrency(t *testing.T) {
	cases := []struct {
		code string
		ok   bool
	}{
		{"EUR", true},
		{"USD", true},
		{"JPY", true},
		{"KWD", true},
		{"eur", false},
		{"EU", false},
		{"EURO", false},
		{"", false},
		{"E1R", false},
	}
	for _, tc := range cases {
		if got := extapi.IsValidCurrency(tc.code); got != tc.ok {
			t.Errorf("IsValidCurrency(%q) = %v, want %v", tc.code, got, tc.ok)
		}
	}
}

func TestParseMinorUnits_OK(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		exponent int
		want     int64
	}{
		{"eu_plain", "80,49", 2, 8049},
		{"eu_grouped", "1.234,56", 2, 123456},
		{"us_grouped", "1,234.56", 2, 123456},
		{"us_plain", "80.49", 2, 8049},
		{"integer_pads", "100", 2, 10000},
		{"short_frac_pads", "80,4", 2, 8040},
		{"jpy_integer", "1234", 0, 1234},
		{"jpy_grouped", "1.234", 0, 1234},
		{"kwd_three", "1.234", 3, 1234},
		{"kwd_grouped", "1,234.567", 3, 1234567},
		{"leading_zero_frac", "0,50", 2, 50},
		{"only_frac", ",49", 2, 49},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extapi.ParseMinorUnits(tc.in, tc.exponent)
			if err != nil {
				t.Fatalf("ParseMinorUnits(%q, %d): %v", tc.in, tc.exponent, err)
			}
			if got != tc.want {
				t.Fatalf("ParseMinorUnits(%q, %d) = %d, want %d", tc.in, tc.exponent, got, tc.want)
			}
		})
	}
}

func TestParseMinorUnits_Reject(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		exponent int
	}{
		{"empty", "", 2},
		{"spaces", "80, 49", 2},
		{"letters", "80,4a", 2},
		{"negative", "-80,49", 2},
		{"plus", "+80,49", 2},
		{"zero", "0", 2},
		{"zero_frac", "0,00", 2},
		{"extra_frac_eu", "80,499", 2},
		{"extra_frac_us", "80.499", 2},
		{"jpy_frac", "100,5", 0},
		{"bad_group", "1,23.45", 2},
		{"neg_exponent", "1", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := extapi.ParseMinorUnits(tc.in, tc.exponent); err == nil {
				t.Fatalf("ParseMinorUnits(%q, %d): want error", tc.in, tc.exponent)
			}
		})
	}
}
