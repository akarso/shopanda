// Package pluginsdk provides typed registration helpers for plugin authors.
//
// Use pluginsdk to reduce ceremony when registering pricing steps, import row hooks,
// and outbound integration sync jobs on plugin.App during Init.
//
// Sync jobs: Integration(slug).RegisterCron / RegisterOnEvent / RegisterManual.
// Event-driven durable work → RegisterOnEvent (serve wires enqueue with
// trigger=event). Run-now → Enqueue after the composition root binds the
// resolved jobs.Queue (envelope trigger=manual). Keep Bus.OnAsync bodies
// short; do not call third parties inline.
//
// Stable contracts (hook names, step anchors, sync triggers) remain in pkg/extapi.
package pluginsdk
