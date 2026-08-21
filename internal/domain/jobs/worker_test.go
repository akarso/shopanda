package jobs_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
)

// --- mock queue ---

type mockQueue struct {
	mu        sync.Mutex
	jobs      []*jobs.Job
	completed []string
	failed    []string
}

func (m *mockQueue) Enqueue(_ context.Context, job jobs.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs = append(m.jobs, &job)
	return nil
}

func (m *mockQueue) Dequeue(_ context.Context) (*jobs.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.jobs) == 0 {
		return nil, nil
	}
	j := m.jobs[0]
	m.jobs = m.jobs[1:]
	j.Status = jobs.StatusProcessing
	j.Attempts++
	return j, nil
}

func (m *mockQueue) Complete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completed = append(m.completed, id)
	return nil
}

func (m *mockQueue) Fail(_ context.Context, id string, _ error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failed = append(m.failed, id)
	return nil
}

// --- mock handler ---

type mockHandler struct {
	jobType  string
	handleFn func(ctx context.Context, job jobs.Job) error
}

func (h *mockHandler) Type() string                                   { return h.jobType }
func (h *mockHandler) Handle(ctx context.Context, job jobs.Job) error { return h.handleFn(ctx, job) }

// --- mock logger ---

type mockLogger struct {
	mu   sync.Mutex
	msgs []string
}

func (l *mockLogger) Info(msg string, _ map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, msg)
}

func (l *mockLogger) Error(msg string, _ error, _ map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, msg)
}

// --- mock metrics ---

type mockMetrics struct {
	mu       sync.Mutex
	failures []string
}

func (m *mockMetrics) JobFailure(jobType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures = append(m.failures, jobType)
}

// --- tests ---

func TestWorker_ProcessesJob(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}
	var processed bool

	handler := &mockHandler{
		jobType: "test_job",
		handleFn: func(_ context.Context, j jobs.Job) error {
			processed = true
			return nil
		},
	}

	w := jobs.NewWorker(q, log, 50*time.Millisecond)
	w.Register(handler)

	job, _ := jobs.NewJob("j1", "test_job", nil)
	_ = q.Enqueue(context.Background(), job)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Start(ctx)

	if !processed {
		t.Fatal("expected job to be processed")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.completed) != 1 || q.completed[0] != "j1" {
		t.Errorf("completed = %v, want [j1]", q.completed)
	}
}

func TestWorker_FailsUnknownType(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}

	w := jobs.NewWorker(q, log, 50*time.Millisecond)
	// No handlers registered.

	job, _ := jobs.NewJob("j1", "unknown", nil)
	_ = q.Enqueue(context.Background(), job)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Start(ctx)

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.failed) != 1 || q.failed[0] != "j1" {
		t.Errorf("failed = %v, want [j1]", q.failed)
	}
}

func TestWorker_HandlerError(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}

	handler := &mockHandler{
		jobType:  "fail_job",
		handleFn: func(_ context.Context, _ jobs.Job) error { return errors.New("boom") },
	}

	w := jobs.NewWorker(q, log, 50*time.Millisecond)
	w.Register(handler)

	job, _ := jobs.NewJob("j1", "fail_job", nil)
	_ = q.Enqueue(context.Background(), job)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Start(ctx)

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.failed) != 1 {
		t.Errorf("failed = %v, want [j1]", q.failed)
	}
}

func TestWorker_StopSignal(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}

	w := jobs.NewWorker(q, log, 50*time.Millisecond)

	done := make(chan struct{})
	go func() {
		w.Start(context.Background())
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	w.Stop()

	select {
	case <-done:
		// OK
	case <-time.After(time.Second):
		t.Fatal("worker did not stop within timeout")
	}
}

func TestWorker_HandlerError_RecordsMetric(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}
	m := &mockMetrics{}

	handler := &mockHandler{
		jobType:  "fail_job",
		handleFn: func(_ context.Context, _ jobs.Job) error { return errors.New("boom") },
	}

	w := jobs.NewWorker(q, log, 50*time.Millisecond).WithMetrics(m)
	w.Register(handler)

	job, _ := jobs.NewJob("j1", "fail_job", nil)
	_ = q.Enqueue(context.Background(), job)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Start(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.failures) != 1 || m.failures[0] != "fail_job" {
		t.Errorf("failures = %v, want [fail_job]", m.failures)
	}
}

func TestWorker_FailsUnknownType_RecordsMetric(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}
	m := &mockMetrics{}

	w := jobs.NewWorker(q, log, 50*time.Millisecond).WithMetrics(m)

	// A distinctly bogus type (not literally "unknown") proves JobFailure
	// gets the fixed sentinel, not the raw unregistered queue value passed
	// straight through as a Prometheus label.
	job, _ := jobs.NewJob("j1", "no-such-handler-registered", nil)
	_ = q.Enqueue(context.Background(), job)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Start(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.failures) != 1 || m.failures[0] != "unknown" {
		t.Errorf("failures = %v, want [unknown]", m.failures)
	}
}

func TestWorker_SuccessfulJob_DoesNotRecordFailureMetric(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}
	m := &mockMetrics{}

	handler := &mockHandler{
		jobType:  "ok_job",
		handleFn: func(_ context.Context, _ jobs.Job) error { return nil },
	}

	w := jobs.NewWorker(q, log, 50*time.Millisecond).WithMetrics(m)
	w.Register(handler)

	job, _ := jobs.NewJob("j1", "ok_job", nil)
	_ = q.Enqueue(context.Background(), job)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Start(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.failures) != 0 {
		t.Errorf("failures = %v, want none for a successful job", m.failures)
	}
}

func TestWorker_WithoutMetrics_DoesNotPanicOnFailure(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}

	handler := &mockHandler{
		jobType:  "fail_job",
		handleFn: func(_ context.Context, _ jobs.Job) error { return errors.New("boom") },
	}

	w := jobs.NewWorker(q, log, 50*time.Millisecond) // no WithMetrics call
	w.Register(handler)

	job, _ := jobs.NewJob("j1", "fail_job", nil)
	_ = q.Enqueue(context.Background(), job)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	w.Start(ctx)
}

func TestWorker_DuplicateHandlerPanics(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}
	w := jobs.NewWorker(q, log, time.Second)

	handler := &mockHandler{jobType: "dup", handleFn: func(_ context.Context, _ jobs.Job) error { return nil }}
	w.Register(handler)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	w.Register(handler)
}

func TestWorker_EmptyQueueNoOp(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}

	w := jobs.NewWorker(q, log, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	w.Start(ctx)

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.completed) != 0 {
		t.Errorf("expected no completed jobs, got %d", len(q.completed))
	}
	if len(q.failed) != 0 {
		t.Errorf("expected no failed jobs, got %d", len(q.failed))
	}
}
