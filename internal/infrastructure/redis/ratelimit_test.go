package redis

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/ratelimit"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/goleak"
)

func setupLimiterFactory(t *testing.T) (*miniredis.Miniredis, *goredis.Client, *LimiterFactory) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	factory := NewLimiterFactory(client, "shopanda", nil)
	t.Cleanup(func() { _ = factory.Close() })
	return mr, client, factory
}

func hangingLimiterClient(t *testing.T) *goredis.Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				time.Sleep(30 * time.Second)
				_ = c.Close()
			}(c)
		}
	}()
	client, err := NewLimiterClient("redis://"+ln.Addr().String(), 0)
	if err != nil {
		t.Fatalf("NewLimiterClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func allow(lim ratelimit.Allow, key string) bool {
	return lim.Allow(context.Background(), key).Allowed
}

func spec(name string, rate float64, burst int) ratelimit.Spec {
	return ratelimit.Spec{Name: name, Rate: rate, Burst: burst}
}

func TestSlidingWindow_BurstAndReject(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	// rate=1 burst=2 → 2s window, capacity 2
	lim := factory.New(spec("default", 1, 2))

	if !allow(lim, "1.2.3.4") {
		t.Fatal("request 1 should be allowed")
	}
	if !allow(lim, "1.2.3.4") {
		t.Fatal("request 2 should be allowed (burst=2)")
	}
	if allow(lim, "1.2.3.4") {
		t.Fatal("request 3 should be rejected")
	}
	if !allow(lim, "9.9.9.9") {
		t.Fatal("independent key should be allowed")
	}
}

func TestSlidingWindow_SubMillisecondRate(t *testing.T) {
	mr, _, factory := setupLimiterFactory(t)
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	mr.SetTime(start)
	// 2000/1 → 500µs. A millisecond-rounded window would be 1ms and would
	// still reject at 600µs.
	lim := factory.New(spec("fast", 2000, 1))
	if !allow(lim, "ip") {
		t.Fatal("first request should be allowed")
	}
	if allow(lim, "ip") {
		t.Fatal("second request should be rejected immediately")
	}
	mr.SetTime(start.Add(400 * time.Microsecond))
	if allow(lim, "ip") {
		t.Fatal("400µs later should still be rejected")
	}
	mr.SetTime(start.Add(600 * time.Microsecond))
	if !allow(lim, "ip") {
		t.Fatal("after the 500µs window the next request should be allowed")
	}
}

func TestSlidingWindow_CanceledSuccessfulProbeClosesCircuit(t *testing.T) {
	old := circuitHold
	circuitHold = 40 * time.Millisecond
	t.Cleanup(func() { circuitHold = old })

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	client.AddHook(delayHook{60 * time.Millisecond})
	factory := NewLimiterFactory(client, "shopanda", nil)
	t.Cleanup(func() { _ = factory.Close() })
	lim := factory.New(spec("default", 10, 10))
	factory.OpenCircuit()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	d := lim.Allow(ctx, "probe")
	if !d.Canceled || d.BackendError {
		t.Fatalf("canceled successful probe = %+v, want Canceled without BackendError", d)
	}

	start := time.Now()
	live := lim.Allow(context.Background(), "live")
	if live.BackendError {
		t.Fatal("circuit must close after a successful probe even if that client disconnected")
	}
	if !live.Allowed {
		t.Fatal("live request should hit healthy Redis")
	}
	if time.Since(start) < 40*time.Millisecond {
		t.Fatal("live request skipped Redis; circuit was still treating Redis as down")
	}
}

type delayHook struct{ d time.Duration }

func (h delayHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (h delayHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		time.Sleep(h.d)
		return next(ctx, cmd)
	}
}

func (h delayHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

func TestSlidingWindow_HonorsRateViaWindow(t *testing.T) {
	mr, _, factory := setupLimiterFactory(t)
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	mr.SetTime(start)
	// 10/20 → 2s window, capacity 20 — not 20/s
	lim := factory.New(spec("default", 10, 20))
	for i := 0; i < 20; i++ {
		if !allow(lim, "ip") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if allow(lim, "ip") {
		t.Fatal("request 21 should be rejected")
	}
	mr.SetTime(start.Add(time.Second))
	if allow(lim, "ip") {
		t.Fatal("1s later should still be rejected (2s window)")
	}
	mr.SetTime(start.Add(2*time.Second + time.Millisecond))
	if !allow(lim, "ip") {
		t.Fatal("after the 2s window the next request should be allowed")
	}
}

func TestSlidingWindow_RejectsAroundFixedWindowBoundary(t *testing.T) {
	mr, _, factory := setupLimiterFactory(t)
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	mr.SetTime(start)
	lim := factory.New(spec("boundary", 1, 1))

	if !allow(lim, "ip") {
		t.Fatal("first request should be allowed")
	}
	mr.SetTime(start.Add(999 * time.Millisecond))
	if allow(lim, "ip") {
		t.Fatal("request 999ms later should still be rejected (sliding window)")
	}
	mr.SetTime(start.Add(1001 * time.Millisecond))
	if !allow(lim, "ip") {
		t.Fatal("request after the window elapsed should be allowed")
	}
}

func TestSlidingWindow_TwoInstancesShareLimit(t *testing.T) {
	_, client, _ := setupLimiterFactory(t)
	a := NewLimiterFactory(client, "shopanda", nil)
	b := NewLimiterFactory(client, "shopanda", nil)
	limA := a.New(spec("default", 1, 2))
	limB := b.New(spec("default", 1, 2))

	if !allow(limA, "ip") || !allow(limB, "ip") {
		t.Fatal("first two admits (one per instance) should share the burst of 2")
	}
	if allow(limA, "ip") || allow(limB, "ip") {
		t.Fatal("third admit from either instance should be rejected")
	}
}

func TestSlidingWindow_TwoFactoriesNoMemberCollision(t *testing.T) {
	_, client, _ := setupLimiterFactory(t)
	a := NewLimiterFactory(client, "shopanda", nil)
	b := NewLimiterFactory(client, "shopanda", nil)
	limA := a.New(spec("default", 1, 2))
	limB := b.New(spec("default", 1, 2))

	if !allow(limA, "ip") {
		t.Fatal("A1")
	}
	if !allow(limB, "ip") {
		t.Fatal("B1")
	}
	if allow(limA, "ip") {
		t.Fatal("A2 should be rejected — unique members must increment the count")
	}
}

func TestSlidingWindow_NamedLimitersIsolated(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	def := factory.New(spec("default", 1, 1))
	route := factory.New(spec("route:/api/v1/auth/login", 1, 1))

	if !allow(def, "ip") {
		t.Fatal("default first should be allowed")
	}
	if allow(def, "ip") {
		t.Fatal("default second should be rejected")
	}
	if !allow(route, "ip") {
		t.Fatal("route limiter should not share the default key")
	}
}

func TestSlidingWindow_KeyTTLSet(t *testing.T) {
	mr, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("default", 1, 1))
	if !allow(lim, "ip") {
		t.Fatal("allow")
	}
	ttl := mr.TTL("shopanda-rl:default:ip")
	if ttl <= 0 {
		t.Fatalf("TTL = %v, want > 0", ttl)
	}
}

func TestSlidingWindow_FailOpenByDefault(t *testing.T) {
	mr, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("down", 10, 10))
	mr.Close()
	if !allow(lim, "ip") {
		t.Fatal("Allow should admit when Redis is down (fail-open)")
	}
}

func TestSlidingWindow_FailClosed(t *testing.T) {
	mr, _, factory := setupLimiterFactory(t)
	lim := factory.New(ratelimit.Spec{Name: "down", Rate: 10, Burst: 10, OnError: ratelimit.OnErrorClosed})
	mr.Close()
	if allow(lim, "ip") {
		t.Fatal("Allow should deny when Redis is down and on_error=closed")
	}
}

func TestSlidingWindow_CancelledContextDenied(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	open := factory.New(spec("open", 10, 10))
	local := factory.New(ratelimit.Spec{Name: "local", Rate: 1, Burst: 1, OnError: ratelimit.OnErrorLocal})
	closed := factory.New(ratelimit.Spec{Name: "closed", Rate: 10, Burst: 10, OnError: ratelimit.OnErrorClosed})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, lim := range []ratelimit.Allow{open, local, closed} {
		d := lim.Allow(ctx, "ip")
		if d.Allowed || d.BackendError || !d.Canceled {
			t.Fatalf("cancelled context must deny uncounted, got %+v", d)
		}
	}
}

func TestSlidingWindow_CancelledContextDoesNotTripCircuit(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	var backendHits atomic.Int64
	factory.SetErrorObserver(func(_, _ string) { backendHits.Add(1) })
	lim := factory.New(spec("ctx", 10, 10))
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = lim.Allow(ctx, "ip")
	}
	if backendHits.Load() != 0 {
		t.Fatalf("cancellations recorded %d backend errors", backendHits.Load())
	}
	d := lim.Allow(context.Background(), "ip")
	if d.BackendError {
		t.Fatal("cancellations must not trip the circuit")
	}
	if !d.Allowed {
		t.Fatal("live Redis should admit after cancellations")
	}
}

func TestSlidingWindow_CancelledContextDoesNotConsumeLocal(t *testing.T) {
	mr, _, factory := setupLimiterFactory(t)
	lim := factory.New(ratelimit.Spec{Name: "auth", Rate: 1, Burst: 1, OnError: ratelimit.OnErrorLocal})
	mr.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if lim.Allow(ctx, "ip").Allowed {
		t.Fatal("cancelled must deny")
	}
	if !allow(lim, "ip") {
		t.Fatal("local fallback budget must still have its first token")
	}
}

func TestSlidingWindow_DisconnectDuringRedisIsCanceled(t *testing.T) {
	factory := NewLimiterFactory(hangingLimiterClient(t), "shopanda", nil)
	t.Cleanup(func() { _ = factory.Close() })
	var backendHits atomic.Int64
	factory.SetErrorObserver(func(_, _ string) { backendHits.Add(1) })
	lim := factory.New(spec("default", 10, 10))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	d := lim.Allow(ctx, "ip")
	if !d.Canceled || d.Allowed {
		t.Fatalf("disconnect during Redis must be Canceled, got %+v", d)
	}
	if d.BackendError {
		t.Fatal("gone client is not a backend error / fail-open admit")
	}
	if backendHits.Load() != 0 {
		t.Fatal("disconnect during a Redis timeout must not record a backend error")
	}
}

func TestSlidingWindow_DisconnectDuringRedisDoesNotTripCircuit(t *testing.T) {
	factory := NewLimiterFactory(hangingLimiterClient(t), "shopanda", nil)
	t.Cleanup(func() { _ = factory.Close() })
	var backendHits atomic.Int64
	factory.SetErrorObserver(func(_, _ string) { backendHits.Add(1) })
	lim := factory.New(spec("default", 10, 10))
	var wg sync.WaitGroup
	wg.Add(circuitLowErrors)
	for i := 0; i < circuitLowErrors; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				time.Sleep(50 * time.Millisecond)
				cancel()
			}()
			d := lim.Allow(ctx, "ip")
			if !d.Canceled || d.BackendError {
				t.Errorf("disconnect during Redis = %+v, want Canceled", d)
			}
		}()
	}
	wg.Wait()
	if backendHits.Load() != 0 {
		t.Fatalf("disconnects recorded %d backend errors", backendHits.Load())
	}
	start := time.Now()
	d := lim.Allow(context.Background(), "ip")
	if time.Since(start) < 150*time.Millisecond {
		t.Fatal("circuit must still talk to Redis after disconnect-timeouts")
	}
	if !d.Allowed || !d.BackendError {
		t.Fatalf("live timeout = %+v, want fail-open (circuit still closed)", d)
	}
}

func TestSlidingWindow_ConcurrentDoesNotOverAdmit(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("race", 10, 10))

	const workers = 50
	var admitted atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if allow(lim, "ip") {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := admitted.Load(); got != 10 {
		t.Fatalf("admitted = %d, want 10", got)
	}
}

func TestLimiterFactory_OwnedClose(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	factory := NewOwnedLimiterFactory(client, "p", nil)
	if err := factory.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := client.Ping(context.Background()).Err(); err == nil {
		t.Fatal("Ping succeeded after Close; owned client should be closed")
	}
}

func TestRateLimitPrefix(t *testing.T) {
	if got := RateLimitPrefix("shopanda"); got != "shopanda-rl:" {
		t.Fatalf("got %q", got)
	}
	if got := RateLimitPrefix("shopanda:"); got != "shopanda-rl:" {
		t.Fatalf("got %q", got)
	}
	if got := RateLimitPrefix(""); got != "shopanda-rl:" {
		t.Fatalf("got %q", got)
	}
}

func TestSlidingWindow_FractionalRateMatchesAverage(t *testing.T) {
	mr, _, factory := setupLimiterFactory(t)
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	mr.SetTime(start)
	// 15/20 → 4/3s window (1333333µs), capacity 20 ≈ 15/s
	lim := factory.New(spec("default", 15, 20))
	window := ratelimit.Window(15, 20)
	for i := 0; i < 20; i++ {
		if !allow(lim, "ip") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if allow(lim, "ip") {
		t.Fatal("request 21 should be rejected")
	}
	mr.SetTime(start.Add(window - time.Microsecond))
	if allow(lim, "ip") {
		t.Fatal("just inside the window should still be rejected")
	}
	mr.SetTime(start.Add(window + time.Microsecond))
	if !allow(lim, "ip") {
		t.Fatal("after the window the next request should be allowed")
	}
}

func TestSlidingWindow_OnErrorLocal(t *testing.T) {
	mr, _, factory := setupLimiterFactory(t)
	lim := factory.New(ratelimit.Spec{Name: "auth", Rate: 1, Burst: 1, OnError: ratelimit.OnErrorLocal})
	mr.Close()
	if !allow(lim, "ip") {
		t.Fatal("first local fallback should admit")
	}
	if allow(lim, "ip") {
		t.Fatal("local fallback should enforce burst=1")
	}
}

func TestNewLimiterClient_AppliesTimeouts(t *testing.T) {
	c, err := NewLimiterClient("redis://127.0.0.1:1", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	opt := c.Options()
	if !opt.ContextTimeoutEnabled {
		t.Fatal("ContextTimeoutEnabled")
	}
	if opt.DialTimeout != slidingWindowAllow || opt.ReadTimeout != slidingWindowAllow || opt.WriteTimeout != slidingWindowAllow {
		t.Fatalf("timeouts = dial %v read %v write %v, want %v", opt.DialTimeout, opt.ReadTimeout, opt.WriteTimeout, slidingWindowAllow)
	}
	if opt.MaxRetries != 0 {
		t.Fatalf("MaxRetries = %d, want 0 (disabled; NewClient maps -1 → 0)", opt.MaxRetries)
	}
	if opt.PoolSize != DefaultLimiterPoolSize() {
		t.Fatalf("PoolSize = %d, want %d", opt.PoolSize, DefaultLimiterPoolSize())
	}
}

func TestSlidingWindow_RetryAfter(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("default", 10, 20))
	d := lim.Allow(context.Background(), "ip")
	if d.RetryAfter != 2*time.Second {
		t.Fatalf("RetryAfter = %v, want 2s", d.RetryAfter)
	}
}

func TestSlidingWindow_SlowRedisTimeout(t *testing.T) {
	factory := NewLimiterFactory(hangingLimiterClient(t), "shopanda", nil)
	t.Cleanup(func() { _ = factory.Close() })
	lim := factory.New(spec("default", 10, 10))

	start := time.Now()
	d := lim.Allow(context.Background(), "ip")
	elapsed := time.Since(start)
	if elapsed < 150*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("slow Redis Allow took %v, want ~200ms (production limiter client)", elapsed)
	}
	if !d.Allowed || !d.BackendError {
		t.Fatalf("decision = %+v, want fail-open after timeout", d)
	}
}

func tripWithHangingAllows(lim ratelimit.Allow) {
	var wg sync.WaitGroup
	wg.Add(circuitMinRequests)
	for i := 0; i < circuitMinRequests; i++ {
		go func() {
			defer wg.Done()
			_ = lim.Allow(context.Background(), "ip")
		}()
	}
	wg.Wait()
}

func TestSlidingWindow_CircuitBreakerTripsOnTimeouts(t *testing.T) {
	factory := NewLimiterFactory(hangingLimiterClient(t), "shopanda", nil)
	t.Cleanup(func() { _ = factory.Close() })
	lim := factory.New(spec("default", 10, 10))

	start := time.Now()
	tripWithHangingAllows(lim)
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("concurrent trip took %v, want ~200ms", elapsed)
	}
	start = time.Now()
	d := lim.Allow(context.Background(), "ip")
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("circuit-open Allow took %v, want immediate fallback", time.Since(start))
	}
	if !d.Allowed || !d.BackendError {
		t.Fatalf("decision = %+v, want fail-open backend error", d)
	}
}

func TestSlidingWindow_CircuitRecoversAfterHold(t *testing.T) {
	old := circuitHold
	circuitHold = 80 * time.Millisecond
	t.Cleanup(func() { circuitHold = old })

	_, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("default", 10, 10))
	factory.OpenCircuit()
	time.Sleep(100 * time.Millisecond)
	d := lim.Allow(context.Background(), "ip")
	if !d.Allowed || d.BackendError {
		t.Fatalf("probe after hold should hit Redis, got %+v", d)
	}
}

func TestSlidingWindow_CircuitOpenIncrementsObserver(t *testing.T) {
	factory := NewLimiterFactory(hangingLimiterClient(t), "shopanda", nil)
	t.Cleanup(func() { _ = factory.Close() })
	var circuitHits atomic.Int64
	factory.SetErrorObserver(func(_, reason string) {
		if reason == errReasonCircuit {
			circuitHits.Add(1)
		}
	})
	lim := factory.New(spec("default", 10, 10))
	tripWithHangingAllows(lim)
	_ = lim.Allow(context.Background(), "ip")
	if circuitHits.Load() < 1 {
		t.Fatal("observer should fire with reason=circuit_open while the circuit is held")
	}
}

func TestSlidingWindow_ErrorRateTripsCircuit(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("default", 100, 100))
	for i := 0; i < circuitMinRequests/2; i++ {
		if !allow(lim, fmt.Sprintf("ok-%d", i)) {
			t.Fatalf("success %d", i)
		}
		factory.recordError("default", errors.New("timeout"), errReasonError, false)
	}
	start := time.Now()
	d := lim.Allow(context.Background(), "ok")
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("error-rate trip should skip Redis, took %v", time.Since(start))
	}
	if !d.BackendError {
		t.Fatalf("want fallback after %d req at ≥50%% errors, got %+v", circuitMinRequests, d)
	}
}

func TestSlidingWindow_LowErrorRateDoesNotTrip(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("default", 100, 100))
	for i := 0; i < 100; i++ {
		if !allow(lim, fmt.Sprintf("ok-%d", i)) {
			t.Fatalf("success %d", i)
		}
		if i%20 == 0 {
			factory.recordError("default", errors.New("blip"), errReasonError, false)
		}
	}
	d := lim.Allow(context.Background(), "fresh")
	if d.BackendError {
		t.Fatal("5% errors at high volume must not trip the circuit")
	}
}

func TestSlidingWindow_PoolTimeoutReason(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				time.Sleep(30 * time.Second)
				_ = c.Close()
			}(c)
		}
	}()
	client, err := NewLimiterClient("redis://"+ln.Addr().String(), 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	factory := NewLimiterFactory(client, "shopanda", nil)
	t.Cleanup(func() { _ = factory.Close() })
	var poolHits atomic.Int64
	factory.SetErrorObserver(func(_, reason string) {
		if reason == errReasonPoolTimeout {
			poolHits.Add(1)
		}
	})
	lim := factory.New(spec("default", 100, 100))
	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = lim.Allow(context.Background(), "ip")
		}()
	}
	wg.Wait()
	if poolHits.Load() < 1 {
		t.Fatal("expected at least one pool_timeout when concurrent Allows exceed PoolSize=2")
	}
}

func TestSlidingWindow_HalfOpenSingleProbe(t *testing.T) {
	old := circuitHold
	circuitHold = 50 * time.Millisecond
	t.Cleanup(func() { circuitHold = old })

	factory := NewLimiterFactory(hangingLimiterClient(t), "shopanda", nil)
	t.Cleanup(func() { _ = factory.Close() })
	lim := factory.New(spec("default", 10, 10))
	tripWithHangingAllows(lim)
	time.Sleep(70 * time.Millisecond)

	var other time.Duration
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		start := time.Now()
		_ = lim.Allow(context.Background(), "b")
		other = time.Since(start)
	}()
	start := time.Now()
	_ = lim.Allow(context.Background(), "a")
	probe := time.Since(start)
	wg.Wait()
	if probe < 100*time.Millisecond {
		t.Fatalf("probe took %v, want ~200ms", probe)
	}
	if other > 50*time.Millisecond {
		t.Fatalf("non-probe took %v, want immediate fallback", other)
	}
}

func TestSlidingWindow_SlowRedisNoGoroutineLeak(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var acceptWG, connWG sync.WaitGroup
	acceptWG.Add(1)
	go func() {
		defer acceptWG.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			connWG.Add(1)
			go func(c net.Conn) {
				defer connWG.Done()
				select {
				case <-done:
				case <-time.After(time.Second):
				}
				_ = c.Close()
			}(c)
		}
	}()
	client, err := NewLimiterClient("redis://"+ln.Addr().String(), 0)
	if err != nil {
		t.Fatal(err)
	}
	factory := NewOwnedLimiterFactory(client, "shopanda", nil)
	lim := factory.New(spec("default", 10, 10))
	for i := 0; i < 3; i++ {
		_ = lim.Allow(context.Background(), "ip")
	}
	if err := factory.Close(); err != nil {
		t.Fatal(err)
	}
	close(done)
	_ = ln.Close()
	acceptWG.Wait()
	connWG.Wait()
}

func TestLimiterPoolSize_CappedAt64(t *testing.T) {
	if got := limiterPoolSize(1); got != 10 {
		t.Fatalf("GOMAXPROCS=1: got %d want 10", got)
	}
	if got := limiterPoolSize(8); got != 64 {
		t.Fatalf("GOMAXPROCS=8: got %d want 64", got)
	}
	if got := limiterPoolSize(0); got != 8 {
		t.Fatalf("GOMAXPROCS=0: got %d want 8", got)
	}
}

func TestBackendReason_UsesErrorsIsOnly(t *testing.T) {
	if got := backendReason(errors.New("redis: connection pool timeout")); got != errReasonError {
		t.Fatalf("string match = %q, want %q", got, errReasonError)
	}
	if got := backendReason(fmt.Errorf("wrap: %w", goredis.ErrPoolTimeout)); got != errReasonPoolTimeout {
		t.Fatalf("wrapped ErrPoolTimeout = %q, want %q", got, errReasonPoolTimeout)
	}
}

func TestSlidingWindow_StallUnderLoadDoesNotTrip(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("default", 1000, 1000))
	// 80 successes then 20 concurrent timeouts ≈ 20% errors — a 300ms
	// stall at high RPS, without treating consecutive timeouts as a trip.
	for i := 0; i < 80; i++ {
		factory.recordSuccess(false)
	}
	var wg sync.WaitGroup
	wg.Add(20)
	for i := 0; i < 20; i++ {
		go func() {
			defer wg.Done()
			factory.recordError("default", errors.New("timeout"), errReasonError, false)
		}()
	}
	wg.Wait()
	d := lim.Allow(context.Background(), "fresh")
	if d.BackendError {
		t.Fatal("20% errors from a stall must not trip the circuit")
	}
}

func TestSlidingWindow_LowTrafficAllErrorsTrip(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("default", 10, 10))
	for i := 0; i < circuitLowErrors; i++ {
		factory.recordError("default", errors.New("timeout"), errReasonError, false)
	}
	start := time.Now()
	d := lim.Allow(context.Background(), "ip")
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("low-traffic trip should skip Redis, took %v", time.Since(start))
	}
	if !d.BackendError {
		t.Fatalf("5 errors and no successes must open the circuit, got %+v", d)
	}
}

func TestSlidingWindow_LowTrafficFourErrorsDoNotTrip(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("default", 10, 10))
	for i := 0; i < circuitLowErrors-1; i++ {
		factory.recordError("default", errors.New("timeout"), errReasonError, false)
	}
	d := lim.Allow(context.Background(), "ip")
	if d.BackendError {
		t.Fatal("4 errors in a quiet window must not trip")
	}
}

func TestSlidingWindow_LowTrafficMixedDoesNotTrip(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("default", 10, 10))
	factory.recordSuccess(false)
	for i := 0; i < circuitLowErrors; i++ {
		factory.recordError("default", errors.New("timeout"), errReasonError, false)
	}
	d := lim.Allow(context.Background(), "ip")
	if d.BackendError {
		t.Fatal("5 errors plus a success (not 100% of a quiet window) must not trip")
	}
}

func TestSlidingWindow_LowTrafficErrorsSpacedBeyondWindowDoNotTrip(t *testing.T) {
	_, _, factory := setupLimiterFactory(t)
	lim := factory.New(spec("default", 10, 10))
	gap := time.Duration(circuitBucketCount)*circuitBucket + 100*time.Millisecond
	for i := 0; i < circuitLowErrors; i++ {
		if i > 0 {
			time.Sleep(gap)
		}
		factory.recordError("default", errors.New("timeout"), errReasonError, false)
	}
	d := lim.Allow(context.Background(), "ip")
	if d.BackendError {
		t.Fatal("errors spaced more than 2s apart must not trip the circuit")
	}
}

func TestSlidingWindow_FailedProbeReleasesSlot(t *testing.T) {
	old := circuitHold
	circuitHold = 50 * time.Millisecond
	t.Cleanup(func() { circuitHold = old })

	factory := NewLimiterFactory(hangingLimiterClient(t), "shopanda", nil)
	t.Cleanup(func() { _ = factory.Close() })
	lim := factory.New(spec("default", 10, 10))
	factory.OpenCircuit()
	time.Sleep(70 * time.Millisecond)

	start := time.Now()
	_ = lim.Allow(context.Background(), "a")
	first := time.Since(start)
	if first < 100*time.Millisecond {
		t.Fatalf("first probe took %v, want ~200ms", first)
	}

	start = time.Now()
	d := lim.Allow(context.Background(), "b")
	held := time.Since(start)
	if held > 50*time.Millisecond {
		t.Fatalf("after failed probe Allow took %v, want immediate hold", held)
	}
	if !d.BackendError {
		t.Fatal("failed probe must re-arm the 5s hold")
	}

	time.Sleep(70 * time.Millisecond)
	start = time.Now()
	_ = lim.Allow(context.Background(), "c")
	third := time.Since(start)
	if third < 100*time.Millisecond {
		t.Fatalf("probe after re-armed hold took %v; probing stuck?", third)
	}
}

func BenchmarkSlidingWindow_Allow(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(mr.Close)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	b.Cleanup(func() { _ = client.Close() })
	factory := NewLimiterFactory(client, "shopanda", nil)
	b.Cleanup(func() { _ = factory.Close() })
	lim := factory.New(spec("bench", 1_000_000, 1_000_000))
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = lim.Allow(context.Background(), "ip")
		}
	})
}

func BenchmarkLimiterFactory_NoteParallel(b *testing.B) {
	factory := NewLimiterFactory(nil, "shopanda", nil)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			factory.note(false)
		}
	})
}
