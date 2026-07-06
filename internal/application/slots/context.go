package slots

import (
	"fmt"
	"regexp"
	"strings"
)

// Renderer produces HTML for a slot placement.
type Renderer func(ctx *RenderContext) string

// RenderContext carries page data available to slot renderers.
type RenderContext struct {
	Anchor string
	Data   interface{}
}

// NewRenderContext creates render context for anchor with page data.
func NewRenderContext(anchor string, data interface{}) *RenderContext {
	return &RenderContext{
		Anchor: strings.TrimSpace(anchor),
		Data:   data,
	}
}

var anchorNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// ValidateAnchorName checks slot anchor naming conventions.
func ValidateAnchorName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("slots: anchor name must not be empty")
	}
	if !anchorNamePattern.MatchString(name) {
		return fmt.Errorf("slots: anchor name %q must be dot-separated lowercase segments (e.g. pdp.price)", name)
	}
	return nil
}
