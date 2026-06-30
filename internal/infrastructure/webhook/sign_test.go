package webhook_test

import (
	"testing"

	webhookinfra "github.com/akarso/shopanda/internal/infrastructure/webhook"
)

func TestSignBody_Deterministic(t *testing.T) {
	body := []byte(`{"event":"order.paid"}`)
	a := webhookinfra.SignBody("secret", body)
	b := webhookinfra.SignBody("secret", body)
	if a != b || a == "" {
		t.Fatalf("signatures = %q %q", a, b)
	}
}
