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
// Invoke / InvokeAdapted: the first handler error stops the chain and is
// returned to the caller. InvokeContinueOnError (order.line.display) reports
// errors via callback and keeps running later handlers.
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

// InvokeContinueOnError runs order.line.display handlers in priority order.
// Handler errors and recovered panics are reported via onErr (when non-nil)
// and do not stop later handlers.
//
// Cancel is checked only between handlers: a handler that has already started
// is not interrupted. On cancel, onErr is called with an empty registrant
// (chain-level), not the next plugin name.
//
// Before each handler, order_id / variant_id / language are reset to the chain
// values and extensions is set to a deep copy of the original map so in-place
// writes cannot retarget a later plugin. On handler error or panic, fields are
// restored from a by-value snapshot (including when the handler mutated
// elements or replaced the pointer). Successful appends are kept for later
// handlers.
//
// This method is specialized for order.line.display; do not reuse it for other
// hooks without revisiting the sealed keys and fields rollback.
func (r *Registry) InvokeContinueOnError(ctx context.Context, hookCtx *Context, onErr func(registrant string, err error)) {
	if r == nil || hookCtx == nil {
		return
	}
	if strings.TrimSpace(hookCtx.Name) == "" {
		if onErr != nil {
			onErr("", fmt.Errorf("hooks: context name must not be empty"))
		}
		return
	}

	r.mu.RLock()
	handlers := append([]registeredHandler(nil), r.handlers[hookCtx.Name]...)
	r.mu.RUnlock()

	sealedOrderID, hasOrderID := hookCtx.Get("order_id")
	sealedVariantID, hasVariantID := hookCtx.Get("variant_id")
	sealedLanguage, hasLanguage := hookCtx.Get("language")
	var sealedExt map[string]interface{}
	if rawExt, ok := hookCtx.Get("extensions"); ok && rawExt != nil {
		if m, ok := rawExt.(map[string]interface{}); ok {
			sealedExt = deepCopyJSONMap(m)
		}
	}

	for _, h := range handlers {
		if ctx != nil {
			select {
			case <-ctx.Done():
				if onErr != nil {
					onErr("", ctx.Err())
				}
				return
			default:
			}
		}

		if hasOrderID {
			hookCtx.Set("order_id", sealedOrderID)
		} else if hookCtx.Payload != nil {
			delete(hookCtx.Payload, "order_id")
		}
		if hasVariantID {
			hookCtx.Set("variant_id", sealedVariantID)
		} else if hookCtx.Payload != nil {
			delete(hookCtx.Payload, "variant_id")
		}
		if hasLanguage {
			hookCtx.Set("language", sealedLanguage)
		} else if hookCtx.Payload != nil {
			delete(hookCtx.Payload, "language")
		}
		if sealedExt != nil {
			hookCtx.Set("extensions", deepCopyJSONMap(sealedExt))
		} else if hookCtx.Payload != nil {
			delete(hookCtx.Payload, "extensions")
		}

		fieldsSnap := takeOrderLineFieldsSnapshot(hookCtx)
		err := r.runHandler(hookCtx, h)

		// Always re-seal identity for the next handler (success or failure).
		if hasOrderID {
			hookCtx.Set("order_id", sealedOrderID)
		}
		if hasVariantID {
			hookCtx.Set("variant_id", sealedVariantID)
		}
		if hasLanguage {
			hookCtx.Set("language", sealedLanguage)
		}
		if sealedExt != nil {
			hookCtx.Set("extensions", deepCopyJSONMap(sealedExt))
		}

		if err != nil {
			restoreOrderLineFieldsSnapshot(hookCtx, fieldsSnap)
			if onErr != nil {
				onErr(h.registrant, err)
			}
		}
	}
}

func deepCopyJSONMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = deepCopyJSONValue(v)
	}
	return out
}

func deepCopyJSONValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		return deepCopyJSONMap(x)
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = deepCopyJSONValue(e)
		}
		return out
	default:
		return v
	}
}

// InvokeAdapted runs all handlers for hookCtx.Name in priority order, like
// Invoke, but calls adapt(registrant, hookCtx) before each handler so that
// handler gets its own view of the payload. Used when a payload value is
// sensitive to which plugin is receiving it (e.g. a plugin-only extension
// value that must not leak to every handler just because one handler's
// registrant is trusted to see it). adapt may mutate hookCtx or return a
// Clone with per-registrant keys (cart.validate clones so private cart
// rows never sit on the shared context). Pointer payload values remain
// shared across clones when adapt copies them. A nil adapt or nil return
// falls back to the unadapted hookCtx.
func (r *Registry) InvokeAdapted(ctx context.Context, hookCtx *Context, adapt func(registrant string, src *Context) *Context) error {
	if adapt == nil {
		return r.Invoke(ctx, hookCtx)
	}
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
		view := adapt(h.registrant, hookCtx)
		if view == nil {
			view = hookCtx
		}
		if err := r.runHandler(view, h); err != nil {
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
