// Package pluginsdk provides typed registration helpers for plugin authors.
//
// Use pluginsdk to reduce ceremony when registering pricing steps, import row hooks,
// and outbound integration sync jobs on plugin.App during Init.
//
// Stable contracts (hook names, step anchors, sync triggers) remain in pkg/extapi.
package pluginsdk
