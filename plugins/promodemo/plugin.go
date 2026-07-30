package promodemo

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates custom catalog promotion rule types via PromotionRules registration.
type Plugin struct{}

// New returns the promotion rule reference plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "promodemo/reference" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("promodemo plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("promodemo plugin: config not configured")
	}
	if !app.Config.Plugins.PromoDemo.Enabled {
		return fmt.Errorf("promodemo plugin: disabled (plugins.promodemo.enabled=false)")
	}
	sdk := pluginsdk.New(app, p.Name())
	promo := sdk.Promotion()
	if err := promo.RegisterCatalogCondition(RuleMinLineTotal, evalMinLineTotal); err != nil {
		return err
	}
	return promo.RegisterCatalogAction(RuleLineBonusPercent, evalLineBonusPercent)
}
