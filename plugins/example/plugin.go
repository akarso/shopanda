package example

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// PermissionReportsRead is an example plugin-defined admin permission.
const PermissionReportsRead = "example.reports.read"

// RoleAdmin mirrors the core admin role name for permission grants.
// Plugins grant against stable role name strings — see RegisterPermission.
const RoleAdmin = "admin"

// Plugin demonstrates a third-party-style external plugin.
type Plugin struct {
	feeMinorUnits int64
}

// New returns the example external plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "example/demo" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("example plugin: host not configured")
	}
	if !h.ConfigBool("plugins.example.enabled") {
		return fmt.Errorf("example plugin: disabled (plugins.example.enabled=false)")
	}
	p.feeMinorUnits = h.ConfigInt64(configKeyFeeMinorUnits)

	if err := h.RegisterConfig(exampleFeeConfigDefinition(&p.feeMinorUnits)); err != nil {
		return fmt.Errorf("example plugin: register config: %w", err)
	}
	sdk := pluginsdk.NewHost(h, p.Name())
	sdk.Pricing().Register(NewExampleFeeStep(&p.feeMinorUnits), pluginsdk.After("promotions"))
	if err := registerOrderCreatedSync(sdk); err != nil {
		return fmt.Errorf("example plugin: sync job: %w", err)
	}
	if err := h.RegisterPermission(PermissionReportsRead, RoleAdmin); err != nil {
		return fmt.Errorf("example plugin: register permission: %w", err)
	}
	registerCLICommands(sdk, &p.feeMinorUnits)
	return nil
}
