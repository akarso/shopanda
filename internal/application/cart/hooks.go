package cart

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/application/hooks"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/pluginwrite"
	"github.com/akarso/shopanda/pkg/extapi"
)

// cartViewFromDomain builds the public, read-only "cart" hook payload from
// the internal cart aggregate. Hook handlers (core or plugin) only ever see
// this view — never the domain type — so plugins can type-assert against
// extapi.CartView without importing internal/domain/cart.
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
