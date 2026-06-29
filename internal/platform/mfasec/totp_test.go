package mfasec_test

import (
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/mfasec"
)

func TestValidateTOTP_RoundTrip(t *testing.T) {
	secret, err := mfasec.GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	code, err := mfasec.GenerateTOTPCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateTOTPCode: %v", err)
	}
	if !mfasec.ValidateTOTP(code, secret) {
		t.Fatal("expected valid totp code")
	}
}
