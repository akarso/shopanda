package pricing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/domain/pricing"
)

type stubStep struct {
	name string
	fn   func(*pricing.PricingContext) error
}

func (s *stubStep) Name() string                                               { return s.name }
func (s *stubStep) Apply(_ context.Context, ctx *pricing.PricingContext) error { return s.fn(ctx) }

func TestPipelineExecute_NoSteps(t *testing.T) {
	p := pricing.NewPipeline()
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("new context: %v", err)
	}
	if err := p.Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("execute: %v", err)
	}
}

func TestPipelineExecute_SingleStep(t *testing.T) {
	called := false
	step := &stubStep{name: "test", fn: func(ctx *pricing.PricingContext) error {
		called = true
		return nil
	}}
	p := pricing.NewPipeline(step)
	pctx, _ := pricing.NewPricingContext("EUR")
	if err := p.Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !called {
		t.Fatal("step was not called")
	}
}

func TestPipelineExecute_StepOrder(t *testing.T) {
	var order []string
	s1 := &stubStep{name: "first", fn: func(ctx *pricing.PricingContext) error {
		order = append(order, "first")
		return nil
	}}
	s2 := &stubStep{name: "second", fn: func(ctx *pricing.PricingContext) error {
		order = append(order, "second")
		return nil
	}}
	p := pricing.NewPipeline(s1, s2)
	pctx, _ := pricing.NewPricingContext("EUR")
	if err := p.Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("order = %v, want [first second]", order)
	}
}

func TestPipelineExecute_StopOnError(t *testing.T) {
	called := false
	s1 := &stubStep{name: "fail", fn: func(ctx *pricing.PricingContext) error {
		return errors.New("boom")
	}}
	s2 := &stubStep{name: "skip", fn: func(ctx *pricing.PricingContext) error {
		called = true
		return nil
	}}
	p := pricing.NewPipeline(s1, s2)
	pctx, _ := pricing.NewPricingContext("EUR")
	err := p.Execute(context.Background(), &pctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("second step should not have been called")
	}
}

func TestPipelineExecute_ErrorWrapsStepName(t *testing.T) {
	step := &stubStep{name: "broken", fn: func(ctx *pricing.PricingContext) error {
		return errors.New("whoops")
	}}
	p := pricing.NewPipeline(step)
	pctx, _ := pricing.NewPricingContext("EUR")
	err := p.Execute(context.Background(), &pctx)
	if err == nil {
		t.Fatal("expected error")
	}
	want := `pipeline: step "broken": whoops`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}
