# Customization Platform (Extension Fields, Hooks, Slots, Assets)

Status: proposal  
Audience: core maintainers, plugin authors, integrators

---

## 1) Problem

Shopanda currently supports explicit extension mechanisms (events, pipelines, workflows, plugin routes), but common customizations still feel fragmented:

- "Custom product attribute" has no single end-to-end convention.
- Persisted vs computed data uses different ad hoc patterns.
- Rendering often implies template edits.
- Hooks/slots are static; adding a new one can require core intervention.
- Frontend snippets (CSS/JS/HTML) need a stable injection model independent of full theme overrides.

We need one architecture for "small customization" and "advanced extension" that remains deterministic and explicit.

---

## 2) Goals

1. Define a single **Extension Field** model reusable across entities and view contexts.
2. Support both **public** and **private** extension fields.
3. Expose stable CRUD APIs for extension field definitions and values.
4. Make the same model accessible from:
   - admin UI
   - GraphQL
   - plugin Go code
5. Provide a uniform mechanism for hooks, UI slots, and asset injection.
6. Avoid mandatory theme/core forks for tiny frontend customizations.

---

## 3) Non-goals

- Runtime loading via Go `.so` plugins (see PR-544 research).
- Visual drag-and-drop page builder.
- Full low-code rule engine in this first iteration.

---

## 4) Core Concept: Extension Fields

Extension fields are namespaced, typed definitions with declared scope and lifecycle.

### 4.1 Field Definition

Required attributes:

- `code`: namespaced key, e.g. `acme.gift.wrap_level`
- `label`
- `description`
- `type`: `string|int|bool|enum|json|money|date|datetime`
- `scope`:
  - `entity` (persisted): `product|variant|cart_item|order_item|customer|...`
  - `context` (computed): `pdp|plp_item|cart_view|checkout_view|...`
- `storage_mode`:
  - `stored` (durable value)
  - `computed` (resolver-only, no DB write)
  - `snapshot` (copied to downstream entity on transition)
- `visibility`:
  - `public` (admin-visible/editable if allowed)
  - `private` (hidden in admin by default)
- `access`:
  - `read_roles`
  - `write_roles`
  - optional plugin-only write flag
- `validation`:
  - static constraints (`required`, `min`, `max`, `regex`, enum options)
  - optional validator hook

### 4.2 Private Fields

`visibility = private` means:

- hidden from default admin forms
- excluded from generic admin list endpoints unless explicit privileged include flag
- still available to internal/plugin runtime and authorized API consumers
- auditable in logs via **metadata only** (`field_code`, `target_type`, `target_id`, actor, action, timestamp); payload values MUST NOT be emitted — any log path that touches private field data MUST redact or omit raw values

Private fields solve "merchant must not tinker with every extension."

---

## 5) Value Model

Use one consistent value envelope:

- `field_code`
- `target_type` (`product`, `cart_item`, etc.)
- `target_id`
- typed payload (`string_value`, `int_value`, `json_value`, etc. or normalized JSON + type)
- metadata (`source`, `updated_by`, timestamps)

Recommended persistence:

- JSONB column for fast rollout, with optional side tables/indexes for heavy-query fields.
- For high-cardinality frequently filtered fields, permit promoted indexed storage via migration.

---

## 6) Standard Lifecycle and Propagation

Define deterministic stages:

1. Define field (registry)
2. Write source value (entity or computed resolver)
3. Resolve for context (PDP/PLP/cart/checkout)
4. Snapshot on transitions (e.g. `cart_item -> order_item`)
5. Expose in API/render serializers

Example policy:

- product-level field `acme.option.material` stored on product
- selected value captured as cart item extension
- copied to order item extension at checkout
- shown on cart, checkout, order detail via shared renderer

---

## 7) Stable APIs (REST)

All extension operations must be available through documented stable endpoints.

### 7.1 Registry APIs

- `GET /api/v1/admin/extensions/fields`
- `POST /api/v1/admin/extensions/fields`
- `GET /api/v1/admin/extensions/fields/{code}`
- `PUT /api/v1/admin/extensions/fields/{code}`
- `DELETE /api/v1/admin/extensions/fields/{code}` (soft-delete recommended)

Filters:

- `scope`, `target_type`, `visibility`, `include_private`

### 7.2 Value APIs

- `GET /api/v1/admin/extensions/values/{targetType}/{targetID}`
- `PUT /api/v1/admin/extensions/values/{targetType}/{targetID}` (upsert batch)
- `DELETE /api/v1/admin/extensions/values/{targetType}/{targetID}/{fieldCode}`

Optional entity-convenience aliases:

- `/api/v1/admin/products/{id}/extensions`
- `/api/v1/admin/cart-items/{id}/extensions`

### 7.3 Access behavior

- Private fields omitted unless caller has explicit capability.
- Write checks combine field ACL + endpoint ACL.
- Include explicit error codes: `forbidden_private_field`, `field_validation_failed`, etc.

---

## 8) GraphQL and Plugin Parity

### 8.1 GraphQL

Add schema exposing the same registry/value model:

- `extensionFields(...)`
- `extensionValues(targetType, targetId, includePrivate)`
- mutations for upsert/delete definitions and values

GraphQL must enforce same ACL and visibility semantics as REST.

### 8.2 Plugin code

Provide service interfaces in `plugin.App`, e.g.:

- `Extensions().RegisterField(def FieldDef) error`
- `Extensions().SetValues(target, values) error`
- `Extensions().GetValues(target, query) (...)`

No plugin should bypass the registry; all writes route through this service.

---

## 9) Universal Hooks and Slots

### 9.1 Hook Registry

Support:

- static core hooks (must-have)
- dynamic namespaced hooks registered by plugins

Conventions:

- `*.before`, `*.after`, `*.error`
- discoverable hook catalog endpoint for tooling/docs

This addresses "what if I need `product.add_to_cart.after` and only `.before` exists?"

### 9.2 Slot Registry

Support:

- standard slots in core templates
- dynamic slots in template markup via slot markers (no Go code change per slot)

Plugins register renderers against slot names with deterministic ordering.

### 9.3 Multi-plugin composition

**Chain handlers, not plugins.** Many plugins compose through ordered pipelines, workflows, and hook chains that share a mutable context — not through plugin-to-plugin imports or runtime dependency graphs.

| Need | Mechanism |
| --- | --- |
| Same-request behavior (step B uses step A's output) | Pipeline / workflow step or hook chain with explicit priority |
| Durable data across requests (cart → order) | Extension fields + snapshot policy |
| React after something happened | Events (unordered; not for synchronous chaining) |

Plugin `depends_on` (if added) should affect **init order only** — field registration before dependent handlers. Runtime sequence is defined by handler position on the shared hook or pipeline.

Full walkthrough (checkout custom field + dependent plugin): [Multi-Plugin Composition](../../guides/PLUGIN_COMPOSITION.md).

---

## 10) Asset Injection (without theme forks)

Introduce plugin asset manifest:

- CSS bundles
- JS bundles
- optional inline fragments
- target placement (`layout.head`, `layout.footer`, route, slot)

Requirements:

- deterministic load order
- CSP nonce support
- route-level gating
- optional style isolation conventions

This allows small CSS/JS/HTML customizations independent of custom themes.

---

## 11) Admin UX Model

Default behavior:

- generic "Extensions" panel per entity editor
- renders only `visibility=public` fields user can read
- field types mapped to standard widgets

Advanced behavior:

- plugin can provide custom renderer/editor for a field code
- private fields remain hidden unless an operator mode/capability is enabled

---

## 12) Suggested Phase Plan (small PRs)

1. **Platform core**
   - field definition model, registry service, base persistence
2. **Value APIs**
   - target-based CRUD with ACL and private visibility enforcement
3. **Entity integration**
   - product + cart_item + order_item extensions, checkout snapshot policy
4. **UI slots + assets**
   - slot markers, renderer registry, asset manifest loading
5. **GraphQL parity**
   - extension field/value queries + mutations
6. **Reference plugin**
   - "custom dropdown option" end-to-end using only extension APIs

---

## 13) Acceptance Criteria

- Developer can add a custom product attribute without core template override.
- Value can flow to cart/checkout/order via declared snapshot policy.
- Same operation can be done from admin, REST, GraphQL, and plugin code.
- Private fields are hidden from merchant admin by default.
- New hooks/slots can be registered without modifying core hook/slot enums.
- CSS/JS/HTML snippets can be mounted without a full custom theme.

