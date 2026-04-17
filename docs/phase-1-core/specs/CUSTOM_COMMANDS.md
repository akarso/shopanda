## 19. Custom Commands

---

CLI supports custom commands via registration.

---

### Command Definition

```go
type Command struct {
    Name        string
    Description string
    Run         func(ctx Context, args []string) error
}
```

---

---

### Registration

```go
RegisterCommand(cmd Command)
```

---

---

### Naming Convention

```text
<domain>:<action>
```

---

Examples:

```text
cache:clear
search:reindex
stripe:sync
```

---

---

### Plugin Integration

Plugins may register commands during Init():

```go
func (p Plugin) Init(app *App) error {
    RegisterCommand(...)
}
```

---

---

### Constraints

* command names must be unique
* commands must be explicit
* no automatic discovery

---

---

### Summary

Custom commands provide:

> a simple and extensible way to extend CLI functionality without introducing complexity.

---
