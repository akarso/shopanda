package payment

import "fmt"

// ProviderRegistry holds registered payment providers keyed by method.
// Multiple methods may be active at once (e.g. manual and stripe).
type ProviderRegistry struct {
	providers map[PaymentMethod]Provider
	order     []PaymentMethod
}

// NewProviderRegistry creates an empty ProviderRegistry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[PaymentMethod]Provider),
	}
}

// Register adds a payment provider. Panics when provider is nil or method already registered.
func (r *ProviderRegistry) Register(p Provider) {
	if r == nil {
		panic("payment: registry must not be nil")
	}
	if p == nil {
		panic("payment: provider must not be nil")
	}
	method := p.Method()
	if method == "" || !method.IsValid() {
		panic(fmt.Sprintf("payment: invalid provider method %q", method))
	}
	if _, exists := r.providers[method]; exists {
		panic(fmt.Sprintf("payment: provider already registered for method %q", method))
	}
	r.providers[method] = p
	r.order = append(r.order, method)
}

// Get returns the provider for method.
func (r *ProviderRegistry) Get(method PaymentMethod) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.providers[method]
	return p, ok
}

// Methods returns registered methods in registration order.
func (r *ProviderRegistry) Methods() []PaymentMethod {
	if r == nil || len(r.order) == 0 {
		return nil
	}
	out := make([]PaymentMethod, len(r.order))
	copy(out, r.order)
	return out
}

// Len returns the number of registered providers.
func (r *ProviderRegistry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.order)
}

// DefaultMethod returns the preferred method when checkout input omits payment_method.
// Manual is preferred when registered; otherwise the first registered method is used.
func (r *ProviderRegistry) DefaultMethod() PaymentMethod {
	if r == nil || len(r.order) == 0 {
		return ""
	}
	if _, ok := r.providers[MethodManual]; ok {
		return MethodManual
	}
	return r.order[0]
}

// Resolve selects a provider for the given method string.
// Empty method uses DefaultMethod.
func (r *ProviderRegistry) Resolve(method string) (Provider, error) {
	if r == nil || len(r.order) == 0 {
		return nil, fmt.Errorf("payment: no providers configured")
	}
	m := PaymentMethod(method)
	if m == "" {
		m = r.DefaultMethod()
	}
	p, ok := r.Get(m)
	if !ok {
		return nil, fmt.Errorf("payment: method %q is unavailable", method)
	}
	return p, nil
}

// Refunder returns a Refunder for method when the provider implements it.
func (r *ProviderRegistry) Refunder(method PaymentMethod) (Refunder, bool) {
	p, ok := r.Get(method)
	if !ok {
		return nil, false
	}
	ref, ok := p.(Refunder)
	return ref, ok
}
