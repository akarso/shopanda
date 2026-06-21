package stripe

import (
	"fmt"
	"os"

	instripe "github.com/akarso/shopanda/internal/infrastructure/stripepay"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// PaymentPlugin registers the Stripe payment provider when enabled and configured.
type PaymentPlugin struct{}

func NewPaymentPlugin() *PaymentPlugin { return &PaymentPlugin{} }

func (p *PaymentPlugin) Name() string { return "core/stripe" }

func (p *PaymentPlugin) Init(app *plugin.App) error {
	if app.Config == nil || !app.Config.Payment.Stripe.Enabled {
		return nil
	}

	stripeKey := os.Getenv("SHOPANDA_PAYMENT_STRIPE_SECRET_KEY")
	if stripeKey == "" {
		if app.Config.Payment.Stripe.SecretKey != "" && app.Logger != nil {
			app.Logger.Warn("payment.stripe.yaml_secret_ignored", map[string]interface{}{
				"message": "Stripe secret_key in YAML is ignored; set SHOPANDA_PAYMENT_STRIPE_SECRET_KEY env var",
			})
		}
		if app.Logger != nil {
			app.Logger.Warn("payment.stripe.no_secret", map[string]interface{}{
				"message": "Stripe enabled but SHOPANDA_PAYMENT_STRIPE_SECRET_KEY not set; falling back to manual provider",
			})
		}
		return nil
	}

	sp, err := instripe.NewProvider(stripeKey)
	if err != nil {
		return fmt.Errorf("stripe provider: %w", err)
	}
	app.RegisterPaymentProvider(sp)
	if app.Logger != nil {
		app.Logger.Info("payment.provider.stripe", nil)
	}
	return nil
}
