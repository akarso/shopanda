// Package pluginsdk provides typed registration helpers for plugin authors.
//
// Preferred Init shape (Track E): implement pluginsdk.Plugin with Init(Host),
// then register via pluginsdk.Adapt(p) on the core plugin.Registry.
// Host covers hooks, routes, permissions, and pricing/checkout/composition
// registration without importing internal/platform/plugin in product Init code.
//
// Dual-mode: existing plugins may still use Init(*plugin.App). SDK.New accepts
// *plugin.App; NewHost accepts Host.
//
// Use pluginsdk to reduce ceremony when registering pricing steps, import row hooks,
// and outbound integration sync jobs during Init.
//
// Sync jobs: Integration(slug).RegisterCron / RegisterOnEvent / RegisterManual.
// Event-driven durable work → RegisterOnEvent (serve wires enqueue with
// trigger=event). Run-now → Enqueue after the composition root binds the
// resolved jobs.Queue (envelope trigger=manual). Keep Bus.OnAsync bodies
// short; do not call third parties inline.
//
// Stable contracts (hook names, step anchors, sync triggers) remain in pkg/extapi.
package pluginsdk
