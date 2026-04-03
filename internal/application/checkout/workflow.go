package checkout

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
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
	steps []Step
	bus   *event.Bus
	log   logger.Logger
}

// NewWorkflow creates a Workflow with the given steps.
// Steps execute in the order provided.
func NewWorkflow(steps []Step, bus *event.Bus, log logger.Logger) *Workflow {
	if bus == nil {
		panic("checkout: bus must not be nil")
	}
	if log == nil {
		panic("checkout: logger must not be nil")
	}
	return &Workflow{steps: steps, bus: bus, log: log}
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
func (w *Workflow) Execute(ctx context.Context, cctx *Context) error {
	for _, step := range w.steps {
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

		if err := step.Execute(cctx); err != nil {
			cctx.Trace = append(cctx.Trace, TraceEntry{
				Step:   step.Name(),
				Status: "error",
				Err:    err.Error(),
			})
			w.log.Error("checkout.step.failed", err, map[string]interface{}{
				"cart_id": cctx.CartID,
				"step":    step.Name(),
			})
			if pubErr := w.publishEvent(ctx, EventCheckoutFailed, "checkout.workflow", CheckoutFailedData{
				CartID:   cctx.CartID,
				StepName: step.Name(),
				Error:    err.Error(),
			}); pubErr != nil {
				return fmt.Errorf("checkout: step %q failed: %w (publish: %v)", step.Name(), err, pubErr)
			}
			return fmt.Errorf("checkout: step %q failed: %w", step.Name(), err)
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
	if err := w.publishEvent(ctx, EventCheckoutCompleted, "checkout.workflow", CheckoutCompletedData{
		CartID:  cctx.CartID,
		OrderID: orderID,
	}); err != nil {
		return err
	}

	return nil
}

// Steps returns the number of registered steps.
func (w *Workflow) Steps() int {
	return len(w.steps)
}
