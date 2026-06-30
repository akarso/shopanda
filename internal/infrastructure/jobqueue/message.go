package jobqueue

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

const (
	BackoffBase = 5 * time.Second
	BackoffMax  = 5 * time.Minute
	JitterRatio = 0.25
)

// Message is the JSON envelope stored in broker-backed job queues.
type Message struct {
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

// FromJob converts a domain job into a broker message.
func FromJob(job jobs.Job) Message {
	payload := job.Payload
	if payload == nil {
		payload = map[string]interface{}{}
	}
	return Message{
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

// ToJob converts a broker message into a domain job.
func (m Message) ToJob() jobs.Job {
	payload := m.Payload
	if payload == nil {
		payload = map[string]interface{}{}
	}
	return jobs.Job{
		ID:         m.ID,
		Type:       m.Type,
		Payload:    payload,
		Status:     m.Status,
		Attempts:   m.Attempts,
		MaxRetries: m.MaxRetries,
		RunAt:      m.RunAt,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}

// Encode serializes a job message.
func Encode(msg Message) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("jobqueue: marshal job %q: %w", msg.ID, err)
	}
	return data, nil
}

// Decode deserializes a job message.
func Decode(data []byte) (Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return Message{}, fmt.Errorf("jobqueue: unmarshal job: %w", err)
	}
	if err := validateMessage(msg); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func validateMessage(msg Message) error {
	if strings.TrimSpace(msg.ID) == "" {
		return fmt.Errorf("jobqueue: job id is required")
	}
	if strings.TrimSpace(msg.Type) == "" {
		return fmt.Errorf("jobqueue: job type is required")
	}
	return nil
}

// RetryDelay returns exponential backoff with jitter for the given attempt index.
func RetryDelay(attempt int) time.Duration {
	delay := float64(BackoffBase) * math.Pow(2, float64(attempt))
	if delay > float64(BackoffMax) {
		delay = float64(BackoffMax)
	}
	jitter := delay * JitterRatio * (2*rand.Float64() - 1)
	total := delay + jitter
	if total > float64(BackoffMax) {
		total = float64(BackoffMax)
	}
	if total < 0 {
		total = 0
	}
	return time.Duration(total)
}
