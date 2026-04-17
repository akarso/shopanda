# 👤 Customer & Auth Domain — v0 Specification

## 1. Overview

This module handles:

* customer accounts
* authentication (login/logout)
* sessions / tokens
* password reset

Design goals:

* simple default (email + password)
* extensible (OAuth, SSO via plugins)
* secure by default
* no framework lock-in

---

## 2. Core Concepts

---

### 2.1 Customer

Represents a user account.

```go
type Customer struct {
    ID        string
    Email     string

    PasswordHash string

    Meta      map[string]interface{}

    CreatedAt time.Time
    UpdatedAt time.Time
}
```

---

### 2.2 Auth Identity (future-ready)

Allows multiple auth methods.

```go
type Identity struct {
    ID          string
    CustomerID  string

    Provider    string // "password", "google", etc.
    Identifier  string // email or external ID

    Credentials map[string]interface{}

    CreatedAt   time.Time
}
```

---

👉 v0 uses only:

* `provider = "password"`

---

## 3. Authentication Methods

---

### v0 Supported:

* email + password

---

### Future (plugins):

* OAuth (Google, Apple)
* magic links
* SSO

---

## 4. Password Handling

---

### Rules:

* store only hashed passwords
* use strong hashing (bcrypt or argon2)

---

### Example:

```go
hash := HashPassword(password)
```

---

### Verification:

```go
ComparePassword(hash, input)
```

---

---

## 5. Sessions vs Tokens

---

## v0: Token-based (stateless)

* JWT-like tokens OR opaque tokens
* no server-side session storage required

---

### Token structure:

```go
type AuthToken struct {
    CustomerID string
    ExpiresAt  time.Time
}
```

---

### Transport:

```http
Authorization: Bearer <token>
```

---

---

## 6. Token Strategy

---

### Option A (recommended v0):

Opaque tokens:

* random string
* stored in DB

```go
type Session struct {
    ID          string
    CustomerID  string
    ExpiresAt   time.Time
}
```

---

👉 Pros:

* revocable
* simple
* secure

---

### Option B (future):

JWT plugin

---

## 7. Registration

---

### Endpoint:

```http
POST /auth/register
```

---

### Input:

```json
{
  "email": "user@example.com",
  "password": "secret"
}
```

---

### Flow:

1. validate input
2. check email uniqueness
3. hash password
4. create customer
5. create identity

---

### Events:

* `customer.created`

---

## 8. Login

---

### Endpoint:

```http
POST /auth/login
```

---

### Input:

```json
{
  "email": "user@example.com",
  "password": "secret"
}
```

---

### Flow:

1. find identity
2. verify password
3. create session/token

---

### Response:

```json
{
  "token": "...",
  "expires_at": "..."
}
```

---

---

## 9. Logout

---

### Endpoint:

```http
POST /auth/logout
```

---

### Behavior:

* invalidate session/token

---

---

## 10. Current User

---

### Endpoint:

```http
GET /auth/me
```

---

Returns:

* customer data

---

---

## 11. Password Reset

---

### Step 1: Request reset

```http
POST /auth/password-reset/request
```

---

* generate token
* send email (event)

---

### Step 2: Confirm reset

```http
POST /auth/password-reset/confirm
```

```json
{
  "token": "...",
  "new_password": "..."
}
```

---

---

## 12. Security Rules

---

* passwords must be hashed
* tokens must expire
* rate limiting (future)
* no sensitive data in responses

---

---

## 13. Extensibility

---

Plugins can:

* add auth providers
* override token strategy
* add MFA
* add custom validation

---

### Example:

```go
RegisterAuthProvider("google", GoogleProvider{})
```

---

---

## 14. Storage (Postgres)

---

### Customers

```sql
customers (
  id UUID PRIMARY KEY,
  email TEXT UNIQUE NOT NULL,
  password_hash TEXT,
  meta JSONB,
  created_at TIMESTAMP,
  updated_at TIMESTAMP
)
```

---

### Identities

```sql
identities (
  id UUID PRIMARY KEY,
  customer_id TEXT,
  provider TEXT,
  identifier TEXT,
  credentials JSONB
)
```

---

### Sessions

```sql
sessions (
  id UUID PRIMARY KEY,
  customer_id TEXT,
  expires_at TIMESTAMP
)
```

---

---

## 15. Non-Goals (v0)

* no OAuth implementation
* no MFA
* no complex permission system
* no social login UI

---

---

## 16. Summary

Auth v0 provides:

> a simple, secure authentication system that can evolve into more complex identity solutions via plugins.

It ensures:

* clean separation of auth logic
* extensibility
* minimal initial complexity

---
