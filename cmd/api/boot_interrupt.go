package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

// bootSignal watches SIGINT/SIGTERM during process boot and cancels a context
// for Migrator work. Unlike signal.NotifyContext, Stop keeps a buffered
// delivery so a signal that arrives in the microseconds around the end of
// boot is not discarded when the watcher is torn down.
type bootSignal struct {
	ctx      context.Context
	cancel   context.CancelFunc
	sigCh    chan os.Signal
	signaled atomic.Bool
	wg       sync.WaitGroup
}

// startBootSignal registers SIGINT/SIGTERM on a buffered channel and returns a
// cancellable boot context. Call Stop exactly once when boot work finishes.
func startBootSignal() *bootSignal {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	return newBootSignal(sigCh)
}

// newBootSignal wires an already-notified (or test-injected) channel.
func newBootSignal(sigCh chan os.Signal) *bootSignal {
	ctx, cancel := context.WithCancel(context.Background())
	b := &bootSignal{ctx: ctx, cancel: cancel, sigCh: sigCh}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		select {
		case <-sigCh:
			b.signaled.Store(true)
			cancel()
		case <-ctx.Done():
		}
	}()
	return b
}

// Context is cancelled when a boot signal is received.
func (b *bootSignal) Context() context.Context {
	if b == nil {
		return context.Background()
	}
	return b.ctx
}

// Stop unregisters the signal channel (restoring default terminate behaviour
// for later phases) and reports whether a boot signal was seen. After
// signal.Stop returns, any delivery that raced into the buffer is still
// readable; if the watcher goroutine already consumed it, signaled is set.
// Always call Stop — even when Context was already cancelled — so the
// channel is unregistered.
func (b *bootSignal) Stop() error {
	if b == nil {
		return nil
	}
	signal.Stop(b.sigCh)
	select {
	case <-b.sigCh:
		b.signaled.Store(true)
	default:
	}
	b.cancel()
	b.wg.Wait()
	if b.signaled.Load() {
		return fmt.Errorf("boot interrupted")
	}
	return nil
}
