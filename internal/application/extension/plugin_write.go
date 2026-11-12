package extension

import (
	"context"

	"github.com/akarso/shopanda/internal/platform/pluginwrite"
)

// WithPluginWrite marks ctx as a trusted write by registrant for the named
// Access.PluginOnlyWrite field codes only; every other plugin-only field in the
// same call stays forbidden. It panics on a blank registrant, no codes or a
// blank code so misuse fails at the call site, not as a runtime 403. A panic is
// NOT recovered outside HTTP handlers (events, workers, goroutines): never build
// the codes from runtime/request input.
//
// "Plugin-only" means trusted in-process code, not the owning plugin: the
// registry does not record field owners, and the registrant is an advisory label.
func WithPluginWrite(ctx context.Context, registrant string, fieldCodes ...string) context.Context {
	return pluginwrite.Grant(ctx, registrant, fieldCodes...)
}

// StripPluginWrite returns ctx without any plugin write marker. The event bus
// strips it on Publish by default; core code that forwards ctx elsewhere
// (pricing, hooks) should call it too.
func StripPluginWrite(ctx context.Context) context.Context { return pluginwrite.Strip(ctx) }

func pluginWriteFor(ctx context.Context, code string) (string, bool) {
	return pluginwrite.For(ctx, code)
}
