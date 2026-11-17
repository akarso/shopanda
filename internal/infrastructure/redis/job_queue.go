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

var _ jobs.Queue = (*JobQueue)(nil)

const (
	queueBackoffBase        = 5 * time.Second
	queueBackoffMax         = 5 * time.Minute
	queueJitterRatio        = 0.25
	terminalJobRetentionTTL = 7 * 24 * time.Hour
)

const promoteDelayedLua = `
local delayed = KEYS[1]
local ready = KEYS[2]
local now = tonumber(ARGV[1])
local ids = redis.call('ZRANGEBYSCORE', delayed, '-inf', now, 'LIMIT', 0, 100)
for _, id in ipairs(ids) do
  redis.call('ZREM', delayed, id)
  redis.call('LPUSH', ready, id)
end
return #ids
`

// processingMemberLua is inlined by complete/fail scripts: true when id is
// on the processing list. Checked before any hash/delayed write so a
// divergent list/hash cannot publish a second schedule, and encode runs
// before LREM so an encode error cannot drop the claim.
const processingMemberCheck = `
local function on_processing(list_key, id)
  local list = redis.call('LRANGE', list_key, 0, -1)
  for _, v in ipairs(list) do
    if v == id then return true end
  end
  return false
end
`

// completeJobLua CAS-checks attempt/status (decode only), then SETs a
// Go-marshaled blob. Never cjson.encode — Redis cjson turns empty JSON
// arrays into objects, which poisons payloads like product_ids:[].
// KEYS: job, processing. ARGV: attempt, id, encoded_json, ttl_ms (0=persist).
const completeJobLua = processingMemberCheck + `
local raw = redis.call('GET', KEYS[1])
if not raw then return -1 end
local job = cjson.decode(raw)
if job['status'] ~= 'processing' or tonumber(job['attempts']) ~= tonumber(ARGV[1]) then
  return 0
end
if not on_processing(KEYS[2], ARGV[2]) then
  return 0
end
local ttl = tonumber(ARGV[4])
if ttl > 0 then
  redis.call('SET', KEYS[1], ARGV[3], 'PX', ttl)
else
  redis.call('SET', KEYS[1], ARGV[3])
end
redis.call('LREM', KEYS[2], 1, ARGV[2])
return 1
`

// failJobLua CAS-checks then SETs a Go-marshaled blob (+ ZADD on retry).
// Never cjson.encode (empty-array corruption). KEYS: job, processing,
// delayed. ARGV: attempt, id, encoded_json, terminal (1/0), run_at_ms, ttl_ms.
const failJobLua = processingMemberCheck + `
local raw = redis.call('GET', KEYS[1])
if not raw then return -1 end
local job = cjson.decode(raw)
if job['status'] ~= 'processing' or tonumber(job['attempts']) ~= tonumber(ARGV[1]) then
  return 0
end
if not on_processing(KEYS[2], ARGV[2]) then
  return 0
end
local ttl = tonumber(ARGV[6])
if tonumber(ARGV[4]) == 1 then
  if ttl > 0 then
    redis.call('SET', KEYS[1], ARGV[3], 'PX', ttl)
  else
    redis.call('SET', KEYS[1], ARGV[3])
  end
else
  redis.call('SET', KEYS[1], ARGV[3])
  redis.call('ZADD', KEYS[3], tonumber(ARGV[5]), ARGV[2])
end
redis.call('LREM', KEYS[2], 1, ARGV[2])
return 1
`

// JobQueue implements jobs.Queue using Redis lists and a delayed sorted set.
type JobQueue struct {
	client *goredis.Client
	prefix string
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

func (q *JobQueue) jobKey(id string) string { return q.prefix + "job:" + id }
func (q *JobQueue) readyKey() string        { return q.prefix + "ready" }
func (q *JobQueue) processingKey() string   { return q.prefix + "processing" }
func (q *JobQueue) delayedKey() string      { return q.prefix + "delayed" }

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
	ttl := jobRetentionTTL(job.Status)
	if err := q.client.Set(ctx, q.jobKey(job.ID), data, ttl).Err(); err != nil {
		return fmt.Errorf("job_queue: save job %q: %w", job.ID, err)
	}
	return nil
}

func (q *JobQueue) releaseClaim(ctx context.Context, id string) error {
	if _, err := q.client.LRem(ctx, q.processingKey(), 1, id).Result(); err != nil {
		return fmt.Errorf("job_queue: release claim %q: %w", id, err)
	}
	if err := q.client.LPush(ctx, q.readyKey(), id).Err(); err != nil {
		return fmt.Errorf("job_queue: requeue %q: %w", id, err)
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
		n, err := q.client.Eval(ctx, promoteDelayedLua, []string{q.delayedKey(), q.readyKey()},
			time.Now().UTC().UnixMilli()).Int()
		if err != nil {
			return fmt.Errorf("job_queue: promote delayed: %w", err)
		}
		if n == 0 {
			return nil
		}
	}
}

// Enqueue inserts a new job into the queue.
func (q *JobQueue) Enqueue(ctx context.Context, job jobs.Job) error {
	stored := toStoredJob(job)
	if stored.Status == "" {
		stored.Status = jobs.StatusPending
	}
	if stored.MaxRetries == 0 {
		stored.MaxRetries = jobs.DefaultMaxRetries
	}
	if err := q.saveJob(ctx, stored); err != nil {
		return err
	}

	now := time.Now().UTC()
	if stored.RunAt.After(now) {
		if err := q.client.ZAdd(ctx, q.delayedKey(), goredis.Z{
			Score:  float64(stored.RunAt.UnixMilli()),
			Member: stored.ID,
		}).Err(); err != nil {
			return fmt.Errorf("job_queue: enqueue delayed: %w", err)
		}
		return nil
	}

	if err := q.client.LPush(ctx, q.readyKey(), stored.ID).Err(); err != nil {
		return fmt.Errorf("job_queue: enqueue ready: %w", err)
	}
	return nil
}

// Dequeue atomically claims the next pending job.
func (q *JobQueue) Dequeue(ctx context.Context) (*jobs.Job, error) {
	if err := q.promoteDelayed(ctx); err != nil {
		return nil, err
	}

	id, err := q.client.RPopLPush(ctx, q.readyKey(), q.processingKey()).Result()
	if err == goredis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("job_queue: dequeue claim: %w", err)
	}

	stored, err := q.loadJob(ctx, id)
	if err != nil {
		if releaseErr := q.releaseClaim(ctx, id); releaseErr != nil {
			return nil, fmt.Errorf("job_queue: load job %q: %w (release claim: %v)", id, err, releaseErr)
		}
		return nil, err
	}

	stored.Status = jobs.StatusProcessing
	stored.Attempts++
	stored.UpdatedAt = time.Now().UTC()
	if err := q.saveJob(ctx, stored); err != nil {
		if releaseErr := q.releaseClaim(ctx, id); releaseErr != nil {
			return nil, fmt.Errorf("job_queue: save job %q: %w (release claim: %v)", id, err, releaseErr)
		}
		return nil, err
	}

	job := stored.toJob()
	return &job, nil
}

// Complete marks a job as done for the matching attempt.
func (q *JobQueue) Complete(ctx context.Context, id string, attempt int) error {
	stored, err := q.loadJob(ctx, id)
	if err != nil {
		if errors.Is(err, errJobNotFound) {
			return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
		}
		return err
	}
	if stored.Status != jobs.StatusProcessing || stored.Attempts != attempt {
		return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}
	stored.Status = jobs.StatusDone
	stored.UpdatedAt = time.Now().UTC()
	blob, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("job_queue: complete marshal %q: %w", id, err)
	}
	ttlMs := int64(terminalJobRetentionTTL / time.Millisecond)
	n, err := q.client.Eval(ctx, completeJobLua,
		[]string{q.jobKey(id), q.processingKey()},
		attempt, id, blob, ttlMs,
	).Int()
	if err != nil {
		return fmt.Errorf("job_queue: complete: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("job_queue: complete %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}
	return nil
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
	stored, err := q.loadJob(ctx, id)
	if err != nil {
		if errors.Is(err, errJobNotFound) {
			return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
		}
		return err
	}
	// Pre-check attempt/status without mutating — Lua re-checks under one script.
	if stored.Status != jobs.StatusProcessing || stored.Attempts != attempt {
		return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}

	now := time.Now().UTC()
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
	// encoding/json preserves empty arrays ([]); Redis cjson.encode does not.
	blob, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("job_queue: fail marshal %q: %w", id, err)
	}

	n, err := q.client.Eval(ctx, failJobLua,
		[]string{q.jobKey(id), q.processingKey(), q.delayedKey()},
		attempt, id, blob, terminal, runAtMs, ttlMs,
	).Int()
	if err != nil {
		return fmt.Errorf("job_queue: fail: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("job_queue: fail %s attempt %d: %w", id, attempt, jobs.ErrOutcomeNotApplied)
	}
	return nil
}

// Requeue is unsupported on this broker-backed queue (no queryable processing row).
func (q *JobQueue) Requeue(context.Context, string, int) error {
	return jobs.ErrRequeueUnsupported
}

