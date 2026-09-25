package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// checkTimeout bounds a single Allow call's Redis round trip explicitly,
// rather than relying solely on the go-redis client's own implicit
// default read/write timeouts (unlike ConnectURL's explicit 5s ping
// timeout) — those defaults can be overridden via the connection URL, at
// which point Allow's "fail open on error" guarantee would stop covering
// a hang (only explicit errors), leaving the request path blocked for as
// long as whatever timeout the URL configured, or indefinitely with none.
const checkTimeout = 250 * time.Millisecond

// slidingWindowScript admits a request iff fewer than ARGV[3] entries remain
// in the window after pruning anything older than ARGV[2] — a sliding
// window LOG (per-request timestamps in a ZSET), not a fixed-window
// counter, so a client can never get 2x the limit by timing requests
// around a window boundary the way a naive INCR+EXPIRE counter allows.
// One round trip, atomic: two concurrent Allow calls racing the same key
// always see a consistent ZCARD relative to each other.
//
// KEYS[1] = the per-(scope,client-key) ZSET.
// ARGV[1] = now, ARGV[2] = window start (now - window), both unix ms
//
//	scores — ms fits exactly in a float64 (2^53 ints) for centuries past
//	the epoch, unlike nanoseconds, which would silently lose precision.
//
// ARGV[3] = limit (burst). ARGV[4] = this request's unique member.
// ARGV[5] = window duration in ms, used as the key's own TTL so an
//
//	abandoned key (no further requests) doesn't outlive its own window's
//	worth of inactivity.
var slidingWindowScript = goredis.NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[2])
local count = redis.call('ZCARD', KEYS[1])
if count < tonumber(ARGV[3]) then
  redis.call('ZADD', KEYS[1], ARGV[1], ARGV[4])
  redis.call('PEXPIRE', KEYS[1], ARGV[5])
  return 1
end
redis.call('PEXPIRE', KEYS[1], ARGV[5])
return 0
`)

// RateLimiter is a Redis-backed sliding-window rate limiter (PR-1041):
// unlike internal/platform/ratelimit.Limiter (in-process, independent per
// replica), every replica sharing the same Redis backend enforces the
// SAME limit — the actual fix for the gap RUNBOOK.md's "Rate limiting and
// login lockout" section discloses: "limits are not shared across
// instances" no longer holds once rate_limit.driver=redis.
//
// Rate/Burst (the existing token-bucket config shape, unchanged so a
// deployment can switch drivers without redefining its limits) map onto
// this algorithm as: allow up to Burst requests in any Burst/Rate-second
// sliding window — the same peak burst size a token bucket of that
// capacity allows, and the same long-run average rate (Burst requests /
// (Burst/Rate) seconds = Rate requests/sec).
type RateLimiter struct {
	client *goredis.Client
	log    Logger
	prefix string // full key prefix: NormalizeKeyPrefix(keyPrefix) + "ratelimit:" + scope + ":"
	limit  int64
	window time.Duration
	// instanceID is a random value generated once per RateLimiter (i.e.
	// per process, per scope — see NewRateLimiter) so ZSET members are
	// unique across REPLICAS, not just within one process. seq alone is
	// not enough: every replica's own counter starts at 0, so two
	// replicas' Nth call to the same scope at the same millisecond would
	// otherwise produce the identical member string, and the second
	// ZADD would silently overwrite the first entry's score instead of
	// adding a new one — undercounting real admitted traffic and
	// defeating the whole point of a shared, cross-replica limit.
	instanceID string
	// seq disambiguates same-millisecond members within this instanceID;
	// see instanceID's own comment for why it alone is not sufficient.
	seq int64
	// now is a test-only clock override (see export_test.go); nil uses
	// time.Now.
	now func() time.Time
}

// NewRateLimiter returns a RateLimiter enforcing rate requests/sec with a
// burst capacity of burst, scoped under keyPrefix+"ratelimit:"+scope+":" —
// scope must be unique per configured limit (e.g. "default", or
// "route:"+pathPrefix for a per-route rule) so independent limits sharing
// one Redis backend never collide on the same client key. client must be
// non-nil and already connected (see ConnectURL); log may be nil (no-op).
func NewRateLimiter(client *goredis.Client, keyPrefix, scope string, rate float64, burst int, log Logger) (*RateLimiter, error) {
	if client == nil {
		return nil, fmt.Errorf("redis ratelimit: nil client")
	}
	if scope == "" {
		return nil, fmt.Errorf("redis ratelimit: empty scope")
	}
	if rate <= 0 || burst <= 0 {
		return nil, fmt.Errorf("redis ratelimit: rate and burst must be positive (got rate=%v burst=%d)", rate, burst)
	}
	window := time.Duration(float64(burst) / rate * float64(time.Second))
	if window <= 0 {
		window = time.Millisecond
	}
	instanceID, err := randomInstanceID()
	if err != nil {
		return nil, fmt.Errorf("redis ratelimit: generate instance id: %w", err)
	}
	return &RateLimiter{
		client:     client,
		log:        log,
		prefix:     NormalizeKeyPrefix(keyPrefix) + "ratelimit:" + scope + ":",
		limit:      int64(burst),
		window:     window,
		instanceID: instanceID,
	}, nil
}

// randomInstanceID returns 16 random hex characters (8 bytes,
// crypto/rand) — enough entropy that two RateLimiter instances (i.e. two
// replicas, or two independently-constructed limiters within one
// process) collide with negligible probability, unlike a counter that
// deterministically starts at the same value everywhere.
func randomInstanceID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Allow reports whether a request for key should be permitted. On any
// Redis error, Allow fails OPEN — logs a warning and returns true — the
// same "transient store errors must not take down the request path"
// reasoning auth.Service.reserveLockoutAttempt already applies to login
// lockout: a rate limiter that fails closed turns a Redis blip into a
// full outage, which is a strictly worse availability failure than the
// theoretical abuse window an open-fail leaves.
func (l *RateLimiter) Allow(key string) bool {
	nowFn := l.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	nowMs := now.UnixMilli()
	windowStartMs := now.Add(-l.window).UnixMilli()
	windowMs := l.window.Milliseconds()
	if windowMs <= 0 {
		windowMs = 1
	}
	seq := atomic.AddInt64(&l.seq, 1)
	member := fmt.Sprintf("%d-%s-%d", nowMs, l.instanceID, seq)

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	res, err := slidingWindowScript.Run(ctx, l.client,
		[]string{l.prefix + key},
		nowMs, windowStartMs, l.limit, member, windowMs,
	).Result()
	if err != nil {
		if l.log != nil {
			l.log.Error("redis.ratelimit.check_failed", err, map[string]interface{}{
				"key": key,
			})
		}
		return true
	}
	n, _ := res.(int64)
	return n == 1
}
