package payment

import "errors"

// ErrSignatureMissing indicates the request had no signature header.
var ErrSignatureMissing = errors.New("payment: webhook signature missing")

// ErrSignatureInvalid indicates the signature did not match.
var ErrSignatureInvalid = errors.New("payment: webhook signature invalid")

// WebhookVerifier verifies the authenticity of an incoming webhook request.
type WebhookVerifier interface {
	// Verify checks that signature is valid for the given raw body and provider.
	// Returns ErrSignatureMissing when signature is empty, ErrSignatureInvalid
	// when verification fails, or nil when the signature is valid.
	Verify(provider string, signature string, body []byte) error
}
