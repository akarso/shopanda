# Shopanda Code Conventions

## HTTP Handler Pattern
- Struct holding dependencies, constructor with nil panic guards
- Endpoint methods return http.HandlerFunc
- Request body: json.NewDecoder(r.Body).Decode(&req)
- Path params: r.PathValue("paramName") (Go 1.22 native ServeMux)
- Success: JSON(w, status, data) — wraps in Response{Data: data}
- Error: JSONError(w, err) — maps apperror codes to HTTP status
- Pagination: parsePagination(r) → (offset, limit, error), default 20, max 100

## Route Registration
- router.HandleFunc("METHOD /path", handler) for unprotected
- router.Handle("METHOD /path", requireAuth(handler)) for authenticated
- router.Handle("METHOD /path", requireAdmin(handler)) for admin
- Public routes: products list/get, categories, payment webhooks
- Webhook routes are PUBLIC (no auth)

## Middleware Chain (in order)
1. RecoveryMiddleware — catch panics → 500
2. RequestIDMiddleware — assign unique request ID
3. LoggingMiddleware — structured logging
4. AuthMiddleware — extract JWT, set Identity in context

## Event Pattern
- `_ = bus.Publish(ctx, event.New(name, source, data))` — discard publish errors
- Event names: "domain.entity.action" (e.g. "catalog.product.created")
- On() for sync handlers (errors abort), OnAsync() for async (errors logged only)

## Error Handling
- apperror.Validation(msg) → 422
- apperror.NotFound(msg) → 404
- apperror.Conflict(msg) → 409
- apperror.Internal(msg) → 500
- apperror.Wrap(code, msg, err) for wrapping
- All take single string argument (no fmt-style)

## Postgres Repository Pattern
- hydrateXxx scan functions (work with both *sql.Row and *sql.Rows)
- Constraint-name switch for 23505 (unique violation) and 23503 (FK violation) errors
- Deferred field mutation (e.g. UpdatedAt only set after successful DB update)
- WithTx(tx) returns a copy with transaction-scoped connection
- RowsAffected() == 0 → NotFound for updates/deletes

## Logger Signatures
- Info(event string, metadata map[string]string)
- Warn(event string, metadata map[string]string)
- Error(event string, err error, metadata map[string]string) — 3 args for Error
- No PII in logs

## Auth System
- JWT HMAC-SHA256 with `gen` claim for token generation tracking
- ValidatingTokenParser checks gen against customer record on each request
- issuer.Create(subject, role string, gen int64) → (token, error)
- auth.NewService(customers, resets, jwtIssuer, bus, log, resetTTL)
- Password reset: SHA-256 hashed tokens, 1 hour TTL

## Domain Entity Pattern
- Constructors validate required fields, return (Entity, error)
- NewXxxFromDB for hydration from database (no validation, initializes nil maps)
- Timestamps: time.Now().UTC() in constructors
- Status types: string const with IsValid() method

## Testing
- Table-driven tests with subtests: t.Run(tt.name, ...)
- Mock repos implement interface with minimal stubs
- When interface gains new method, ALL mock implementations must be updated

## Response Format
```json
{"data": {...}, "error": null}
{"data": null, "error": {"code": "...", "message": "..."}}
```
