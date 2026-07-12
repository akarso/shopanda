package cart

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/application/hooks"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
)

func (s *Service) invokeCartHook(ctx context.Context, name string, hookCtx *hooks.Context) error {
	if s.hooks == nil {
		return nil
	}
	if err := s.hooks.Invoke(ctx, hookCtx); err != nil {
		return fmt.Errorf("cart service: hook %q: %w", name, err)
	}
	return nil
}

func newCartItemHookContext(name, cartID, customerID, variantID string, quantity int, c *domainCart.Cart) *hooks.Context {
	hookCtx := hooks.NewContext(name)
	hookCtx.Set("cart_id", cartID)
	hookCtx.Set("customer_id", customerID)
	hookCtx.Set("variant_id", variantID)
	hookCtx.Set("quantity", quantity)
	hookCtx.Set("cart", c)
	return hookCtx
}

func (s *Service) invokeCartItemBeforeHook(ctx context.Context, name, cartID, customerID, variantID string, quantity int, c *domainCart.Cart) error {
	return s.invokeCartHook(ctx, name, newCartItemHookContext(name, cartID, customerID, variantID, quantity, c))
}

func (s *Service) invokeCartItemAfterHook(ctx context.Context, name, cartID, customerID, variantID string, quantity int, c *domainCart.Cart) error {
	return s.invokeCartHook(ctx, name, newCartItemHookContext(name, cartID, customerID, variantID, quantity, c))
}
