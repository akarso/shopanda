package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	goredis "github.com/redis/go-redis/v9"
)

// errJobNotFound is returned (wrapped) when the job hash key is missing.
var errJobNotFound = errors.New("job not found")

// errSaveRefused means saveJobMetaLua saw a state it must not overwrite
// (a reclaim or outcome took the id first).
var errSaveRefused = errors.New("job_queue: save refused by meta state")

var _ jobs.Queue = (*JobQueue)(nil)

const (
	queueBackoffBase        = 5 * time.Second
	queueBackoffMax         = 5 * time.Minute
	queueJitterRatio        = 0.25
	terminalJobRetentionTTL = 7 * 24 * time.Hour
)

// redisNowMsLua is the shared Redis-clock helper prepended to scripts that
// stamp or age claims, so claim/begin/repair never mix host clocks.
const redisNowMsLua = `
local function redis_now_ms()
  local t = redis.call('TIME')
  return tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
end
`

const redisNowMsEval = redisNowMsLua + `return redis_now_ms()`

// promoteDelayedLua moves due delayed members onto ready using Redis TIME
// so a skewed host clock cannot promote early.
// KEYS: delayed, ready.
const promoteDelayedLua = redisNowMsLua + `
local delayed = KEYS[1]
local ready = KEYS[2]
local now = redis_now_ms()
local ids = redis.call('ZRANGEBYSCORE', delayed, '-inf', now, 'LIMIT', 0, 100)
for _, id in ipairs(ids) do
  redis.call('ZREM', delayed, id)
  -- Drop any ready copies left beside the delayed member, then push once.
  redis.call('LREM', ready, 0, id)
  redis.call('LPUSH', ready, id)
end
return #ids
`

// claimJobLua pops ready and claims only pending (or meta-less) ids. Any
// other meta (processing duplicate, reclaiming, terminal) is popped and
// dropped without touching meta or membership: commit/abort/repair own
// scheduling of those ids. updated_at_ms/claimed_at_ms come from Redis TIME
// after the pop.
// KEYS: ready, processing_list, processing_set, delayed. ARGV: meta_key_prefix.
// Returns {id, attempts, meta_status, claimed_at_ms} or false.
const claimJobLua = redisNowMsLua + `
local id = redis.call('RPOP', KEYS[1])
if not id then return false end
local meta = ARGV[1] .. id
local status = redis.call('HGET', meta, 'status')
local attempts = tonumber(redis.call('HGET', meta, 'attempts') or '0')
if status and status ~= 'pending' then
  return {id, tostring(attempts), status, '0'}
end
local now = redis_now_ms()
redis.call('LPUSH', KEYS[2], id)
redis.call('SADD', KEYS[3], id)
redis.call('ZREM', KEYS[4], id)
attempts = attempts + 1
redis.call('HSET', meta,
  'status', 'processing',
  'attempts', attempts,
  'updated_at_ms', now,
  'claimed_at_ms', now)
redis.call('HDEL', meta, 'reclaiming_at_ms')
return {id, tostring(attempts), 'processing', tostring(now)}
`

// releaseClaimLua undoes this Dequeue's claim (meta processing, same
// attempt and claimed_at_ms, on set). Any other state means a reclaim or
// outcome took the id — leave it alone. When pushing ready, a delayed
// member wins: strip ready copies and do not LPUSH (preserves backoff).
// KEYS: ready, processing_list, processing_set, meta, delayed.
// ARGV: id, attempt, claimed_at_ms, restore_status, restore_attempts, push_ready.
const releaseClaimLua = `
local meta = KEYS[4]
local status = redis.call('HGET', meta, 'status')
local attempts = redis.call('HGET', meta, 'attempts')
local claimed = redis.call('HGET', meta, 'claimed_at_ms')
if status ~= 'processing' or tonumber(attempts) ~= tonumber(ARGV[2]) or claimed ~= ARGV[3] then
  return 0
end
redis.call('SREM', KEYS[3], ARGV[1])
redis.call('LREM', KEYS[2], 0, ARGV[1])
redis.call('HSET', meta, 'status', ARGV[4], 'attempts', ARGV[5])
redis.call('HDEL', meta, 'claimed_at_ms')
if ARGV[6] == '1' then
  if redis.call('ZSCORE', KEYS[5], ARGV[1]) then
    redis.call('LREM', KEYS[1], 0, ARGV[1])
  else
    redis.call('LPUSH', KEYS[1], ARGV[1])
  end
end
return 1
`

// reclaimAgeLua reports how long a reclaim has held meta, on the Redis
// clock. A missing reclaiming_at_ms falls back to updated_at_ms so a
// half-written row still ages out of the lease.
// KEYS: meta. Returns {status, age_ms}.
const reclaimAgeLua = redisNowMsLua + `
local status = redis.call('HGET', KEYS[1], 'status') or ''
local started = tonumber(redis.call('HGET', KEYS[1], 'reclaiming_at_ms'))
if not started then started = tonumber(redis.call('HGET', KEYS[1], 'updated_at_ms')) or 0 end
return {status, tostring(redis_now_ms() - started)}
`

// orphanCleanupLua repairs divergent membership. Never touches a live
// processing claim. A reclaiming row is repaired only after the lease
// (min_age): blob+meta → pending, off processing, LPUSH ready only when
// the id is on neither ready nor delayed. pending+membership is returned
// to ready only when the claim stamp is older than min-age.
// KEYS: processing_list, processing_set, meta, ready, job, delayed.
// ARGV: id, min_age_ms, pending_blob (reclaiming repair; '' skips it).
const orphanCleanupLua = redisNowMsLua + `
local meta = KEYS[3]
local status = redis.call('HGET', meta, 'status')
local id = ARGV[1]
local now = redis_now_ms()
local min_age = tonumber(ARGV[2])
-- Redis 5 has no LPOS. Delayed wins: strip every ready copy and do not
-- LPUSH. Otherwise collapse ready then push one.
local function schedule_once()
  if redis.call('ZSCORE', KEYS[6], id) then
    redis.call('LREM', KEYS[4], 0, id)
    return
  end
  redis.call('LREM', KEYS[4], 0, id)
  redis.call('LPUSH', KEYS[4], id)
end
if status == 'processing' then
  return 0
end
if status == 'reclaiming' then
  if not ARGV[3] or ARGV[3] == '' then
    return 0
  end
  local started = tonumber(redis.call('HGET', meta, 'reclaiming_at_ms'))
  if not started then started = tonumber(redis.call('HGET', meta, 'updated_at_ms')) or 0 end
  if (now - started) < min_age then
    return 0
  end
  local attempts = redis.call('HGET', meta, 'attempts') or '0'
  redis.call('SET', KEYS[5], ARGV[3])
  redis.call('HSET', meta, 'status', 'pending', 'attempts', attempts, 'updated_at_ms', now)
  redis.call('HDEL', meta, 'reclaiming_at_ms')
  redis.call('HDEL', meta, 'claimed_at_ms')
  redis.call('SREM', KEYS[2], id)
  redis.call('LREM', KEYS[1], 0, id)
  schedule_once()
  return 3
end
if status == 'pending' then
  local on_set = redis.call('SISMEMBER', KEYS[2], id) == 1
  if not on_set then
    return 0
  end
  local stamp = tonumber(redis.call('HGET', meta, 'claimed_at_ms'))
  if not stamp then stamp = tonumber(redis.call('HGET', meta, 'updated_at_ms')) or 0 end
  if (now - stamp) < min_age then
    return 0
  end
  redis.call('SREM', KEYS[2], id)
  redis.call('LREM', KEYS[1], 0, id)
  schedule_once()
  return 2
end
redis.call('SREM', KEYS[2], id)
redis.call('LREM', KEYS[1], 0, id)
return 1
`

// completeJobLua CAS-checks claim meta + SET membership, then SETs a
// Go-marshaled blob. Drops delayed and every ready copy, and applies the
// same TTL to meta as the blob so Enqueue can reuse the id after expiry.
// KEYS: job, processing_list, processing_set, meta, delayed, ready.
// ARGV: attempt, id, blob, ttl_ms.
const completeJobLua = redisNowMsLua + `
local meta = KEYS[4]
local status = redis.call('HGET', meta, 'status')
local attempts = redis.call('HGET', meta, 'attempts')
local on_set = redis.call('SISMEMBER', KEYS[3], ARGV[2]) == 1
local now = redis_now_ms()
local function drop_orphan()
  redis.call('SREM', KEYS[3], ARGV[2])
  redis.call('LREM', KEYS[2], 0, ARGV[2])
end
if not status then
  drop_orphan()
  return -1
end
if status == 'reclaiming' then
  return -3
end
if status == 'pending' then
  -- Ready/delayed or live claim gap — never strip membership.
  return 0
end
if status == 'processing' and tonumber(attempts) ~= tonumber(ARGV[1]) then
  return 0
end
if status ~= 'processing' then
  drop_orphan()
  return 0
end
if not on_set then
  redis.call('LREM', KEYS[2], 0, ARGV[2])
  return -2
end
local ttl = tonumber(ARGV[4])
if ttl > 0 then
  redis.call('SET', KEYS[1], ARGV[3], 'PX', ttl)
  redis.call('HSET', meta, 'status', 'done', 'attempts', ARGV[1], 'updated_at_ms', now)
  redis.call('PEXPIRE', meta, ttl)
else
  redis.call('SET', KEYS[1], ARGV[3])
  redis.call('HSET', meta, 'status', 'done', 'attempts', ARGV[1], 'updated_at_ms', now)
end
redis.call('HDEL', meta, 'reclaiming_at_ms')
redis.call('HDEL', meta, 'claimed_at_ms')
redis.call('ZREM', KEYS[5], ARGV[2])
redis.call('LREM', KEYS[6], 0, ARGV[2])
redis.call('SREM', KEYS[3], ARGV[2])
redis.call('LREM', KEYS[2], 1, ARGV[2])
return 1
`

// failJobLua — same membership rules as complete. Delayed retries strip
// every ready copy in the same script; terminal fails drop delayed and
// ready, and expire meta with the blob TTL.
// KEYS: job, processing_list, processing_set, delayed, meta, ready.
// ARGV: attempt, id, blob, terminal, run_at_ms, ttl_ms.
const failJobLua = redisNowMsLua + `
local meta = KEYS[5]
local now = redis_now_ms()
local status = redis.call('HGET', meta, 'status')
local attempts = redis.call('HGET', meta, 'attempts')
local on_set = redis.call('SISMEMBER', KEYS[3], ARGV[2]) == 1
local function drop_orphan()
  redis.call('SREM', KEYS[3], ARGV[2])
  redis.call('LREM', KEYS[2], 0, ARGV[2])
end
if not status then
  drop_orphan()
  return -1
end
if status == 'reclaiming' then
  return -3
end
if status == 'pending' then
  return 0
end
if status == 'processing' and tonumber(attempts) ~= tonumber(ARGV[1]) then
  return 0
end
if status ~= 'processing' then
  drop_orphan()
  return 0
end
if not on_set then
  redis.call('LREM', KEYS[2], 0, ARGV[2])
  return -2
end
local ttl = tonumber(ARGV[6])
-- ARGV[5] is an absolute run_at_ms computed from Redis TIME in Go.
if tonumber(ARGV[4]) == 1 then
  if ttl > 0 then
    redis.call('SET', KEYS[1], ARGV[3], 'PX', ttl)
    redis.call('HSET', meta, 'status', 'failed', 'attempts', ARGV[1], 'updated_at_ms', now)
    redis.call('PEXPIRE', meta, ttl)
  else
    redis.call('SET', KEYS[1], ARGV[3])
    redis.call('HSET', meta, 'status', 'failed', 'attempts', ARGV[1], 'updated_at_ms', now)
  end
  redis.call('ZREM', KEYS[4], ARGV[2])
  redis.call('LREM', KEYS[6], 0, ARGV[2])
else
  redis.call('SET', KEYS[1], ARGV[3])
  redis.call('LREM', KEYS[6], 0, ARGV[2])
  redis.call('ZADD', KEYS[4], tonumber(ARGV[5]), ARGV[2])
  redis.call('HSET', meta, 'status', 'pending', 'attempts', ARGV[1], 'updated_at_ms', now)
end
redis.call('HDEL', meta, 'reclaiming_at_ms')
redis.call('HDEL', meta, 'claimed_at_ms')
redis.call('SREM', KEYS[3], ARGV[2])
redis.call('LREM', KEYS[2], 1, ARGV[2])
return 1
`

// beginRequeueLua CAS-moves processing → reclaiming under one script.
// Keeps original updated_at_ms (claim age); sets reclaiming_at_ms for
// stale-reclaim recovery. Id leaves processing set/list; not on ready.
// KEYS: job, processing_list, processing_set, meta.
// ARGV: attempt, id, min_age_ms, reclaiming_blob.
// Returns: 1 ok, -1 missing, -2 membership, -3 not processing, -4 wrong
// attempt, -5 too fresh.
const beginRequeueLua = redisNowMsLua + `
local now = redis_now_ms()
local meta = KEYS[4]
local status = redis.call('HGET', meta, 'status')
local attempts = redis.call('HGET', meta, 'attempts')
local updated = tonumber(redis.call('HGET', meta, 'updated_at_ms'))
local on_set = redis.call('SISMEMBER', KEYS[3], ARGV[2]) == 1
if not status then
  redis.call('SREM', KEYS[3], ARGV[2])
  redis.call('LREM', KEYS[2], 0, ARGV[2])
  return -1
end
if status ~= 'processing' then
  return -3
end
if tonumber(attempts) ~= tonumber(ARGV[1]) then
  return -4
end
if not updated or (now - updated) < tonumber(ARGV[3]) then
  return -5
end
if not on_set then
  redis.call('LREM', KEYS[2], 0, ARGV[2])
  return -2
end
redis.call('SET', KEYS[1], ARGV[4])
redis.call('HSET', meta, 'status', 'reclaiming', 'attempts', ARGV[1], 'reclaiming_at_ms', now)
redis.call('SREM', KEYS[3], ARGV[2])
redis.call('LREM', KEYS[2], 0, ARGV[2])
return 1
`

// commitRequeueLua: reclaiming → pending and schedule once on ready (unless
// already delayed). KEYS: job, ready, meta, delayed. ARGV: attempt, id, pending_blob.
const commitRequeueLua = redisNowMsLua + `
local meta = KEYS[3]
local status = redis.call('HGET', meta, 'status')
local attempts = redis.call('HGET', meta, 'attempts')
if status ~= 'reclaiming' or tonumber(attempts) ~= tonumber(ARGV[1]) then
  return 0
end
local now = redis_now_ms()
redis.call('SET', KEYS[1], ARGV[3])
redis.call('HSET', meta, 'status', 'pending', 'attempts', ARGV[1], 'updated_at_ms', now)
redis.call('HDEL', meta, 'reclaiming_at_ms')
-- Fresh reclaim is off ready; LPUSH the only copy without scanning.
-- If a delayed member already exists, strip ready copies instead.
if redis.call('ZSCORE', KEYS[4], ARGV[2]) then
  redis.call('LREM', KEYS[2], 0, ARGV[2])
else
  redis.call('LPUSH', KEYS[2], ARGV[2])
end
return 1
`

// ensurePendingScheduledLua puts a pending id on ready exactly once when it
// is on neither ready nor delayed (crashed commit / repair race).
// KEYS: ready, delayed, meta. ARGV: id. Returns 1 scheduled/present, 0 not pending.
const ensurePendingScheduledLua = `
local status = redis.call('HGET', KEYS[3], 'status')
if status ~= 'pending' then
  return 0
end
-- Delayed wins: clear ready duplicates; do not LPUSH.
if redis.call('ZSCORE', KEYS[2], ARGV[1]) then
  redis.call('LREM', KEYS[1], 0, ARGV[1])
  return 1
end
-- Recovery path may already have copies — collapse then push one.
redis.call('LREM', KEYS[1], 0, ARGV[1])
redis.call('LPUSH', KEYS[1], ARGV[1])
return 1
`

// enqueueJobLua writes blob+meta and schedules once. Rejects when meta
// already exists (second Enqueue of the same id). A new id is not on
// ready/delayed — no LREM/ZREM on the hot path.
// KEYS: job, meta, ready, delayed.
// ARGV: blob, status, attempts, updated_at_ms, ttl_ms, id, run_at_ms.
// Returns 1 ok, 0 already exists.
const enqueueJobLua = redisNowMsLua + `
local meta = KEYS[2]
if redis.call('HGET', meta, 'status') then
  return 0
end
local now = redis_now_ms()
local id = ARGV[6]
local run_at = tonumber(ARGV[7])
local ttl = tonumber(ARGV[5])
redis.call('SET', KEYS[1], ARGV[1])
redis.call('HSET', meta, 'status', ARGV[2], 'attempts', ARGV[3], 'updated_at_ms', now)
if ttl > 0 then
  redis.call('PEXPIRE', KEYS[1], ttl)
  redis.call('PEXPIRE', meta, ttl)
end
if run_at > now then
  redis.call('ZADD', KEYS[4], run_at, id)
else
  redis.call('LPUSH', KEYS[3], id)
end
return 1
`

// abortRequeueLua: reclaiming → processing + restore set/list. Restores
// the prior updated_at_ms so a failed hook can be retried without waiting
// another full min-age. SADD is idempotent; LPUSH only when the id was
// not already on the set (avoids duplicate processing-list entries).
// KEYS: job, processing_list, processing_set, meta.
// ARGV: attempt, id, processing_blob, original_updated_at_ms.
const abortRequeueLua = `
local meta = KEYS[4]
local status = redis.call('HGET', meta, 'status')
local attempts = redis.call('HGET', meta, 'attempts')
if status ~= 'reclaiming' or tonumber(attempts) ~= tonumber(ARGV[1]) then
  return 0
end
redis.call('SET', KEYS[1], ARGV[3])
redis.call('HSET', meta, 'status', 'processing', 'attempts', ARGV[1], 'updated_at_ms', ARGV[4])
redis.call('HDEL', meta, 'reclaiming_at_ms')
local added = redis.call('SADD', KEYS[3], ARGV[2])
if added == 1 then
  redis.call('LPUSH', KEYS[2], ARGV[2])
end
return 1
`

// saveJobMetaLua writes the blob and meta only when compatible with the
// live state. Nothing overwrites reclaiming (only commit/abort/repair leave
// it). A processing write must match the claim claimJobLua just stamped
// (meta processing, same attempt); a pending write never lands on a claim.
// KEYS: job, meta. ARGV: blob, status, attempts, updated_at_ms, ttl_ms.
// Returns 1 ok, 0 refused.
const saveJobMetaLua = `
local meta = KEYS[2]
local want = ARGV[2]
local cur = redis.call('HGET', meta, 'status')
if cur == 'reclaiming' then return 0 end
if want == 'processing' then
  if cur ~= 'processing' then return 0 end
  if tonumber(redis.call('HGET', meta, 'attempts')) ~= tonumber(ARGV[3]) then return 0 end
elseif want == 'pending' then
  if cur == 'processing' then return 0 end
end
redis.call('SET', KEYS[1], ARGV[1])
redis.call('HSET', meta, 'status', want, 'attempts', ARGV[3], 'updated_at_ms', ARGV[4])
local ttl = tonumber(ARGV[5])
if ttl > 0 then
  redis.call('PEXPIRE', KEYS[1], ttl)
  redis.call('PEXPIRE', meta, ttl)
end
return 1
`

// ErrProcessingMembership means the job hash/meta looks like a live claim
// but the id is not on the processing set (divergent state — alert, not
// a quiet not_applied).
var ErrProcessingMembership = errors.New("jobs: redis processing membership missing")

// JobQueue implements jobs.Queue using Redis lists and a delayed sorted set.
type JobQueue struct {
	client *goredis.Client
	prefix string
	// dequeueStage, when set, is called at Dequeue checkpoints
	// ("claimed", "loaded") so tests can interleave a reclaim.
	dequeueStage func(stage, id string, attempt int)
}

// QueueConfig holds Redis job queue connection settings.
type QueueConfig struct {
	URL       string
	KeyPrefix string
	// Context is used for the connectivity PING. Nil means context.Background().
	Context context.Context
}

// NewJobQueue creates a JobQueue and verifies the Redis connection with PING.
func NewJobQueue(cfg QueueConfig) (*JobQueue, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("NewJobQueue: empty url")
	}
	client, err := ConnectURLContext(cfg.Context, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("NewJobQueue: %w", err)
	}
	prefix := NormalizeKeyPrefix(cfg.KeyPrefix)
	if prefix == "" {
		prefix = "shopanda:queue:"
	}
	return &JobQueue{client: client, prefix: prefix}, nil
}

type storedJob struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Payload    map[string]interface{} `json:"payload"`
	Status     jobs.Status            `json:"status"`
	Attempts   int                    `json:"attempts"`
	MaxRetries int                    `json:"max_retries"`
	RunAt      time.Time              `json:"run_at"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
	LastError  string                 `json:"last_error,omitempty"`
}

func toStoredJob(job jobs.Job) storedJob {
	payload := job.Payload
	if payload == nil {
		payload = map[string]interface{}{}
	}
	return storedJob{
		ID:         job.ID,
		Type:       job.Type,
		Payload:    payload,
		Status:     job.Status,
		Attempts:   job.Attempts,
		MaxRetries: job.MaxRetries,
		RunAt:      job.RunAt,
		CreatedAt:  job.CreatedAt,
		UpdatedAt:  job.UpdatedAt,
	}
}

func (s storedJob) toJob() jobs.Job {
	payload := s.Payload
	if payload == nil {
		payload = map[string]interface{}{}
	}
	return jobs.Job{
		ID:         s.ID,
		Type:       s.Type,
		Payload:    payload,
		Status:     s.Status,
		Attempts:   s.Attempts,
		MaxRetries: s.MaxRetries,
		RunAt:      s.RunAt,
		CreatedAt:  s.CreatedAt,
		UpdatedAt:  s.UpdatedAt,
	}
}

func (q *JobQueue) jobKey(id string) string       { return q.prefix + "job:" + id }
func (q *JobQueue) jobMetaKey(id string) string   { return q.prefix + "jobmeta:" + id }
func (q *JobQueue) jobMetaPrefix() string         { return q.prefix + "jobmeta:" }
func (q *JobQueue) readyKey() string              { return q.prefix + "ready" }
func (q *JobQueue) processingKey() string         { return q.prefix + "processing" }
func (q *JobQueue) processingSetKey() string      { return q.prefix + "processing_set" }
func (q *JobQueue) delayedKey() string            { return q.prefix + "delayed" }

type claimMeta struct {
	Status   jobs.Status
	Attempts int
}

func (q *JobQueue) loadClaimMeta(ctx context.Context, id string) (claimMeta, error) {
	vals, err := q.client.HMGet(ctx, q.jobMetaKey(id), "status", "attempts").Result()
	if err != nil {
		return claimMeta{}, fmt.Errorf("job_queue: meta %q: %w", id, err)
	}
	var m claimMeta
	if vals[0] != nil {
		m.Status = jobs.Status(fmt.Sprint(vals[0]))
	}
	if vals[1] != nil {
		fmt.Sscanf(fmt.Sprint(vals[1]), "%d", &m.Attempts)
	}
	return m, nil
}

func jobRetentionTTL(status jobs.Status) time.Duration {
	switch status {
	case jobs.StatusDone, jobs.StatusFailed:
		return terminalJobRetentionTTL
	default:
		return 0
	}
}

func (q *JobQueue) saveJob(ctx context.Context, job storedJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("job_queue: marshal job %q: %w", job.ID, err)
	}
	ttlMs := int64(jobRetentionTTL(job.Status) / time.Millisecond)
	n, err := q.client.Eval(ctx, saveJobMetaLua,
		[]string{q.jobKey(job.ID), q.jobMetaKey(job.ID)},
		data, string(job.Status), job.Attempts, job.UpdatedAt.UTC().UnixMilli(), ttlMs,
	).Int()
	if err != nil {
		return fmt.Errorf("job_queue: save job %q: %w", job.ID, err)
	}
	if n != 1 {
		return fmt.Errorf("job_queue: save job %q: %w", job.ID, errSaveRefused)
	}
	return nil
}

func (q *JobQueue) redisNowMs(ctx context.Context) (int64, error) {
	n, err := q.client.Eval(ctx, redisNowMsEval, nil).Int64()
	if err != nil {
		return 0, fmt.Errorf("job_queue: redis TIME: %w", err)
	}
	return n, nil
}

func (q *JobQueue) cleanupOrphans(ctx context.Context, id string, minAge time.Duration, pendingBlob string) error {
	if minAge <= 0 {
		minAge = jobs.DefaultRequeueMinAge
	}
	if _, err := q.client.Eval(ctx, orphanCleanupLua,
		[]string{q.processingKey(), q.processingSetKey(), q.jobMetaKey(id), q.readyKey(), q.jobKey(id), q.delayedKey()},
		id, minAge.Milliseconds(), pendingBlob,
	).Result(); err != nil {
		return fmt.Errorf("job_queue: orphan cleanup %s: %w", id, err)
	}
	return nil
}

// repairStaleReclaim turns a reclaim older than the DefaultRequeueMinAge
// lease into pending+ready (once). Younger reclaims are left to their
// holder's commit/abort.
func (q *JobQueue) repairStaleReclaim(ctx context.Context, id string) error {
	stored, err := q.loadJob(ctx, id)
	if err != nil {
		return err
	}
	nowMs, err := q.redisNowMs(ctx)
	if err != nil {
		return err
	}
	now := time.UnixMilli(nowMs).UTC()
	pending := stored
	pending.Status = jobs.StatusPending
	pending.RunAt = now
	pending.UpdatedAt = now
	blob, err := json.Marshal(pending)
	if err != nil {
		return fmt.Errorf("job_queue: repair marshal %s: %w", id, err)
	}
	return q.cleanupOrphans(ctx, id, jobs.DefaultRequeueMinAge, string(blob))
}

// ensurePendingScheduled LPUSHes ready when meta is pending and the id is
// on neither ready nor delayed (crashed commit after the meta flip).
func (q *JobQueue) ensurePendingScheduled(ctx context.Context, id string) error {
	n, err := q.client.Eval(ctx, ensurePendingScheduledLua,
		[]string{q.readyKey(), q.delayedKey(), q.jobMetaKey(id)},
		id,
	).Int()
	if err != nil {
		return fmt.Errorf("job_queue: ensure pending schedule %s: %w", id, err)
	}
	if n != 1 {
		return fmt.Errorf("job_queue: ensure pending schedule %s: status left pending", id)
	}
	return nil
}

// durableCtx ignores caller cancel so abort/commit after begin cannot be
// skipped by a canceled reclaim request.
func durableCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
}

// abortReclaim retries reclaiming → processing until Lua returns 1 or the
// status is no longer reclaiming. Preserves originalUpdatedAtMs. Uses a
// fresh durable window per attempt so a single 10s timeout cannot leave
// reclaiming stuck after cancel.
func (q *JobQueue) abortReclaim(ctx context.Context, id string, attempt int, procBlob []byte, originalUpdatedAtMs int64) error {
	var last error
	for i := 0; i < 8; i++ {
		dctx, cancel := durableCtx(ctx)
		n, err := q.client.Eval(dctx, abortRequeueLua,
			[]string{q.jobKey(id), q.processingKey(), q.processingSetKey(), q.jobMetaKey(id)},
			attempt, id, procBlob, originalUpdatedAtMs,
		).Int()
		cancel()
		if err != nil {
			last = err
			continue
		}
		if n == 1 {
			return nil
		}
		st, herr := q.client.HGet(context.WithoutCancel(ctx), q.jobMetaKey(id), "status").Result()
		if herr != nil && herr != goredis.Nil {
			last = herr
			continue
		}
		if st != string(jobs.StatusReclaiming) {
			return nil
		}
		last = fmt.Errorf("abort returned %d", n)
	}
	if last == nil {
		last = errors.New("abort exhausted")
	}
	return fmt.Errorf("job_queue: requeue abort %s: %w", id, last)
}

// commitReclaim retries reclaiming → pending+ready until success or status
// leaves reclaiming (then abort restores processing). Meta pending alone is
// not enough — the id must also be on ready or delayed.
func (q *JobQueue) commitReclaim(ctx context.Context, id string, attempt int, pendingBlob []byte, procBlob []byte, originalUpdatedAtMs int64) error {
	var last error
	for i := 0; i < 8; i++ {
		dctx, cancel := durableCtx(ctx)
		n, err := q.client.Eval(dctx, commitRequeueLua,
			[]string{q.jobKey(id), q.readyKey(), q.jobMetaKey(id), q.delayedKey()},
			attempt, id, pendingBlob,
		).Int()
		cancel()
		if err != nil {
			last = err
			continue
		}
		if n == 1 {
			return nil
		}
		st, herr := q.client.HGet(context.WithoutCancel(ctx), q.jobMetaKey(id), "status").Result()
		if herr != nil && herr != goredis.Nil {
			last = herr
			continue
		}
		if st == string(jobs.StatusPending) {
			if err := q.ensurePendingScheduled(context.WithoutCancel(ctx), id); err != nil {
				last = err
				continue
			}
			return nil
		}
		if st == string(jobs.StatusReclaiming) {
			last = fmt.Errorf("commit returned %d", n)
			continue
		}
		// Left reclaiming without becoming pending (aborted elsewhere).
		last = fmt.Errorf("status %q after commit miss", st)
		break
	}
	if abortErr := q.abortReclaim(ctx, id, attempt, procBlob, originalUpdatedAtMs); abortErr != nil {
		return fmt.Errorf("job_queue: requeue commit failed: %v (also abort: %v)", last, abortErr)
	}
	if last == nil {
		last = errors.New("commit exhausted")
	}
	return fmt.Errorf("job_queue: requeue commit %s: %w", id, last)
}

// releaseClaim undoes this Dequeue's own claim (CAS on attempt and
// claimed_at_ms). When a reclaim or outcome took the id first it is a
// no-op, so ready is never pushed behind beginRequeueLua's back.
func (q *JobQueue) releaseClaim(ctx context.Context, id string, attempt int, claimedAtMs string, restore jobs.Status, restoreAttempts int, pushReady bool) error {
	push := "0"
	if pushReady {
		push = "1"
	}
	if err := q.client.Eval(ctx, releaseClaimLua,
		[]string{q.readyKey(), q.processingKey(), q.processingSetKey(), q.jobMetaKey(id), q.delayedKey()},
		id, attempt, claimedAtMs, string(restore), restoreAttempts, push,
	).Err(); err != nil {
		return fmt.Errorf("job_queue: release claim %q: %w", id, err)
	}
	return nil
}

func (q *JobQueue) loadJob(ctx context.Context, id string) (storedJob, error) {
	raw, err := q.client.Get(ctx, q.jobKey(id)).Bytes()
	if err == goredis.Nil {
		return storedJob{}, fmt.Errorf("job_queue: job %s: %w", id, errJobNotFound)
	}
	if err != nil {
		return storedJob{}, fmt.Errorf("job_queue: load job %q: %w", id, err)
	}
	var job storedJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return storedJob{}, fmt.Errorf("job_queue: unmarshal job %q: %w", id, err)
	}
	return job, nil
}

func (q *JobQueue) promoteDelayed(ctx context.Context) error {
	for {
		n, err := q.client.Eval(ctx, promoteDelayedLua, []string{q.delayedKey(), q.readyKey()}).Int()
		if err != nil {
			return fmt.Errorf("job_queue: promote delayed: %w", err)
		}
		if n == 0 {
			return nil
		}
	}
}

// Enqueue inserts a new job into the queue. A second Enqueue of the same
// id is rejected; ready and delayed membership are cleared before the new
// schedule is written so the id cannot sit in both places.
func (q *JobQueue) Enqueue(ctx context.Context, job jobs.Job) error {
	stored := toStoredJob(job)
	if stored.Status == "" {
		stored.Status = jobs.StatusPending
	}
	if stored.MaxRetries == 0 {
		stored.MaxRetries = jobs.DefaultMaxRetries
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("job_queue: marshal job %q: %w", stored.ID, err)
	}
	ttlMs := int64(jobRetentionTTL(stored.Status) / time.Millisecond)
	runAtMs := stored.RunAt.UTC().UnixMilli()
	n, err := q.client.Eval(ctx, enqueueJobLua,
		[]string{q.jobKey(stored.ID), q.jobMetaKey(stored.ID), q.readyKey(), q.delayedKey()},
		data, string(stored.Status), stored.Attempts, stored.UpdatedAt.UTC().UnixMilli(), ttlMs, stored.ID, runAtMs,
	).Int()
	if err != nil {
		return fmt.Errorf("job_queue: enqueue %q: %w", stored.ID, err)
	}
	if n != 1 {
		return fmt.Errorf("job_queue: enqueue %q: already exists", stored.ID)
	}
	return nil
}

// Dequeue atomically claims the next pending job. Once beginRequeueLua has
// taken an id, Dequeue never resolves, releases, or re-pushes it: only
// commit, abort, or stale-reclaim repair schedule it again.
func (q *JobQueue) Dequeue(ctx context.Context) (*jobs.Job, error) {
	if err := q.promoteDelayed(ctx); err != nil {
		return nil, err
	}

	const maxSkips = 64
	for skip := 0; skip < maxSkips; skip++ {
		raw, err := q.client.Eval(ctx, claimJobLua,
			[]string{q.readyKey(), q.processingKey(), q.processingSetKey(), q.delayedKey()},
			q.jobMetaPrefix(),
		).Result()
		if err == goredis.Nil || raw == false || raw == nil {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("job_queue: dequeue claim: %w", err)
		}
		fields, ok := raw.([]interface{})
		if !ok || len(fields) < 4 {
			return nil, nil
		}
		id, _ := fields[0].(string)
		if id == "" {
			return nil, nil
		}
		if fmt.Sprint(fields[2]) != string(jobs.StatusProcessing) {
			continue
		}
		var attempt int
		fmt.Sscanf(fmt.Sprint(fields[1]), "%d", &attempt)
		claimedAt := fmt.Sprint(fields[3])
		var claimedAtMs int64
		fmt.Sscanf(claimedAt, "%d", &claimedAtMs)
		q.stage("claimed", id, attempt)

		stored, err := q.loadJob(ctx, id)
		if err != nil {
			pushReady := !errors.Is(err, errJobNotFound)
			if releaseErr := q.releaseClaim(ctx, id, attempt, claimedAt, jobs.StatusPending, attempt-1, pushReady); releaseErr != nil {
				return nil, fmt.Errorf("%w (release claim: %v)", err, releaseErr)
			}
			if !pushReady {
				continue
			}
			return nil, err
		}
		q.stage("loaded", id, attempt)

		switch stored.Status {
		case jobs.StatusReclaiming:
			continue
		case jobs.StatusDone, jobs.StatusFailed, jobs.StatusCancelled:
			if releaseErr := q.releaseClaim(ctx, id, attempt, claimedAt, stored.Status, stored.Attempts, false); releaseErr != nil {
				return nil, fmt.Errorf("job_queue: skip status %s %q: %v", stored.Status, id, releaseErr)
			}
			continue
		}

		stored.Status = jobs.StatusProcessing
		stored.Attempts = attempt
		stored.UpdatedAt = time.UnixMilli(claimedAtMs).UTC()
		if err := q.saveJob(ctx, stored); err != nil {
			if errors.Is(err, errSaveRefused) {
				continue
			}
			if releaseErr := q.releaseClaim(ctx, id, attempt, claimedAt, jobs.StatusPending, attempt-1, true); releaseErr != nil {
				return nil, fmt.Errorf("%w (release claim: %v)", err, releaseErr)
			}
			return nil, err
		}
		job := stored.toJob()
		return &job, nil
	}
	return nil, nil
}

func (q *JobQueue) stage(name, id string, attempt int) {
	if q.dequeueStage != nil {
		q.dequeueStage(name, id, attempt)
	}
}

// Complete marks a job as done for the matching attempt.
func (q *JobQueue) Complete(ctx context.Context, id string, attempt int) error {
	meta, err := q.loadClaimMeta(ctx, id)
	if err != nil {
		return err
	}
	if meta.Status == jobs.StatusReclaiming {
		if err := q.repairStaleReclaim(ctx, id); err != nil {
			return fmt.Errorf("job_queue: complete %s attempt %d: stale reclaim repair: %w", id, attempt, err)
		}
		return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}
	if meta.Status == "" {
		if err := q.cleanupOrphans(ctx, id, jobs.DefaultRequeueMinAge, ""); err != nil {
			return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, err)
		}
		return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}
	if meta.Status != jobs.StatusProcessing || meta.Attempts != attempt {
		// pending: ready/delayed or live gap — cleanup only when aged crash.
		if meta.Status != jobs.StatusProcessing {
			if err := q.cleanupOrphans(ctx, id, jobs.DefaultRequeueMinAge, ""); err != nil {
				return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, err)
			}
		}
		return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}
	stored, err := q.loadJob(ctx, id)
	if err != nil {
		if errors.Is(err, errJobNotFound) {
			return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
		}
		return err
	}
	nowMs, err := q.redisNowMs(ctx)
	if err != nil {
		return err
	}
	stored.Status = jobs.StatusDone
	stored.Attempts = attempt
	stored.UpdatedAt = time.UnixMilli(nowMs).UTC()
	blob, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("job_queue: complete marshal %q: %w", id, err)
	}
	ttlMs := int64(terminalJobRetentionTTL / time.Millisecond)
	n, err := q.client.Eval(ctx, completeJobLua,
		[]string{q.jobKey(id), q.processingKey(), q.processingSetKey(), q.jobMetaKey(id), q.delayedKey(), q.readyKey()},
		attempt, id, blob, ttlMs,
	).Int()
	if err != nil {
		return fmt.Errorf("job_queue: complete: %w", err)
	}
	switch n {
	case 1:
		return nil
	case -2:
		return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, ErrProcessingMembership)
	case -3:
		return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	default:
		return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}
}

func queueRetryDelay(attempt int) time.Duration {
	delay := float64(queueBackoffBase) * math.Pow(2, float64(attempt))
	if delay > float64(queueBackoffMax) {
		delay = float64(queueBackoffMax)
	}
	jitter := delay * queueJitterRatio * (2*rand.Float64() - 1)
	return time.Duration(delay + jitter)
}

// Fail re-queues a job for retry or marks it as permanently failed.
func (q *JobQueue) Fail(ctx context.Context, id string, attempt int, jobErr error) error {
	meta, err := q.loadClaimMeta(ctx, id)
	if err != nil {
		return err
	}
	if meta.Status == jobs.StatusReclaiming {
		if err := q.repairStaleReclaim(ctx, id); err != nil {
			return fmt.Errorf("job_queue: fail %s attempt %d: stale reclaim repair: %w", id, attempt, err)
		}
		return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}
	if meta.Status == "" {
		if err := q.cleanupOrphans(ctx, id, jobs.DefaultRequeueMinAge, ""); err != nil {
			return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, err)
		}
		return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}
	if meta.Status != jobs.StatusProcessing || meta.Attempts != attempt {
		if meta.Status != jobs.StatusProcessing {
			if err := q.cleanupOrphans(ctx, id, jobs.DefaultRequeueMinAge, ""); err != nil {
				return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, err)
			}
		}
		return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}

	stored, err := q.loadJob(ctx, id)
	if err != nil {
		if errors.Is(err, errJobNotFound) {
			return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
		}
		return err
	}
	nowMs, err := q.redisNowMs(ctx)
	if err != nil {
		return err
	}
	now := time.UnixMilli(nowMs).UTC()
	stored.Attempts = attempt
	stored.UpdatedAt = now
	if jobErr != nil {
		stored.LastError = jobErr.Error()
	}
	terminal := 0
	runAtMs := int64(0)
	ttlMs := int64(0)
	if stored.Attempts >= stored.MaxRetries {
		terminal = 1
		stored.Status = jobs.StatusFailed
		ttlMs = int64(terminalJobRetentionTTL / time.Millisecond)
	} else {
		runAt := now.Add(queueRetryDelay(attempt - 1))
		stored.Status = jobs.StatusPending
		stored.RunAt = runAt
		runAtMs = runAt.UnixMilli()
	}
	blob, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("job_queue: fail marshal %q: %w", id, err)
	}

	n, err := q.client.Eval(ctx, failJobLua,
		[]string{q.jobKey(id), q.processingKey(), q.processingSetKey(), q.delayedKey(), q.jobMetaKey(id), q.readyKey()},
		attempt, id, blob, terminal, runAtMs, ttlMs,
	).Int()
	if err != nil {
		return fmt.Errorf("job_queue: fail: %w", err)
	}
	switch n {
	case 1:
		return nil
	case -2:
		return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, ErrProcessingMembership)
	case -3:
		return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	default:
		return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}
}

// Requeue moves a processing claim back to pending (keeps attempts).
func (q *JobQueue) Requeue(ctx context.Context, id string, attempt int, opts ...jobs.RequeueOption) error {
	return q.RequeueWithBeforeCommit(ctx, id, attempt, nil, opts...)
}

// RequeueWithBeforeCommit CAS-moves processing → reclaiming (off ready),
// runs before, then pending+ready or restores processing. Abort/commit use
// a durable context and retry so reclaiming is not a sink. Hook failure
// never LPUSHes ready; original updated_at is preserved on abort.
func (q *JobQueue) RequeueWithBeforeCommit(ctx context.Context, id string, attempt int, before jobs.RequeueBeforeCommit, opts ...jobs.RequeueOption) error {
	settings := jobs.ResolveRequeueOptions(opts)
	if settings.Terminal {
		// Terminal re-pend is Postgres jobs-table only for now (Reader/Admin
		// parity). Products needing fence+attempts on Redis should pin postgres.
		return fmt.Errorf("job_queue: requeue %s: %w", id, jobs.ErrRequeueUnsupported)
	}
	minAge := settings.MinAge
	stored, err := q.loadJob(ctx, id)
	if err != nil {
		if errors.Is(err, errJobNotFound) {
			return fmt.Errorf("job_queue: requeue %s: %w", id, jobs.ErrRequeueNotProcessing)
		}
		return err
	}

	originalUpdatedAtMs := stored.UpdatedAt.UTC().UnixMilli()
	meta, metaErr := q.loadClaimMeta(ctx, id)
	if metaErr != nil {
		return metaErr
	}
	if meta.Status == jobs.StatusReclaiming || stored.Status == jobs.StatusReclaiming {
		// In-progress while another reclaim holds meta, measured against the
		// fixed DefaultRequeueMinAge lease — never this call's min-age. A
		// failed age read retries and then errors; it never aborts.
		age, ageErr := q.reclaimAge(ctx, id)
		if ageErr != nil {
			return fmt.Errorf("job_queue: requeue %s: reclaim age: %w", id, ageErr)
		}
		if age < jobs.DefaultRequeueMinAge {
			return fmt.Errorf("job_queue: requeue %s: %w", id, jobs.ErrRequeueInProgress)
		}
		proc := stored
		proc.Status = jobs.StatusProcessing
		proc.UpdatedAt = time.UnixMilli(originalUpdatedAtMs).UTC()
		procBlob, mErr := json.Marshal(proc)
		if mErr != nil {
			return fmt.Errorf("job_queue: requeue recover marshal: %w", mErr)
		}
		if abortErr := q.abortReclaim(ctx, id, attempt, procBlob, originalUpdatedAtMs); abortErr != nil {
			return abortErr
		}
		stored, err = q.loadJob(ctx, id)
		if err != nil {
			return err
		}
		if stored.Status != jobs.StatusProcessing {
			return fmt.Errorf("job_queue: requeue %s status %s after abort: %w", id, stored.Status, jobs.ErrRequeueNotProcessing)
		}
		originalUpdatedAtMs = stored.UpdatedAt.UTC().UnixMilli()
	}

	reclaiming := stored
	reclaiming.Status = jobs.StatusReclaiming
	// Keep UpdatedAt as the original claim time in the blob.
	reclaimBlob, err := json.Marshal(reclaiming)
	if err != nil {
		return fmt.Errorf("job_queue: requeue marshal: %w", err)
	}

	n, err := q.client.Eval(ctx, beginRequeueLua,
		[]string{q.jobKey(id), q.processingKey(), q.processingSetKey(), q.jobMetaKey(id)},
		attempt, id, minAge.Milliseconds(), reclaimBlob,
	).Int()
	if err != nil {
		return fmt.Errorf("job_queue: requeue begin: %w", err)
	}
	switch n {
	case 1:
		// ok — must abort or commit from here
	case -1, -3:
		return fmt.Errorf("job_queue: requeue %s: %w", id, jobs.ErrRequeueNotProcessing)
	case -2:
		return fmt.Errorf("job_queue: requeue %s: %w", id, ErrProcessingMembership)
	case -4:
		return fmt.Errorf("job_queue: requeue %s attempt %d: %w", id, attempt, jobs.ErrRequeueWrongAttempt)
	case -5:
		return fmt.Errorf("job_queue: requeue %s: %w", id, jobs.ErrRequeueTooFresh)
	default:
		return fmt.Errorf("job_queue: requeue %s: %w", id, jobs.ErrRequeueConflict)
	}

	proc := stored
	proc.Status = jobs.StatusProcessing
	proc.UpdatedAt = time.UnixMilli(originalUpdatedAtMs).UTC()
	procBlob, err := json.Marshal(proc)
	if err != nil {
		_ = q.abortReclaim(ctx, id, attempt, reclaimBlob, originalUpdatedAtMs)
		return fmt.Errorf("job_queue: requeue processing marshal: %w", err)
	}

	var beforeErr error
	if before != nil {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					beforeErr = fmt.Errorf("job_queue: requeue before-commit panic: %v", rec)
				}
			}()
			beforeErr = before(ctx, id, attempt)
		}()
	}
	if beforeErr != nil {
		if abortErr := q.abortReclaim(ctx, id, attempt, procBlob, originalUpdatedAtMs); abortErr != nil {
			return fmt.Errorf("%w (also abort: %v)", beforeErr, abortErr)
		}
		return beforeErr
	}

	nowMs, err := q.redisNowMs(ctx)
	if err != nil {
		_ = q.abortReclaim(ctx, id, attempt, procBlob, originalUpdatedAtMs)
		return err
	}
	now := time.UnixMilli(nowMs).UTC()
	pending := stored
	pending.Status = jobs.StatusPending
	pending.RunAt = now
	pending.UpdatedAt = now
	pendingBlob, err := json.Marshal(pending)
	if err != nil {
		_ = q.abortReclaim(ctx, id, attempt, procBlob, originalUpdatedAtMs)
		return fmt.Errorf("job_queue: requeue commit marshal: %w", err)
	}

	return q.commitReclaim(ctx, id, attempt, pendingBlob, procBlob, originalUpdatedAtMs)
}

// reclaimAge returns how long the current reclaim has held meta (Redis
// clock; missing reclaiming_at_ms falls back to updated_at_ms).
func (q *JobQueue) reclaimAge(ctx context.Context, id string) (time.Duration, error) {
	var last error
	for i := 0; i < 5; i++ {
		raw, err := q.client.Eval(ctx, reclaimAgeLua, []string{q.jobMetaKey(id)}).Result()
		if err != nil {
			last = err
			continue
		}
		fields, ok := raw.([]interface{})
		if !ok || len(fields) < 2 {
			last = fmt.Errorf("unexpected reply %v", raw)
			continue
		}
		var ms int64
		if _, err := fmt.Sscanf(fmt.Sprint(fields[1]), "%d", &ms); err != nil {
			last = err
			continue
		}
		return time.Duration(ms) * time.Millisecond, nil
	}
	return 0, last
}

var _ jobs.RequeueHookable = (*JobQueue)(nil)

