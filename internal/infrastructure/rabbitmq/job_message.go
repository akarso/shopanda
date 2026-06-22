package rabbitmq

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// jobMessage is the JSON envelope stored in RabbitMQ message bodies.
type jobMessage struct {
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

func toJobMessage(job jobs.Job) jobMessage {
	payload := job.Payload
	if payload == nil {
		payload = map[string]interface{}{}
	}
	return jobMessage{
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

func (m jobMessage) toJob() jobs.Job {
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

func encodeJobMessage(job jobs.Job) ([]byte, error) {
	data, err := json.Marshal(toJobMessage(job))
	if err != nil {
		return nil, fmt.Errorf("job_queue: marshal job %q: %w", job.ID, err)
	}
	return data, nil
}

func decodeJobMessage(data []byte) (jobs.Job, error) {
	var msg jobMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return jobs.Job{}, fmt.Errorf("job_queue: unmarshal job: %w", err)
	}
	return msg.toJob(), nil
}

// EncodeJobMessageForTest exposes job JSON encoding for unit tests.
func EncodeJobMessageForTest(job jobs.Job) ([]byte, error) {
	return encodeJobMessage(job)
}

// DecodeJobMessageForTest exposes job JSON decoding for unit tests.
func DecodeJobMessageForTest(data []byte) (jobs.Job, error) {
	return decodeJobMessage(data)
}
