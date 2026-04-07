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

// Worker polls the queue and dispatches jobs to registered handlers.
type Worker struct {
	queue    Queue
	handlers map[string]Handler
	log      Logger
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
		poll:     pollInterval,
		stop:     make(chan struct{}),
	}
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
		if failErr := w.queue.Fail(ctx, job.ID, err); failErr != nil {
			w.log.Error("worker.fail.error", failErr, map[string]interface{}{"job_id": job.ID})
		}
		return
	}

	if err := w.queue.Complete(ctx, job.ID); err != nil {
		w.log.Error("worker.job.complete_failed", err, map[string]interface{}{
			"job_id": job.ID,
		})
		return
	}

	w.log.Info("worker.job.done", map[string]interface{}{
		"job_id":   job.ID,
		"job_type": job.Type,
	})
}
