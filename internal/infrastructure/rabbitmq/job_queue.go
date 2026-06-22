package rabbitmq

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	amqp "github.com/rabbitmq/amqp091-go"
)

var _ jobs.Queue = (*JobQueue)(nil)

const (
	queueBackoffBase = 5 * time.Second
	queueBackoffMax  = 5 * time.Minute
	queueJitterRatio = 0.25
)

// JobQueue implements jobs.Queue using RabbitMQ durable queues.
//
// Retry strategy: on Fail, the in-flight message is acked and re-published to the
// main queue with a per-message expiration (backoff). RunAt delays use the same
// expiration mechanism on Enqueue. Permanently failed jobs are published to a
// separate durable failed queue for observability.
type JobQueue struct {
	conn        *amqp.Connection
	ch          *amqp.Channel
	mainQueue   string
	failedQueue string
	mu          sync.Mutex
	inflight    map[string]inflightEntry
}

type inflightEntry struct {
	tag uint64
	job jobMessage
}

// QueueConfig holds RabbitMQ job queue connection settings.
type QueueConfig struct {
	URL         string
	QueuePrefix string
}

// NewJobQueue connects to RabbitMQ and declares durable job queues.
func NewJobQueue(cfg QueueConfig) (*JobQueue, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("NewJobQueue: empty url")
	}

	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("NewJobQueue: dial: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("NewJobQueue: channel: %w", err)
	}

	prefix := cfg.QueuePrefix
	if prefix == "" {
		prefix = "shopanda"
	}
	mainQueue := prefix + ".jobs"
	failedQueue := prefix + ".jobs.failed"

	for _, name := range []string{mainQueue, failedQueue} {
		if _, err := ch.QueueDeclare(name, true, false, false, false, nil); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return nil, fmt.Errorf("NewJobQueue: declare queue %q: %w", name, err)
		}
	}

	return &JobQueue{
		conn:        conn,
		ch:          ch,
		mainQueue:   mainQueue,
		failedQueue: failedQueue,
		inflight:    make(map[string]inflightEntry),
	}, nil
}

func (q *JobQueue) publish(ctx context.Context, queue string, msg jobMessage) error {
	_ = ctx
	body, err := jsonMarshalJobMessage(msg)
	if err != nil {
		return err
	}
	pub := amqp.Publishing{
		ContentType:  "application/json",
		Body:         body,
		DeliveryMode: amqp.Persistent,
	}
	if delay := time.Until(msg.RunAt); delay > 0 {
		pub.Expiration = strconv.FormatInt(delay.Milliseconds(), 10)
	}
	if err := q.ch.Publish("", queue, false, false, pub); err != nil {
		return fmt.Errorf("job_queue: publish: %w", err)
	}
	return nil
}

// Enqueue inserts a new job into the queue.
func (q *JobQueue) Enqueue(ctx context.Context, job jobs.Job) error {
	msg := toJobMessage(job)
	if msg.Status == "" {
		msg.Status = jobs.StatusPending
	}
	if msg.MaxRetries == 0 {
		msg.MaxRetries = jobs.DefaultMaxRetries
	}
	return q.publish(ctx, q.mainQueue, msg)
}

// Dequeue atomically claims the next pending job.
func (q *JobQueue) Dequeue(ctx context.Context) (*jobs.Job, error) {
	_ = ctx
	delivery, ok, err := q.ch.Get(q.mainQueue, false)
	if err != nil {
		return nil, fmt.Errorf("job_queue: dequeue: %w", err)
	}
	if !ok {
		return nil, nil
	}

	msg, err := decodeJobMessageBytes(delivery.Body)
	if err != nil {
		_ = delivery.Nack(false, true)
		return nil, err
	}

	msg.Status = jobs.StatusProcessing
	msg.Attempts++
	msg.UpdatedAt = time.Now().UTC()

	q.mu.Lock()
	q.inflight[msg.ID] = inflightEntry{tag: delivery.DeliveryTag, job: msg}
	q.mu.Unlock()

	job := msg.toJob()
	return &job, nil
}

func (q *JobQueue) takeInflight(id string) (inflightEntry, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	entry, ok := q.inflight[id]
	if !ok {
		return inflightEntry{}, fmt.Errorf("job_queue: job %s not found or not processing", id)
	}
	delete(q.inflight, id)
	return entry, nil
}

// Complete marks a job as done.
func (q *JobQueue) Complete(ctx context.Context, id string) error {
	entry, err := q.takeInflight(id)
	if err != nil {
		return err
	}
	if err := q.ch.Ack(entry.tag, false); err != nil {
		return fmt.Errorf("job_queue: complete ack: %w", err)
	}
	_ = ctx
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
func (q *JobQueue) Fail(ctx context.Context, id string, jobErr error) error {
	entry, err := q.takeInflight(id)
	if err != nil {
		return err
	}
	if err := q.ch.Ack(entry.tag, false); err != nil {
		return fmt.Errorf("job_queue: fail ack: %w", err)
	}

	msg := entry.job
	now := time.Now().UTC()
	msg.UpdatedAt = now
	if jobErr != nil {
		msg.LastError = jobErr.Error()
	}

	if msg.Attempts >= msg.MaxRetries {
		msg.Status = jobs.StatusFailed
		return q.publish(ctx, q.failedQueue, msg)
	}

	delay := queueRetryDelay(msg.Attempts - 1)
	msg.Status = jobs.StatusPending
	msg.RunAt = now.Add(delay)
	return q.publish(ctx, q.mainQueue, msg)
}

// json helpers keep encode/decode in job_message.go testable from this package.
func jsonMarshalJobMessage(msg jobMessage) ([]byte, error) {
	return encodeJobMessage(msg.toJob())
}

func decodeJobMessageBytes(data []byte) (jobMessage, error) {
	job, err := decodeJobMessage(data)
	if err != nil {
		return jobMessage{}, err
	}
	return toJobMessage(job), nil
}
