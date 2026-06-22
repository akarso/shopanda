package payment_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/payment"
)

type stubPayProvider struct {
	method payment.PaymentMethod
}

func (s *stubPayProvider) Method() payment.PaymentMethod { return s.method }

func (s *stubPayProvider) Initiate(_ context.Context, _ *payment.Payment) (payment.ProviderResult, error) {
	return payment.ProviderResult{}, nil
}

func TestProviderRegistry_RegisterAndResolve(t *testing.T) {
	reg := payment.NewProviderRegistry()
	manual := &stubPayProvider{method: payment.MethodManual}
	stripe := &stubPayProvider{method: payment.MethodStripe}

	reg.Register(manual)
	reg.Register(stripe)

	if reg.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", reg.Len())
	}

	p, err := reg.Resolve("stripe")
	if err != nil {
		t.Fatalf("Resolve(stripe) error: %v", err)
	}
	if p.Method() != payment.MethodStripe {
		t.Fatalf("Method() = %q, want stripe", p.Method())
	}

	def, err := reg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve('') error: %v", err)
	}
	if def.Method() != payment.MethodManual {
		t.Fatalf("default Method() = %q, want manual", def.Method())
	}
}

func TestProviderRegistry_DuplicateMethodPanics(t *testing.T) {
	reg := payment.NewProviderRegistry()
	reg.Register(&stubPayProvider{method: payment.MethodManual})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for duplicate method registration")
		}
	}()
	reg.Register(&stubPayProvider{method: payment.MethodManual})
}

func TestProviderRegistry_ResolveUnavailable(t *testing.T) {
	reg := payment.NewProviderRegistry()
	reg.Register(&stubPayProvider{method: payment.MethodManual})

	if _, err := reg.Resolve("stripe"); err == nil {
		t.Fatal("Resolve(stripe) expected error")
	}
}
