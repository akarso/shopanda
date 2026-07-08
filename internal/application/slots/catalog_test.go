package slots

import (
	"testing"
)

func TestStandardAnchorNames_UniqueAndValid(t *testing.T) {
	seen := make(map[string]struct{})
	for _, anchor := range StandardAnchors() {
		if anchor.Name == "" {
			t.Fatal("standard anchor name must not be empty")
		}
		if err := ValidateAnchorName(anchor.Name); err != nil {
			t.Fatalf("ValidateAnchorName(%q): %v", anchor.Name, err)
		}
		if _, ok := seen[anchor.Name]; ok {
			t.Fatalf("duplicate standard anchor %q", anchor.Name)
		}
		seen[anchor.Name] = struct{}{}
	}
	if len(StandardAnchorNames()) != len(seen) {
		t.Fatalf("StandardAnchorNames length mismatch")
	}
}
