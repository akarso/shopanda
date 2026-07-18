package exportctx

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/akarso/shopanda/internal/platform/logger"
)

// CatalogEntry describes registered export row hooks for an entity.
type CatalogEntry struct {
	Entity   string
	Hook     string
	Handlers []CatalogHandler
}

// CatalogHandler describes one row hook registration.
type CatalogHandler struct {
	Priority   int
	Registrant string
}

type registeredHandler struct {
	entity     string
	hook       string
	priority   int
	registrant string
	handler    RowHandler
}

// Registry stores export row hook handlers and executes ordered chains per entity.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string][]registeredHandler
	log      logger.Logger
}

// NewRegistry creates an empty export row hook registry.
func NewRegistry(log logger.Logger) *Registry {
	return &Registry{
		handlers: make(map[string][]registeredHandler),
		log:      log,
	}
}

// Register adds a row handler for entity at priority (lower runs first).
func (r *Registry) Register(entity string, priority int, registrant string, handler RowHandler) error {
	if r == nil {
		return fmt.Errorf("exportctx: registry must not be nil")
	}
	hook, err := RowHookName(entity)
	if err != nil {
		return err
	}
	registrant = strings.TrimSpace(registrant)
	if registrant == "" {
		return fmt.Errorf("exportctx: registrant must not be empty")
	}
	if handler == nil {
		return fmt.Errorf("exportctx: handler must not be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[hook] = append(r.handlers[hook], registeredHandler{
		entity:     entity,
		hook:       hook,
		priority:   priority,
		registrant: registrant,
		handler:    handler,
	})
	sort.SliceStable(r.handlers[hook], func(i, j int) bool {
		return r.handlers[hook][i].priority < r.handlers[hook][j].priority
	})
	return nil
}

// Invoke runs all row handlers for rowCtx.Entity in priority order.
func (r *Registry) Invoke(_ context.Context, rowCtx *RowContext) error {
	if r == nil || rowCtx == nil {
		return nil
	}
	hook, err := RowHookName(rowCtx.Entity)
	if err != nil {
		return err
	}

	r.mu.RLock()
	handlers := append([]registeredHandler(nil), r.handlers[hook]...)
	r.mu.RUnlock()

	for _, h := range handlers {
		if err := r.runHandler(rowCtx, h); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) runHandler(rowCtx *RowContext, h registeredHandler) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			if r.log != nil {
				r.log.Error("exportctx.handler.panic", fmt.Errorf("%v", rec), map[string]interface{}{
					"entity":     rowCtx.Entity,
					"hook":       h.hook,
					"registrant": h.registrant,
				})
			}
			err = fmt.Errorf("export row hook %q handler %q panicked: %v", h.hook, h.registrant, rec)
		}
	}()
	return h.handler(rowCtx)
}

// Catalog returns registered export row hooks grouped by entity.
func (r *Registry) Catalog() []CatalogEntry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.handlers) == 0 {
		return nil
	}

	byEntity := make(map[string]CatalogEntry)
	for hook, regs := range r.handlers {
		if len(regs) == 0 {
			continue
		}
		entity := regs[0].entity
		entry := byEntity[entity]
		entry.Entity = entity
		entry.Hook = hook
		for _, reg := range regs {
			entry.Handlers = append(entry.Handlers, CatalogHandler{
				Priority:   reg.priority,
				Registrant: reg.registrant,
			})
		}
		byEntity[entity] = entry
	}

	names := make([]string, 0, len(byEntity))
	for entity := range byEntity {
		names = append(names, entity)
	}
	sort.Strings(names)

	out := make([]CatalogEntry, 0, len(names))
	for _, entity := range names {
		out = append(out, byEntity[entity])
	}
	return out
}

// RowHookCatalog returns hook point names for all supported entities.
func RowHookCatalog() []string {
	entities := EntityCatalog()
	out := make([]string, len(entities))
	for i, entity := range entities {
		name, _ := RowHookName(entity)
		out[i] = name
	}
	return out
}
