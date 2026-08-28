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
	mu              sync.Mutex
	jobs            []*jobs.Job
	completed       []string
	failed          []string
	failErrs        []error // jobErr as observed by each Fail call, in order
	completeFn      func(id string) error
	failCtxErrs     []error // ctx.Err() as observed by each Fail call, in order
	completeCtxErrs []error
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

func (m *mockQueue) Complete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completeCtxErrs = append(m.completeCtxErrs, ctx.Err())
	if m.completeFn != nil {
		if err := m.completeFn(id); err != nil {
			return err
		}
	}
	m.completed = append(m.completed, id)
	return nil
}

func (m *mockQueue) Fail(ctx context.Context, id string, jobErr error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCtxErrs = append(m.failCtxErrs, ctx.Err())
	m.failErrs = append(m.failErrs, jobErr)
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
	// Pins the fix for the diagnostically most important case having no
	// last_error at all: an unregistered job type is exactly the kind of
	// misconfiguration an operator needs a reason for, and Fail only
	// persists last_error when jobErr is non-nil (see job_queue.go).
	if len(q.failErrs) != 1 || q.failErrs[0] == nil {
		t.Fatalf("failErrs = %v, want a single non-nil error", q.failErrs)
	}
	if got := q.failErrs[0].Error(); got != `no handler registered for job type "unknown"` {
		t.Errorf("failErrs[0] = %q, want a message naming the unregistered job type", got)
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

// TestWorker_CompleteFailure_RecordsMetric pins the gap where a handler
// succeeds but queue.Complete fails: the job may be redelivered (e.g.
// duplicate webhook sends) while dashboards previously showed zero errors
// because only the log recorded it.
func TestWorker_CompleteFailure_RecordsMetric(t *testing.T) {
	q := &mockQueue{
		completeFn: func(string) error { return errors.New("complete failed: connection reset") },
	}
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
	if len(m.failures) != 1 || m.failures[0] != "ok_job" {
		t.Errorf("failures = %v, want [ok_job] — queue.Complete failing after a successful handler must still be counted", m.failures)
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

// TestWorker_HandlerPanic_DoesNotCrashWorker pins the fix for a missing
// recover() around a handler's Handle call: before the fix, a panicking
// handler propagated out of processNext and killed the worker's entire
// polling loop (every other queued job, not just the one that panicked).
// The fix converts the panic into a normal error on the same path a
// handler-returned error already takes.
func TestWorker_HandlerPanic_DoesNotCrashWorker(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}

	handler := &mockHandler{
		jobType:  "panic_job",
		handleFn: func(_ context.Context, _ jobs.Job) error { panic("job handler blew up") },
	}

	w := jobs.NewWorker(q, log, 50*time.Millisecond)
	w.Register(handler)

	job, _ := jobs.NewJob("j1", "panic_job", nil)
	_ = q.Enqueue(context.Background(), job)

	// A second, ordinary job enqueued after the panicking one — if the
	// panic actually killed the worker loop, this would never get
	// processed at all.
	job2, _ := jobs.NewJob("j2", "panic_job", nil)
	_ = q.Enqueue(context.Background(), job2)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Start(ctx) // must return normally when ctx expires, not panic out
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return — the panic likely escaped processNext")
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.failed) != 2 {
		t.Errorf("failed = %v, want both j1 and j2 marked failed (worker kept processing after the panic)", q.failed)
	}
}

// TestWorker_OutcomeRecording_SurvivesContextCancellation pins the fix for
// Fail/Complete reusing the worker's poll-loop context: that context is
// exactly what shutdown cancels, so recording a job's outcome on it meant
// a shutdown racing with an in-flight handler could abort the Fail/
// Complete call, leaving the job neither completed nor properly failed.
// The handler below cancels the outer context itself (simulating shutdown
// firing mid-handle) before returning — the resulting Fail/Complete call
// must still see a live (non-cancelled) context.
func TestWorker_OutcomeRecording_SurvivesContextCancellation(t *testing.T) {
	q := &mockQueue{}
	log := &mockLogger{}

	ctx, cancel := context.WithCancel(context.Background())
	handler := &mockHandler{
		jobType: "shutdown_job",
		handleFn: func(_ context.Context, _ jobs.Job) error {
			cancel() // simulate SIGTERM arriving while this job is mid-handle
			return errors.New("boom")
		},
	}

	w := jobs.NewWorker(q, log, 20*time.Millisecond)
	w.Register(handler)

	job, _ := jobs.NewJob("j1", "shutdown_job", nil)
	_ = q.Enqueue(context.Background(), job)

	w.Start(ctx) // returns promptly: ctx is already cancelled by the handler

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.failed) != 1 {
		t.Fatalf("failed = %v, want [j1]", q.failed)
	}
	if len(q.failCtxErrs) != 1 || q.failCtxErrs[0] != nil {
		t.Errorf("Fail's ctx.Err() = %v, want nil — the outcome-recording context must not inherit the poll-loop context's cancellation", q.failCtxErrs)
	}
}
