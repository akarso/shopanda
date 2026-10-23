package theme

import (
	"html/template"
)

// FragmentFunc is the {{fragment}} template helper.
// Implementations belong in the HTTP adapter: storefront routes and htmx
// markup must not live in domain (AGENTS.md — no HTTP logic in domain).
type FragmentFunc func(args ...interface{}) template.HTML

// WithFragment registers the {{fragment}} helper supplied by the HTTP adapter.
func WithFragment(fn FragmentFunc) Option {
	return func(o *loadOptions) {
		o.fragment = fn
	}
}

func fragmentFuncMap(fn FragmentFunc) template.FuncMap {
	if fn == nil {
		fn = func(args ...interface{}) template.HTML {
			return template.HTML("<!-- fragment: not configured -->")
		}
	}
	return template.FuncMap{
		"fragment": fn,
	}
}
