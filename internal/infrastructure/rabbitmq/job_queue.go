package rabbitmq

import (
	"context"
	"encoding/json"
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
// Delayed delivery uses a dead-letter delay queue: messages published with a TTL
// are routed to the main queue after expiration. Retries and RunAt scheduling
// use the same mechanism. Permanently failed jobs are published to a separate
// durable failed queue for observability.
type JobQueue struct {
	conn        *amqp.Connection
	ch          *amqp.Channel
	mainQueue   string
	delayQueue  string
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
	delayQueue := prefix + ".jobs.delay"
	failedQueue := prefix + ".jobs.failed"

	delayArgs := amqp.Table{
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": mainQueue,
	}

	for _, spec := range []struct {
		name string
		args amqp.Table
	}{
		{mainQueue, nil},
		{delayQueue, delayArgs},
		{failedQueue, nil},
	} {
		if _, err := ch.QueueDeclare(spec.name, true, false, false, false, spec.args); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return nil, fmt.Errorf("NewJobQueue: declare queue %q: %w", spec.name, err)
		}
	}

	return &JobQueue{
		conn:        conn,
		ch:          ch,
		mainQueue:   mainQueue,
		delayQueue:  delayQueue,
		failedQueue: failedQueue,
		inflight:    make(map[string]inflightEntry),
	}, nil
}

func (q *JobQueue) publishTo(ctx context.Context, queue string, msg jobMessage, delay time.Duration) error {
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
	if delay > 0 {
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
	if delay := time.Until(msg.RunAt); delay > 0 {
		return q.publishTo(ctx, q.delayQueue, msg, delay)
	}
	return q.publishTo(ctx, q.mainQueue, msg, 0)
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
		_ = q.ch.Publish("", q.failedQueue, false, false, amqp.Publishing{
			ContentType:  "application/octet-stream",
			Body:         delivery.Body,
			DeliveryMode: amqp.Persistent,
		})
		_ = delivery.Nack(false, false)
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

func (q *JobQueue) inflightEntry(id string) (inflightEntry, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	entry, ok := q.inflight[id]
	if !ok {
		return inflightEntry{}, fmt.Errorf("job_queue: job %s not found or not processing", id)
	}
	return entry, nil
}

func (q *JobQueue) removeInflight(id string) {
	q.mu.Lock()
	delete(q.inflight, id)
	q.mu.Unlock()
}

// Complete marks a job as done.
func (q *JobQueue) Complete(ctx context.Context, id string) error {
	entry, err := q.inflightEntry(id)
	if err != nil {
		return err
	}
	if err := q.ch.Ack(entry.tag, false); err != nil {
		return fmt.Errorf("job_queue: complete ack: %w", err)
	}
	q.removeInflight(id)
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
	entry, err := q.inflightEntry(id)
	if err != nil {
		return err
	}

	msg := entry.job
	now := time.Now().UTC()
	msg.UpdatedAt = now
	if jobErr != nil {
		msg.LastError = jobErr.Error()
	}

	var publishErr error
	if msg.Attempts >= msg.MaxRetries {
		msg.Status = jobs.StatusFailed
		publishErr = q.publishTo(ctx, q.failedQueue, msg, 0)
	} else {
		delay := queueRetryDelay(msg.Attempts - 1)
		msg.Status = jobs.StatusPending
		msg.RunAt = now.Add(delay)
		publishErr = q.publishTo(ctx, q.delayQueue, msg, delay)
	}
	if publishErr != nil {
		return publishErr
	}

	if err := q.ch.Ack(entry.tag, false); err != nil {
		return fmt.Errorf("job_queue: fail ack: %w", err)
	}
	q.removeInflight(id)
	return nil
}

func jsonMarshalJobMessage(msg jobMessage) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("job_queue: marshal job %q: %w", msg.ID, err)
	}
	return data, nil
}

func decodeJobMessageBytes(data []byte) (jobMessage, error) {
	var msg jobMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return jobMessage{}, fmt.Errorf("job_queue: unmarshal job: %w", err)
	}
	return msg, nil
}
