package stripepay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/payment"
)

func makeSignature(secret, timestamp string, payload []byte) string {
	signed := fmt.Sprintf("%s.%s", timestamp, payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signed))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature_Valid(t *testing.T) {
	secret := "whsec_test123"
	payload := []byte(`{"id":"evt_1","type":"payment_intent.succeeded"}`)
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := makeSignature(secret, ts, payload)
	header := fmt.Sprintf("t=%s,v1=%s", ts, sig)

	if err := VerifySignature(secret, header, payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifySignature_MultipleV1(t *testing.T) {
	secret := "whsec_test123"
	payload := []byte(`{"id":"evt_1"}`)
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := makeSignature(secret, ts, payload)
	header := fmt.Sprintf("t=%s,v1=badhex,v1=%s", ts, sig)

	if err := VerifySignature(secret, header, payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifySignature_EmptyHeader(t *testing.T) {
	err := VerifySignature("secret", "", []byte("body"))
	if !errors.Is(err, payment.ErrSignatureMissing) {
		t.Fatalf("want ErrSignatureMissing, got %v", err)
	}
}

func TestVerifySignature_NoTimestamp(t *testing.T) {
	err := VerifySignature("secret", "v1=abc123", []byte("body"))
	if !errors.Is(err, payment.ErrSignatureInvalid) {
		t.Fatalf("want ErrSignatureInvalid, got %v", err)
	}
}

func TestVerifySignature_NoV1(t *testing.T) {
	err := VerifySignature("secret", "t=12345", []byte("body"))
	if !errors.Is(err, payment.ErrSignatureInvalid) {
		t.Fatalf("want ErrSignatureInvalid, got %v", err)
	}
}

func TestVerifySignature_BadTimestamp(t *testing.T) {
	err := VerifySignature("secret", "t=notanumber,v1=abc", []byte("body"))
	if !errors.Is(err, payment.ErrSignatureInvalid) {
		t.Fatalf("want ErrSignatureInvalid, got %v", err)
	}
}

func TestVerifySignature_WrongSignature(t *testing.T) {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	header := fmt.Sprintf("t=%s,v1=0000000000000000000000000000000000000000000000000000000000000000", ts)
	err := VerifySignature("secret", header, []byte("body"))
	if !errors.Is(err, payment.ErrSignatureInvalid) {
		t.Fatalf("want ErrSignatureInvalid, got %v", err)
	}
}

func TestVerifySignature_ExpiredTimestamp(t *testing.T) {
	secret := "whsec_test123"
	payload := []byte(`{"id":"evt_1"}`)
	// Timestamp 10 minutes ago — beyond 5 min tolerance.
	ts := fmt.Sprintf("%d", time.Now().Add(-10*time.Minute).Unix())
	sig := makeSignature(secret, ts, payload)
	header := fmt.Sprintf("t=%s,v1=%s", ts, sig)

	err := VerifySignature(secret, header, payload)
	if !errors.Is(err, payment.ErrSignatureInvalid) {
		t.Fatalf("want ErrSignatureInvalid, got %v", err)
	}
}

func TestVerifySignature_FutureTimestamp(t *testing.T) {
	secret := "whsec_test123"
	payload := []byte(`{"id":"evt_1"}`)
	// Timestamp 10 minutes in the future.
	ts := fmt.Sprintf("%d", time.Now().Add(10*time.Minute).Unix())
	sig := makeSignature(secret, ts, payload)
	header := fmt.Sprintf("t=%s,v1=%s", ts, sig)

	err := VerifySignature(secret, header, payload)
	if !errors.Is(err, payment.ErrSignatureInvalid) {
		t.Fatalf("want ErrSignatureInvalid, got %v", err)
	}
}

func TestVerifySignature_WithinTolerance(t *testing.T) {
	secret := "whsec_test123"
	payload := []byte(`{"id":"evt_1"}`)
	// Timestamp 2 minutes ago — within 5 min tolerance.
	ts := fmt.Sprintf("%d", time.Now().Add(-2*time.Minute).Unix())
	sig := makeSignature(secret, ts, payload)
	header := fmt.Sprintf("t=%s,v1=%s", ts, sig)

	if err := VerifySignature(secret, header, payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
