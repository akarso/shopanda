package slots

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/akarso/shopanda/internal/platform/logger"
)

type slotKey struct {
	anchor    string
	placement Placement
}

type registeredRenderer struct {
	priority   int
	registrant string
	renderer   Renderer
}

// Registry stores slot renderers and executes them in priority order.
// Panics in renderers are recovered and logged so page rendering continues.
type Registry struct {
	mu        sync.RWMutex
	renderers map[slotKey][]registeredRenderer
	log       logger.Logger
}

// NewRegistry creates an empty slot registry.
func NewRegistry(log logger.Logger) *Registry {
	return &Registry{
		renderers: make(map[slotKey][]registeredRenderer),
		log:       log,
	}
}

// RegisterRenderer adds a renderer for anchor at placement (lower priority runs first).
func (r *Registry) RegisterRenderer(anchor string, placement Placement, priority int, registrant string, renderer Renderer) error {
	if r == nil {
		return fmt.Errorf("slots: registry must not be nil")
	}
	anchor = strings.TrimSpace(anchor)
	if err := ValidateAnchorName(anchor); err != nil {
		return err
	}
	switch placement {
	case PlacementBefore, PlacementAfter, PlacementPrepend, PlacementAppend:
	default:
		return fmt.Errorf("slots: invalid placement %q", placement)
	}
	registrant = strings.TrimSpace(registrant)
	if registrant == "" {
		return fmt.Errorf("slots: registrant must not be empty")
	}
	if renderer == nil {
		return fmt.Errorf("slots: renderer must not be nil")
	}

	key := slotKey{anchor: anchor, placement: placement}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renderers[key] = append(r.renderers[key], registeredRenderer{
		priority:   priority,
		registrant: registrant,
		renderer:   renderer,
	})
	sort.SliceStable(r.renderers[key], func(i, j int) bool {
		return r.renderers[key][i].priority < r.renderers[key][j].priority
	})
	return nil
}

// Render runs all renderers for anchor at placement and concatenates their HTML.
// Unknown anchors or placements with no renderers return an empty string.
func (r *Registry) Render(anchor string, placement Placement, data interface{}) string {
	if r == nil {
		return ""
	}
	anchor = strings.TrimSpace(anchor)
	if anchor == "" {
		return ""
	}

	key := slotKey{anchor: anchor, placement: placement}
	r.mu.RLock()
	regs := append([]registeredRenderer(nil), r.renderers[key]...)
	r.mu.RUnlock()
	if len(regs) == 0 {
		return ""
	}

	ctx := NewRenderContext(anchor, data)
	var out strings.Builder
	for _, reg := range regs {
		out.WriteString(r.runRenderer(ctx, reg))
	}
	return out.String()
}

func (r *Registry) runRenderer(ctx *RenderContext, reg registeredRenderer) (html string) {
	defer func() {
		if rec := recover(); rec != nil {
			if r.log != nil {
				r.log.Error("slots.renderer.panic", fmt.Errorf("%v", rec), map[string]interface{}{
					"anchor":     ctx.Anchor,
					"registrant": reg.registrant,
				})
			}
			html = ""
		}
	}()
	return reg.renderer(ctx)
}
