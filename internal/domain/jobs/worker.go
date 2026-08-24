package jobs

import (
	"context"
	"sync"
	"time"
)

// Logger is the logging interface used by Worker.
type Logger interface {
	Info(msg string, fields map[string]interface{})
	Error(msg string, err error, fields map[string]interface{})
}

// MetricsRecorder records job failure counts. jobType is always one of the
// registered handlers' compile-time type strings, or the fixed sentinel
// "unknown" for a dequeued job whose type matches no registered handler —
// never the raw, unbounded value read off the queue.
type MetricsRecorder interface {
	JobFailure(jobType string)
}

// unknownJobType labels JobFailure for a dequeued job type with no
// registered handler, instead of passing the raw queue value straight
// through as a Prometheus label (unbounded cardinality risk: a malformed
// row or a since-removed job type would otherwise mint a permanent series).
const unknownJobType = "unknown"

// noopMetrics discards recordings; the default until WithMetrics is called,
// so processNext never needs a nil check.
type noopMetrics struct{}

func (noopMetrics) JobFailure(string) {}

// Worker polls the queue and dispatches jobs to registered handlers.
type Worker struct {
	queue    Queue
	handlers map[string]Handler
	log      Logger
	metrics  MetricsRecorder
	poll     time.Duration
	mu       sync.RWMutex
	stop     chan struct{}
	stopOnce sync.Once
}

// NewWorker creates a Worker that polls queue at the given interval.
func NewWorker(queue Queue, log Logger, pollInterval time.Duration) *Worker {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	return &Worker{
		queue:    queue,
		handlers: make(map[string]Handler),
		log:      log,
		metrics:  noopMetrics{},
		poll:     pollInterval,
		stop:     make(chan struct{}),
	}
}

// WithMetrics sets the metrics recorder used to count job failures. Optional
// — if never called, failures are simply not recorded. Returns the Worker
// for chaining.
//
// Not safe to call concurrently with Start or with another WithMetrics call:
// the field it sets is read without synchronization on the processing path.
// Call it once during wiring, before Start.
func (w *Worker) WithMetrics(m MetricsRecorder) *Worker {
	if m != nil {
		w.metrics = m
	}
	return w
}

// Register adds a handler for a job type. Panics on duplicate registration.
func (w *Worker) Register(h Handler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.handlers[h.Type()]; exists {
		panic("jobs.Worker: duplicate handler for type " + h.Type())
	}
	w.handlers[h.Type()] = h
}

// Start begins polling the queue in a blocking loop.
// It returns when the context is cancelled or Stop is called.
func (w *Worker) Start(ctx context.Context) {
	w.mu.RLock()
	nHandlers := len(w.handlers)
	w.mu.RUnlock()

	w.log.Info("worker.started", map[string]interface{}{
		"poll_interval": w.poll.String(),
		"handlers":      nHandlers,
	})

	ticker := time.NewTicker(w.poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("worker.stopped", map[string]interface{}{"reason": "context"})
			return
		case <-w.stop:
			w.log.Info("worker.stopped", map[string]interface{}{"reason": "stop"})
			return
		case <-ticker.C:
			w.processNext(ctx)
		}
	}
}

// Stop signals the worker to stop processing. Safe to call multiple times.
func (w *Worker) Stop() {
	w.stopOnce.Do(func() { close(w.stop) })
}

func (w *Worker) processNext(ctx context.Context) {
	job, err := w.queue.Dequeue(ctx)
	if err != nil {
		w.log.Error("worker.dequeue.failed", err, nil)
		return
	}
	if job == nil {
		return
	}

	w.mu.RLock()
	h, ok := w.handlers[job.Type]
	w.mu.RUnlock()

	if !ok {
		w.log.Error("worker.handler.not_found", nil, map[string]interface{}{
			"job_id":   job.ID,
			"job_type": job.Type,
		})
		w.metrics.JobFailure(unknownJobType)
		if failErr := w.queue.Fail(ctx, job.ID, nil); failErr != nil {
			w.log.Error("worker.fail.error", failErr, map[string]interface{}{"job_id": job.ID})
		}
		return
	}

	w.log.Info("worker.job.processing", map[string]interface{}{
		"job_id":   job.ID,
		"job_type": job.Type,
		"attempt":  job.Attempts,
	})

	if err := h.Handle(ctx, *job); err != nil {
		w.log.Error("worker.job.failed", err, map[string]interface{}{
			"job_id":   job.ID,
			"job_type": job.Type,
			"attempt":  job.Attempts,
		})
		w.metrics.JobFailure(job.Type)
		if failErr := w.queue.Fail(ctx, job.ID, err); failErr != nil {
			w.log.Error("worker.fail.error", failErr, map[string]interface{}{"job_id": job.ID})
		}
		return
	}

	if err := w.queue.Complete(ctx, job.ID); err != nil {
		w.log.Error("worker.job.complete_failed", err, map[string]interface{}{
			"job_id": job.ID,
		})
		// The handler itself succeeded, but the job may now be redelivered
		// (e.g. duplicate webhook sends) since the queue never learned it
		// finished — record it as a failure so dashboards aren't silent
		// about a real, actionable problem.
		w.metrics.JobFailure(job.Type)
		return
	}

	w.log.Info("worker.job.done", map[string]interface{}{
		"job_id":   job.ID,
		"job_type": job.Type,
	})
}
