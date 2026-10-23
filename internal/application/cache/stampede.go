package cache

import (
	"fmt"
	"sync"
	"time"
)

// DefaultStampedeWait is how long a concurrent miss waiter blocks for the
// in-flight render before falling back to rendering itself (PR-1046).
const DefaultStampedeWait = 2 * time.Second

// MissCoalescer is a process-local per-key in-flight lock for cache misses.
// The first caller for a key runs fn; concurrent callers wait up to wait
// for that result. Past the wait they run fn independently rather than hang.
// Cross-instance coordination is out of scope (see PR-1046).
type MissCoalescer struct {
	mu sync.Mutex
	m  map[string]*missCall
}

type missCall struct {
	done chan struct{}
	val  any
	err  error
}

// NewMissCoalescer returns an empty coalescer.
func NewMissCoalescer() *MissCoalescer {
	return &MissCoalescer{m: make(map[string]*missCall)}
}

// Do runs fn once per key while a call is in flight. wait bounds how long
// followers block; wait <= 0 uses DefaultStampedeWait. shared is true when
// this caller reused another in-flight call's result (not when it timed out
// and rendered independently).
//
// If fn panics, waiters observe a non-nil error (not a false success), the
// in-flight entry is closed/removed, and the panic is re-raised to the leader.
func (c *MissCoalescer) Do(key string, wait time.Duration, fn func() (any, error)) (val any, err error, shared bool) {
	if c == nil {
		v, e := fn()
		return v, e, false
	}
	if wait <= 0 {
		wait = DefaultStampedeWait
	}

	c.mu.Lock()
	if call, ok := c.m[key]; ok {
		c.mu.Unlock()
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-call.done:
			return call.val, call.err, true
		case <-timer.C:
			v, e := fn()
			return v, e, false
		}
	}
	call := &missCall{done: make(chan struct{})}
	c.m[key] = call
	c.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("cache.miss_coalesce: panic: %v", r)
			val = nil
			call.val, call.err = val, err
			close(call.done)
			c.mu.Lock()
			delete(c.m, key)
			c.mu.Unlock()
			panic(r)
		}
		call.val, call.err = val, err
		close(call.done)
		c.mu.Lock()
		delete(c.m, key)
		c.mu.Unlock()
	}()

	val, err = fn()
	return val, err, false
}
