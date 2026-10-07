package cache_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cacheApp "github.com/akarso/shopanda/internal/application/cache"
)

func TestMissCoalescer_SingleRender(t *testing.T) {
	c := cacheApp.NewMissCoalescer()
	var renders atomic.Int32
	var wg sync.WaitGroup
	const n = 20
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err, _ := c.Do("k", time.Second, func() (any, error) {
				renders.Add(1)
				time.Sleep(50 * time.Millisecond)
				return "ok", nil
			})
			if err != nil {
				t.Errorf("Do: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := renders.Load(); got != 1 {
		t.Fatalf("renders = %d, want 1", got)
	}
}

func TestMissCoalescer_TimeoutFallsBack(t *testing.T) {
	c := cacheApp.NewMissCoalescer()
	started := make(chan struct{})
	var renders atomic.Int32

	go func() {
		_, _, _ = c.Do("k", time.Second, func() (any, error) {
			renders.Add(1)
			close(started)
			time.Sleep(200 * time.Millisecond)
			return "leader", nil
		})
	}()
	<-started

	_, err, shared := c.Do("k", 20*time.Millisecond, func() (any, error) {
		renders.Add(1)
		return "fallback", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if shared {
		t.Fatal("timed-out waiter must not report shared")
	}
	if got := renders.Load(); got < 2 {
		t.Fatalf("renders = %d, want >= 2 (leader + fallback)", got)
	}
}

func TestMissCoalescer_PanicCleansInFlight(t *testing.T) {
	c := cacheApp.NewMissCoalescer()
	func() {
		defer func() { _ = recover() }()
		_, _, _ = c.Do("poison", time.Second, func() (any, error) {
			panic("render boom")
		})
	}()

	start := time.Now()
	val, err, shared := c.Do("poison", 500*time.Millisecond, func() (any, error) {
		return "ok", nil
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if shared {
		t.Fatal("after panic cleanup, next Do must be the leader")
	}
	if val != "ok" {
		t.Fatalf("val = %v", val)
	}
	if elapsed >= 400*time.Millisecond {
		t.Fatalf("elapsed %v — poisoned key still forced a wait", elapsed)
	}
}

func TestMissCoalescer_PanicWaiterSeesError(t *testing.T) {
	c := cacheApp.NewMissCoalescer()
	inFlight := make(chan struct{})
	proceed := make(chan struct{})
	leaderDone := make(chan struct{})

	go func() {
		defer close(leaderDone)
		defer func() { _ = recover() }()
		_, _, _ = c.Do("k", time.Second, func() (any, error) {
			close(inFlight)
			<-proceed
			panic("render boom")
		})
	}()
	<-inFlight

	var wg sync.WaitGroup
	const n = 4
	errs := make([]error, n)
	sharedFlags := make([]bool, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i], sharedFlags[i] = c.Do("k", time.Second, func() (any, error) {
				t.Error("waiter must not fall back when sharing a panicked leader")
				return "fallback", nil
			})
		}(i)
	}
	time.Sleep(30 * time.Millisecond) // park waiters on done
	close(proceed)
	<-leaderDone
	wg.Wait()

	for i := 0; i < n; i++ {
		if !sharedFlags[i] {
			t.Fatalf("waiter %d shared = false", i)
		}
		if errs[i] == nil {
			t.Fatalf("waiter %d got nil err (false success after leader panic)", i)
		}
	}
}
