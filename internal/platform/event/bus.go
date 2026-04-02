package event

import (
	"context"
	"fmt"
	"sync"

	"github.com/akarso/shopanda/internal/platform/logger"
)

// Handler processes an event. Returning an error from a sync handler aborts
// the operation; errors from async handlers are logged but do not propagate.
type Handler func(ctx context.Context, evt Event) error

// Bus is an in-process event bus supporting synchronous and asynchronous
// dispatch.
type Bus struct {
	mu    sync.RWMutex
	sync  map[string][]Handler
	async map[string][]Handler
	log   logger.Logger
}

// NewBus creates a Bus.
func NewBus(log logger.Logger) *Bus {
	return &Bus{
		sync:  make(map[string][]Handler),
		async: make(map[string][]Handler),
		log:   log,
	}
}

// On registers a synchronous handler for the given event name.
// Sync handlers run in the caller\'s goroutine; if any returns an error
// the Publish call returns that error immediately and remaining sync
// handlers are skipped.
func (b *Bus) On(name string, h Handler) {
	if h == nil {
		panic(fmt.Sprintf("event.Bus.On(%q): handler must not be nil", name))
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sync[name] = append(b.sync[name], h)
}

// OnAsync registers an asynchronous handler for the given event name.
// Async handlers run in separate goroutines after all sync handlers
// have succeeded.
func (b *Bus) OnAsync(name string, h Handler) {
	if h == nil {
		panic(fmt.Sprintf("event.Bus.OnAsync(%q): handler must not be nil", name))
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.async[name] = append(b.async[name], h)
}

// Publish dispatches an event to all registered handlers.
//
//  1. Sync handlers execute sequentially in registration order.
//     If any sync handler returns an error, Publish returns immediately
//     and async handlers are NOT invoked.
//  2. Async handlers each run in their own goroutine; errors are logged.
func (b *Bus) Publish(ctx context.Context, evt Event) error {
	b.mu.RLock()
	syncH := b.sync[evt.Name]
	asyncH := b.async[evt.Name]
	b.mu.RUnlock()

	b.log.Info("event.published", map[string]interface{}{
		"event_id":   evt.ID,
		"event_name": evt.Name,
		"source":     evt.Source,
	})

	// Phase 1: synchronous handlers.
	for _, h := range syncH {
		if err := h(ctx, evt); err != nil {
			b.log.Error("event.sync_handler.failed", err, map[string]interface{}{
				"event_id":   evt.ID,
				"event_name": evt.Name,
			})
			return err
		}
	}

	// Phase 2: asynchronous handlers (fire-and-forget).
	for _, h := range asyncH {
		go func(handler Handler) {
			if err := handler(ctx, evt); err != nil {
				b.log.Error("event.async_handler.failed", err, map[string]interface{}{
					"event_id":   evt.ID,
					"event_name": evt.Name,
				})
			}
		}(h)
	}

	return nil
}

// Handlers returns the total number of registered handlers (sync + async)
// for the given event name. Useful for diagnostics.
func (b *Bus) Handlers(name string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.sync[name]) + len(b.async[name])
}
