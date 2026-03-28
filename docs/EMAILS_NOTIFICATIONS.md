# 📧 Email & Notification System — v0 Specification

## 1. Overview

Handles:

* transactional emails (orders, password reset)
* notifications triggered by events
* template-based rendering

Design goals:

* event-driven
* template-based
* extensible delivery (SMTP, API)
* customizable templates

---

## 2. Core Concepts

---

### 2.1 Notification

```go
type Notification struct {
    Type      string
    Recipient string

    Data      map[string]interface{}
}
```

---

---

### 2.2 Notifier Interface

---

```go
type Notifier interface {
    Name() string

    Send(n Notification) error
}
```

---

---

## 3. Email Notifier (Core)

---

### SMTP-based

```go
type EmailNotifier struct{}
```

---

---

## 4. Templates

---

### Template structure:

```plaintext
/templates/emails/
  order_confirmation.html
  password_reset.html
```

---

---

### Render:

```go
RenderEmail("order_confirmation", data)
```

---

---

## 5. Triggering Notifications

---

Via events:

```go
On("order.paid", func(e Event) {
    Dispatch(Job{
        Type: "send_email",
        Payload: {
            "template": "order_confirmation",
            "email": e.CustomerEmail,
        },
    })
})
```

---

---

## 6. Template Customization

---

### Sources:

---

#### 1. File-based (default)

* version-controlled
* developer-friendly

---

#### 2. DB-stored (optional)

* editable via admin

---

👉 resolution order:

```text
DB template → file template
```

---

---

## 7. Plugin Extensibility

---

Plugins can:

* register templates
* modify content
* add notification types

---

```go
RegisterNotification("order.shipped", ...)
```

---

---

## 8. Multi-Channel (Future)

---

Same system can support:

* email
* SMS
* push notifications

---

---

## 9. Security

---

* escape HTML
* validate recipients
* avoid sensitive data leaks

---

---

## 10. Non-Goals (v0)

---

* no marketing campaigns
* no template builder UI
* no A/B testing

---

---

## 11. Summary

Notification system v0 provides:

> an event-driven, template-based system for transactional communication.

It ensures:

* decoupling from core logic
* async delivery
* customization

---
