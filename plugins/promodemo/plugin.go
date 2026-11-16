package promodemo

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates custom catalog promotion rule types via PromotionRules registration.
type Plugin struct{}

// New returns the promotion rule reference plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "promodemo/reference" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("promodemo plugin: host not configured")
	}
	if !h.ConfigBool("plugins.promodemo.enabled") {
		return fmt.Errorf("promodemo plugin: disabled (plugins.promodemo.enabled=false)")
	}
	sdk := pluginsdk.NewHost(h, p.Name())
	promo := sdk.Promotion()
	if err := promo.RegisterCatalogCondition(RuleMinLineTotal, evalMinLineTotal); err != nil {
		return err
	}
	return promo.RegisterCatalogAction(RuleLineBonusPercent, evalLineBonusPercent)
}
