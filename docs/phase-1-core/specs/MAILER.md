# 📧 Mailer System — v0 Specification

## 1. Overview

Provides:

* email sending capability
* async delivery via queue
* pluggable providers

---

## 2. Architecture

---

```text
enqueue → worker → mailer → provider
```

---

---

## 3. Message

---

```go
type Message struct {
    To      string
    Subject string
    Body    string
}
```

---

---

## 4. Mailer Interface

---

```go
type Mailer interface {
    Send(ctx context.Context, msg Message) error
}
```

---

---

## 5. Default Implementation

---

* SMTP-based
* minimal configuration

---

---

## 6. Queue Integration

---

Emails are always sent via queue:

```text
enqueue("email.send")
```

---

---

## 7. Templates

---

* rendered before enqueue
* HTML-based

---

---

## 8. Configuration

---

```yaml
mail:
  driver: smtp
```

---

---

## 9. Extensibility

---

Plugins may provide:

* external providers
* tracking
* analytics

---

---

## 10. Constraints

---

* no synchronous sending
* no provider lock-in
* no tracking in core

---

---

## 11. Summary

Mailer system provides:

> a simple, reliable, and scalable approach to email delivery using async processing and pluggable providers.

---
