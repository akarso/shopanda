package extref

import (
	"strings"

	"github.com/akarso/shopanda/pkg/extapi"
)

// addItemBeforeHandler demonstrates reading pending extension inputs and actor
// identity from the hook payload (PR-1548).
//
// Blank design_id is rejected by ValidateBatch (field Required). Differing
// stored design_id is rejected before Save and again in UpsertBatchSelective
// (ExclusiveString conditional upsert in one tx with plain fields).
func addItemBeforeHandler(hctx *extapi.HookContext) error {
	_, _ = hctx.Get("actor_user_id")
	_, _ = hctx.Get("actor_role")
	_, _ = hctx.Get("extensions")
	_, _ = hctx.Get("plugin_write_codes")
	return nil
}

// cartValidateDesignHandler blocks cart mutations when a stored or pending
// line carries an empty design_id (visible only to this registrant).
// During AddItem, values live on "pending_line" until the extension upsert.
func cartValidateDesignHandler(hctx *extapi.HookContext) error {
	check := func(variantID string, exts []extapi.CartLineExtension) {
		for _, e := range exts {
			if e.Code != FieldCodeDesignID {
				continue
			}
			s, _ := e.Value.(string)
			if strings.TrimSpace(s) == "" {
				hctx.AppendValidationError(extapi.CartValidationIssue{
					Code:      "extref.design_id_required",
					Message:   "design_id is required on this line",
					VariantID: variantID,
				})
			}
		}
	}
	if raw, ok := hctx.Get("cart"); ok && raw != nil {
		if view, ok := raw.(*extapi.CartView); ok && view != nil {
			for _, line := range view.Items {
				check(line.VariantID, line.Extensions)
			}
		}
	}
	if raw, ok := hctx.Get("pending_line"); ok && raw != nil {
		if pending, ok := raw.(*extapi.PendingCartLine); ok && pending != nil {
			check(pending.VariantID, pending.Extensions)
		}
	}
	return nil
}
