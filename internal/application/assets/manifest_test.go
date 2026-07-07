package assets_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/assets"
)

func TestManifest_ValidateRejectsOffOriginPaths(t *testing.T) {
	cases := []string{
		"//evil.example.com/asset.js",
		"https://evil.example.com/asset.js",
		"http://evil.example.com/asset.js",
	}
	for _, path := range cases {
		err := (assets.Manifest{
			Path: path, Kind: assets.KindJS, Placement: assets.PlacementHead,
		}).Validate()
		if err == nil || !strings.Contains(err.Error(), "same-origin") {
			t.Fatalf("Validate(%q) = %v, want same-origin error", path, err)
		}
	}
}

func TestManifest_ValidateAcceptsSameOriginPath(t *testing.T) {
	err := (assets.Manifest{
		Path: "/plugins/cart.js", Kind: assets.KindJS, Placement: assets.PlacementFooter,
	}).Validate()
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
