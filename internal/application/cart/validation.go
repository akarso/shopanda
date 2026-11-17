package cart

import (
	"context"

	"github.com/akarso/shopanda/internal/application/hooks"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/pkg/extapi"
)

func (s *Service) validationIssues(ctx context.Context, c *domainCart.Cart, pending *extapi.PendingCartLine) ([]extapi.CartValidationIssue, error) {
	issues := []extapi.CartValidationIssue{}
	hookCtx := hooks.NewContext(hooks.HookCartValidate)
	hookCtx.Set("cart_id", c.ID)
	hookCtx.Set("customer_id", c.CustomerID)
	hookCtx.Set("validation_errors", &issues)

	view, err := s.CartView(ctx, c)
	if err != nil {
		return nil, err
	}
	// Overlay pending extension codes onto the in-memory line so validate
	// sees one effective line (pending wins on the same code). Stored-only
	// CartView would miss in-flight AddItem values until upsert.
	view = overlayPendingOnCartView(view, pending)
	hookCtx.Set("cart", view)

	var checker pluginOnlyFieldChecker
	if po, ok := s.extensions.(pluginOnlyFieldChecker); ok {
		checker = po
	}
	// Per-registrant cart view + pending_line so plugin-only values (e.g.
	// design_id) are visible only to the owning handler — same rule as
	// add_item.before. pending_line is set only when AddItem carries extensions.
	adapt := func(registrant string, src *hooks.Context) *hooks.Context {
		src.Set("cart", redactCartViewForRegistrant(view, registrant, checker))
		if pending != nil {
			src.Set("pending_line", redactPendingLine(pending, registrant, checker))
		}
		return src
	}
	if err := s.invokeCartHookAdapted(ctx, hooks.HookCartValidate, hookCtx, adapt); err != nil {
		return nil, err
	}
	return hooks.ValidationIssuesFromContext(hookCtx), nil
}

// ValidationIssues runs the cart.validate hook chain for the given cart snapshot.
func (s *Service) ValidationIssues(ctx context.Context, c *domainCart.Cart) ([]extapi.CartValidationIssue, error) {
	if c == nil {
		return nil, nil
	}
	return s.validationIssues(ctx, c, nil)
}

func (s *Service) enforceCartValidation(ctx context.Context, cartID string, c *domainCart.Cart, pending *extapi.PendingCartLine) error {
	issues, err := s.validationIssues(ctx, c, pending)
	if err != nil {
		return err
	}
	if !hooks.HasBlockingValidationIssues(issues) {
		return nil
	}
	persisted, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return err
	}
	return &ValidationFailed{Cart: persisted, Issues: issues}
}
