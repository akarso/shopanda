package extref

import (
	"fmt"
	"net/http"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// PluginName is the stable registrant / owner string for this reference plugin.
const PluginName = "extref/reference"

// FieldCodeGiftNote is a plugin-only cart line extension demonstrated by extref.
const FieldCodeGiftNote = "extref.gift_note"

// FieldCodeDesignID is a plugin-only line field used by the design-on-line example.
const FieldCodeDesignID = "extref.design_id"

// Plugin demonstrates public SDK-only extension patterns (Track E / PR-1551+1553).
type Plugin struct{}

// New returns the reference plugin as a pluginsdk.Plugin (register via pluginsdk.Adapt).
func New() pluginsdk.Plugin { return &Plugin{} }

// Needs declares CapCart; serve binds CartPort late (see EXTENSION_POINTS pitfalls).
func (p *Plugin) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapCart}
}

func (p *Plugin) Name() string { return PluginName }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("extref: host not configured")
	}
	h.Require(pluginsdk.CapCart)

	// Fail-closed: CapCart is declared, but the port may still be unbound until
	// serve wires it. Public design-line registration requires Cart() now when
	// the host already has a port (harness/tests); health stays available either way.
	if err := h.RegisterField(extapi.FieldDef{
		Code:        FieldCodeGiftNote,
		Label:       "Gift note (extref)",
		Description: "Reference plugin-only cart line field",
		Type:        extapi.FieldTypeString,
		Scope:       extapi.TargetCartItem,
		StorageMode: extapi.StorageStored,
		Visibility:  extapi.VisibilityPublic,
		Access:      extapi.Access{PluginOnlyWrite: true},
	}); err != nil {
		return fmt.Errorf("extref: register field: %w", err)
	}
	if err := h.RegisterField(extapi.FieldDef{
		Code:        FieldCodeDesignID,
		Label:       "Design id (extref)",
		Description: "Reference plugin-only design-on-line field",
		Type:        extapi.FieldTypeString,
		Scope:       extapi.TargetCartItem,
		StorageMode: extapi.StorageStored,
		Visibility:  extapi.VisibilityPublic,
		Access:      extapi.Access{PluginOnlyWrite: true, ExclusiveString: true},
		Validation:  extapi.Validation{Required: true},
	}); err != nil {
		return fmt.Errorf("extref: register design_id field: %w", err)
	}

	if err := h.Hooks(p.Name()).Register(extapi.HookCartAddItemBefore, 100, addItemBeforeHandler); err != nil {
		return fmt.Errorf("extref: register cart.add_item.before: %w", err)
	}
	if err := h.Hooks(p.Name()).Register(extapi.HookCartValidate, 100, cartValidateDesignHandler); err != nil {
		return fmt.Errorf("extref: register cart.validate: %w", err)
	}

	if err := h.RegisterPublicRoute("/extref/health", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("extref ok\n"))
	})); err != nil {
		return fmt.Errorf("extref: public route: %w", err)
	}

	handler, err := newDesignLineHandler(h)
	if err != nil {
		return err
	}
	if err := h.RegisterPublicRoute("/extref/design-line", handler); err != nil {
		return fmt.Errorf("extref: design-line route: %w", err)
	}
	return nil
}
