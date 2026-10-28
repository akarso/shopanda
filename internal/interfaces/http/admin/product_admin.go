package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type bundleComponentTypeGuard interface {
	CountByBundleID(ctx context.Context, bundleProductID string) (int, error)
	CountByComponentID(ctx context.Context, componentProductID string) (int, error)
}

// txStarter begins database transactions. *sql.DB implements it.
type txStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// txProductRepo is a product repository that can bind to a transaction.
type txProductRepo interface {
	catalog.ProductRepository
	WithTx(tx *sql.Tx) catalog.ProductRepository
}

// lockingProductRepo is a transaction-bound product repository that can lock a row.
// LockTypeAndModes returns type, modes, explode, and status from the same FOR UPDATE.
type lockingProductRepo interface {
	catalog.ProductRepository
	LockTypeAndModes(ctx context.Context, id string) (catalog.Type, catalog.PricingMode, catalog.StockMode, bool, catalog.Status, error)
}

// linkedParentLockRepo reads requires_linked_parent from the locked row.
// An omitted update uses it so a stale FindByID cannot write the flag back.
type linkedParentLockRepo interface {
	RequiresLinkedParentForUpdate(ctx context.Context, id string) (bool, error)
}

// txBundleComponentRepo is a bundle component repository that can bind to a transaction.
type txBundleComponentRepo interface {
	WithTx(tx *sql.Tx) catalog.BundleComponentRepository
}

type groupedMemberTypeGuard interface {
	CountByGroupID(ctx context.Context, groupProductID string) (int, error)
	CountByMemberID(ctx context.Context, memberProductID string) (int, error)
}

// txGroupedMemberRepo is a grouped member repository that can bind to a transaction.
// WithTx is implemented on the postgres adapter only.
type txGroupedMemberRepo interface {
	WithTx(tx *sql.Tx) catalog.GroupedMemberRepository
}

type downloadableFileTypeGuard interface {
	CountByProductID(ctx context.Context, productID string) (int, error)
}

// txDownloadableFileRepo is a downloadable file repository that can bind to a transaction.
type txDownloadableFileRepo interface {
	WithTx(tx *sql.Tx) catalog.DownloadableFileRepository
}

// ProductAdminHandler serves product write endpoints.
type ProductAdminHandler struct {
	repo              catalog.ProductRepository
	bundleComponents  bundleComponentTypeGuard
	groupedMembers    groupedMemberTypeGuard
	downloadableFiles downloadableFileTypeGuard
	variants          catalog.VariantRepository
	prices            pricing.PriceRepository
	stock             inventory.StockRepository
	txStarter         txStarter
	bus               *event.Bus
	auditor           *admin.Auditor
	log               logger.Logger

	// facts + opts + enforced back the "visibility" payload on Get. Nil facts
	// (the default — WithVisibilityFacts not called) omits the
	// key entirely; existing callers see no behavior change. enforced mirrors
	// catalog.visibility.enforce; the GET payload reports it as-is (the SPA
	// shows a section-level "Enforcement off — preview" caption when it is
	// false, instead of annotating every individual badge).
	facts    catalog.VisibilityFactsAdminSource
	opts     catalog.VisibilityOptions
	enforced bool
}

type productCategoryAssignmentReader interface {
	ListCategoryIDsByProduct(ctx context.Context, productID string) ([]string, error)
}

// adminAxisExplanation is the GET /admin/products/{id} wire DTO for one
// visibility axis explanation. catalog.AxisExplanation carries
// no JSON tags — the domain stays HTTP-agnostic — so this type owns the
// snake_case shape. FailureCodes is always emitted (never omitted, even
// empty) so OpenAPI examples and the actual wire response match exactly on
// every axis, forced or auto.
type adminAxisExplanation struct {
	Mode         string   `json:"mode"`
	Override     bool     `json:"override"`
	Resolved     bool     `json:"resolved"`
	Effective    bool     `json:"effective"`
	FailureCodes []string `json:"failure_codes"`
}

func newAdminAxisExplanation(exp catalog.AxisExplanation) adminAxisExplanation {
	codes := exp.FailureCodes
	if codes == nil {
		codes = []string{}
	}
	return adminAxisExplanation{
		Mode:         string(exp.Mode),
		Override:     exp.Override,
		Resolved:     exp.Resolved,
		Effective:    exp.Effective,
		FailureCodes: codes,
	}
}

// NewProductAdminHandler creates a ProductAdminHandler.
func NewProductAdminHandler(repo catalog.ProductRepository, bus *event.Bus) *ProductAdminHandler {
	log := logger.New("info")
	return NewProductAdminHandlerWithAuditor(repo, bus, admin.NewAuditor(log), log)
}

// NewProductAdminHandlerWithAuditor creates a ProductAdminHandler with a custom auditor.
func NewProductAdminHandlerWithAuditor(repo catalog.ProductRepository, bus *event.Bus, auditor *admin.Auditor, log logger.Logger) *ProductAdminHandler {
	if log == nil {
		log = logger.New("warn")
	}
	return &ProductAdminHandler{repo: repo, bus: bus, auditor: auditor, log: log}
}

// WithVisibilityFacts wires AutoBasis facts for the GET "visibility" payload
// (PR-1059). facts loads regardless of product Status — unlike the public
// VisibilityFactsSource gate, the admin GUI must explain axes for
// draft/archived products too. enforced mirrors catalog.visibility.enforce
// (whether the public axis gate is actually on) so the GET payload can tell
// the SPA whether "effective" reflects reality or a hypothetical. Returns the
// receiver for chaining, matching storefront handlers' WithVisibilityFacts
// convention.
func (h *ProductAdminHandler) WithVisibilityFacts(facts catalog.VisibilityFactsAdminSource, opts catalog.VisibilityOptions, enforced bool) *ProductAdminHandler {
	h.facts = facts
	h.opts = opts
	h.enforced = enforced
	return h
}

// WithBundleComponents wires bundle composition counts for product type-change guards (PR-1060).
// The field stays optional: updates that do not change type succeed when it is nil.
// A type change with a nil guard fails closed.
func (h *ProductAdminHandler) WithBundleComponents(components bundleComponentTypeGuard) *ProductAdminHandler {
	h.bundleComponents = components
	return h
}

// WithGroupedMembers wires grouped membership counts for product type-change guards (PR-1061).
// The field stays optional: updates that do not change type succeed when it is nil.
// A type change with a nil guard fails closed. Membership counts do not affect Purchasable or visibility.
func (h *ProductAdminHandler) WithGroupedMembers(members groupedMemberTypeGuard) *ProductAdminHandler {
	h.groupedMembers = members
	return h
}

// WithDownloadableFiles wires downloadable attachment counts for type-change
// and StatusActive guards (PR-1074). The field stays optional: updates that
// do not change type and are not becoming / staying Active downloadable via
// Create, Draft→Active, or type-change succeed when it is nil. Soft-write on
// an already-Active file-less downloadable returns before the nil-guard check
// (sell path still blocks purchase). Create / Draft→Active / type-change onto
// Active downloadable with a nil guard fail closed.
func (h *ProductAdminHandler) WithDownloadableFiles(files downloadableFileTypeGuard) *ProductAdminHandler {
	h.downloadableFiles = files
	return h
}

// WithTxStarter wires the transaction beginner used for every product update
// that persists. *sql.DB satisfies this. The update locks the row and re-reads
// type, pricing_mode, and stock_mode before writing, so a nil starter fails closed.
func (h *ProductAdminHandler) WithTxStarter(starter txStarter) *ProductAdminHandler {
	h.txStarter = starter
	return h
}

// WithVariantPrices wires the reads that reject dynamic pricing while a
// variant of the product still has a stored price row. Nil repos fail closed
// when a write would persist dynamic mode. cmd/api always sets both.
func (h *ProductAdminHandler) WithVariantPrices(variants catalog.VariantRepository, prices pricing.PriceRepository) *ProductAdminHandler {
	h.variants = variants
	h.prices = prices
	return h
}

// WithVariantStock wires the reads that reject dynamic stock while a variant
// of the product still has stock.quantity > 0. Nil stock fails closed when a
// write would persist dynamic stock mode. cmd/api always sets it.
func (h *ProductAdminHandler) WithVariantStock(stock inventory.StockRepository) *ProductAdminHandler {
	h.stock = stock
	return h
}

const ownPriceVariantPage = 100

func (h *ProductAdminHandler) rejectDynamicOwnPrices(ctx context.Context, product *catalog.Product) error {
	if product == nil || product.Type != catalog.TypeBundle || product.PricingMode != catalog.PricingModeDynamic {
		return nil
	}
	if h.variants == nil || h.prices == nil {
		return apperror.Internal("product admin: variant price check is not configured")
	}
	// An empty id has no price rows. A new product id lists zero variants
	// and returns nil once the repositories above are wired.
	if strings.TrimSpace(product.ID) == "" {
		return nil
	}
	offset := 0
	for {
		variants, err := h.variants.ListByProductID(ctx, product.ID, offset, ownPriceVariantPage)
		if err != nil {
			return fmt.Errorf("product admin: list variants: %w", err)
		}
		for _, variant := range variants {
			rows, err := h.prices.ListByVariantID(ctx, variant.ID)
			if err != nil {
				return fmt.Errorf("product admin: list prices: %w", err)
			}
			if len(rows) > 0 {
				return apperror.Validation(catalog.DynamicBundleOwnPricesMessage)
			}
		}
		if len(variants) < ownPriceVariantPage {
			return nil
		}
		offset += ownPriceVariantPage
	}
}

func (h *ProductAdminHandler) rejectDynamicOwnStock(ctx context.Context, product *catalog.Product) error {
	return h.rejectDynamicOwnStockOn(ctx, product, h.variants, h.stock)
}

func (h *ProductAdminHandler) rejectDynamicOwnStockOn(ctx context.Context, product *catalog.Product, variants catalog.VariantRepository, stock inventory.StockRepository) error {
	if product == nil || product.Type != catalog.TypeBundle || product.StockMode != catalog.StockModeDynamic {
		return nil
	}
	if variants == nil || stock == nil {
		return apperror.Internal("product admin: variant stock check is not configured")
	}
	if strings.TrimSpace(product.ID) == "" {
		return nil
	}
	offset := 0
	for {
		page, err := variants.ListByProductID(ctx, product.ID, offset, ownPriceVariantPage)
		if err != nil {
			return fmt.Errorf("product admin: list variants: %w", err)
		}
		for _, variant := range page {
			entry, err := readVariantStock(ctx, stock, variant.ID)
			if err != nil {
				return fmt.Errorf("product admin: get stock: %w", err)
			}
			if entry.Quantity > 0 {
				return apperror.Validation(catalog.DynamicBundleOwnStockMessage)
			}
		}
		if len(page) < ownPriceVariantPage {
			return nil
		}
		offset += ownPriceVariantPage
	}
}

// readVariantStock prefers SELECT … FOR UPDATE on a transaction-bound stock
// repo so a concurrent adjust cannot commit a quantity between this check
// and the product update.
func readVariantStock(ctx context.Context, stock inventory.StockRepository, variantID string) (inventory.StockEntry, error) {
	type stockForUpdate interface {
		GetStockForUpdate(ctx context.Context, variantID string) (inventory.StockEntry, error)
	}
	if locked, ok := stock.(stockForUpdate); ok {
		return locked.GetStockForUpdate(ctx, variantID)
	}
	return stock.GetStock(ctx, variantID)
}

func bindVariantStock(tx *sql.Tx, variants catalog.VariantRepository, stock inventory.StockRepository) (catalog.VariantRepository, inventory.StockRepository, error) {
	if variants != nil {
		txVariants, ok := variants.(interface {
			WithTx(tx *sql.Tx) catalog.VariantRepository
		})
		if !ok {
			return nil, nil, fmt.Errorf("product admin: variant repository does not support transactions")
		}
		variants = txVariants.WithTx(tx)
	}
	if stock != nil {
		txStock, ok := stock.(interface {
			WithTx(tx *sql.Tx) inventory.StockRepository
		})
		if !ok {
			return nil, nil, fmt.Errorf("product admin: stock repository does not support transactions")
		}
		stock = txStock.WithTx(tx)
	}
	return variants, stock, nil
}

func (h *ProductAdminHandler) validateProductTypeChange(ctx context.Context, components bundleComponentTypeGuard, members groupedMemberTypeGuard, files downloadableFileTypeGuard, productID string, oldType, newType catalog.Type) error {
	if oldType == newType {
		return nil
	}
	if components == nil {
		return apperror.Internal("product admin: bundle component repository is not configured")
	}
	if members == nil {
		return apperror.Internal("product admin: grouped member repository is not configured")
	}
	if files == nil {
		return apperror.Internal("product admin: downloadable file repository is not configured")
	}
	// Row-gated: illicit component rows block a non-bundle target even when the
	// locked type is not bundle (the stored type can be wrong).
	if newType != catalog.TypeBundle {
		n, err := components.CountByBundleID(ctx, productID)
		if err != nil {
			return fmt.Errorf("product admin: count bundle components: %w", err)
		}
		if n > 0 {
			return apperror.Validation("cannot change type: product has bundle components")
		}
	}
	if newType.IsValid() && !newType.IsBundleComponentLeafType() {
		n, err := components.CountByComponentID(ctx, productID)
		if err != nil {
			return fmt.Errorf("product admin: count bundle component usage: %w", err)
		}
		if n > 0 {
			return apperror.Validation("cannot change type: product is used as a bundle component")
		}
	}
	// Same rule for grouped membership. CountByGroupID > 0 blocks any target
	// other than grouped, including when the locked type was never grouped.
	if newType != catalog.TypeGrouped {
		n, err := members.CountByGroupID(ctx, productID)
		if err != nil {
			return fmt.Errorf("product admin: count grouped members: %w", err)
		}
		if n > 0 {
			return apperror.Validation("cannot change type: product has grouped members")
		}
	}
	if newType.IsValid() && !newType.IsGroupedMemberLeafType() {
		n, err := members.CountByMemberID(ctx, productID)
		if err != nil {
			return fmt.Errorf("product admin: count grouped member usage: %w", err)
		}
		if n > 0 {
			return apperror.Validation("cannot change type: product is used as a grouped member")
		}
	}
	// Leaving downloadable with attached files is blocked (PR-1074). Admin
	// HTTP for attach/remove is PR-1078; operators must clear rows first.
	if newType != catalog.TypeDownloadable {
		n, err := files.CountByProductID(ctx, productID)
		if err != nil {
			return fmt.Errorf("product admin: count downloadable files: %w", err)
		}
		if n > 0 {
			return apperror.Validation("cannot change type: product has downloadable files")
		}
	}
	return nil
}

// ensureActiveDownloadableHasFile rejects becoming StatusActive on a
// downloadable product with zero attachments (PR-1074). Soft-write: an
// already-active downloadable with zero files may keep Active for name-only
// (and other non-activating) updates — sell path and visibility still block
// purchase. Active→Draft is always allowed. Create / Draft→Active / type
// change onto active downloadable without files still fail closed. Admin
// attach HTTP remains PR-1078.
func (h *ProductAdminHandler) ensureActiveDownloadableHasFile(
	ctx context.Context,
	files downloadableFileTypeGuard,
	product *catalog.Product,
	previousStatus catalog.Status,
	previousType catalog.Type,
) error {
	if product == nil || product.Type != catalog.TypeDownloadable || product.Status != catalog.StatusActive {
		return nil
	}
	if previousType == catalog.TypeDownloadable && previousStatus == catalog.StatusActive {
		return nil
	}
	if files == nil {
		return apperror.Internal("product admin: downloadable file repository is not configured")
	}
	n, err := files.CountByProductID(ctx, product.ID)
	if err != nil {
		return fmt.Errorf("product admin: count downloadable files: %w", err)
	}
	if n < 1 {
		return apperror.Validation(catalog.DownloadableRequiresFileMessage)
	}
	return nil
}

func (h *ProductAdminHandler) getAdminID(r *http.Request) string {
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil || ac.AdminID == "" {
		id := auth.IdentityFrom(r.Context())
		if id.Role == identity.RoleAdmin {
			h.log.Warn("admin.context.missing", map[string]interface{}{
				"component":   "product_admin.getAdminID",
				"path":        r.URL.Path,
				"admin_id":    "system",
				"identity_id": id.UserID,
				"reason":      "admin context missing or empty",
			})
		}
		return "system"
	}
	return ac.AdminID
}

func productAdminScopeDetailsFromRequest(r *http.Request) map[string]interface{} {
	return fullAdminScopeDetailsFromRequest(r)
}

// List handles GET /api/v1/admin/products.
func (h *ProductAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := httpshared.ParsePagination(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		adminID := h.getAdminID(r)
		filter := catalog.ListFilter{Offset: offset, Limit: limit}

		rawType := strings.TrimSpace(r.URL.Query().Get("type"))
		if rawType != "" {
			typ := catalog.Type(rawType)
			if !typ.IsValid() {
				msg := catalog.InvalidTypeMessage(typ)
				details := productAdminScopeDetailsFromRequest(r)
				details["offset"] = offset
				details["limit"] = limit
				details["type"] = rawType
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditProductRead,
					ResourceType: "products",
					Details:      details,
					Result:       "error",
					Error:        msg,
				})
				httpshared.JSONError(w, apperror.Validation(msg))
				return
			}
			filter.Type = typ
		}

		products, err := h.repo.List(r.Context(), filter)
		if err != nil {
			details := productAdminScopeDetailsFromRequest(r)
			details["offset"] = offset
			details["limit"] = limit
			if filter.Type != "" {
				details["type"] = string(filter.Type)
			}
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "products",
				Details:      details,
				Result:       "error",
				Error:        err.Error(),
			})
			httpshared.JSONError(w, err)
			return
		}
		if products == nil {
			products = []catalog.Product{}
		}

		details := productAdminScopeDetailsFromRequest(r)
		details["offset"] = offset
		details["limit"] = limit
		if filter.Type != "" {
			details["type"] = string(filter.Type)
		}
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductRead,
			ResourceType: "products",
			Result:       "success",
			Details:      details,
		})

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"products": products,
		})
	}
}

// Get handles GET /api/v1/admin/products/{id}.
func (h *ProductAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := r.PathValue("id")
		adminID := h.getAdminID(r)
		if pid == "" {
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				Result:       "error",
				Error:        "product id is required",
				Details:      details,
			})
			httpshared.JSONError(w, apperror.Validation("product id is required"))
			return
		}

		product, err := h.repo.FindByID(r.Context(), pid)
		if err != nil {
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}
		if product == nil {
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        "product not found",
				Details:      details,
			})
			httpshared.JSONError(w, apperror.NotFound("product not found"))
			return
		}

		assignments, ok := h.repo.(productCategoryAssignmentReader)
		if !ok {
			err := apperror.Internal("product repository missing category lookup capability")
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        "product repository missing category lookup capability",
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}

		categoryIDs, err := assignments.ListCategoryIDsByProduct(r.Context(), pid)
		if err != nil {
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}
		if categoryIDs == nil {
			categoryIDs = []string{}
		}

		// visibility is informational (not a gate — see WithVisibilityFacts)
		// and must never turn a facts-load failure into a 500 for the whole
		// product read: a transient facts error degrades to a logged warning
		// and a "visibility_error" string in the response, with "visibility"
		// omitted. A missing snapshot (snap, ok := snaps[pid] is false, no
		// error) also sets "visibility_error": this repo's facts query
		// joins live product/variant/stock/price rows (no async "first
		// refresh" step), so the only way FindByID above finds the product
		// but the facts query below does not is a concurrent delete between
		// the two reads — a real (if rare) failure to explain, not a
		// "nothing to show yet" pending state like a true pre-save create.
		var visibilityPayload map[string]interface{}
		var visibilityErr string
		if h.facts != nil {
			snaps, err := h.facts.LoadManyAnyStatus(r.Context(), []string{pid})
			if err != nil {
				h.log.Warn("product_admin.visibility_facts.load_failed", map[string]interface{}{
					"component":  "product_admin.Get",
					"product_id": pid,
					"error":      err.Error(),
				})
				visibilityErr = "visibility facts unavailable"
			} else if snap, ok := snaps[pid]; ok {
				// Prefer the loaded product row's Status/VisibilityModes over
				// the snapshot's — they can diverge if the product changed
				// between the FindByID above and this LoadManyAnyStatus call.
				inputs := snap.Inputs
				inputs.RequiresLinkedParent = product.RequiresLinkedParent
				inputs.Assignment = catalog.AssignmentContextFrom(r.Context())
				autoBasis, codes, axesExp := catalog.ExplainAxes(product.VisibilityModes, product.Status, inputs, h.opts)
				if codes == nil {
					codes = []string{}
				}
				// input_failure_codes excludes AutoBasisCodeStatusInactive so
				// the SPA can recompute auto_basis/failure_codes when the form
				// status changes. failure_codes stays the full list for the
				// product's saved status.
				inputCodes := catalog.AutoBasisInputFailureCodes(snap.Inputs, h.opts)
				if inputCodes == nil {
					inputCodes = []string{}
				}
				visibilityPayload = map[string]interface{}{
					"enforced":                   h.enforced,
					"status":                     string(product.Status),
					"auto_basis":                 autoBasis,
					"failure_codes":              codes,
					"input_failure_codes":        inputCodes,
					"requires_linked_parent":     product.RequiresLinkedParent,
					"missing_downloadable_file":  inputs.MissingDownloadableFile,
					"axes": map[string]interface{}{
						"catalog":      newAdminAxisExplanation(axesExp.Catalog),
						"search":       newAdminAxisExplanation(axesExp.Search),
						"individually": newAdminAxisExplanation(axesExp.Individually),
						"purchasable":  newAdminAxisExplanation(axesExp.Purchasable),
					},
				}
			} else {
				h.log.Warn("product_admin.visibility_facts.snapshot_missing", map[string]interface{}{
					"component":  "product_admin.Get",
					"product_id": pid,
				})
				visibilityErr = "product not found in visibility facts"
			}
		}

		details := productAdminScopeDetailsFromRequest(r)
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductRead,
			ResourceType: "product",
			ResourceID:   pid,
			Result:       "success",
			Details:      details,
		})

		resp := map[string]interface{}{
			"product":      product,
			"category_ids": categoryIDs,
		}
		if visibilityPayload != nil {
			resp["visibility"] = visibilityPayload
		}
		if visibilityErr != "" {
			resp["visibility_error"] = visibilityErr
		}
		httpshared.JSON(w, http.StatusOK, resp)
	}
}

// visibilityModesRequest is the JSON shape for a *partial* update of
// catalog.VisibilityAxes on create/update requests. Every field
// is an optional pointer: an omitted axis keeps whatever mergeVisibilityModes
// was given as a base (DefaultVisibilityAxes() on create, the product's
// existing modes on update) — a caller that only wants to flip one axis does
// not have to resend the other three.
type visibilityModesRequest struct {
	Catalog      *string `json:"catalog"`
	Search       *string `json:"search"`
	Individually *string `json:"individually"`
	Purchasable  *string `json:"purchasable"`
}

// mergeVisibilityModes applies only the present (non-nil) fields of req onto
// base, leaving every omitted axis untouched, then validates the merged
// result. A nil req returns base unchanged. Empty string is not a valid mode
// (caught by Validate, same as any other unrecognised value).
func mergeVisibilityModes(base catalog.VisibilityAxes, req *visibilityModesRequest) (catalog.VisibilityAxes, error) {
	if req == nil {
		return base, nil
	}
	out := base
	if req.Catalog != nil {
		out.Catalog = catalog.VisibilityMode(*req.Catalog)
	}
	if req.Search != nil {
		out.Search = catalog.VisibilityMode(*req.Search)
	}
	if req.Individually != nil {
		out.Individually = catalog.VisibilityMode(*req.Individually)
	}
	if req.Purchasable != nil {
		out.Purchasable = catalog.VisibilityMode(*req.Purchasable)
	}
	if err := out.Validate(); err != nil {
		return catalog.VisibilityAxes{}, err
	}
	return out, nil
}

// visibilityModesMap renders axes as a plain string map for audit Details
// (before/after snapshots on Update).
func visibilityModesMap(axes catalog.VisibilityAxes) map[string]string {
	return map[string]string{
		"catalog":      string(axes.Catalog),
		"search":       string(axes.Search),
		"individually": string(axes.Individually),
		"purchasable":  string(axes.Purchasable),
	}
}

// createProductRequest is the JSON body for creating a product.
type createProductRequest struct {
	Name                 string                  `json:"name"`
	Slug                 string                  `json:"slug"`
	Description          string                  `json:"description"`
	Type                 string                  `json:"type"`
	PricingMode          *string                 `json:"pricing_mode"`
	StockMode            *string                 `json:"stock_mode"`
	ExplodeOrderLines    *bool                   `json:"explode_order_lines"`
	RequiresLinkedParent *bool                   `json:"requires_linked_parent"`
	VisibilityModes      *visibilityModesRequest `json:"visibility_modes"`
	Attributes           map[string]interface{}  `json:"attributes"`
}

// updateProductRequest is the JSON body for updating a product.
type updateProductRequest struct {
	Name                 *string                 `json:"name"`
	Slug                 *string                 `json:"slug"`
	Description          *string                 `json:"description"`
	Status               *string                 `json:"status"`
	Type                 *string                 `json:"type"`
	PricingMode          *string                 `json:"pricing_mode"`
	StockMode            *string                 `json:"stock_mode"`
	ExplodeOrderLines    *bool                   `json:"explode_order_lines"`
	RequiresLinkedParent *bool                   `json:"requires_linked_parent"`
	VisibilityModes      *visibilityModesRequest `json:"visibility_modes"`
	Attributes           map[string]interface{}  `json:"attributes"`
}

// Create handles POST /api/v1/admin/products.
func (h *ProductAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminID := h.getAdminID(r)
		details := productAdminScopeDetailsFromRequest(r)

		var req createProductRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        "invalid request body",
				Details:      details,
			})
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		product, err := catalog.NewProduct(id.New(), req.Name, req.Slug)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		product.Description = req.Description
		if rawType := strings.TrimSpace(req.Type); rawType != "" {
			product.Type = catalog.Type(rawType)
		}
		if err := applyCreatePricingMode(&product, req); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}
		if err := applyCreateStockMode(&product, req); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}
		applyCreateRequiresLinkedParent(&product, req)
		if err := applyCreateExplodeOrderLines(&product, req); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}
		// Omitted VisibilityModes (or an omitted axis within it) keeps
		// NewProduct's DefaultVisibilityAxes() (auto) for that axis; present
		// axes merge on top. Invalid mode values are caught here.
		modes, err := mergeVisibilityModes(product.VisibilityModes, req.VisibilityModes)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		product.VisibilityModes = modes
		if req.Attributes != nil {
			product.Attributes = req.Attributes
		}
		if err := h.rejectDynamicOwnPrices(r.Context(), &product); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        adminAuditMessage(err),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}
		if err := h.rejectDynamicOwnStock(r.Context(), &product); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        adminAuditMessage(err),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}
		if err := product.Validate(); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		// Defense in depth: create request has no status field today (always
		// Draft via NewProduct). If Active create is added later, require a file.
		if err := h.ensureActiveDownloadableHasFile(r.Context(), h.downloadableFiles, &product, "", ""); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        adminAuditMessage(err),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}

		if err := h.repo.Create(r.Context(), &product); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				ResourceID:   product.ID,
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			httpshared.JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductCreate,
			ResourceType: "product",
			ResourceID:   product.ID,
			Result:       "success",
			Details:      details,
		})

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventProductCreated, "product.admin", catalog.ProductCreatedData{
			ProductID: product.ID,
			Name:      product.Name,
			Slug:      product.Slug,
			Status:    product.Status,
		}))

		httpshared.JSON(w, http.StatusCreated, map[string]interface{}{
			"product": product,
		})
	}
}

// Update handles PUT /api/v1/admin/products/{id}.
func adminAuditMessage(err error) string {
	var appErr *apperror.Error
	if errors.As(err, &appErr) && appErr.Message != "" {
		return appErr.Message
	}
	return err.Error()
}

func applyCreatePricingMode(product *catalog.Product, req createProductRequest) error {
	raw := ""
	if req.PricingMode != nil {
		raw = *req.PricingMode
	}
	if err := product.ApplyPricingMode(raw, req.PricingMode != nil); err != nil {
		return apperror.Validation(err.Error())
	}
	return nil
}

func applyPricingModePatch(product *catalog.Product, req updateProductRequest) error {
	raw := ""
	if req.PricingMode != nil {
		raw = *req.PricingMode
	}
	if err := product.ApplyPricingMode(raw, req.PricingMode != nil); err != nil {
		return apperror.Validation(err.Error())
	}
	return nil
}

func applyCreateStockMode(product *catalog.Product, req createProductRequest) error {
	raw := ""
	if req.StockMode != nil {
		raw = *req.StockMode
	}
	if err := product.ApplyStockMode(raw, req.StockMode != nil); err != nil {
		return apperror.Validation(err.Error())
	}
	return nil
}

func applyStockModePatch(product *catalog.Product, req updateProductRequest) error {
	raw := ""
	if req.StockMode != nil {
		raw = *req.StockMode
	}
	if err := product.ApplyStockMode(raw, req.StockMode != nil); err != nil {
		return apperror.Validation(err.Error())
	}
	return nil
}

func applyCreateRequiresLinkedParent(product *catalog.Product, req createProductRequest) {
	if req.RequiresLinkedParent == nil {
		return
	}
	product.RequiresLinkedParent = *req.RequiresLinkedParent
}

func applyRequiresLinkedParentPatch(product *catalog.Product, req updateProductRequest) {
	if req.RequiresLinkedParent == nil {
		return
	}
	product.RequiresLinkedParent = *req.RequiresLinkedParent
}

func applyCreateExplodeOrderLines(product *catalog.Product, req createProductRequest) error {
	value := false
	if req.ExplodeOrderLines != nil {
		value = *req.ExplodeOrderLines
	}
	if err := product.ApplyExplodeOrderLines(value, req.ExplodeOrderLines != nil); err != nil {
		return apperror.Validation(err.Error())
	}
	return nil
}

func applyExplodeOrderLinesPatch(product *catalog.Product, req updateProductRequest) error {
	value := false
	if req.ExplodeOrderLines != nil {
		value = *req.ExplodeOrderLines
	}
	if err := product.ApplyExplodeOrderLines(value, req.ExplodeOrderLines != nil); err != nil {
		return apperror.Validation(err.Error())
	}
	return nil
}

func applyProductScalarPatch(product *catalog.Product, req updateProductRequest) error {
	if req.Name != nil {
		if *req.Name == "" {
			return apperror.Validation("name must not be empty")
		}
		product.Name = *req.Name
	}
	if req.Slug != nil {
		if *req.Slug == "" {
			return apperror.Validation("slug must not be empty")
		}
		product.Slug = *req.Slug
	}
	if req.Description != nil {
		product.Description = *req.Description
	}
	if req.Status != nil {
		s := catalog.Status(*req.Status)
		if !s.IsValid() {
			return apperror.Validation("invalid status")
		}
		product.Status = s
	}
	return nil
}

func applyProductVisibilityAndAttributes(product *catalog.Product, req updateProductRequest) (before, after catalog.VisibilityAxes, changed bool, err error) {
	before = product.VisibilityModes
	after, err = mergeVisibilityModes(product.VisibilityModes, req.VisibilityModes)
	if err != nil {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
	}
	product.VisibilityModes = after
	changed = req.VisibilityModes != nil && before != after
	if req.Attributes != nil {
		product.Attributes = req.Attributes
	}
	return before, after, changed, nil
}

// updateProductWithTypeLock locks the product row, validates an optional type
// change against that locked type, then writes every patched field in the same
// transaction. Every persisting product update uses it, including a name-only
// edit. newType nil keeps the locked type. Omitted pricing_mode, stock_mode,
// status, and requires_linked_parent keep the locked row. An omitted
// explode_order_lines keeps the locked flag only while the product is still
// allowed to explode; otherwise it is stored false. ApplyPricingMode,
// ApplyStockMode, and ApplyExplodeOrderLines do not see the earlier FindByID.
func (h *ProductAdminHandler) updateProductWithTypeLock(ctx context.Context, product *catalog.Product, req updateProductRequest, newType *catalog.Type) (before, after catalog.VisibilityAxes, changed bool, err error) {
	if h.txStarter == nil {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, apperror.Internal("product admin: transaction starter is not configured")
	}
	tx, err := h.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, fmt.Errorf("product admin: begin tx: %w", err)
	}
	defer tx.Rollback()

	txProducts, ok := h.repo.(txProductRepo)
	if !ok {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, fmt.Errorf("product admin: product repository does not support transactions")
	}
	boundProducts := txProducts.WithTx(tx)
	locker, ok := boundProducts.(lockingProductRepo)
	if !ok {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, fmt.Errorf("product admin: product repository does not support row locks")
	}
	oldType, lockedPricing, lockedStock, lockedExplode, lockedStatus, err := locker.LockTypeAndModes(ctx, product.ID)
	if err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
		}
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, fmt.Errorf("product admin: lock product: %w", err)
	}
	// Omitted pricing_mode, stock_mode, explode_order_lines, and status defaults
	// use the locked row, not the earlier FindByID.
	product.Type = oldType
	product.PricingMode = lockedPricing
	product.StockMode = lockedStock
	product.ExplodeOrderLines = lockedExplode
	if req.Status == nil {
		product.Status = lockedStatus
	}

	components := h.bundleComponents
	if h.bundleComponents != nil {
		txComponents, ok := h.bundleComponents.(txBundleComponentRepo)
		if !ok {
			return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, fmt.Errorf("product admin: bundle component repository does not support transactions")
		}
		components = txComponents.WithTx(tx)
	}
	members := h.groupedMembers
	if h.groupedMembers != nil {
		txMembers, ok := h.groupedMembers.(txGroupedMemberRepo)
		if !ok {
			return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, fmt.Errorf("product admin: grouped member repository does not support transactions")
		}
		members = txMembers.WithTx(tx)
	}
	files := h.downloadableFiles
	if h.downloadableFiles != nil {
		txFiles, ok := h.downloadableFiles.(txDownloadableFileRepo)
		if !ok {
			return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, fmt.Errorf("product admin: downloadable file repository does not support transactions")
		}
		files = txFiles.WithTx(tx)
	}
	if newType != nil {
		if err := h.validateProductTypeChange(ctx, components, members, files, product.ID, oldType, *newType); err != nil {
			return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
		}
		product.Type = *newType
	}
	if err := applyPricingModePatch(product, req); err != nil {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
	}
	if err := applyStockModePatch(product, req); err != nil {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
	}
	if err := applyExplodeOrderLinesPatch(product, req); err != nil {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
	}
	// Omitted requires_linked_parent keeps the locked row, not the earlier FindByID.
	if req.RequiresLinkedParent == nil {
		reader, ok := boundProducts.(linkedParentLockRepo)
		if !ok {
			return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, fmt.Errorf("product admin: product repository does not support requires_linked_parent lock read")
		}
		flag, err := reader.RequiresLinkedParentForUpdate(ctx, product.ID)
		if err != nil {
			if apperror.Is(err, apperror.CodeNotFound) {
				return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
			}
			return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, fmt.Errorf("product admin: read requires_linked_parent: %w", err)
		}
		product.RequiresLinkedParent = flag
	}
	applyRequiresLinkedParentPatch(product, req)
	// Own-price rejection applies when this request changes type or mode.
	// A name-only edit must keep the locked mode and must not fail closed
	// because variant price repos are unwired.
	if newType != nil || req.PricingMode != nil {
		if err := h.rejectDynamicOwnPrices(ctx, product); err != nil {
			return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
		}
	}
	if newType != nil || req.StockMode != nil {
		variants, stock, err := bindVariantStock(tx, h.variants, h.stock)
		if err != nil {
			return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
		}
		if err := h.rejectDynamicOwnStockOn(ctx, product, variants, stock); err != nil {
			return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
		}
	}
	before, after, changed, err = applyProductVisibilityAndAttributes(product, req)
	if err != nil {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, apperror.Validation(err.Error())
	}
	// Scalars (name/slug/description) were applied on product before the lock
	// in Update(); status was re-applied from the locked row when omitted.
	// Soft-write uses locked status and oldType (not FindByID) so a concurrent
	// Active→Draft or type change cannot skip the file check or overwrite Draft.
	if err := h.ensureActiveDownloadableHasFile(ctx, files, product, lockedStatus, oldType); err != nil {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
	}
	if err := product.Validate(); err != nil {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, apperror.Validation(err.Error())
	}
	if err := locker.Update(ctx, product); err != nil {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return catalog.VisibilityAxes{}, catalog.VisibilityAxes{}, false, fmt.Errorf("product admin: commit: %w", err)
	}
	return before, after, changed, nil
}

func (h *ProductAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := r.PathValue("id")
		adminID := h.getAdminID(r)
		if pid == "" {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				Result:       "error",
				Error:        "product id is required",
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, apperror.Validation("product id is required"))
			return
		}

		var req updateProductRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        "invalid request body",
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		product, err := h.repo.FindByID(r.Context(), pid)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, err)
			return
		}
		if product == nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        "product not found",
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, apperror.NotFound("product not found"))
			return
		}

		if err := applyProductScalarPatch(product, req); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        adminAuditMessage(err),
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, err)
			return
		}

		var newType *catalog.Type
		if req.Type != nil {
			rawType := strings.TrimSpace(*req.Type)
			if rawType == "" {
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditProductUpdate,
					ResourceType: "product",
					ResourceID:   pid,
					Result:       "error",
					Error:        "type must not be empty",
					Details:      productAdminScopeDetailsFromRequest(r),
				})
				httpshared.JSONError(w, apperror.Validation("type must not be empty"))
				return
			}
			parsed := catalog.Type(rawType)
			newType = &parsed
		}

		var modesBefore, modes catalog.VisibilityAxes
		visibilityModesChanged := false
		// Always lock before persisting. An omitted type, pricing_mode,
		// stock_mode, and status are taken from the locked row. An omitted
		// explode flag is cleared when the locked type and the patched modes
		// are no longer allowed to explode. Writing the earlier FindByID
		// snapshot would clobber a concurrent switch.
		modesBefore, modes, visibilityModesChanged, err = h.updateProductWithTypeLock(r.Context(), product, req, newType)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			httpshared.JSONError(w, err)
			return
		}

		successDetails := productAdminScopeDetailsFromRequest(r)
		if visibilityModesChanged {
			successDetails["visibility_modes_before"] = visibilityModesMap(modesBefore)
			successDetails["visibility_modes_after"] = visibilityModesMap(modes)
		}
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductUpdate,
			ResourceType: "product",
			ResourceID:   pid,
			Result:       "success",
			Details:      successDetails,
		})

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventProductUpdated, "product.admin", catalog.ProductUpdatedData{
			ProductID: product.ID,
			Name:      product.Name,
			Slug:      product.Slug,
			Status:    product.Status,
		}))

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"product": product,
		})
	}
}
