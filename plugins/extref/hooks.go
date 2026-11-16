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

// cartValidateDesignHandler blocks cart mutations when a stored line carries
// an empty design_id (visible only to this registrant on cart.validate).
func cartValidateDesignHandler(hctx *extapi.HookContext) error {
	raw, ok := hctx.Get("cart")
	if !ok || raw == nil {
		return nil
	}
	view, ok := raw.(*extapi.CartView)
	if !ok || view == nil {
		return nil
	}
	for _, line := range view.Items {
		for _, e := range line.Extensions {
			if e.Code != FieldCodeDesignID {
				continue
			}
			s, _ := e.Value.(string)
			if strings.TrimSpace(s) == "" {
				hctx.AppendValidationError(extapi.CartValidationIssue{
					Code:      "extref.design_id_required",
					Message:   "design_id is required on this line",
					VariantID: line.VariantID,
				})
			}
		}
	}
	return nil
}
