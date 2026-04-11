package rule_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/akarso/shopanda/internal/domain/rule"
)

// testCtx is a minimal context for testing rule primitives.
type testCtx struct {
	Value  int
	Labels []string
}

// --- Condition tests ---

func TestConditionFunc_True(t *testing.T) {
	cond := rule.ConditionFunc[testCtx](func(ctx testCtx) bool {
		return ctx.Value > 0
	})
	if !cond.Evaluate(testCtx{Value: 1}) {
		t.Error("expected true for Value=1")
	}
}

func TestConditionFunc_False(t *testing.T) {
	cond := rule.ConditionFunc[testCtx](func(ctx testCtx) bool {
		return ctx.Value > 0
	})
	if cond.Evaluate(testCtx{Value: 0}) {
		t.Error("expected false for Value=0")
	}
}

// --- Rule tests ---

func TestRule_AppliesWhenConditionTrue(t *testing.T) {
	r := rule.Rule[testCtx]{
		Name: "double",
		Condition: rule.ConditionFunc[testCtx](func(ctx testCtx) bool {
			return ctx.Value > 0
		}),
		Apply: func(ctx *testCtx) error {
			ctx.Value *= 2
			return nil
		},
	}

	ctx := testCtx{Value: 5}
	if !r.Condition.Evaluate(ctx) {
		t.Fatal("condition should match")
	}
	if err := r.Apply(&ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if ctx.Value != 10 {
		t.Errorf("Value = %d, want 10", ctx.Value)
	}
}

// --- Executor tests ---

func TestExecutor_Empty(t *testing.T) {
	exec := rule.NewExecutor[testCtx]()
	ctx := testCtx{Value: 42}
	if err := exec.Execute(&ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ctx.Value != 42 {
		t.Errorf("Value = %d, want 42 (unchanged)", ctx.Value)
	}
}

func TestExecutor_MatchingRule(t *testing.T) {
	exec := rule.NewExecutor(
		rule.Rule[testCtx]{
			Name:      "add-ten",
			Condition: rule.ConditionFunc[testCtx](func(ctx testCtx) bool { return ctx.Value < 100 }),
			Apply:     func(ctx *testCtx) error { ctx.Value += 10; return nil },
		},
	)

	ctx := testCtx{Value: 5}
	if err := exec.Execute(&ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ctx.Value != 15 {
		t.Errorf("Value = %d, want 15", ctx.Value)
	}
}

func TestExecutor_NonMatchingRule(t *testing.T) {
	exec := rule.NewExecutor(
		rule.Rule[testCtx]{
			Name:      "add-ten",
			Condition: rule.ConditionFunc[testCtx](func(ctx testCtx) bool { return ctx.Value > 100 }),
			Apply:     func(ctx *testCtx) error { ctx.Value += 10; return nil },
		},
	)

	ctx := testCtx{Value: 5}
	if err := exec.Execute(&ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ctx.Value != 5 {
		t.Errorf("Value = %d, want 5 (unchanged)", ctx.Value)
	}
}

func TestExecutor_SequentialOrder(t *testing.T) {
	exec := rule.NewExecutor(
		rule.Rule[testCtx]{
			Name:      "append-a",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(ctx *testCtx) error { ctx.Labels = append(ctx.Labels, "a"); return nil },
		},
		rule.Rule[testCtx]{
			Name:      "append-b",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(ctx *testCtx) error { ctx.Labels = append(ctx.Labels, "b"); return nil },
		},
		rule.Rule[testCtx]{
			Name:      "append-c",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(ctx *testCtx) error { ctx.Labels = append(ctx.Labels, "c"); return nil },
		},
	)

	ctx := testCtx{}
	if err := exec.Execute(&ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(ctx.Labels) != 3 || ctx.Labels[0] != "a" || ctx.Labels[1] != "b" || ctx.Labels[2] != "c" {
		t.Errorf("Labels = %v, want [a b c]", ctx.Labels)
	}
}

func TestExecutor_SkipsNonMatching(t *testing.T) {
	exec := rule.NewExecutor(
		rule.Rule[testCtx]{
			Name:      "append-a",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(ctx *testCtx) error { ctx.Labels = append(ctx.Labels, "a"); return nil },
		},
		rule.Rule[testCtx]{
			Name:      "append-b-skip",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return false }),
			Apply:     func(ctx *testCtx) error { ctx.Labels = append(ctx.Labels, "b"); return nil },
		},
		rule.Rule[testCtx]{
			Name:      "append-c",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(ctx *testCtx) error { ctx.Labels = append(ctx.Labels, "c"); return nil },
		},
	)

	ctx := testCtx{}
	if err := exec.Execute(&ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(ctx.Labels) != 2 || ctx.Labels[0] != "a" || ctx.Labels[1] != "c" {
		t.Errorf("Labels = %v, want [a c]", ctx.Labels)
	}
}

func TestExecutor_ErrorHaltsExecution(t *testing.T) {
	exec := rule.NewExecutor(
		rule.Rule[testCtx]{
			Name:      "append-a",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(ctx *testCtx) error { ctx.Labels = append(ctx.Labels, "a"); return nil },
		},
		rule.Rule[testCtx]{
			Name:      "fail",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(_ *testCtx) error { return errors.New("boom") },
		},
		rule.Rule[testCtx]{
			Name:      "append-c",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(ctx *testCtx) error { ctx.Labels = append(ctx.Labels, "c"); return nil },
		},
	)

	ctx := testCtx{}
	err := exec.Execute(&ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	// Only "a" should have been applied; "c" should not.
	if len(ctx.Labels) != 1 || ctx.Labels[0] != "a" {
		t.Errorf("Labels = %v, want [a]", ctx.Labels)
	}
}

func TestExecutor_ErrorWrapsRuleName(t *testing.T) {
	exec := rule.NewExecutor(
		rule.Rule[testCtx]{
			Name:      "my-rule",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(_ *testCtx) error { return errors.New("oops") },
		},
	)

	ctx := testCtx{}
	err := exec.Execute(&ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	want := `rule "my-rule": oops`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestExecutor_ConditionSeesCurrentState(t *testing.T) {
	// First rule mutates Value; second rule's condition should see the mutated value.
	exec := rule.NewExecutor(
		rule.Rule[testCtx]{
			Name:      "set-200",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(ctx *testCtx) error { ctx.Value = 200; return nil },
		},
		rule.Rule[testCtx]{
			Name:      "only-if-big",
			Condition: rule.ConditionFunc[testCtx](func(ctx testCtx) bool { return ctx.Value >= 200 }),
			Apply:     func(ctx *testCtx) error { ctx.Labels = append(ctx.Labels, "big"); return nil },
		},
	)

	ctx := testCtx{Value: 1}
	if err := exec.Execute(&ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ctx.Value != 200 {
		t.Errorf("Value = %d, want 200", ctx.Value)
	}
	if len(ctx.Labels) != 1 || ctx.Labels[0] != "big" {
		t.Errorf("Labels = %v, want [big]", ctx.Labels)
	}
}

func TestExecutor_MultipleRulesMutate(t *testing.T) {
	exec := rule.NewExecutor(
		rule.Rule[testCtx]{
			Name:      "add-1",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(ctx *testCtx) error { ctx.Value++; return nil },
		},
		rule.Rule[testCtx]{
			Name:      "add-1-again",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(ctx *testCtx) error { ctx.Value++; return nil },
		},
		rule.Rule[testCtx]{
			Name:      "add-1-final",
			Condition: rule.ConditionFunc[testCtx](func(_ testCtx) bool { return true }),
			Apply:     func(ctx *testCtx) error { ctx.Value++; return nil },
		},
	)

	ctx := testCtx{Value: 0}
	if err := exec.Execute(&ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ctx.Value != 3 {
		t.Errorf("Value = %d, want 3", ctx.Value)
	}
}

// TestExecutor_PricingAnalog demonstrates the pattern that will be used
// by the promotion system: condition checks a threshold, action applies
// a discount.
func TestExecutor_PricingAnalog(t *testing.T) {
	type orderCtx struct {
		Subtotal int
		Discount int
	}

	tenPercentOver100 := rule.Rule[orderCtx]{
		Name: "10%-over-100",
		Condition: rule.ConditionFunc[orderCtx](func(ctx orderCtx) bool {
			return ctx.Subtotal > 10000
		}),
		Apply: func(ctx *orderCtx) error {
			ctx.Discount = ctx.Subtotal / 10
			return nil
		},
	}

	exec := rule.NewExecutor(tenPercentOver100)

	t.Run("below threshold", func(t *testing.T) {
		ctx := orderCtx{Subtotal: 5000}
		if err := exec.Execute(&ctx); err != nil {
			t.Fatal(err)
		}
		if ctx.Discount != 0 {
			t.Errorf("Discount = %d, want 0", ctx.Discount)
		}
	})

	t.Run("above threshold", func(t *testing.T) {
		ctx := orderCtx{Subtotal: 20000}
		if err := exec.Execute(&ctx); err != nil {
			t.Fatal(err)
		}
		if ctx.Discount != 2000 {
			t.Errorf("Discount = %d, want 2000", ctx.Discount)
		}
	})
}

// Verify ConditionFunc satisfies the Condition interface at compile time.
var _ rule.Condition[int] = rule.ConditionFunc[int](nil)

func ExampleExecutor() {
	type cart struct {
		Total    int
		Discount int
	}

	exec := rule.NewExecutor(
		rule.Rule[cart]{
			Name: "5-off-over-50",
			Condition: rule.ConditionFunc[cart](func(c cart) bool {
				return c.Total > 5000
			}),
			Apply: func(c *cart) error {
				c.Discount = 500
				return nil
			},
		},
	)

	c := cart{Total: 7500}
	_ = exec.Execute(&c)
	fmt.Printf("discount=%d\n", c.Discount)
	// Output: discount=500
}
