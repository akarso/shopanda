package checkout

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/metrics"
)

// Event constants for checkout workflow observability.
const (
	EventStepStarted       = "checkout.step.started"
	EventStepCompleted     = "checkout.step.completed"
	EventCheckoutFailed    = "checkout.failed"
	EventCheckoutCompleted = "checkout.completed"
)

// StepStartedData is the event payload for step lifecycle events.
type StepStartedData struct {
	CartID   string
	StepName string
}

// StepCompletedData is the event payload for step completion.
type StepCompletedData struct {
	CartID   string
	StepName string
}

// CheckoutFailedData is the event payload when checkout fails.
type CheckoutFailedData struct {
	CartID   string
	StepName string
	Error    string
}

// CheckoutCompletedData is the event payload when checkout succeeds.
type CheckoutCompletedData struct {
	CartID  string
	OrderID string
}

// Workflow executes a sequence of Steps against a Context.
type Workflow struct {
	steps   []Step
	bus     *event.Bus
	log     logger.Logger
	metrics metrics.Recorder
	tracer  trace.Tracer
}

// NewWorkflow creates a Workflow with the given steps.
// Steps execute in the order provided.
//
// The tracer is resolved via otel.Tracer(...) here, at construction time
// (once per process, when cmd/api wires the checkout workflow) rather than
// cached in a package variable — see the equivalent note on
// shared.TracingMiddleware for why: a Tracer handle obtained before
// tracing.Setup installs a real SDK provider only migrates to it once
// (OTel's internal sync.Once delegation) and would otherwise stay bound to
// whatever was global at that first call. Constructing Workflow after
// Setup has run avoids that pitfall entirely. Before Setup runs (or when
// tracing is disabled), this is OTel's documented no-op tracer.
func NewWorkflow(steps []Step, bus *event.Bus, log logger.Logger) *Workflow {
	if bus == nil {
		panic("checkout: bus must not be nil")
	}
	if log == nil {
		panic("checkout: logger must not be nil")
	}
	return &Workflow{
		steps:   steps,
		bus:     bus,
		log:     log,
		metrics: metrics.Noop(),
		tracer:  otel.Tracer("github.com/akarso/shopanda/internal/application/checkout"),
	}
}

// WithMetrics sets the metrics recorder used to record checkout outcomes.
// Optional; if never called, outcomes are simply not recorded. Returns the
// Workflow for chaining.
//
// Not safe to call concurrently with Execute or with another WithMetrics
// call: the field it sets is read without synchronization on the checkout
// path. Call it once during wiring, before the Workflow is used to process
// requests.
func (w *Workflow) WithMetrics(m metrics.Recorder) *Workflow {
	if m != nil {
		w.metrics = m
	}
	return w
}

// publishEvent publishes an event and logs + returns any error from sync handlers.
func (w *Workflow) publishEvent(ctx context.Context, name, source string, data interface{}) error {
	if err := w.bus.Publish(ctx, event.New(name, source, data)); err != nil {
		w.log.Error("checkout.publish.failed", err, map[string]interface{}{
			"event": name,
		})
		return fmt.Errorf("checkout: publish %s: %w", name, err)
	}
	return nil
}

// Execute runs every step in sequence. It stops on the first error
// and emits lifecycle events for observability.
func (w *Workflow) Execute(ctx context.Context, cctx *Context) (err error) {
	// succeededEventFailed is set only when every step succeeded and the
	// sole remaining error is the final EventCheckoutCompleted publish —
	// distinct from a real checkout failure for metrics purposes (see
	// metrics.OutcomeSucceededEventFailed).
	succeededEventFailed := false
	// span is assigned below, after this defer is registered but before
	// anything that could legitimately panic — it must be declared here
	// (not via := at Start) so the recover branch can safely check it even
	// if a panic somehow occurred before assignment.
	var span trace.Span
	defer func() {
		if r := recover(); r != nil {
			w.metrics.CheckoutResult(metrics.OutcomeFailed)
			if span != nil {
				span.RecordError(fmt.Errorf("panic: %v", r))
				span.SetStatus(codes.Error, "panic")
				span.End()
			}
			panic(r)
		}
		switch {
		case succeededEventFailed:
			w.metrics.CheckoutResult(metrics.OutcomeSucceededEventFailed)
			span.SetAttributes(attribute.Bool("checkout.event_publish_failed", true))
			span.SetStatus(codes.Ok, "")
		case err != nil:
			w.metrics.CheckoutResult(metrics.OutcomeFailed)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		default:
			w.metrics.CheckoutResult(metrics.OutcomeSuccess)
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}()

	// cctx == nil is a contract case every Step already guards against
	// (returning a normal error, not panicking) — this must not dereference
	// cctx.CartID ahead of that, or the recover above would never run.
	cartID := ""
	if cctx != nil {
		cartID = cctx.CartID
	}
	ctx, span = w.tracer.Start(ctx, "checkout.execute", trace.WithAttributes(
		attribute.String("checkout.cart_id", cartID),
	))

	for _, step := range w.steps {
		select {
		case <-ctx.Done():
			// Intentionally counted as OutcomeFailed, not a separate
			// "cancelled" outcome: the customer did not get a completed
			// order either way, and client-cancel/timeout should still
			// surface in the failure rate an operator watches.
			return ctx.Err()
		default:
		}

		w.log.Info(EventStepStarted, map[string]interface{}{
			"cart_id": cctx.CartID,
			"step":    step.Name(),
		})
		if err := w.publishEvent(ctx, EventStepStarted, "checkout.workflow", StepStartedData{
			CartID:   cctx.CartID,
			StepName: step.Name(),
		}); err != nil {
			return err
		}

		stepCtx, stepSpan := w.tracer.Start(ctx, "checkout.step."+step.Name())
		stepErr := step.Execute(stepCtx, cctx)
		if stepErr != nil {
			stepSpan.RecordError(stepErr)
			stepSpan.SetStatus(codes.Error, stepErr.Error())
		} else {
			stepSpan.SetStatus(codes.Ok, "")
		}
		stepSpan.End()

		if stepErr != nil {
			cctx.Trace = append(cctx.Trace, TraceEntry{
				Step:   step.Name(),
				Status: "error",
				Err:    stepErr.Error(),
			})
			w.log.Error("checkout.step.failed", stepErr, map[string]interface{}{
				"cart_id": cctx.CartID,
				"step":    step.Name(),
			})
			if isContextError(stepErr) {
				return stepErr
			}
			if pubErr := w.publishEvent(ctx, EventCheckoutFailed, "checkout.workflow", CheckoutFailedData{
				CartID:   cctx.CartID,
				StepName: step.Name(),
				Error:    stepErr.Error(),
			}); pubErr != nil {
				return fmt.Errorf("checkout: step %q failed: %w (publish: %v)", step.Name(), stepErr, pubErr)
			}
			return fmt.Errorf("checkout: step %q failed: %w", step.Name(), stepErr)
		}

		cctx.Trace = append(cctx.Trace, TraceEntry{
			Step:   step.Name(),
			Status: "ok",
		})
		w.log.Info(EventStepCompleted, map[string]interface{}{
			"cart_id": cctx.CartID,
			"step":    step.Name(),
		})
		if err := w.publishEvent(ctx, EventStepCompleted, "checkout.workflow", StepCompletedData{
			CartID:   cctx.CartID,
			StepName: step.Name(),
		}); err != nil {
			return err
		}
	}

	w.log.Info(EventCheckoutCompleted, map[string]interface{}{
		"cart_id": cctx.CartID,
	})
	orderID := ""
	if cctx.Order != nil {
		orderID = cctx.Order.ID
	}
	if pubErr := w.publishEvent(ctx, EventCheckoutCompleted, "checkout.workflow", CheckoutCompletedData{
		CartID:  cctx.CartID,
		OrderID: orderID,
	}); pubErr != nil {
		succeededEventFailed = true
		return pubErr
	}

	return nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// Steps returns the number of registered steps.
func (w *Workflow) Steps() int {
	return len(w.steps)
}
