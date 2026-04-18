package stripepay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/payment"
)

const defaultTolerance = 5 * time.Minute

// VerifySignature verifies a Stripe webhook signature header against the raw
// payload and webhook secret. The header format is:
//
//	Stripe-Signature: t=<unix_timestamp>,v1=<hmac_hex>[,v1=<hmac_hex>]
//
// The expected signature is HMAC-SHA256("{timestamp}.{payload}", secret).
// Returns payment.ErrSignatureMissing when the header is empty,
// payment.ErrSignatureInvalid on verification failure.
func VerifySignature(secret string, header string, payload []byte) error {
	return verifySignatureWithClock(secret, header, payload, defaultTolerance, time.Now)
}

func verifySignatureWithClock(secret, header string, payload []byte, tolerance time.Duration, now func() time.Time) error {
	if header == "" {
		return payment.ErrSignatureMissing
	}

	parts := strings.Split(header, ",")
	var timestamp string
	var signatures []string
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			timestamp = kv[1]
		case "v1":
			signatures = append(signatures, kv[1])
		}
	}

	if timestamp == "" || len(signatures) == 0 {
		return payment.ErrSignatureInvalid
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return payment.ErrSignatureInvalid
	}

	diff := now().Sub(time.Unix(ts, 0))
	if diff < -tolerance || diff > tolerance {
		return payment.ErrSignatureInvalid
	}

	// Expected: HMAC-SHA256("{t}.{payload}", secret)
	signed := fmt.Sprintf("%s.%s", timestamp, payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signed))
	expected := hex.EncodeToString(mac.Sum(nil))

	for _, sig := range signatures {
		if hmac.Equal([]byte(sig), []byte(expected)) {
			return nil
		}
	}

	return payment.ErrSignatureInvalid
}
