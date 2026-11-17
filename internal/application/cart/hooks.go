package cart

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/akarso/shopanda/internal/application/hooks"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/pluginwrite"
	"github.com/akarso/shopanda/pkg/extapi"
)

// cartViewFromDomain builds the public, read-only "cart" hook payload from
// the internal cart aggregate without loading extension rows. Prefer
// Service.CartView when extension values are needed (cart.validate, CartPort).
func cartViewFromDomain(c *domainCart.Cart) *extapi.CartView {
	if c == nil {
		return nil
	}
	items := make([]extapi.CartLineView, len(c.Items))
	for i, it := range c.Items {
		items[i] = extapi.CartLineView{VariantID: it.VariantID, Quantity: it.Quantity}
	}
	return &extapi.CartView{ID: c.ID, CustomerID: c.CustomerID, Items: items}
}

// CartView builds an extapi.CartView including stored line extension values.
// Private fields are omitted (visibility-filtered ListByCartItems). Plugin-only
// owner scoping is applied later by redactCartViewForRegistrant / FilterCartViewForPlugin.
func (s *Service) CartView(ctx context.Context, c *domainCart.Cart) (*extapi.CartView, error) {
	if c == nil {
		return nil, nil
	}
	view := cartViewFromDomain(c)
	if s.extensions == nil || len(c.Items) == 0 {
		return view, nil
	}
	variantIDs := make([]string, len(c.Items))
	indexByVariant := make(map[string]int, len(c.Items))
	for i, it := range c.Items {
		variantIDs[i] = it.VariantID
		indexByVariant[it.VariantID] = i
	}
	byVariant, err := s.extensions.ListByCartItems(ctx, c.ID, variantIDs, false)
	if err != nil {
		return nil, fmt.Errorf("cart service: cart view extensions: %w", err)
	}
	for variantID, stored := range byVariant {
		i, ok := indexByVariant[variantID]
		if !ok || len(stored) == 0 {
			continue
		}
		exts := make([]extapi.CartLineExtension, 0, len(stored))
		for _, v := range stored {
			exts = append(exts, extapi.CartLineExtension{
				Code:  v.FieldCode,
				Value: payloadPrimary(v.Payload),
			})
		}
		view.Items[i].Extensions = exts
	}
	return view, nil
}

func payloadPrimary(p domainext.ValuePayload) interface{} {
	if p.StringValue != nil {
		return *p.StringValue
	}
	if p.IntValue != nil {
		return *p.IntValue
	}
	if p.BoolValue != nil {
		return *p.BoolValue
	}
	if len(p.JSONValue) > 0 {
		var out interface{}
		if err := json.Unmarshal(p.JSONValue, &out); err != nil {
			// Corrupt payload: surface raw bytes rather than dropping the field.
			return string(p.JSONValue)
		}
		return out
	}
	return nil
}

func (s *Service) invokeCartHook(ctx context.Context, name string, hookCtx *hooks.Context) error {
	if s.hooks == nil {
		return nil
	}
	if err := s.hooks.Invoke(ctx, hookCtx); err != nil {
		return fmt.Errorf("cart service: hook %q: %w", name, err)
	}
	return nil
}

// invokeCartHookAdapted is invokeCartHook for payloads that need a
// per-registrant adapted view (see hooks.Registry.InvokeAdapted).
func (s *Service) invokeCartHookAdapted(ctx context.Context, name string, hookCtx *hooks.Context, adapt func(registrant string, src *hooks.Context) *hooks.Context) error {
	if s.hooks == nil {
		return nil
	}
	if err := s.hooks.InvokeAdapted(ctx, hookCtx, adapt); err != nil {
		return fmt.Errorf("cart service: hook %q: %w", name, err)
	}
	return nil
}

type cartItemHookOpts struct {
	extensions []domainext.ValueInput
	// writeCtx is the pre-strip context (may carry pluginwrite grants).
	writeCtx context.Context
}

type pluginOnlyFieldChecker interface {
	IsPluginOnlyWrite(code string) bool
	// FieldOwner returns the registrant that owns code, or "" when unowned.
	FieldOwner(code string) string
}

type exclusiveStringChecker interface {
	IsExclusiveString(code string) bool
}

func collectPluginWriteCodes(writeCtx context.Context, inputs []domainext.ValueInput) []string {
	if writeCtx == nil || len(inputs) == 0 {
		return nil
	}
	var codes []string
	seen := make(map[string]struct{})
	for _, in := range inputs {
		if _, ok := pluginwrite.For(writeCtx, in.FieldCode); !ok {
			continue
		}
		if _, dup := seen[in.FieldCode]; dup {
			continue
		}
		seen[in.FieldCode] = struct{}{}
		codes = append(codes, in.FieldCode)
	}
	return codes
}

// pluginWriteCodesForRegistrant returns the subset of trusted write codes
// the current handler is allowed to see: owned plugin-only fields only when
// registrant matches the owner; unowned plugin-only fields are never listed
// (an empty owner is treated as redacted for every handler).
func pluginWriteCodesForRegistrant(trusted []string, registrant string, pluginOnly pluginOnlyFieldChecker) []string {
	if len(trusted) == 0 {
		return nil
	}
	out := make([]string, 0, len(trusted))
	for _, code := range trusted {
		if pluginOnly != nil && pluginOnly.IsPluginOnlyWrite(code) {
			owner := pluginOnly.FieldOwner(code)
			if owner == "" || owner != registrant {
				continue
			}
		}
		out = append(out, code)
	}
	return out
}

// extensionInputsForHook exposes field codes always. Plugin-only values are
// omitted unless writeCtx granted that code AND the current handler's
// registrant is the field's owner. An empty owner is treated as redacted for
// every handler (fields registered outside plugin Init are unowned and must
// not leak a trusted plugin-only value to the whole hook chain). Callers
// invoke this once per handler via hooks.Registry.InvokeAdapted, passing
// that handler's registrant.
func extensionInputsForHook(inputs []domainext.ValueInput, trusted []string, registrant string, pluginOnly pluginOnlyFieldChecker) []map[string]interface{} {
	trustedSet := make(map[string]struct{}, len(trusted))
	for _, c := range trusted {
		trustedSet[c] = struct{}{}
	}
	out := make([]map[string]interface{}, 0, len(inputs))
	for _, in := range inputs {
		row := map[string]interface{}{"field_code": in.FieldCode}
		redact := pluginOnly != nil && pluginOnly.IsPluginOnlyWrite(in.FieldCode)
		if redact {
			if _, ok := trustedSet[in.FieldCode]; ok {
				owner := ""
				if pluginOnly != nil {
					owner = pluginOnly.FieldOwner(in.FieldCode)
				}
				// Empty owner: always redact (unowned plugin-only).
				if owner != "" && owner == registrant {
					redact = false
				}
			}
		}
		if !redact {
			row["value"] = in.Value
		}
		out = append(out, row)
	}
	return out
}

// redactCartViewForRegistrant returns a shallow copy of view that omits
// plugin-only line extensions the registrant does not own (same rule as
// FilterCartViewForPlugin — no ValueRedacted leak of hidden field codes).
func redactCartViewForRegistrant(view *extapi.CartView, registrant string, pluginOnly pluginOnlyFieldChecker) *extapi.CartView {
	return filterCartViewExtensions(view, registrant, pluginOnly)
}

// redactPendingLine copies pending and omits plugin-only extensions the
// registrant does not own (same rule as CartView line extensions).
func redactPendingLine(pending *extapi.PendingCartLine, registrant string, pluginOnly pluginOnlyFieldChecker) *extapi.PendingCartLine {
	if pending == nil {
		return nil
	}
	out := &extapi.PendingCartLine{
		VariantID: pending.VariantID,
		Quantity:  pending.Quantity,
	}
	if len(pending.Extensions) == 0 {
		return out
	}
	kept := make([]extapi.CartLineExtension, 0, len(pending.Extensions))
	for _, e := range pending.Extensions {
		if pluginOnly != nil && pluginOnly.IsPluginOnlyWrite(e.Code) {
			owner := pluginOnly.FieldOwner(e.Code)
			if registrant == "" || owner == "" || owner != registrant {
				continue
			}
		}
		kept = append(kept, e)
	}
	out.Extensions = kept
	return out
}

// pendingLineFromAdd builds the cart.validate pending_line payload. Nil when
// there are no extension inputs (quantity-only AddItem must not look like a
// second line with a missing design_id).
func pendingLineFromAdd(variantID string, resultingQty int, inputs []domainext.ValueInput) *extapi.PendingCartLine {
	if len(inputs) == 0 {
		return nil
	}
	exts := make([]extapi.CartLineExtension, 0, len(inputs))
	for _, in := range inputs {
		exts = append(exts, extapi.CartLineExtension{
			Code:  in.FieldCode,
			Value: cloneExtensionValue(in.Value),
		})
	}
	return &extapi.PendingCartLine{
		VariantID:  variantID,
		Quantity:   resultingQty,
		Extensions: exts,
	}
}

// overlayPendingOnCartView returns a shallow cart copy where the pending
// variant's extensions are the stored set with pending codes overlaid
// (pending wins on the same code). One effective line for cart.validate.
func overlayPendingOnCartView(view *extapi.CartView, pending *extapi.PendingCartLine) *extapi.CartView {
	if view == nil || pending == nil {
		return view
	}
	out := &extapi.CartView{
		ID:         view.ID,
		CustomerID: view.CustomerID,
		Items:      make([]extapi.CartLineView, len(view.Items)),
	}
	copy(out.Items, view.Items)
	for i, line := range out.Items {
		if line.VariantID != pending.VariantID {
			continue
		}
		byCode := make(map[string]extapi.CartLineExtension, len(line.Extensions)+len(pending.Extensions))
		order := make([]string, 0, len(line.Extensions)+len(pending.Extensions))
		seen := make(map[string]struct{}, len(line.Extensions)+len(pending.Extensions))
		for _, e := range line.Extensions {
			byCode[e.Code] = e
			if _, ok := seen[e.Code]; !ok {
				seen[e.Code] = struct{}{}
				order = append(order, e.Code)
			}
		}
		for _, e := range pending.Extensions {
			byCode[e.Code] = e
			if _, ok := seen[e.Code]; !ok {
				seen[e.Code] = struct{}{}
				order = append(order, e.Code)
			}
		}
		merged := make([]extapi.CartLineExtension, 0, len(order))
		for _, code := range order {
			merged = append(merged, byCode[code])
		}
		out.Items[i] = extapi.CartLineView{
			VariantID:  line.VariantID,
			Quantity:   pending.Quantity,
			Extensions: merged,
		}
	}
	return out
}

func cloneExtensionValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[k] = cloneExtensionValue(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = cloneExtensionValue(val)
		}
		return out
	default:
		return v
	}
}

func filterCartViewExtensions(view *extapi.CartView, pluginName string, pluginOnly pluginOnlyFieldChecker) *extapi.CartView {
	if view == nil {
		return nil
	}
	out := &extapi.CartView{
		ID:         view.ID,
		CustomerID: view.CustomerID,
		Items:      make([]extapi.CartLineView, len(view.Items)),
	}
	for i, line := range view.Items {
		var kept []extapi.CartLineExtension
		for _, e := range line.Extensions {
			if pluginOnly != nil && pluginOnly.IsPluginOnlyWrite(e.Code) {
				owner := pluginOnly.FieldOwner(e.Code)
				if pluginName == "" || owner == "" || owner != pluginName {
					continue
				}
			}
			kept = append(kept, e)
		}
		out.Items[i] = extapi.CartLineView{
			VariantID:  line.VariantID,
			Quantity:   line.Quantity,
			Extensions: kept,
		}
	}
	return out
}

// FilterCartViewForPlugin keeps non-plugin-only extensions and plugin-only
// extensions owned by pluginName (CartPort.Get via AsHostFor / Adapt).
func (s *Service) FilterCartViewForPlugin(view *extapi.CartView, pluginName string) *extapi.CartView {
	var pluginOnly pluginOnlyFieldChecker
	if po, ok := s.extensions.(pluginOnlyFieldChecker); ok {
		pluginOnly = po
	}
	return filterCartViewExtensions(view, pluginName, pluginOnly)
}

func (s *Service) invokeCartItemHook(ctx context.Context, name, cartID, customerID, variantID string, quantity int, c *domainCart.Cart, opts cartItemHookOpts) error {
	hookCtx := hooks.NewContext(name)
	hookCtx.Set("cart_id", cartID)
	hookCtx.Set("customer_id", customerID)
	hookCtx.Set("variant_id", variantID)
	hookCtx.Set("quantity", quantity)
	hookCtx.Set("cart", cartViewFromDomain(c))

	var checker pluginOnlyFieldChecker
	if po, ok := s.extensions.(pluginOnlyFieldChecker); ok {
		checker = po
	}
	writeCodes := collectPluginWriteCodes(opts.writeCtx, opts.extensions)
	id := auth.IdentityFrom(ctx)
	hookCtx.Set("actor_user_id", id.UserID)
	hookCtx.Set("actor_role", string(id.Role))

	if len(opts.extensions) == 0 {
		return s.invokeCartHook(ctx, name, hookCtx)
	}
	adapt := func(registrant string, src *hooks.Context) *hooks.Context {
		visible := pluginWriteCodesForRegistrant(writeCodes, registrant, checker)
		if len(visible) > 0 {
			src.Set("plugin_write_codes", visible)
		} else {
			src.Set("plugin_write_codes", []string(nil))
		}
		src.Set("extensions", extensionInputsForHook(opts.extensions, visible, registrant, checker))
		return src
	}
	return s.invokeCartHookAdapted(ctx, name, hookCtx, adapt)
}
