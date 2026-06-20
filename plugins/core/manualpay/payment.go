package manualpay

import (
	inmanual "github.com/akarso/shopanda/internal/infrastructure/manualpay"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// PaymentPlugin registers the built-in manual payment provider when no other provider is active.
type PaymentPlugin struct{}

func NewPaymentPlugin() *PaymentPlugin { return &PaymentPlugin{} }

func (p *PaymentPlugin) Name() string { return "core/manualpay" }

func (p *PaymentPlugin) Init(app *plugin.App) error {
	if _, ok := app.PaymentProvider(); ok {
		return nil
	}
	app.RegisterPaymentProvider(inmanual.NewProvider())
	return nil
}
