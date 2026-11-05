// Package pluginsdk provides typed registration helpers for plugin authors.
//
// Use pluginsdk to reduce ceremony when registering pricing steps, import row hooks,
// and outbound integration sync jobs on plugin.App during Init.
//
// Sync jobs: Integration(slug).RegisterCron / RegisterOnEvent / RegisterManual,
// then Enqueue after the composition root BindQueue's the resolved jobs.Queue.
// Prefer Enqueue from a tiny Bus.OnAsync body over inline third-party I/O.
//
// Stable contracts (hook names, step anchors, sync triggers) remain in pkg/extapi.
package pluginsdk
