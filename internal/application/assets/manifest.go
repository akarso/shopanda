package assets

import (
	"fmt"
	"strings"
)

// Kind identifies an asset bundle type.
type Kind string

const (
	KindCSS Kind = "css"
	KindJS  Kind = "js"
)

// Placement identifies where an asset is injected in the layout.
type Placement string

const (
	PlacementHead   Placement = "head"
	PlacementFooter Placement = "footer"
)

// Manifest describes a plugin static asset to inject on matching routes.
type Manifest struct {
	Path      string
	Kind      Kind
	Placement Placement
	Routes    []string
	Priority  int
}

// Validate checks manifest fields.
func (m Manifest) Validate() error {
	path := strings.TrimSpace(m.Path)
	if path == "" {
		return fmt.Errorf("assets: path must not be empty")
	}
	if strings.HasPrefix(path, "//") {
		return fmt.Errorf("assets: path %q must be same-origin", path)
	}
	if strings.Contains(path, "://") {
		return fmt.Errorf("assets: path %q must be same-origin", path)
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("assets: path %q must be absolute", path)
	}
	switch m.Kind {
	case KindCSS, KindJS:
	default:
		return fmt.Errorf("assets: invalid kind %q", m.Kind)
	}
	switch m.Placement {
	case PlacementHead, PlacementFooter:
	default:
		return fmt.Errorf("assets: invalid placement %q", m.Placement)
	}
	if m.Placement == PlacementFooter && m.Kind == KindCSS {
		return fmt.Errorf("assets: css bundles must use head placement")
	}
	return nil
}

// Bundle groups resolved assets for layout injection.
type Bundle struct {
	HeadCSS  []Resolved
	HeadJS   []Resolved
	FooterJS []Resolved
}

// Resolved is a manifest entry ready for template rendering.
type Resolved struct {
	URL string
}
