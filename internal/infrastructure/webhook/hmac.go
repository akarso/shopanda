package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"github.com/akarso/shopanda/internal/domain/payment"
)

// Compile-time check.
var _ payment.WebhookVerifier = (*HMACVerifier)(nil)

// HMACVerifier verifies webhook signatures using HMAC-SHA256 with
// per-provider secrets. Providers without a configured secret are
// rejected with ErrSignatureMissing (defence-in-depth: no secret means
// the provider is either unconfigured or signatures are not enforced yet).
type HMACVerifier struct {
	secrets map[string]string // provider → secret
}

// NewHMACVerifier creates an HMACVerifier from a provider→secret map.
func NewHMACVerifier(secrets map[string]string) *HMACVerifier {
	cp := make(map[string]string, len(secrets))
	for k, v := range secrets {
		cp[k] = v
	}
	return &HMACVerifier{secrets: cp}
}

// Verify checks the HMAC-SHA256 signature for a given provider.
// Expected format of signature: hex-encoded HMAC-SHA256 digest.
func (v *HMACVerifier) Verify(provider string, signature string, body []byte) error {
	secret, ok := v.secrets[provider]
	if !ok || secret == "" {
		// No secret configured — skip verification for this provider.
		// This allows providers like "manual" to work without signatures.
		return nil
	}

	if signature == "" {
		return payment.ErrSignatureMissing
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return payment.ErrSignatureInvalid
	}

	return nil
}
