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

// CatalogEntry describes a registered slot anchor and its renderers.
type CatalogEntry struct {
	Name     string
	Handlers []CatalogHandler
}

// CatalogHandler describes one renderer registration.
type CatalogHandler struct {
	Placement  Placement
	Priority   int
	Registrant string
}

// Registry stores slot renderers and executes them in priority order.
// Panics in renderers are recovered and logged so page rendering continues.
type Registry struct {
	mu           sync.RWMutex
	renderers    map[slotKey][]registeredRenderer
	log          logger.Logger
	themeMarkers map[string]struct{}
	devMode      bool
}

// NewRegistry creates an empty slot registry.
func NewRegistry(log logger.Logger) *Registry {
	return &Registry{
		renderers: make(map[slotKey][]registeredRenderer),
		log:       log,
	}
}

// SetThemeMarkers configures active theme slot markers for dev diagnostics.
// When dev mode is enabled, RegisterRenderer warns for anchors absent from markers.
func (r *Registry) SetThemeMarkers(anchors []string) {
	if r == nil {
		return
	}
	markers := make(map[string]struct{}, len(anchors))
	for _, anchor := range anchors {
		anchor = strings.TrimSpace(anchor)
		if anchor == "" {
			continue
		}
		markers[anchor] = struct{}{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.themeMarkers = markers
}

// SetDevMode enables dev-only slot registration diagnostics.
func (r *Registry) SetDevMode(enabled bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devMode = enabled
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
	r.warnUnmarkedAnchorLocked(anchor, registrant)
	return nil
}

func (r *Registry) warnUnmarkedAnchorLocked(anchor, registrant string) {
	if !r.devMode || len(r.themeMarkers) == 0 || r.log == nil {
		return
	}
	if _, ok := r.themeMarkers[anchor]; ok {
		return
	}
	r.log.Warn("slots.registration.unmarked_anchor", map[string]interface{}{
		"anchor":     anchor,
		"registrant": registrant,
	})
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

// Catalog returns registered slot renderers for tooling and admin APIs.
func (r *Registry) Catalog() []CatalogEntry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.renderers) == 0 {
		return nil
	}

	byAnchor := make(map[string][]CatalogHandler)
	for key, regs := range r.renderers {
		for _, reg := range regs {
			byAnchor[key.anchor] = append(byAnchor[key.anchor], CatalogHandler{
				Placement:  key.placement,
				Priority:   reg.priority,
				Registrant: reg.registrant,
			})
		}
	}

	names := make([]string, 0, len(byAnchor))
	for name := range byAnchor {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]CatalogEntry, 0, len(names))
	for _, name := range names {
		handlers := byAnchor[name]
		sort.SliceStable(handlers, func(i, j int) bool {
			if placementRank(handlers[i].Placement) != placementRank(handlers[j].Placement) {
				return placementRank(handlers[i].Placement) < placementRank(handlers[j].Placement)
			}
			if handlers[i].Priority != handlers[j].Priority {
				return handlers[i].Priority < handlers[j].Priority
			}
			return handlers[i].Registrant < handlers[j].Registrant
		})
		out = append(out, CatalogEntry{Name: name, Handlers: handlers})
	}
	return out
}
