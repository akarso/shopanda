# JWT Implementation — Decision Note

## Why custom HMAC-SHA256 instead of an external library?

1. **Minimal dependency footprint**: The only cryptographic primitive needed is
   HMAC-SHA256, which is available in the Go standard library (`crypto/hmac`,
   `crypto/sha256`). Adding a full JWT library (e.g., `golang-jwt/jwt`) would
   pull in a transitive dependency tree for features we do not need (RSA, ECDSA,
   EdDSA, JWK, JWE, etc.).

2. **Auditability**: A ~100-line implementation is easier to review and reason
   about than a multi-thousand-line library with historical CVEs.

3. **Single algorithm**: Shopanda uses symmetric HMAC-SHA256 exclusively. There
   is no algorithm negotiation and no `"alg": "none"` attack surface.

## Known tradeoffs

- **No `nbf` / `jti` / audience claims** — not needed for the current auth
  model. Can be added if requirements change.
- **No clock skew tolerance** — tokens expire at the exact `exp` second.
  Acceptable for a single-service deployment.
- **No key rotation** — a single static secret is used. Key rotation would
  require a multi-key verifier (future work).

## Test coverage

See `jwt_test.go` for: round-trip create/parse, expired tokens, signature
tampering (wrong secret), malformed tokens, empty subject validation, empty
secret validation, zero TTL validation, header/alg enforcement, padded base64
payloads, invalid JSON payloads.
