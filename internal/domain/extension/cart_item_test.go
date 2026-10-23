package extension

import "testing"

func TestParseCartItemTargetID(t *testing.T) {
	tests := []struct {
		in          string
		cartID      string
		variantID   string
		ok          bool
	}{
		{"cart-1:var-1", "cart-1", "var-1", true},
		{" cart-1 : var-2 ", "cart-1", "var-2", true},
		{"cart-1:var:with:colons", "cart-1", "var:with:colons", true},
		{"", "", "", false},
		{"nocolon", "", "", false},
		{":var-1", "", "", false},
		{"cart-1:", "", "", false},
	}
	for _, tt := range tests {
		cartID, variantID, ok := ParseCartItemTargetID(tt.in)
		if ok != tt.ok || cartID != tt.cartID || variantID != tt.variantID {
			t.Fatalf("ParseCartItemTargetID(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.in, cartID, variantID, ok, tt.cartID, tt.variantID, tt.ok)
		}
	}
}
