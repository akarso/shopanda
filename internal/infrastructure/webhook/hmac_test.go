package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/domain/payment"
)

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestHMACVerifier_ValidSignature(t *testing.T) {
	v := NewHMACVerifier(map[string]string{"stripe": "s3cret"})
	body := `{"id":"evt_1"}`
	sig := sign("s3cret", body)

	if err := v.Verify("stripe", sig, []byte(body)); err != nil {
		t.Fatalf("Verify() = %v, want nil", err)
	}
}

func TestHMACVerifier_InvalidSignature(t *testing.T) {
	v := NewHMACVerifier(map[string]string{"stripe": "s3cret"})
	body := `{"id":"evt_1"}`

	err := v.Verify("stripe", "deadbeef", []byte(body))
	if !errors.Is(err, payment.ErrSignatureInvalid) {
		t.Fatalf("Verify() = %v, want ErrSignatureInvalid", err)
	}
}

func TestHMACVerifier_MissingSignature(t *testing.T) {
	v := NewHMACVerifier(map[string]string{"stripe": "s3cret"})

	err := v.Verify("stripe", "", []byte(`{"id":"evt_1"}`))
	if !errors.Is(err, payment.ErrSignatureMissing) {
		t.Fatalf("Verify() = %v, want ErrSignatureMissing", err)
	}
}

func TestHMACVerifier_NoSecretForProvider(t *testing.T) {
	v := NewHMACVerifier(map[string]string{"stripe": "s3cret"})

	// "manual" has no secret - verification is skipped
	if err := v.Verify("manual", "", []byte(`{}`)); err != nil {
		t.Fatalf("Verify() = %v, want nil (skip)", err)
	}
}

func TestHMACVerifier_NilSecretsMap(t *testing.T) {
	v := NewHMACVerifier(nil)

	if err := v.Verify("manual", "", []byte(`{}`)); err != nil {
		t.Fatalf("Verify() = %v, want nil", err)
	}
}

func TestHMACVerifier_TamperedBody(t *testing.T) {
	v := NewHMACVerifier(map[string]string{"stripe": "s3cret"})
	body := `{"id":"evt_1"}`
	sig := sign("s3cret", body)

	tampered := `{"id":"evt_2"}`
	err := v.Verify("stripe", sig, []byte(tampered))
	if !errors.Is(err, payment.ErrSignatureInvalid) {
		t.Fatalf("Verify() = %v, want ErrSignatureInvalid", err)
	}
}
