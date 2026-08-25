package checkout

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/akarso/shopanda/internal/platform/apperror"
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

// runStep executes step inside its own child span, guaranteeing the span
// is always ended — including when step.Execute panics. A straight-line
// "Start, call, End" sequence (the previous shape of this code) leaks the
// span on panic: End() sits after the call, so a panic skips it entirely,
// and only the outer root span (Execute's own recover) ever closes. This
// re-panics after recording the panic on the step's own span, so the
// panic still propagates to Execute's recover exactly as before — the
// step span now just correctly reflects the panic before that happens.
func (w *Workflow) runStep(ctx context.Context, step Step, cctx *Context) (err error) {
	stepCtx, stepSpan := w.tracer.Start(ctx, "checkout.step."+step.Name())
	defer func() {
		if r := recover(); r != nil {
			// Fixed, non-sensitive text, not fmt.Errorf("panic: %v", r):
			// a recovered panic value can be anything a step's business
			// logic constructed, including customer or payment-processor
			// detail — the same reasoning spanSafeError already applies
			// to normal errors. The original value still propagates via
			// panic(r) below for logging/recovery elsewhere; only the
			// span recording is bounded.
			stepSpan.RecordError(errors.New("panic"))
			stepSpan.SetStatus(codes.Error, "panic")
			stepSpan.End()
			panic(r)
		}
		if err != nil {
			safeErr := spanSafeError(err)
			stepSpan.RecordError(safeErr)
			stepSpan.SetStatus(codes.Error, safeErr.Error())
		} else {
			stepSpan.SetStatus(codes.Ok, "")
		}
		stepSpan.End()
	}()
	return step.Execute(stepCtx, cctx)
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
				// See runStep's identical recover branch: fixed text,
				// not the raw panic value, which may carry sensitive
				// detail. panic(r) below still propagates it unbounded.
				span.RecordError(errors.New("panic"))
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
			safeErr := spanSafeError(err)
			span.RecordError(safeErr)
			span.SetStatus(codes.Error, safeErr.Error())
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

		stepErr := w.runStep(ctx, step, cctx)
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
		// Deliberately not returned: the order was created and payment
		// captured by this point (every step above already succeeded) —
		// only a downstream notification/event-bus subscriber failed to
		// receive the completion event. Returning pubErr here would make
		// checkout.Service.Checkout (which propagates this error as-is)
		// report the checkout as FAILED to its caller, even though the
		// customer's order is real — the HTTP layer would then tell the
		// customer to retry, risking a genuine duplicate order, to work
		// around what is actually a notification-delivery problem.
		// publishEvent already logged this failure, and
		// succeededEventFailed (set below) keeps it visible as its own
		// metrics outcome distinct from both success and a real failure.
		succeededEventFailed = true
		return nil
	}

	return nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// spanSafeError returns a bounded, span-safe substitute for err. err.Error()
// is not guaranteed free of customer or payment-processor detail — any
// wrapped validation or provider error can carry whatever the underlying
// library put in its message — and checkout spans may export to a
// third-party OTLP collector, the same reasoning
// shared.TracingMiddleware already applies to raw URL paths.
//
//   - A context error (isContextError) returns the bare context.Canceled
//     or context.DeadlineExceeded sentinel — fixed, safe strings — not
//     the original error, which errors.Is would still match through an
//     arbitrary wrapping prefix a caller might have added (e.g. a step
//     wrapping ctx.Err() with customer or cart detail in its message).
//   - An *apperror.Error's bounded Code (e.g. "validation", "not_found")
//     is used in place of its free-text Message/wrapped error.
//   - Anything else becomes a fixed generic message.
//
// The exact original text is not lost: publishEvent and every step
// failure path already log the full error via w.log — this only bounds
// what additionally leaves the process through the span exporter.
func spanSafeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return fmt.Errorf("%s", appErr.Code)
	}
	return errors.New("error")
}

// Steps returns the number of registered steps.
func (w *Workflow) Steps() int {
	return len(w.steps)
}
