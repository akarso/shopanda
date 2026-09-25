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

// slidingWindowScript admits a request iff fewer than ARGV[2] entries
// remain in the window after pruning anything older than "now - ARGV[1]"
// — a sliding window LOG (per-request timestamps in a ZSET), not a
// fixed-window counter, so a client can never get 2x the limit by
// timing requests around a window boundary the way a naive INCR+EXPIRE
// counter allows. One round trip, atomic: two concurrent Allow calls
// racing the same key always see a consistent ZCARD relative to each
// other.
//
// Time comes from the Redis server itself (redis.call('TIME')), not
// from the calling process's own clock. Every replica calling this
// script — potentially with a different, skewed wall clock — must
// agree on ONE timeline for scoring and pruning; if each replica used
// its own local time instead, a replica whose clock runs ahead could
// prune another replica's still-valid entries before they've actually
// aged out from that replica's perspective, undercounting real admitted
// traffic and letting more through than the configured shared burst —
// exactly the failure mode a shared limiter exists to prevent. TIME
// inside a script is Redis's own documented idiom for this; scripts
// are replicated by effect (not by re-executing the script), so this
// doesn't introduce nondeterminism between primary and replicas either.
//
// KEYS[1] = the per-(scope,client-key) ZSET.
// ARGV[1] = window duration in ms — used both to compute the window
//
//	start (now - ARGV[1]) and as the key's own PEXPIRE TTL, so an
//	abandoned key (no further requests) doesn't outlive its own
//	window's worth of inactivity.
//
// ARGV[2] = limit (burst). ARGV[3] = this request's unique member.
var slidingWindowScript = goredis.NewScript(`
local now = redis.call('TIME')
local nowMs = math.floor(tonumber(now[1]) * 1000 + tonumber(now[2]) / 1000)
local windowStartMs = nowMs - tonumber(ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', windowStartMs)
local count = redis.call('ZCARD', KEYS[1])
if count < tonumber(ARGV[2]) then
  redis.call('ZADD', KEYS[1], nowMs, ARGV[3])
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  return 1
end
redis.call('PEXPIRE', KEYS[1], ARGV[1])
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
// (Burst/Rate) seconds = Rate requests/sec). One real behavioral
// difference switching drivers with unchanged config does NOT preserve:
// how soon a client can send one more request right after fully
// consuming a burst. A token bucket refills continuously — e.g. the
// default rate=10/burst=20 admits one more request ~100ms after
// exhausting the burst. This sliding-window LOG instead requires the
// OLDEST of the burst's entries to age out of the (here, 2-second)
// window before admitting a new one — for a true simultaneous burst,
// that's a ~2-second wait, not ~100ms, before the next admission. Both
// converge to the same steady-state average rate; only burst-recovery
// timing differs. See RUNBOOK.md's "Rate limiting and login lockout"
// section before switching an existing deployment's driver with config
// tuned around the memory driver's recovery behavior.
type RateLimiter struct {
	client   *goredis.Client
	log      Logger
	prefix   string // full key prefix: NormalizeKeyPrefix(keyPrefix) + "ratelimit:" + scope + ":"
	limit    int64
	windowMs int64
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
}

// NewRateLimiter returns a RateLimiter enforcing rate requests/sec with a
// burst capacity of burst, scoped under keyPrefix+"ratelimit:"+scope+":" —
// scope must be unique per configured limit (e.g. "default", or
// "route:"+pathPrefix for a per-route rule) so independent limits sharing
// one Redis backend never collide on the same client key. client must be
// non-nil (see ConnectURL/NewLazyClient); log may be nil (no-op).
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
	// windowMs, not window itself, is what the script actually uses (it
	// only understands millisecond scores/TTLs) — clamp THAT to at least
	// 1, not window to at least 1ns. A window under 1ms (e.g. rate=2000,
	// burst=1: 0.5ms) is already positive in nanoseconds, so the old
	// "window <= 0" check never caught it, but Duration.Milliseconds()
	// truncates it to 0 regardless. Passed to the script, ARGV[1]=0
	// makes windowStartMs == nowMs, so ZREMRANGEBYSCORE prunes every
	// entry (including ones just added this millisecond), ZCARD always
	// reads back 0, every check is admitted, and PEXPIRE with a 0 TTL
	// deletes the key immediately — the limiter silently stops enforcing
	// anything for that rate/burst combination instead of erroring or
	// degrading gracefully.
	windowMs := window.Milliseconds()
	if windowMs < 1 {
		windowMs = 1
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
		windowMs:   windowMs,
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
	seq := atomic.AddInt64(&l.seq, 1)
	member := fmt.Sprintf("%s-%d", l.instanceID, seq)

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	res, err := slidingWindowScript.Run(ctx, l.client,
		[]string{l.prefix + key},
		l.windowMs, l.limit, member,
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
