package hooks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/akarso/shopanda/internal/platform/logger"
)

// CatalogEntry describes a registered hook and its handlers.
type CatalogEntry struct {
	Name     string
	Handlers []CatalogHandler
}

// CatalogHandler describes one handler registration.
type CatalogHandler struct {
	Priority   int
	Registrant string
}

type registeredHandler struct {
	priority   int
	registrant string
	handler    Handler
}

// Registry stores hook handlers and executes ordered chains.
// Panics in handlers are recovered and returned as errors so core state is not corrupted.
// The first handler error stops the chain and is returned to the caller.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string][]registeredHandler
	log      logger.Logger
}

// NewRegistry creates an empty hook registry.
func NewRegistry(log logger.Logger) *Registry {
	return &Registry{
		handlers: make(map[string][]registeredHandler),
		log:      log,
	}
}

// Register adds a handler for hook at priority (lower runs first).
func (r *Registry) Register(hook string, priority int, registrant string, handler Handler) error {
	if r == nil {
		return fmt.Errorf("hooks: registry must not be nil")
	}
	if err := ValidateHookName(hook); err != nil {
		return err
	}
	registrant = strings.TrimSpace(registrant)
	if registrant == "" {
		return fmt.Errorf("hooks: registrant must not be empty")
	}
	if handler == nil {
		return fmt.Errorf("hooks: handler must not be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[hook] = append(r.handlers[hook], registeredHandler{
		priority:   priority,
		registrant: registrant,
		handler:    handler,
	})
	sort.SliceStable(r.handlers[hook], func(i, j int) bool {
		return r.handlers[hook][i].priority < r.handlers[hook][j].priority
	})
	return nil
}

// Invoke runs all handlers for hookCtx.Name in priority order.
func (r *Registry) Invoke(_ context.Context, hookCtx *Context) error {
	if r == nil || hookCtx == nil {
		return nil
	}
	if strings.TrimSpace(hookCtx.Name) == "" {
		return fmt.Errorf("hooks: context name must not be empty")
	}

	r.mu.RLock()
	handlers := append([]registeredHandler(nil), r.handlers[hookCtx.Name]...)
	r.mu.RUnlock()

	for _, h := range handlers {
		if err := r.runHandler(hookCtx, h); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) runHandler(hookCtx *Context, h registeredHandler) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			if r.log != nil {
				r.log.Error("hooks.handler.panic", fmt.Errorf("%v", rec), map[string]interface{}{
					"hook":       hookCtx.Name,
					"registrant": h.registrant,
				})
			}
			err = fmt.Errorf("hook %q handler %q panicked: %v", hookCtx.Name, h.registrant, rec)
		}
	}()
	return h.handler(hookCtx)
}

// Catalog returns registered hooks for tooling and admin APIs.
func (r *Registry) Catalog() []CatalogEntry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.handlers) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]CatalogEntry, 0, len(names))
	for _, name := range names {
		regs := r.handlers[name]
		entry := CatalogEntry{Name: name, Handlers: make([]CatalogHandler, 0, len(regs))}
		for _, reg := range regs {
			entry.Handlers = append(entry.Handlers, CatalogHandler{
				Priority:   reg.priority,
				Registrant: reg.registrant,
			})
		}
		out = append(out, entry)
	}
	return out
}
