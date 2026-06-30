package webhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	webhookinfra "github.com/akarso/shopanda/internal/infrastructure/webhook"
)

func TestSignBody_Deterministic(t *testing.T) {
	body := []byte(`{"event":"order.paid"}`)
	secret := "secret"

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))

	got := webhookinfra.SignBody(secret, body)
	if got != want {
		t.Fatalf("SignBody = %q, want %q", got, want)
	}
}
