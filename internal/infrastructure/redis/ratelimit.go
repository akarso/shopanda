package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akarso/shopanda/internal/platform/ratelimit"
	goredis "github.com/redis/go-redis/v9"
)

// slidingWindowScript uses Redis TIME so every instance shares one clock.
// Members are supplied by the caller and must be unique per process.
// KEYS[1]=zset ARGV: window_ms, limit, member, ttl_ms. Returns 1 or 0.
// Requires Redis 5+ (effect replication is the default for scripts that
// call TIME and then write).
var slidingWindowScript = goredis.NewScript(`
local key = KEYS[1]
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local member = ARGV[3]
local ttl = tonumber(ARGV[4])
redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)
local count = redis.call('ZCARD', key)
if count >= limit then
  return 0
end
redis.call('ZADD', key, now, member)
redis.call('PEXPIRE', key, ttl)
return 1
`)

const (
	slidingWindowAllow   = 200 * time.Millisecond
	circuitMinRequests   = 20
	circuitLowErrors     = 5
	circuitBucketCount   = 20
	circuitBucket        = 100 * time.Millisecond
	limiterPoolCap       = 64
	rateLimitKeyInfix    = "-rl:"
	errReasonError       = "error"
	errReasonCircuit     = "circuit_open"
	errReasonPoolTimeout = "pool_timeout"
)

var (
	circuitHold = 5 * time.Second

	_ ratelimit.Factory = (*LimiterFactory)(nil)
	_ ratelimit.Allow   = (*SlidingWindowLimiter)(nil)
)

// DefaultLimiterPoolSize is min(10*GOMAXPROCS, 64), at least 8.
func DefaultLimiterPoolSize() int {
	return limiterPoolSize(runtime.GOMAXPROCS(0))
}

func limiterPoolSize(gomaxprocs int) int {
	n := 10 * gomaxprocs
	if n < 8 {
		return 8
	}
	if n > limiterPoolCap {
		return limiterPoolCap
	}
	return n
}

// RateLimitPrefix turns a key prefix into a sibling rate-limit prefix.
// "shopanda" / "shopanda:" → "shopanda-rl:". Empty becomes "shopanda-rl:".
func RateLimitPrefix(cachePrefix string) string {
	p := strings.TrimSuffix(NormalizeKeyPrefix(cachePrefix), ":")
	if p == "" {
		p = "shopanda"
	}
	return p + rateLimitKeyInfix
}

// rateBucket is one 100ms slot in the 2s error-rate window.
// Counters are atomic; the mutex is only taken when the slot rolls to a new period.
type rateBucket struct {
	mu   sync.Mutex
	gen  atomic.Uint64
	reqs atomic.Int64
	errs atomic.Int64
}

func (b *rateBucket) add(period uint64, isErr bool) {
	if b.gen.Load() != period {
		b.roll(period)
	}
	b.reqs.Add(1)
	if isErr {
		b.errs.Add(1)
	}
}

func (b *rateBucket) roll(period uint64) {
	b.mu.Lock()
	if b.gen.Load() != period {
		b.reqs.Store(0)
		b.errs.Store(0)
		b.gen.Store(period)
	}
	b.mu.Unlock()
}

func (b *rateBucket) snapshot(nowPeriod uint64) (reqs, errs int64) {
	// Three independent atomic loads: a rollover between them can mix
	// gen/reqs/errs from adjacent periods. Close enough for a breaker
	// threshold — do not add a mutex here; this is on the error path
	// and a one-sample skew does not change the trip decision.
	gen := b.gen.Load()
	if gen == 0 || nowPeriod < gen || nowPeriod-gen >= circuitBucketCount {
		return 0, 0
	}
	return b.reqs.Load(), b.errs.Load()
}

func (b *rateBucket) reset() {
	b.mu.Lock()
	b.gen.Store(0)
	b.reqs.Store(0)
	b.errs.Store(0)
	b.mu.Unlock()
}

// LimiterFactory builds Redis sliding-window limiters that share one client.
type LimiterFactory struct {
	client     *goredis.Client
	prefix     string
	log        Logger
	owns       bool
	instanceID string
	seq        atomic.Uint64
	lastErrLog atomic.Int64
	openUntil  atomic.Int64
	probing    atomic.Int32
	observeErr func(name, reason string)
	buckets    [circuitBucketCount]rateBucket

	localMu sync.Mutex
	locals  []*ratelimit.Limiter
}

// NewLimiterFactory uses client as-is. prefix is rewritten to the sibling
// rate-limit namespace. The caller owns client lifetime.
func NewLimiterFactory(client *goredis.Client, cachePrefix string, log Logger) *LimiterFactory {
	return &LimiterFactory{
		client:     client,
		prefix:     RateLimitPrefix(cachePrefix),
		log:        log,
		instanceID: newInstanceID(),
	}
}

// NewOwnedLimiterFactory is NewLimiterFactory; Close() closes client.
func NewOwnedLimiterFactory(client *goredis.Client, cachePrefix string, log Logger) *LimiterFactory {
	f := NewLimiterFactory(client, cachePrefix, log)
	f.owns = true
	return f
}

// SetErrorObserver records backend failures. reason is error, circuit_open, or pool_timeout.
func (f *LimiterFactory) SetErrorObserver(fn func(name, reason string)) {
	f.observeErr = fn
}

// OpenCircuit starts the factory in the open state so requests use the
// on_error fallback until a half-open probe succeeds. Used when Redis is
// unreachable at process start and on_error is not closed.
func (f *LimiterFactory) OpenCircuit() {
	f.openUntil.Store(time.Now().Add(circuitHold).UnixNano())
}

func newInstanceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// New returns a sliding-window limiter. Window is ceil(burst/rate*1000)
// milliseconds with capacity burst.
func (f *LimiterFactory) New(spec ratelimit.Spec) ratelimit.Allow {
	limit := spec.Burst
	if limit <= 0 {
		limit = 1
	}
	name := spec.Name
	if name == "" {
		name = "default"
	}
	window := ratelimit.Window(spec.Rate, spec.Burst)
	useLocal := ratelimit.UseLocalFallback(spec.OnError)
	var local *ratelimit.Limiter
	if useLocal {
		local = ratelimit.NewLimiter(spec.Rate, spec.Burst)
		f.localMu.Lock()
		f.locals = append(f.locals, local)
		f.localMu.Unlock()
	}
	return &SlidingWindowLimiter{
		factory:   f,
		name:      name,
		limit:     limit,
		window:    window,
		retry:     time.Duration(ratelimit.RetryAfterSeconds(window)) * time.Second,
		local:     local,
		failClose: ratelimit.FailClosed(spec.OnError),
		useLocal:  useLocal,
	}
}

// Client returns the Redis client this factory uses.
func (f *LimiterFactory) Client() *goredis.Client { return f.client }

// Close stops local fallback limiters and closes the Redis client when owned.
func (f *LimiterFactory) Close() error {
	f.localMu.Lock()
	locals := f.locals
	f.locals = nil
	f.localMu.Unlock()
	for _, l := range locals {
		l.Close()
	}
	if f.owns && f.client != nil {
		return f.client.Close()
	}
	return nil
}

// admitRedis reports whether this call should skip Redis. When the hold
// has expired, exactly one caller probes (isProbe=true).
func (f *LimiterFactory) admitRedis() (skip, isProbe bool) {
	until := f.openUntil.Load()
	if until == 0 {
		return false, false
	}
	if time.Now().UnixNano() < until {
		return true, false
	}
	if f.probing.CompareAndSwap(0, 1) {
		return false, true
	}
	return true, false
}

func (f *LimiterFactory) releaseProbe() {
	f.probing.Store(0)
}

func bucketPeriod(now time.Time) uint64 {
	return uint64(now.UnixNano() / int64(circuitBucket))
}

func (f *LimiterFactory) note(isErr bool) {
	period := bucketPeriod(time.Now())
	f.buckets[period%circuitBucketCount].add(period, isErr)
}

func (f *LimiterFactory) windowCounts(nowPeriod uint64) (reqs, errs int64) {
	for i := range f.buckets {
		r, e := f.buckets[i].snapshot(nowPeriod)
		reqs += r
		errs += e
	}
	return reqs, errs
}

func (f *LimiterFactory) resetWindow() {
	for i := range f.buckets {
		f.buckets[i].reset()
	}
}

func tripOnWindow(reqs, errs int64) bool {
	if reqs >= circuitMinRequests {
		return errs*2 >= reqs
	}
	// Quiet instances never fill 20 samples in 2s. Five errors and
	// nothing else in the window is a total outage, not a BGSAVE blip
	// (those have successes in the same window, so errs != reqs).
	return errs >= circuitLowErrors && errs == reqs
}

func (f *LimiterFactory) recordSuccess(isProbe bool) {
	if isProbe {
		f.openUntil.Store(0)
		f.resetWindow()
		return
	}
	f.note(false)
}

func (f *LimiterFactory) recordError(name string, err error, reason string, isProbe bool) {
	f.note(true)
	reqs, errs := f.windowCounts(bucketPeriod(time.Now()))
	if isProbe || tripOnWindow(reqs, errs) {
		f.openUntil.Store(time.Now().Add(circuitHold).UnixNano())
	}
	if reason == "" {
		reason = errReasonError
	}
	f.observe(name, reason)
	if f.log == nil {
		return
	}
	now := time.Now().UnixNano()
	last := f.lastErrLog.Load()
	if last != 0 && now-last < (10*time.Second).Nanoseconds() {
		return
	}
	if !f.lastErrLog.CompareAndSwap(last, now) {
		return
	}
	f.log.Error("ratelimit.redis.error", err, map[string]interface{}{
		"limiter": name,
		"reason":  reason,
	})
}

func (f *LimiterFactory) recordCircuitOpen(name string) {
	f.observe(name, errReasonCircuit)
}

func (f *LimiterFactory) observe(name, reason string) {
	if f.observeErr != nil {
		f.observeErr(name, reason)
	}
}

func backendReason(err error) string {
	if errors.Is(err, goredis.ErrPoolTimeout) {
		return errReasonPoolTimeout
	}
	return errReasonError
}

// SlidingWindowLimiter admits at most limit requests per rolling window for
// each key, using a Redis ZSET scored by Redis TIME.
type SlidingWindowLimiter struct {
	factory   *LimiterFactory
	name      string
	limit     int
	window    time.Duration
	retry     time.Duration
	local     *ratelimit.Limiter
	failClose bool
	useLocal  bool
}

func (l *SlidingWindowLimiter) fallback(ctx context.Context, key string) ratelimit.Decision {
	if l.useLocal && l.local != nil {
		d := l.local.Allow(ctx, key)
		d.BackendError = true
		d.LocalFallback = true
		return d
	}
	return ratelimit.Decision{
		Allowed:      !l.failClose,
		RetryAfter:   l.retry,
		BackendError: true,
	}
}

// Allow reports whether a request for key should be permitted.
func (l *SlidingWindowLimiter) Allow(ctx context.Context, key string) ratelimit.Decision {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		// Client is gone. Deny without recording so a RST_STREAM cannot
		// skip the limiter or drain a local fallback budget. Canceled
		// tells middleware not to log or write a 429.
		return ratelimit.Decision{RetryAfter: l.retry, Canceled: true}
	}
	skip, isProbe := l.factory.admitRedis()
	if skip {
		l.factory.recordCircuitOpen(l.name)
		return l.fallback(ctx, key)
	}
	if isProbe {
		defer l.factory.releaseProbe()
	}
	windowMs := l.window.Milliseconds()
	if windowMs < 1 {
		windowMs = 1000
	}
	ttlMs := windowMs + 1000
	member := fmt.Sprintf("%s-%d", l.factory.instanceID, l.factory.seq.Add(1))
	redisKey := l.factory.prefix + l.name + ":" + key

	// Detach from the request context so a client disconnect cannot abort
	// the Redis call or look like a backend failure.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), slidingWindowAllow)
	defer cancel()
	n, err := slidingWindowScript.Run(runCtx, l.factory.client, []string{redisKey},
		windowMs, l.limit, member, ttlMs).Int()
	if err != nil {
		l.factory.recordError(l.name, err, backendReason(err), isProbe)
		return l.fallback(ctx, key)
	}
	l.factory.recordSuccess(isProbe)
	return ratelimit.Decision{Allowed: n == 1, RetryAfter: l.retry}
}
