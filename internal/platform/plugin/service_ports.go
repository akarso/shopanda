package plugin

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Capability names a composition-root facility plugins may depend on.
type Capability string

const (
	CapCart                Capability = "cart"
	CapCatalog             Capability = "catalog"
	CapOrders              Capability = "orders"
	CapCustomers           Capability = "customers"
	CapDB                  Capability = "db"
	CapIntegrationOrder    Capability = "integration.order_status"
	CapIntegrationShipment Capability = "integration.shipment_status"
	CapIntegrationStock    Capability = "integration.stock"
)

// NeedsDeclarer is optional on internal Plugin implementations.
type NeedsDeclarer interface {
	Needs() []Capability
}

type servicePorts struct {
	mu sync.RWMutex

	catalog   any
	orders    any
	cart      any
	customers any

	required    map[Capability]struct{}
	satisfied   map[Capability]struct{}
	needsFrozen bool

	// requiredBy tracks which plugin(s) declared each capability, so
	// AssertNeedsMet can name them in its error. Best-effort: only populated
	// when RecordNeeds runs while App.CurrentPluginName() is set (i.e. during
	// InitAllContext); direct RecordNeeds calls outside Init (tests) leave it
	// empty for that capability.
	requiredBy map[Capability]map[string]struct{}
}

func (a *App) portsState() *servicePorts {
	if a.servicePorts == nil {
		a.servicePorts = &servicePorts{
			required:  make(map[Capability]struct{}),
			satisfied: make(map[Capability]struct{}),
		}
	}
	return a.servicePorts
}

// RecordNeeds records capabilities a plugin declared before Init, attributed
// to App.CurrentPluginName() (set by InitAllContext around each plugin's
// Init — see NeedsDeclarer handling in registry.go). A no-op once
// FreezePluginNeeds has run (e.g. a late Host.Require call from code that
// outlived Init, such as an HTTP handler closure) — recording after the gate
// has already been checked would be silently ineffective anyway, and a no-op
// keeps that obvious instead of leaving a dangling "required" entry that can
// never be satisfied.
func (a *App) RecordNeeds(caps ...Capability) {
	a.RecordNeedsFor(a.CurrentPluginName(), caps...)
}

// RecordNeedsFor is RecordNeeds with an explicit plugin name, for callers
// that already know it independent of CurrentPluginName (e.g.
// pluginsdk.Host.Require, which is bound to one plugin at Host-creation time
// and may be called by tests/harness code outside InitAllContext).
func (a *App) RecordNeedsFor(pluginName string, caps ...Capability) {
	if a == nil || len(caps) == 0 {
		return
	}
	st := a.portsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.needsFrozen {
		return
	}
	for _, c := range caps {
		if c == "" {
			continue
		}
		st.required[c] = struct{}{}
		if pluginName != "" {
			if st.requiredBy == nil {
				st.requiredBy = make(map[Capability]map[string]struct{})
			}
			if st.requiredBy[c] == nil {
				st.requiredBy[c] = make(map[string]struct{})
			}
			st.requiredBy[c][pluginName] = struct{}{}
		}
	}
}

// RequireCapability records that the current plugin Init depends on cap.
func (a *App) RequireCapability(cap Capability) {
	a.RecordNeeds(cap)
}

// MarkCapabilitySatisfied records that the composition root bound cap.
func (a *App) MarkCapabilitySatisfied(cap Capability) {
	if a == nil || cap == "" {
		return
	}
	st := a.portsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.satisfied[cap] = struct{}{}
}

// FreezePluginNeeds seals capability requirements after InitAll.
func (a *App) FreezePluginNeeds() {
	if a == nil {
		return
	}
	st := a.portsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.needsFrozen = true
}

// BindCatalogPort wires the Host-visible catalog port (pkg/pluginsdk.CatalogPort).
func (a *App) BindCatalogPort(port any) error {
	if a == nil {
		return fmt.Errorf("plugin: BindCatalogPort: app is nil")
	}
	if port == nil {
		return fmt.Errorf("plugin: BindCatalogPort: port must not be nil")
	}
	st := a.portsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.catalog != nil {
		return fmt.Errorf("plugin: BindCatalogPort: already bound")
	}
	st.catalog = port
	st.satisfied[CapCatalog] = struct{}{}
	return nil
}

// BindOrdersPort wires the Host-visible order read port.
func (a *App) BindOrdersPort(port any) error {
	if a == nil {
		return fmt.Errorf("plugin: BindOrdersPort: app is nil")
	}
	if port == nil {
		return fmt.Errorf("plugin: BindOrdersPort: port must not be nil")
	}
	st := a.portsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.orders != nil {
		return fmt.Errorf("plugin: BindOrdersPort: already bound")
	}
	st.orders = port
	st.satisfied[CapOrders] = struct{}{}
	return nil
}

// BindCartPort wires the Host-visible cart mutation port (late-bound in serve).
func (a *App) BindCartPort(port any) error {
	if a == nil {
		return fmt.Errorf("plugin: BindCartPort: app is nil")
	}
	if port == nil {
		return fmt.Errorf("plugin: BindCartPort: port must not be nil")
	}
	st := a.portsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.cart != nil {
		return fmt.Errorf("plugin: BindCartPort: already bound")
	}
	st.cart = port
	st.satisfied[CapCart] = struct{}{}
	return nil
}

// BindCustomerPort wires the Host-visible customer read port.
func (a *App) BindCustomerPort(port any) error {
	if a == nil {
		return fmt.Errorf("plugin: BindCustomerPort: app is nil")
	}
	if port == nil {
		return fmt.Errorf("plugin: BindCustomerPort: port must not be nil")
	}
	st := a.portsState()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.customers != nil {
		return fmt.Errorf("plugin: BindCustomerPort: already bound")
	}
	st.customers = port
	st.satisfied[CapCustomers] = struct{}{}
	return nil
}

// CustomerPort returns the bound customer port, if any.
func (a *App) CustomerPort() (any, bool) {
	if a == nil {
		return nil, false
	}
	st := a.portsState()
	st.mu.RLock()
	defer st.mu.RUnlock()
	if st.customers == nil {
		return nil, false
	}
	return st.customers, true
}

// CatalogPort returns the bound catalog port, if any.
func (a *App) CatalogPort() (any, bool) {
	if a == nil {
		return nil, false
	}
	st := a.portsState()
	st.mu.RLock()
	defer st.mu.RUnlock()
	if st.catalog == nil {
		return nil, false
	}
	return st.catalog, true
}

// OrdersPort returns the bound order port, if any.
func (a *App) OrdersPort() (any, bool) {
	if a == nil {
		return nil, false
	}
	st := a.portsState()
	st.mu.RLock()
	defer st.mu.RUnlock()
	if st.orders == nil {
		return nil, false
	}
	return st.orders, true
}

// CartPort returns the bound cart port, if any.
func (a *App) CartPort() (any, bool) {
	if a == nil {
		return nil, false
	}
	st := a.portsState()
	st.mu.RLock()
	defer st.mu.RUnlock()
	if st.cart == nil {
		return nil, false
	}
	return st.cart, true
}

// AssertNeedsMet returns an error when a recorded capability was not bound.
func (a *App) AssertNeedsMet() error {
	return a.assertNeedsMet(nil)
}

// AssertNeedsMetExcept is AssertNeedsMet but ignores the given capabilities.
// Non-serve composition roots (worker, scheduler, search-reindex, and the
// cache/schedule/extension-field CLIs) run InitAllContext without ever
// binding CartPort — serve is the only process that constructs cart.Service
// and calls BindCartPort — so they must skip CapCart here instead of
// refusing to boot whenever any registered plugin declares Needs(CapCart)
// (e.g. plugins/extref). Serve itself keeps calling the strict AssertNeedsMet
// after BindCartPort.
func (a *App) AssertNeedsMetExcept(skip ...Capability) error {
	return a.assertNeedsMet(skip)
}

func (a *App) assertNeedsMet(skip []Capability) error {
	if a == nil {
		return nil
	}
	skipSet := make(map[Capability]struct{}, len(skip))
	for _, c := range skip {
		skipSet[c] = struct{}{}
	}
	st := a.portsState()
	st.mu.RLock()
	defer st.mu.RUnlock()
	// Deterministic order so repeated failures produce a stable message.
	caps := make([]Capability, 0, len(st.required))
	for cap := range st.required {
		caps = append(caps, cap)
	}
	sort.Slice(caps, func(i, j int) bool { return caps[i] < caps[j] })
	for _, cap := range caps {
		if _, skipped := skipSet[cap]; skipped {
			continue
		}
		if _, ok := st.satisfied[cap]; ok {
			continue
		}
		if names := pluginNames(st.requiredBy[cap]); len(names) > 0 {
			return fmt.Errorf("plugin: required capability %q not bound (needed by %s)", cap, strings.Join(names, ", "))
		}
		return fmt.Errorf("plugin: required capability %q not bound", cap)
	}
	return nil
}

// CapabilityDeclaredBy reports whether pluginName has recorded cap as a Need
// (via NeedsDeclarer or Host.Require) at some point up to this call. Used to
// gate Host getters (BootstrapDB, Catalog, Orders, Customers) so a plugin
// that never declared a capability cannot read the port just because some
// other plugin's Need caused the composition root to bind it.
func (a *App) CapabilityDeclaredBy(cap Capability, pluginName string) bool {
	if a == nil || cap == "" || pluginName == "" {
		return false
	}
	st := a.portsState()
	st.mu.RLock()
	defer st.mu.RUnlock()
	names := st.requiredBy[cap]
	if names == nil {
		return false
	}
	_, ok := names[pluginName]
	return ok
}

func pluginNames(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
