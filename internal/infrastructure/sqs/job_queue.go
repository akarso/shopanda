package sqs

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/infrastructure/jobqueue"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

var _ jobs.Queue = (*JobQueue)(nil)

const (
	maxSQSDelaySeconds      = 900
	maxSQSVisibilitySeconds = 43200
)

// JobQueue implements jobs.Queue using Amazon SQS.
type JobQueue struct {
	client         *sqs.Client
	queueURL       string
	failedQueueURL string

	mu       sync.Mutex
	inflight map[string]inflightEntry
}

type inflightEntry struct {
	receiptHandle string
	job           jobqueue.Message
}

// QueueConfig holds SQS job queue connection settings.
type QueueConfig struct {
	QueueURL       string
	FailedQueueURL string
	Region         string
}

// NewJobQueue creates an SQS-backed job queue client.
func NewJobQueue(ctx context.Context, cfg QueueConfig) (*JobQueue, error) {
	if cfg.QueueURL == "" {
		return nil, fmt.Errorf("NewJobQueue: empty queue_url")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	client := sqs.New(sqs.Options{
		Region: region,
	})
	if _, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(cfg.QueueURL),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	}); err != nil {
		return nil, fmt.Errorf("NewJobQueue: verify queue: %w", err)
	}
	return &JobQueue{
		client:         client,
		queueURL:       cfg.QueueURL,
		failedQueueURL: cfg.FailedQueueURL,
		inflight:       make(map[string]inflightEntry),
	}, nil
}

func sqsDelaySeconds(until time.Time) int32 {
	delay := time.Until(until)
	if delay <= 0 {
		return 0
	}
	seconds := int32(math.Ceil(delay.Seconds()))
	if seconds > maxSQSDelaySeconds {
		return maxSQSDelaySeconds
	}
	return seconds
}

func visibilityTimeout(until time.Time) int32 {
	delay := time.Until(until)
	if delay <= 0 {
		return 0
	}
	seconds := int32(math.Ceil(delay.Seconds()))
	if seconds > maxSQSVisibilitySeconds {
		return maxSQSVisibilitySeconds
	}
	return seconds
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
	body, err := jobqueue.Encode(msg)
	if err != nil {
		return err
	}
	input := &sqs.SendMessageInput{
		QueueUrl:    aws.String(q.queueURL),
		MessageBody: aws.String(string(body)),
	}
	if delay := sqsDelaySeconds(msg.RunAt); delay > 0 {
		input.DelaySeconds = delay
	}
	if _, err := q.client.SendMessage(ctx, input); err != nil {
		return fmt.Errorf("job_queue: enqueue: %w", err)
	}
	return nil
}

// Dequeue atomically claims the next pending job.
func (q *JobQueue) Dequeue(ctx context.Context) (*jobs.Job, error) {
	for {
		out, err := q.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(q.queueURL),
			MaxNumberOfMessages: 1,
			WaitTimeSeconds:     1,
			VisibilityTimeout:   30,
		})
		if err != nil {
			return nil, fmt.Errorf("job_queue: dequeue: %w", err)
		}
		if len(out.Messages) == 0 {
			return nil, nil
		}

		raw := out.Messages[0]
		if raw.Body == nil || raw.ReceiptHandle == nil {
			continue
		}
		jm, err := jobqueue.Decode([]byte(*raw.Body))
		if err != nil {
			_ = q.deleteMessage(ctx, *raw.ReceiptHandle)
			if q.failedQueueURL != "" {
				_, _ = q.client.SendMessage(ctx, &sqs.SendMessageInput{
					QueueUrl:    aws.String(q.failedQueueURL),
					MessageBody: raw.Body,
				})
			}
			continue
		}

		if remaining := time.Until(jm.RunAt); remaining > 0 {
			if _, err := q.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
				QueueUrl:          aws.String(q.queueURL),
				ReceiptHandle:     raw.ReceiptHandle,
				VisibilityTimeout: visibilityTimeout(jm.RunAt),
			}); err != nil {
				return nil, fmt.Errorf("job_queue: defer not-ready job: %w", err)
			}
			continue
		}

		now := time.Now().UTC()
		jm.Status = jobs.StatusProcessing
		jm.Attempts++
		jm.UpdatedAt = now

		q.mu.Lock()
		q.inflight[jm.ID] = inflightEntry{
			receiptHandle: *raw.ReceiptHandle,
			job:           jm,
		}
		q.mu.Unlock()

		job := jm.ToJob()
		return &job, nil
	}
}

func (q *JobQueue) deleteMessage(ctx context.Context, receiptHandle string) error {
	_, err := q.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(q.queueURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	if err != nil {
		return fmt.Errorf("job_queue: delete message: %w", err)
	}
	return nil
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
	if err := q.deleteMessage(ctx, entry.receiptHandle); err != nil {
		return err
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

	body, err := jobqueue.Encode(msg)
	if err != nil {
		return err
	}
	bodyStr := string(body)

	var publishErr error
	if msg.Attempts >= msg.MaxRetries {
		msg.Status = jobs.StatusFailed
		body, err = jobqueue.Encode(msg)
		if err != nil {
			return err
		}
		bodyStr = string(body)
		if q.failedQueueURL != "" {
			_, publishErr = q.client.SendMessage(ctx, &sqs.SendMessageInput{
				QueueUrl:    aws.String(q.failedQueueURL),
				MessageBody: aws.String(bodyStr),
			})
		}
	} else {
		delay := jobqueue.RetryDelay(msg.Attempts - 1)
		msg.Status = jobs.StatusPending
		msg.RunAt = now.Add(delay)
		body, err = jobqueue.Encode(msg)
		if err != nil {
			return err
		}
		bodyStr = string(body)
		input := &sqs.SendMessageInput{
			QueueUrl:    aws.String(q.queueURL),
			MessageBody: aws.String(bodyStr),
		}
		if delaySec := sqsDelaySeconds(msg.RunAt); delaySec > 0 {
			input.DelaySeconds = delaySec
		}
		_, publishErr = q.client.SendMessage(ctx, input)
	}
	if publishErr != nil {
		return fmt.Errorf("job_queue: fail publish: %w", publishErr)
	}

	if err := q.deleteMessage(ctx, entry.receiptHandle); err != nil {
		return err
	}
	q.removeInflight(id)
	return nil
}
