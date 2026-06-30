package webhook_test

import (
	"testing"

	domainwebhook "github.com/akarso/shopanda/internal/domain/webhook"
)

func TestEndpoint_Validate(t *testing.T) {
	supported := domainwebhook.SupportedEventSet()
	ep := &domainwebhook.Endpoint{
		URL:    "https://example.com/hooks",
		Secret: "secret",
		Events: []string{"order.paid"},
	}
	if err := ep.Validate(supported); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !ep.Subscribed("order.paid") {
		t.Fatal("expected subscription")
	}
}

func TestEndpoint_ValidateRejectsHTTP(t *testing.T) {
	ep := &domainwebhook.Endpoint{
		URL:    "http://example.com/hooks",
		Secret: "secret",
		Events: []string{"order.paid"},
	}
	if err := ep.Validate(domainwebhook.SupportedEventSet()); err == nil {
		t.Fatal("expected http url rejection")
	}
}

func TestEndpoint_ValidateRejectsUnsupportedEvent(t *testing.T) {
	ep := &domainwebhook.Endpoint{
		URL:    "https://example.com/hooks",
		Secret: "secret",
		Events: []string{"catalog.product.created"},
	}
	if err := ep.Validate(domainwebhook.SupportedEventSet()); err == nil {
		t.Fatal("expected unsupported event error")
	}
}
