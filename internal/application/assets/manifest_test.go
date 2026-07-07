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
		path := path
		t.Run(path, func(t *testing.T) {
			err := (assets.Manifest{
				Path: path, Kind: assets.KindJS, Placement: assets.PlacementHead,
			}).Validate()
			if err == nil || !strings.Contains(err.Error(), "same-origin") {
				t.Fatalf("Validate(%q) = %v, want same-origin error", path, err)
			}
		})
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

func TestManifest_ValidateRejectsEmptyPath(t *testing.T) {
	err := (assets.Manifest{
		Path: "", Kind: assets.KindJS, Placement: assets.PlacementHead,
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "path must not be empty") {
		t.Fatalf("Validate() err = %v, want empty path error", err)
	}
}

func TestManifest_ValidateRejectsRelativePath(t *testing.T) {
	err := (assets.Manifest{
		Path: "plugins/cart.js", Kind: assets.KindJS, Placement: assets.PlacementHead,
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("Validate() err = %v, want absolute path error", err)
	}
}

func TestManifest_ValidateRejectsInvalidKind(t *testing.T) {
	err := (assets.Manifest{
		Path: "/plugins/cart.js", Kind: assets.Kind("bad"), Placement: assets.PlacementHead,
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "invalid kind") {
		t.Fatalf("Validate() err = %v, want invalid kind error", err)
	}
}

func TestManifest_ValidateRejectsInvalidPlacement(t *testing.T) {
	err := (assets.Manifest{
		Path: "/plugins/cart.js", Kind: assets.KindJS, Placement: assets.Placement("bad"),
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "invalid placement") {
		t.Fatalf("Validate() err = %v, want invalid placement error", err)
	}
}

func TestManifest_ValidateRejectsCSSInFooter(t *testing.T) {
	err := (assets.Manifest{
		Path: "/plugins/cart.css", Kind: assets.KindCSS, Placement: assets.PlacementFooter,
	}).Validate()
	if err == nil || !strings.Contains(err.Error(), "head placement") {
		t.Fatalf("Validate() err = %v, want head placement error", err)
	}
}
