package extref

import (
	"github.com/akarso/shopanda/pkg/extapi"
)

// addItemBeforeHandler demonstrates reading pending extension inputs and actor
// identity from the hook payload (PR-1548).
//
// Plugin-only field enforcement stays in ValidateBatch / UpsertBatch via
// PluginOnlyWrite + WithPluginWrite — not in this hook. A guest (or empty)
// context with a trusted write must be allowed to set gift_note through
// CartPort; storefront requests without trust are rejected at ValidateBatch.
func addItemBeforeHandler(hctx *extapi.HookContext) error {
	_, _ = hctx.Get("actor_user_id")
	_, _ = hctx.Get("actor_role")
	_, _ = hctx.Get("extensions")
	_, _ = hctx.Get("plugin_write_codes")
	return nil
}
