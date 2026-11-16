package maildemo

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Compile-time check that the demo mailer satisfies the mail port.
var _ extapi.MailSender = (*LogMailer)(nil)

// Plugin demonstrates integrator mail port replacement via RegisterMailSender.
type Plugin struct{}

// New returns the mail sender reference plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "maildemo/reference" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("maildemo plugin: host not configured")
	}
	if !h.ConfigBool("plugins.maildemo.enabled") {
		return fmt.Errorf("maildemo plugin: disabled (plugins.maildemo.enabled=false)")
	}
	subjectPrefix := h.ConfigValue("plugins.maildemo.subject_prefix")
	h.RegisterMailSender(NewLogMailer(h.Logger(), subjectPrefix))
	return nil
}
