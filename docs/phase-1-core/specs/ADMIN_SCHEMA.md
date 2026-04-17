# ⚙️ Admin Schema — v0 Specification (Minimal, Schema-Driven)

## 1. Overview

Admin Schema v0 defines a **backend-driven, schema-based system** for building admin UI.

It provides:

* form definitions (create/edit)
* grid definitions (list views)
* extension via registration APIs

Design goals:

* extensibility (plugins can add fields/columns/actions)
* simplicity (no UI framework dependency)
* no template overrides
* deterministic structure

---

## 2. Goals

* define admin UI as **data (schemas)**, not templates
* allow plugins to extend forms and grids
* keep backend in control of logic and validation
* enable simple frontend rendering (plain JS or optional library)

---

## 3. Non-Goals (v0)

* no UI rendering implementation
* no layout system
* no permissions/ACL
* no advanced validation engine
* no async actions
* no form nesting/complex components

---

## 4. Core Concepts

---

### 4.1 Form Schema

Represents create/edit UI.

```go id="u1x9b7"
type Form struct {
    Name   string
    Fields []Field
}
```

---

### 4.2 Field Definition

```go id="l3rmk9"
type Field struct {
    Name        string
    Type        string // text, number, select, checkbox
    Label       string

    Required    bool

    Default     interface{}

    Options     []Option // for select

    Meta        map[string]interface{}
}
```

---

### 4.3 Option (for select fields)

```go id="qk6z4p"
type Option struct {
    Label string
    Value string
}
```

---

## 5. Grid Schema

---

### 5.1 Grid Definition

```go id="0tyc2n"
type Grid struct {
    Name    string
    Columns []Column
}
```

---

### 5.2 Column Definition

```go id="4p3u8y"
type Column struct {
    Name    string
    Label   string

    // Value resolver (executed in backend)
    Value   func(row interface{}) interface{}

    Meta    map[string]interface{}
}
```

---

## 6. Actions (Basic)

---

```go id="s1zn3y"
type Action struct {
    Name    string
    Label   string

    Execute func(ids []string) error
}
```

---

## 7. Registry

Central place for schemas.

---

### 7.1 Registration API

```go id="s2o7yk"
func RegisterForm(name string, form Form)
func RegisterFormField(formName string, field Field)

func RegisterGrid(name string, grid Grid)
func RegisterGridColumn(gridName string, column Column)

func RegisterAction(gridName string, action Action)
```

---

### 7.2 Example

```go id="4dfq2p"
RegisterForm("product.form", Form{
    Name: "product.form",
})

RegisterFormField("product.form", Field{
    Name:  "name",
    Type:  "text",
    Label: "Product Name",
    Required: true,
})
```

---

## 8. Data Binding

* Forms map directly to domain models or DTOs
* Fields correspond to:

  * struct fields
  * or `meta` (JSONB)

---

### Example:

```go id="8rty9c"
product.Meta["country_of_origin"]
```

---

## 9. Validation (Basic)

* required fields validated in application layer
* no complex validation system in v0

---

## 10. Extensibility

Plugins can:

* add fields to forms
* add columns to grids
* add actions

---

### Example: plugin adds field

```go id="y2t9fw"
RegisterFormField("product.form", Field{
    Name:  "origin",
    Type:  "text",
    Label: "Country of Origin",
})
```

---

## 11. API Exposure (Future)

Schemas can be exposed via API:

```http id="zq6e8x"
GET /admin/forms/{name}
GET /admin/grids/{name}
```

---

## 12. Rendering (Out of Scope)

Frontend will:

* fetch schema
* render dynamically

Possible approaches:

* plain JS (default)
* optional Svelte plugin

---

## 13. Constraints

* schema must be deterministic
* no side effects in schema definitions
* no runtime mutation during request

---

## 14. File Placement

```plaintext id="0r9d2f"
/internal/admin
  /schema
    form.go
    grid.go
  /registry
    registry.go
```

---

## 15. Future Extensions

* field types (date, relation, rich text)
* validation rules
* permissions/ACL
* layout hints (grouping, tabs)
* async actions

---

## 16. Summary

Admin Schema v0 provides:

> a minimal, declarative way to define admin UI that is fully extensible via backend registration.

It enables:

* plugin-driven admin customization
* no template overrides
* clean separation of logic and UI

---
