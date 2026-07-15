package extapi

// Integration auth header names (Phase 8 inbound ERP).
const (
	IntegrationHeaderAPIKey     = "X-Integration-Key"
	IntegrationHeaderTimestamp  = "X-Integration-Timestamp"
	IntegrationHeaderNonce      = "X-Integration-Nonce"
	IntegrationHeaderSignature  = "X-Integration-Signature"
)

// Integration auth error codes for structured error responses.
const (
	IntegrationErrorAuthMissingKey       = "auth.missing_key"
	IntegrationErrorAuthInvalidKey       = "auth.invalid_key"
	IntegrationErrorAuthMissingSignature = "auth.missing_signature"
	IntegrationErrorAuthInvalidSignature = "auth.invalid_signature"
	IntegrationErrorAuthExpiredTimestamp = "auth.expired_timestamp"
	IntegrationErrorAuthReplayDetected   = "auth.replay_detected"
)
