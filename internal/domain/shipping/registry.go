package shipping

import "fmt"

// ProviderRegistry holds registered shipping providers keyed by method.
type ProviderRegistry struct {
	providers map[ShippingMethod]Provider
	order     []ShippingMethod
}

// NewProviderRegistry creates an empty ProviderRegistry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[ShippingMethod]Provider),
	}
}

// Register adds a shipping provider. Panics when provider is nil or method already registered.
func (r *ProviderRegistry) Register(p Provider) {
	if r == nil {
		panic("shipping: registry must not be nil")
	}
	if p == nil {
		panic("shipping: provider must not be nil")
	}
	method := p.Method()
	if method == "" || !method.IsValid() {
		panic(fmt.Sprintf("shipping: invalid provider method %q", method))
	}
	if _, exists := r.providers[method]; exists {
		panic(fmt.Sprintf("shipping: provider already registered for method %q", method))
	}
	r.providers[method] = p
	r.order = append(r.order, method)
}

// Get returns the provider for method.
func (r *ProviderRegistry) Get(method ShippingMethod) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.providers[method]
	return p, ok
}

// Providers returns registered providers in registration order.
func (r *ProviderRegistry) Providers() []Provider {
	if r == nil || len(r.order) == 0 {
		return nil
	}
	out := make([]Provider, 0, len(r.order))
	for _, method := range r.order {
		out = append(out, r.providers[method])
	}
	return out
}

// Len returns the number of registered providers.
func (r *ProviderRegistry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.order)
}

// DefaultMethod returns the preferred method when checkout input omits shipping_method.
func (r *ProviderRegistry) DefaultMethod() ShippingMethod {
	if r == nil || len(r.order) == 0 {
		return ""
	}
	if _, ok := r.providers[MethodFlatRate]; ok {
		return MethodFlatRate
	}
	return r.order[0]
}

// Resolve selects a provider for the given method string.
// Empty method uses DefaultMethod.
func (r *ProviderRegistry) Resolve(method string) (Provider, error) {
	if r == nil || len(r.order) == 0 {
		return nil, fmt.Errorf("shipping: no providers configured")
	}
	m := ShippingMethod(method)
	if m == "" {
		m = r.DefaultMethod()
	}
	p, ok := r.Get(m)
	if !ok {
		return nil, fmt.Errorf("shipping: method %q is unavailable", method)
	}
	return p, nil
}
