package kafka

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/infrastructure/jobqueue"
	"github.com/segmentio/kafka-go"
)

var _ jobs.Queue = (*JobQueue)(nil)

const promoteBatchLimit = 100

// JobQueue implements jobs.Queue using Kafka topics.
type JobQueue struct {
	mainWriter   *kafka.Writer
	delayWriter  *kafka.Writer
	failedWriter *kafka.Writer
	mainReader   *kafka.Reader
	delayReader  *kafka.Reader

	mu       sync.Mutex
	inflight map[string]inflightEntry
}

type inflightEntry struct {
	msg kafka.Message
	job jobqueue.Message
}

// QueueConfig holds Kafka job queue connection settings.
type QueueConfig struct {
	Brokers     []string
	TopicPrefix string
}

func isTopicExistsErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Topic already exists") || strings.Contains(msg, "TOPIC_ALREADY_EXISTS")
}

// NewJobQueue connects to Kafka and prepares job topics.
func NewJobQueue(cfg QueueConfig) (*JobQueue, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("NewJobQueue: empty brokers")
	}
	prefix := strings.TrimSpace(cfg.TopicPrefix)
	if prefix == "" {
		prefix = "shopanda"
	}
	mainTopic := prefix + ".jobs"
	delayTopic := prefix + ".jobs.delay"
	failedTopic := prefix + ".jobs.failed"

	conn, err := kafka.Dial("tcp", cfg.Brokers[0])
	if err != nil {
		return nil, fmt.Errorf("NewJobQueue: dial: %w", err)
	}
	defer conn.Close()

	for _, topic := range []string{mainTopic, delayTopic, failedTopic} {
		if err := conn.CreateTopics(kafka.TopicConfig{
			Topic:             topic,
			NumPartitions:     1,
			ReplicationFactor: 1,
		}); err != nil && !isTopicExistsErr(err) {
			return nil, fmt.Errorf("NewJobQueue: create topic %q: %w", topic, err)
		}
	}

	return &JobQueue{
		mainWriter: &kafka.Writer{
			Addr:     kafka.TCP(cfg.Brokers...),
			Topic:    mainTopic,
			Balancer: &kafka.LeastBytes{},
		},
		delayWriter: &kafka.Writer{
			Addr:     kafka.TCP(cfg.Brokers...),
			Topic:    delayTopic,
			Balancer: &kafka.LeastBytes{},
		},
		failedWriter: &kafka.Writer{
			Addr:     kafka.TCP(cfg.Brokers...),
			Topic:    failedTopic,
			Balancer: &kafka.LeastBytes{},
		},
		mainReader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:  cfg.Brokers,
			Topic:    mainTopic,
			GroupID:  prefix + ".worker",
			MinBytes: 1,
			MaxBytes: 10e6,
		}),
		delayReader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:  cfg.Brokers,
			Topic:    delayTopic,
			GroupID:  prefix + ".delay",
			MinBytes: 1,
			MaxBytes: 10e6,
		}),
		inflight: make(map[string]inflightEntry),
	}, nil
}

func (q *JobQueue) publish(ctx context.Context, writer *kafka.Writer, msg jobqueue.Message) error {
	body, err := jobqueue.Encode(msg)
	if err != nil {
		return err
	}
	if err := writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(msg.ID),
		Value: body,
	}); err != nil {
		return fmt.Errorf("job_queue: publish: %w", err)
	}
	return nil
}

func (q *JobQueue) quarantine(ctx context.Context, reader *kafka.Reader, msg kafka.Message) error {
	if err := q.failedWriter.WriteMessages(ctx, kafka.Message{Value: msg.Value}); err != nil {
		return fmt.Errorf("job_queue: quarantine: %w", err)
	}
	if err := reader.CommitMessages(ctx, msg); err != nil {
		return fmt.Errorf("job_queue: quarantine commit: %w", err)
	}
	return nil
}

func (q *JobQueue) promoteDelayed(ctx context.Context) error {
	now := time.Now().UTC()
	for i := 0; i < promoteBatchLimit; i++ {
		fetchCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		msg, err := q.delayReader.FetchMessage(fetchCtx)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("job_queue: promote delayed: %w", err)
		}

		jm, err := jobqueue.Decode(msg.Value)
		if err != nil {
			if qErr := q.quarantine(ctx, q.delayReader, msg); qErr != nil {
				return qErr
			}
			continue
		}

		if jm.RunAt.After(now) {
			return nil
		}

		if err := q.mainWriter.WriteMessages(ctx, kafka.Message{
			Key:   []byte(jm.ID),
			Value: msg.Value,
		}); err != nil {
			return fmt.Errorf("job_queue: promote publish: %w", err)
		}
		if err := q.delayReader.CommitMessages(ctx, msg); err != nil {
			return fmt.Errorf("job_queue: promote commit: %w", err)
		}
	}
	return nil
}

// Enqueue inserts a new job into the queue.
func (q *JobQueue) Enqueue(ctx context.Context, job jobs.Job) error {
	msg := jobqueue.FromJob(job)
	if msg.Status == "" {
		msg.Status = jobs.StatusPending
	}
	if msg.MaxRetries == 0 {
		msg.MaxRetries = jobs.DefaultMaxRetries
	}
	writer := q.mainWriter
	if delay := time.Until(msg.RunAt); delay > 0 {
		writer = q.delayWriter
	}
	return q.publish(ctx, writer, msg)
}

// Dequeue atomically claims the next pending job.
func (q *JobQueue) Dequeue(ctx context.Context) (*jobs.Job, error) {
	if err := q.promoteDelayed(ctx); err != nil {
		return nil, err
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	msg, err := q.mainReader.FetchMessage(fetchCtx)
	cancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, nil
		}
		return nil, fmt.Errorf("job_queue: dequeue: %w", err)
	}

	jm, err := jobqueue.Decode(msg.Value)
	if err != nil {
		if qErr := q.quarantine(ctx, q.mainReader, msg); qErr != nil {
			return nil, qErr
		}
		return nil, err
	}

	now := time.Now().UTC()
	if jm.Status != jobs.StatusProcessing {
		jm.Status = jobs.StatusProcessing
		jm.Attempts++
		jm.UpdatedAt = now
	}
	encoded, err := jobqueue.Encode(jm)
	if err != nil {
		return nil, err
	}
	msg.Value = encoded

	q.mu.Lock()
	q.inflight[jm.ID] = inflightEntry{msg: msg, job: jm}
	q.mu.Unlock()

	job := jm.ToJob()
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
	if err := q.mainReader.CommitMessages(ctx, entry.msg); err != nil {
		return fmt.Errorf("job_queue: complete commit: %w", err)
	}
	q.removeInflight(id)
	return nil
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
		publishErr = q.publish(ctx, q.failedWriter, msg)
	} else {
		delay := jobqueue.RetryDelay(msg.Attempts - 1)
		msg.Status = jobs.StatusPending
		msg.RunAt = now.Add(delay)
		publishErr = q.publish(ctx, q.delayWriter, msg)
	}
	if publishErr != nil {
		return publishErr
	}

	if err := q.mainReader.CommitMessages(ctx, entry.msg); err != nil {
		return fmt.Errorf("job_queue: fail commit: %w", err)
	}
	q.removeInflight(id)
	return nil
}
