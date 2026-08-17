package event

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/platform/logger"
)

// Handler processes an event. Returning an error from a sync handler aborts
// the operation; errors from async handlers are logged but do not propagate.
type Handler func(ctx context.Context, evt Event) error

// drainCancelRemainderCap is the maximum time reserved after grace to let
// stragglers observe cancellation. Remainder is min(timeout/5, this cap) —
// hardcoded, not configurable (see RUNBOOK / PR-1019).
const drainCancelRemainderCap = time.Second

// Bus is an in-process event bus supporting synchronous and asynchronous
// dispatch.
type Bus struct {
	mu    sync.RWMutex
	sync  map[string][]Handler
	async map[string][]Handler
	log   logger.Logger

	shuttingDown   bool
	wg             sync.WaitGroup
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	shutdownOnce   sync.Once
	drainDone      chan struct{}
}

// NewBus creates a Bus.
func NewBus(log logger.Logger) *Bus {
	if log == nil {
		panic("event.NewBus: logger must not be nil")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Bus{
		sync:           make(map[string][]Handler),
		async:          make(map[string][]Handler),
		log:            log,
		shutdownCtx:    ctx,
		shutdownCancel: cancel,
		drainDone:      make(chan struct{}),
	}
}

// On registers a synchronous handler for the given event name.
// Sync handlers run in the caller's goroutine; if any returns an error
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
// have succeeded. They receive the bus shutdown context (not the Publish
// request context) so request cancel does not abort in-flight side effects.
// Process shutdown uses wait-then-cancel: ctx stays live during Drain's
// grace window, then is cancelled so stragglers can exit.
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
//  2. After BeginShutdown, Publish still runs sync handlers (abort
//     semantics unchanged) but does not start new async goroutines.
//  3. Async handlers each run in their own goroutine with the shutdown
//     context; errors are logged.
func (b *Bus) Publish(ctx context.Context, evt Event) error {
	b.mu.RLock()
	syncH := append([]Handler(nil), b.sync[evt.Name]...)
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

	// Phase 2: asynchronous handlers. Admission + WaitGroup add run under
	// RLock so concurrent Publish calls stay parallel. BeginShutdown takes
	// Lock(), which waits for these readers before setting shuttingDown and
	// starting Wait — so Add cannot race with Wait.
	b.mu.RLock()
	if b.shuttingDown {
		b.mu.RUnlock()
		return nil
	}
	asyncH := append([]Handler(nil), b.async[evt.Name]...)
	if n := len(asyncH); n > 0 {
		b.wg.Add(n)
	}
	handlerCtx := b.shutdownCtx
	b.mu.RUnlock()

	for _, h := range asyncH {
		go func(handler Handler) {
			defer b.wg.Done()
			if err := handler(handlerCtx, evt); err != nil {
				b.log.Error("event.async_handler.failed", err, map[string]interface{}{
					"event_id":   evt.ID,
					"event_name": evt.Name,
				})
			}
		}(h)
	}

	return nil
}

// BeginShutdown stops admitting new async publishes and starts waiting for
// in-flight handler goroutines. It does not cancel handler contexts — use
// Drain for wait-then-cancel process shutdown. Safe to call more than once.
func (b *Bus) BeginShutdown() {
	b.shutdownOnce.Do(func() {
		b.mu.Lock()
		b.shuttingDown = true
		b.mu.Unlock()
		go func() {
			b.wg.Wait()
			close(b.drainDone)
		}()
	})
}

// Done closes after BeginShutdown when all async handler goroutines have
// returned. Do not wait on it until BeginShutdown has been called.
func (b *Bus) Done() <-chan struct{} {
	return b.drainDone
}

// Drain is the process-shutdown path: stop admitting, wait for in-flight
// handlers with a live context (grace), then cancel stragglers and wait the
// remainder. Logs event.bus.drain.timeout if handlers outlive the deadline
// (they may still be running when Drain returns).
func (b *Bus) Drain(timeout time.Duration) {
	b.BeginShutdown()
	grace, remainder := drainWindows(timeout)

	if waitDone(b.drainDone, grace) {
		return
	}
	b.shutdownCancel()
	if waitDone(b.drainDone, remainder) {
		return
	}
	b.log.Info("event.bus.drain.timeout", nil)
}

func drainWindows(timeout time.Duration) (grace, remainder time.Duration) {
	if timeout <= 0 {
		return 0, 0
	}
	remainder = timeout / 5
	if remainder > drainCancelRemainderCap {
		remainder = drainCancelRemainderCap
	}
	return timeout - remainder, remainder
}

func waitDone(done <-chan struct{}, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// Handlers returns the total number of registered handlers (sync + async)
// for the given event name. Useful for diagnostics.
func (b *Bus) Handlers(name string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.sync[name]) + len(b.async[name])
}
