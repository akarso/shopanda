package extension

import (
	"context"

	"github.com/akarso/shopanda/internal/platform/pluginwrite"
)

// WithPluginWrite marks ctx as a trusted write by registrant for the named
// Access.PluginOnlyWrite field codes only; every other plugin-only field in the
// same call stays forbidden. It panics on a blank registrant, no codes or a
// blank code so misuse fails at the call site, not as a runtime 403.
//
// Prefer PluginWriteContext (on plugin.App) wherever a panic is not already on a
// recovered goroutine. Unrecovered goroutines crash the process: OnAsync handlers
// (dispatchAsync has no recover) and any goroutine the plugin starts. Sync On
// handlers run on the Publish caller — recovered when Publish is under HTTP
// RecoveryMiddleware or Worker.safeHandle, otherwise they crash that process.
// Init recovers. The worker recovers and fails the job (panic text stored).
// Never build the codes from runtime/request input.
//
// "Plugin-only" means trusted in-process code. For fields registered with an owner
// (plugin Init, or app.ExtensionsFor) the registrant must equal that owner; fields
// without an owner accept any registrant. The registrant is a plain string, so this
// is a guard against mistakes, not a security boundary.
func WithPluginWrite(ctx context.Context, registrant string, fieldCodes ...string) context.Context {
	return pluginwrite.Grant(ctx, registrant, fieldCodes...)
}

// StripPluginWrite returns ctx without any plugin write marker.
// Call sites that forward ctx to foreign/plugin callbacks:
//   - event.Bus.Publish (via pluginwrite.Strip)
//   - pricing.Pipeline.Execute (via pluginwrite.Strip)
//   - jobs.Worker.safeHandle (via pluginwrite.Strip)
//   - checkout.Workflow.Execute
//   - cart.Service.AddItem (extension upsert keeps the grant; stripped ctx is what
//     continues into pricing — other cart methods rely on Pipeline.Execute)
//
// Hook Invoke does not take a context.Context; it cannot forward the marker.
func StripPluginWrite(ctx context.Context) context.Context { return pluginwrite.Strip(ctx) }

func pluginWriteFor(ctx context.Context, code string) (string, bool) {
	return pluginwrite.For(ctx, code)
}
