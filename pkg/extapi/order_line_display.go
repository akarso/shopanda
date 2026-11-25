package extapi

// Display kinds for order-line fields on confirmation email and order pages.
const (
	DisplayKindText  = "text"
	DisplayKindImage = "image"
)

// OrderLineDisplayField is one shopper-facing field a plugin appends on
// order.line.display (or core builds from stored public extensions).
// Kind "image" uses Value as an https URL and Alt as alt text; other kinds
// render as text. Empty Kind defaults to text.
type OrderLineDisplayField struct {
	Code  string
	Label string
	Kind  string
	Value string
	Alt   string
}

// AppendOrderLineDisplayField appends a field to the order.line.display payload.
// Handlers must not return business-rule failures as handler errors when a
// field simply cannot be built; skip the append instead. Handler errors are
// skip-and-log at the call site so other plugins still run.
func (c *HookContext) AppendOrderLineDisplayField(field OrderLineDisplayField) {
	if c == nil || field.Code == "" {
		return
	}
	raw, _ := c.Get("fields")
	var fields *[]OrderLineDisplayField
	if raw != nil {
		fields, _ = raw.(*[]OrderLineDisplayField)
	}
	if fields == nil {
		init := []OrderLineDisplayField{}
		fields = &init
		c.Set("fields", fields)
	}
	*fields = append(*fields, field)
}

// OrderLineDisplayOrderID returns the order id from an order.line.display payload.
func (c *HookContext) OrderLineDisplayOrderID() string {
	return hookPayloadString(c, "order_id")
}

// OrderLineDisplayVariantID returns the variant id from an order.line.display payload.
func (c *HookContext) OrderLineDisplayVariantID() string {
	return hookPayloadString(c, "variant_id")
}

// OrderLineDisplayLanguage returns the BCP 47 primary language tag for this
// enrich (request language on storefront pages, order language on email).
func (c *HookContext) OrderLineDisplayLanguage() string {
	return hookPayloadString(c, "language")
}

// OrderLineDisplayExtensions returns the public stored extension map for the
// line (code → value), or nil when unset.
func (c *HookContext) OrderLineDisplayExtensions() map[string]interface{} {
	if c == nil {
		return nil
	}
	raw, ok := c.Get("extensions")
	if !ok || raw == nil {
		return nil
	}
	m, _ := raw.(map[string]interface{})
	return m
}

func hookPayloadString(c *HookContext, key string) string {
	if c == nil {
		return ""
	}
	raw, ok := c.Get(key)
	if !ok || raw == nil {
		return ""
	}
	s, _ := raw.(string)
	return s
}
