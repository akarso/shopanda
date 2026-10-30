package exporter_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"strings"
	"testing"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// TestPhase12_ProductCSV_ExportImportConsistency is the Track F cross-field
// product round-trip for Phase 12 columns that ship in product CSV today
// (type, status, bundle modes, requires_linked_parent, visibility axes) plus
// variant identity (SKU/name) and description. This is an in-memory export →
// import field check — not a full empty-catalog DB diff across prices/stock/
// attributes/categories.
//
// Assignment units and downloadable_grant_settings have no CSV surface —
// confirmed by absence of those CLI pipelines, not covered here.
func TestPhase12_ProductCSV_ExportImportConsistency(t *testing.T) {
	srcProducts := &mockProductRepo{
		products: []catalog.Product{
			{
				ID: "simple-1", Name: "Card", Slug: "card", Description: "parent",
				Type: catalog.TypeSimple, Status: catalog.StatusActive, RequiresLinkedParent: false,
				VisibilityModes: catalog.VisibilityAxes{
					Catalog: catalog.VisibilityModeVisible, Search: catalog.VisibilityModeAuto,
					Individually: catalog.VisibilityModeVisible, Purchasable: catalog.VisibilityModeAuto,
				},
			},
			{
				ID: "bundle-1", Name: "Kit", Slug: "kit", Description: "bundle",
				Type: catalog.TypeBundle, Status: catalog.StatusActive,
				PricingMode: catalog.PricingModeDynamic, StockMode: catalog.StockModeDynamic,
				ExplodeOrderLines: true,
				VisibilityModes:   catalog.DefaultVisibilityAxes(),
			},
			{
				ID: "grouped-1", Name: "Group", Slug: "group", Description: "grouped parent",
				Type: catalog.TypeGrouped, Status: catalog.StatusDraft,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			},
			{
				ID: "cfg-1", Name: "Configurable", Slug: "configurable", Description: "cfg",
				Type: catalog.TypeConfigurable, Status: catalog.StatusDraft,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			},
			{
				ID: "ticket-1", Name: "Ticket", Slug: "ticket", Description: "linked child",
				Type: catalog.TypeVirtual, Status: catalog.StatusActive, RequiresLinkedParent: true,
				VisibilityModes: catalog.VisibilityAxes{
					Catalog: catalog.VisibilityModeHidden, Search: catalog.VisibilityModeHidden,
					Individually: catalog.VisibilityModeHidden, Purchasable: catalog.VisibilityModeAuto,
				},
			},
			{
				ID: "dl-1", Name: "Ebook", Slug: "ebook", Description: "digital",
				Type: catalog.TypeDownloadable, Status: catalog.StatusDraft,
				VisibilityModes: catalog.VisibilityAxes{
					Catalog: catalog.VisibilityModeAuto, Search: catalog.VisibilityModeVisible,
					Individually: catalog.VisibilityModeAuto, Purchasable: catalog.VisibilityModeVisible,
				},
			},
		},
	}
	srcVariants := &mockVariantRepo{
		variants: map[string][]catalog.Variant{
			"simple-1":  {{ID: "v1", ProductID: "simple-1", SKU: "SKU-CARD", Name: "Default"}},
			"bundle-1":  {{ID: "v2", ProductID: "bundle-1", SKU: "SKU-KIT", Name: "Kit"}},
			"grouped-1": {{ID: "v5", ProductID: "grouped-1", SKU: "SKU-GROUP", Name: "Group"}},
			"cfg-1":     {{ID: "v6", ProductID: "cfg-1", SKU: "SKU-CFG", Name: "Cfg"}},
			"ticket-1":  {{ID: "v3", ProductID: "ticket-1", SKU: "SKU-TICKET", Name: "Pass"}},
			"dl-1":      {{ID: "v4", ProductID: "dl-1", SKU: "SKU-EBOOK", Name: "PDF"}},
		},
	}

	var buf bytes.Buffer
	if _, err := exporter.NewProductExporter(srcProducts, srcVariants).Export(context.Background(), &buf); err != nil {
		t.Fatalf("Export: %v", err)
	}

	dstProducts := &roundTripProductRepo{}
	dstVariants := &recordingVariantRepo{}
	impResult, err := importer.NewProductImporter(dstProducts, dstVariants, nil).Import(context.Background(), bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if impResult.Products != 6 || len(impResult.Errors) != 0 {
		t.Fatalf("import result=%+v", impResult)
	}
	if len(dstProducts.products) != 6 {
		t.Fatalf("created %d products, want 6", len(dstProducts.products))
	}
	if len(dstVariants.variants) != 6 {
		t.Fatalf("created %d variants, want 6", len(dstVariants.variants))
	}

	bySlug := map[string]*catalog.Product{}
	for _, p := range dstProducts.products {
		bySlug[p.Slug] = p
	}
	variantsBySKU := map[string]*catalog.Variant{}
	for _, v := range dstVariants.variants {
		variantsBySKU[v.SKU] = v
	}

	assertVariant := func(sku, name string) {
		t.Helper()
		v := variantsBySKU[sku]
		if v == nil || v.Name != name {
			t.Fatalf("variant %s = %#v, want name %q", sku, v, name)
		}
	}
	assertVariant("SKU-CARD", "Default")
	assertVariant("SKU-KIT", "Kit")
	assertVariant("SKU-GROUP", "Group")
	assertVariant("SKU-CFG", "Cfg")
	assertVariant("SKU-TICKET", "Pass")
	assertVariant("SKU-EBOOK", "PDF")

	card := bySlug["card"]
	if card == nil || card.Type != catalog.TypeSimple || card.Status != catalog.StatusActive || card.Description != "parent" {
		t.Fatalf("card = %#v", card)
	}
	if card.RequiresLinkedParent {
		t.Fatal("card RequiresLinkedParent want false")
	}
	wantCardVis := catalog.VisibilityAxes{
		Catalog: catalog.VisibilityModeVisible, Search: catalog.VisibilityModeAuto,
		Individually: catalog.VisibilityModeVisible, Purchasable: catalog.VisibilityModeAuto,
	}
	if card.VisibilityModes != wantCardVis {
		t.Fatalf("card VisibilityModes = %#v, want %#v", card.VisibilityModes, wantCardVis)
	}

	kit := bySlug["kit"]
	if kit == nil || kit.Type != catalog.TypeBundle || kit.Status != catalog.StatusActive || kit.Description != "bundle" {
		t.Fatalf("kit = %#v", kit)
	}
	if kit.PricingMode != catalog.PricingModeDynamic || kit.StockMode != catalog.StockModeDynamic || !kit.ExplodeOrderLines {
		t.Fatalf("kit modes = %q %q explode=%v", kit.PricingMode, kit.StockMode, kit.ExplodeOrderLines)
	}
	if kit.RequiresLinkedParent {
		t.Fatal("kit RequiresLinkedParent want false")
	}

	group := bySlug["group"]
	if group == nil || group.Type != catalog.TypeGrouped || group.Status != catalog.StatusDraft || group.Description != "grouped parent" {
		t.Fatalf("group = %#v", group)
	}
	if group.RequiresLinkedParent {
		t.Fatal("group RequiresLinkedParent want false")
	}

	cfg := bySlug["configurable"]
	if cfg == nil || cfg.Type != catalog.TypeConfigurable || cfg.Status != catalog.StatusDraft || cfg.Description != "cfg" {
		t.Fatalf("configurable = %#v", cfg)
	}
	if cfg.RequiresLinkedParent {
		t.Fatal("configurable RequiresLinkedParent want false")
	}

	ticket := bySlug["ticket"]
	if ticket == nil || ticket.Type != catalog.TypeVirtual || ticket.Status != catalog.StatusActive || !ticket.RequiresLinkedParent {
		t.Fatalf("ticket = %#v", ticket)
	}
	if ticket.Description != "linked child" {
		t.Fatalf("ticket description = %q", ticket.Description)
	}
	wantTicketVis := catalog.VisibilityAxes{
		Catalog: catalog.VisibilityModeHidden, Search: catalog.VisibilityModeHidden,
		Individually: catalog.VisibilityModeHidden, Purchasable: catalog.VisibilityModeAuto,
	}
	if ticket.VisibilityModes != wantTicketVis {
		t.Fatalf("ticket VisibilityModes = %#v, want %#v", ticket.VisibilityModes, wantTicketVis)
	}

	ebook := bySlug["ebook"]
	if ebook == nil || ebook.Type != catalog.TypeDownloadable || ebook.Status != catalog.StatusDraft || ebook.Description != "digital" {
		t.Fatalf("ebook = %#v", ebook)
	}
	if ebook.RequiresLinkedParent {
		t.Fatal("ebook RequiresLinkedParent want false")
	}
	wantEbookVis := catalog.VisibilityAxes{
		Catalog: catalog.VisibilityModeAuto, Search: catalog.VisibilityModeVisible,
		Individually: catalog.VisibilityModeAuto, Purchasable: catalog.VisibilityModeVisible,
	}
	if ebook.VisibilityModes != wantEbookVis {
		t.Fatalf("ebook VisibilityModes = %#v, want %#v", ebook.VisibilityModes, wantEbookVis)
	}

	// Enum / bool formatting conventions: lowercase tokens, strconv.FormatBool shape.
	records, err := csv.NewReader(bytes.NewReader(buf.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("parse export: %v", err)
	}
	colIdx := map[string]int{}
	for i, h := range records[0] {
		colIdx[h] = i
	}
	for _, col := range []string{"type", "status", "pricing_mode", "stock_mode", "visibility_catalog", "visibility_search", "visibility_individually", "visibility_purchasable"} {
		if _, ok := colIdx[col]; !ok {
			t.Fatalf("missing column %s in %v", col, records[0])
		}
	}
	for _, row := range records[1:] {
		for _, col := range []string{"type", "status", "pricing_mode", "stock_mode", "visibility_catalog", "visibility_search", "visibility_individually", "visibility_purchasable"} {
			val := row[colIdx[col]]
			if val != strings.ToLower(val) {
				t.Errorf("column %s value %q is not lowercase (cross-track convention; tokens are case-sensitive on import)", col, val)
			}
		}
		for _, col := range []string{"explode_order_lines", "requires_linked_parent"} {
			val := row[colIdx[col]]
			if val != "true" && val != "false" {
				t.Errorf("column %s value %q want true|false", col, val)
			}
		}
	}
}

type recordingVariantRepo struct {
	roundTripVariantRepo
	variants []*catalog.Variant
}

func (m *recordingVariantRepo) Create(_ context.Context, v *catalog.Variant) error {
	cp := *v
	m.variants = append(m.variants, &cp)
	return nil
}

// TestPhase12_CompositionCSV_ExportImportConsistency re-imports exported
// bundle/grouped link rows and confirms slug/qty/required formatting matches.
func TestPhase12_CompositionCSV_ExportImportConsistency(t *testing.T) {
	products := &exportCompositionProducts{
		page: []catalog.Product{
			{ID: "b1", Slug: "kit", Type: catalog.TypeBundle, Status: catalog.StatusActive},
			{ID: "c1", Slug: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive},
			{ID: "g1", Slug: "group", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
			{ID: "m1", Slug: "member", Type: catalog.TypeVirtual, Status: catalog.StatusActive},
		},
		byID: map[string]*catalog.Product{
			"b1": {ID: "b1", Slug: "kit", Type: catalog.TypeBundle, Status: catalog.StatusActive},
			"c1": {ID: "c1", Slug: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive},
			"g1": {ID: "g1", Slug: "group", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
			"m1": {ID: "m1", Slug: "member", Type: catalog.TypeVirtual, Status: catalog.StatusActive},
		},
	}
	srcBundles := &exportBundleRepo{byBundle: map[string][]catalog.BundleComponent{
		"b1": {{BundleProductID: "b1", ComponentProductID: "c1", Quantity: 3, Required: false}},
	}}
	srcGrouped := &exportGroupedRepo{byGroup: map[string][]catalog.GroupedMember{
		"g1": {{GroupProductID: "g1", MemberProductID: "m1"}},
	}}

	var bundleBuf, groupedBuf bytes.Buffer
	bundleExp, err := exporter.NewBundleComponentExporter(products, catalogApp.NewBundleService(products, srcBundles, exportCompositionTxStarter())).Export(context.Background(), &bundleBuf)
	if err != nil {
		t.Fatalf("bundle Export: %v", err)
	}
	if bundleExp.Rows != 1 {
		t.Fatalf("bundle rows = %d", bundleExp.Rows)
	}
	groupedExp, err := exporter.NewGroupedMemberExporter(products, catalogApp.NewGroupedService(products, srcGrouped, exportCompositionTxStarter())).Export(context.Background(), &groupedBuf)
	if err != nil {
		t.Fatalf("grouped Export: %v", err)
	}
	if groupedExp.Rows != 1 {
		t.Fatalf("grouped rows = %d", groupedExp.Rows)
	}

	// required uses the same true/false tokens as product CSV bool columns.
	bundleRecords, err := csv.NewReader(bytes.NewReader(bundleBuf.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("parse bundle: %v", err)
	}
	reqIdx := -1
	for i, h := range bundleRecords[0] {
		if h == "required" {
			reqIdx = i
			break
		}
	}
	if reqIdx < 0 {
		t.Fatalf("required column missing in %v", bundleRecords[0])
	}
	if bundleRecords[1][reqIdx] != "false" {
		t.Fatalf("required cell = %q, want false (bool convention)", bundleRecords[1][reqIdx])
	}

	dstBundles := &recordingBundleRepo{}
	dstGrouped := &recordingGroupedRepo{}
	importProducts := &slugMapProductRepo{bySlug: map[string]*catalog.Product{
		"kit":    products.byID["b1"],
		"leaf":   products.byID["c1"],
		"group":  products.byID["g1"],
		"member": products.byID["m1"],
	}}

	bundleImp, err := importer.NewBundleComponentImporter(importProducts, catalogApp.NewBundleService(importProducts, dstBundles, exportCompositionTxStarter())).Import(context.Background(), bytes.NewReader(bundleBuf.Bytes()))
	if err != nil {
		t.Fatalf("bundle Import: %v", err)
	}
	if bundleImp.Added != 1 || len(bundleImp.Errors) != 0 {
		t.Fatalf("bundle import = %+v", bundleImp)
	}
	if len(dstBundles.links) != 1 || dstBundles.links[0].Quantity != 3 || dstBundles.links[0].Required {
		t.Fatalf("bundle links = %+v, want qty=3 required=false", dstBundles.links)
	}

	groupedImp, err := importer.NewGroupedMemberImporter(importProducts, catalogApp.NewGroupedService(importProducts, dstGrouped, exportCompositionTxStarter())).Import(context.Background(), bytes.NewReader(groupedBuf.Bytes()))
	if err != nil {
		t.Fatalf("grouped Import: %v", err)
	}
	if groupedImp.Added != 1 || len(groupedImp.Errors) != 0 {
		t.Fatalf("grouped import = %+v", groupedImp)
	}
	if len(dstGrouped.links) != 1 || dstGrouped.links[0].MemberProductID != "m1" {
		t.Fatalf("grouped links = %+v", dstGrouped.links)
	}
}

type slugMapProductRepo struct {
	catalog.ProductRepository
	bySlug map[string]*catalog.Product
}

func (r *slugMapProductRepo) FindBySlug(_ context.Context, slug string) (*catalog.Product, error) {
	return r.bySlug[slug], nil
}
func (r *slugMapProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	for _, p := range r.bySlug {
		if p != nil && p.ID == id {
			return p, nil
		}
	}
	return nil, nil
}
func (r *slugMapProductRepo) WithTx(*sql.Tx) catalog.ProductRepository { return r }
func (r *slugMapProductRepo) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	p, _ := r.FindByID(context.Background(), id)
	if p == nil {
		return "", apperror.NotFound("product not found")
	}
	return p.Type, nil
}

type recordingBundleRepo struct {
	catalog.BundleComponentRepository
	links []*catalog.BundleComponent
}

func (r *recordingBundleRepo) ListByBundleID(context.Context, string) ([]catalog.BundleComponent, error) {
	return nil, nil
}
func (r *recordingBundleRepo) CountByBundleID(context.Context, string) (int, error) { return 0, nil }
func (r *recordingBundleRepo) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}
func (r *recordingBundleRepo) Upsert(_ context.Context, c *catalog.BundleComponent) error {
	cp := *c
	r.links = append(r.links, &cp)
	return nil
}
func (r *recordingBundleRepo) Remove(context.Context, string, string) error     { return nil }
func (r *recordingBundleRepo) WithTx(*sql.Tx) catalog.BundleComponentRepository { return r }

type recordingGroupedRepo struct {
	catalog.GroupedMemberRepository
	links []*catalog.GroupedMember
}

func (r *recordingGroupedRepo) ListByGroupID(context.Context, string) ([]catalog.GroupedMember, error) {
	return nil, nil
}
func (r *recordingGroupedRepo) CountByGroupID(context.Context, string) (int, error)  { return 0, nil }
func (r *recordingGroupedRepo) CountByMemberID(context.Context, string) (int, error) { return 0, nil }
func (r *recordingGroupedRepo) Insert(_ context.Context, m *catalog.GroupedMember) error {
	cp := *m
	r.links = append(r.links, &cp)
	return nil
}
func (r *recordingGroupedRepo) Remove(context.Context, string, string) error   { return nil }
func (r *recordingGroupedRepo) WithTx(*sql.Tx) catalog.GroupedMemberRepository { return r }

// TestPhase12_LinkedAndDownloadableCSV_ExportImportConsistency covers additive
// relationship CSVs with the same slug/id formatting conventions.
func TestPhase12_LinkedAndDownloadableCSV_ExportImportConsistency(t *testing.T) {
	srcRules := &linkedExportRuleRepo{
		parents: []catalog.LinkedEligibleParentSlugRow{
			{ChildSlug: "ticket", ParentSlug: "card"},
		},
		categories: []catalog.LinkedEligibleCategorySlugRow{
			{ChildSlug: "ticket", CategorySlug: "cards"},
		},
	}
	products := &consistencyLinkedProducts{bySlug: map[string]*catalog.Product{
		"ticket": {ID: "t1", Slug: "ticket", Type: catalog.TypeVirtual, Status: catalog.StatusActive},
		"card":   {ID: "c1", Slug: "card", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	categories := &consistencyLinkedCategories{bySlug: map[string]*catalog.Category{
		"cards": {ID: "cat1", Name: "Cards", Slug: "cards"},
	}}
	exportSvc := catalogApp.NewLinkedEligibilityService(products, categories, srcRules, exportCompositionTxStarter())

	var parentBuf, catBuf bytes.Buffer
	if _, err := exporter.NewLinkedEligibleParentExporter(products, exportSvc).Export(context.Background(), &parentBuf); err != nil {
		t.Fatalf("parent Export: %v", err)
	}
	if _, err := exporter.NewLinkedEligibleCategoryExporter(products, categories, exportSvc).Export(context.Background(), &catBuf); err != nil {
		t.Fatalf("category Export: %v", err)
	}

	dstRules := &consistencyLinkedImportRules{}
	importSvc := catalogApp.NewLinkedEligibilityService(products, categories, dstRules, exportCompositionTxStarter())
	parentImp, err := importer.NewLinkedEligibleParentImporter(products, importSvc).Import(context.Background(), bytes.NewReader(parentBuf.Bytes()))
	if err != nil {
		t.Fatalf("parent Import: %v", err)
	}
	if parentImp.Added != 1 || len(parentImp.Errors) != 0 {
		t.Fatalf("parent import = %+v", parentImp)
	}
	if len(dstRules.parents) != 1 || dstRules.parents[0].ChildProductID != "t1" || dstRules.parents[0].ParentProductID != "c1" {
		t.Fatalf("parents = %+v", dstRules.parents)
	}
	catImp, err := importer.NewLinkedEligibleCategoryImporter(products, categories, importSvc).Import(context.Background(), bytes.NewReader(catBuf.Bytes()))
	if err != nil {
		t.Fatalf("category Import: %v", err)
	}
	if catImp.Added != 1 || len(catImp.Errors) != 0 {
		t.Fatalf("category import = %+v", catImp)
	}
	if len(dstRules.categories) != 1 || dstRules.categories[0].CategoryID != "cat1" {
		t.Fatalf("categories = %+v", dstRules.categories)
	}

	// Downloadable files: metadata columns round-trip; blobs and grant_settings are out of scope.
	// First create the product via product CSV so downloadable import resolves a freshly minted ID.
	prodCSV := &bytes.Buffer{}
	if _, err := exporter.NewProductExporter(
		&mockProductRepo{products: []catalog.Product{{
			ID: "src-dl", Name: "Ebook", Slug: "ebook", Description: "digital",
			Type: catalog.TypeDownloadable, Status: catalog.StatusDraft,
		}}},
		&mockVariantRepo{variants: map[string][]catalog.Variant{
			"src-dl": {{ID: "sv", ProductID: "src-dl", SKU: "SKU-EBOOK", Name: "PDF"}},
		}},
	).Export(context.Background(), prodCSV); err != nil {
		t.Fatalf("product Export for downloadable: %v", err)
	}
	freshProducts := &roundTripProductRepo{}
	if _, err := importer.NewProductImporter(freshProducts, &roundTripVariantRepo{}, nil).Import(context.Background(), bytes.NewReader(prodCSV.Bytes())); err != nil {
		t.Fatalf("product Import for downloadable: %v", err)
	}
	if len(freshProducts.products) != 1 {
		t.Fatalf("fresh products = %d", len(freshProducts.products))
	}
	fresh := freshProducts.products[0]
	dlProducts := &dlExportProductRepo{
		page:   []catalog.Product{*fresh},
		byID:   map[string]*catalog.Product{fresh.ID: fresh},
		bySlug: map[string]*catalog.Product{fresh.Slug: fresh},
	}
	srcFiles := &dlExportFileRepo{rows: []*catalog.DownloadableFile{
		{ProductID: fresh.ID, AssetID: "a1", Position: 2, Label: "Manual"},
	}}
	assets := &dlExportAssetRepo{byID: map[string]*media.Asset{
		"a1": {ID: "a1", Path: "private/downloads/a1/file.pdf", Filename: "file.pdf", MimeType: "application/pdf", Size: 10},
	}}
	dlSvc := catalogApp.NewDownloadableFileService(dlProducts, srcFiles, assets, exportCompositionTxStarter())

	var dlBuf bytes.Buffer
	dlExp, err := exporter.NewDownloadableFileExporter(dlProducts, dlSvc).Export(context.Background(), &dlBuf)
	if err != nil {
		t.Fatalf("downloadable Export: %v", err)
	}
	if dlExp.Rows != 1 {
		t.Fatalf("downloadable rows = %d", dlExp.Rows)
	}
	records, err := csv.NewReader(bytes.NewReader(dlBuf.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("parse downloadable: %v", err)
	}
	if strings.Join(records[0], ",") != "product_slug,asset_id,position,label" {
		t.Fatalf("header = %v", records[0])
	}
	if strings.Join(records[1], ",") != "ebook,a1,2,Manual" {
		t.Fatalf("row = %v", records[1])
	}

	dstFiles := &dlExportFileRepo{}
	dstSvc := catalogApp.NewDownloadableFileService(dlProducts, dstFiles, assets, exportCompositionTxStarter())
	dlImp, err := importer.NewDownloadableFileImporter(dlProducts, dstSvc).Import(context.Background(), bytes.NewReader(dlBuf.Bytes()))
	if err != nil {
		t.Fatalf("downloadable Import: %v", err)
	}
	if dlImp.Added != 1 || len(dlImp.Errors) != 0 {
		t.Fatalf("downloadable import = %+v", dlImp)
	}
	if len(dstFiles.rows) != 1 || dstFiles.rows[0].ProductID != fresh.ID || dstFiles.rows[0].Position != 2 || dstFiles.rows[0].Label != "Manual" {
		t.Fatalf("files = %+v, want productID=%s", dstFiles.rows, fresh.ID)
	}

	// Omitted position/label headers retain existing join metadata on re-import.
	omitImp, err := importer.NewDownloadableFileImporter(dlProducts, dstSvc).Import(context.Background(), strings.NewReader(
		"product_slug,asset_id\nebook,a1\n",
	))
	if err != nil {
		t.Fatalf("omit Import: %v", err)
	}
	if omitImp.Updated != 1 || omitImp.Added != 0 || len(omitImp.Errors) != 0 {
		t.Fatalf("omit import = %+v", omitImp)
	}
	if len(dstFiles.rows) != 1 || dstFiles.rows[0].Position != 2 || dstFiles.rows[0].Label != "Manual" {
		t.Fatalf("retained meta = %+v", dstFiles.rows[0])
	}
}

type consistencyLinkedProducts struct {
	catalog.ProductRepository
	bySlug map[string]*catalog.Product
}

func (r *consistencyLinkedProducts) FindBySlug(_ context.Context, slug string) (*catalog.Product, error) {
	return r.bySlug[slug], nil
}
func (r *consistencyLinkedProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	for _, p := range r.bySlug {
		if p != nil && p.ID == id {
			return p, nil
		}
	}
	return nil, nil
}
func (r *consistencyLinkedProducts) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (r *consistencyLinkedProducts) WithTx(*sql.Tx) catalog.ProductRepository { return r }
func (r *consistencyLinkedProducts) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	p, _ := r.FindByID(context.Background(), id)
	if p == nil {
		return "", apperror.NotFound("product not found")
	}
	return p.Type, nil
}

type consistencyLinkedCategories struct {
	catalog.CategoryRepository
	bySlug map[string]*catalog.Category
}

func (r *consistencyLinkedCategories) FindBySlug(_ context.Context, slug string) (*catalog.Category, error) {
	return r.bySlug[slug], nil
}
func (r *consistencyLinkedCategories) FindByID(_ context.Context, id string) (*catalog.Category, error) {
	for _, c := range r.bySlug {
		if c != nil && c.ID == id {
			return c, nil
		}
	}
	return nil, nil
}

type consistencyLinkedImportRules struct {
	catalog.LinkedEligibilityRepository
	parents    []catalog.LinkedEligibleParent
	categories []catalog.LinkedEligibleCategory
}

func (r *consistencyLinkedImportRules) ListParentsByChildID(context.Context, string) ([]catalog.LinkedEligibleParent, error) {
	return nil, nil
}
func (r *consistencyLinkedImportRules) ListCategoriesByChildID(context.Context, string) ([]catalog.LinkedEligibleCategory, error) {
	return nil, nil
}
func (r *consistencyLinkedImportRules) CountRulesByChildID(context.Context, string) (int, error) {
	return 0, nil
}
func (r *consistencyLinkedImportRules) InsertParent(_ context.Context, p *catalog.LinkedEligibleParent) error {
	r.parents = append(r.parents, *p)
	return nil
}
func (r *consistencyLinkedImportRules) InsertCategory(_ context.Context, c *catalog.LinkedEligibleCategory) error {
	r.categories = append(r.categories, *c)
	return nil
}
func (r *consistencyLinkedImportRules) RemoveParent(context.Context, string, string) error {
	return nil
}
func (r *consistencyLinkedImportRules) RemoveCategory(context.Context, string, string) error {
	return nil
}
func (r *consistencyLinkedImportRules) ListAllParentSlugRows(context.Context) ([]catalog.LinkedEligibleParentSlugRow, error) {
	return nil, nil
}
func (r *consistencyLinkedImportRules) ListAllCategorySlugRows(context.Context) ([]catalog.LinkedEligibleCategorySlugRow, error) {
	return nil, nil
}
func (r *consistencyLinkedImportRules) IsEligible(context.Context, string, string) (bool, error) {
	return false, nil
}
func (r *consistencyLinkedImportRules) WithTx(*sql.Tx) catalog.LinkedEligibilityRepository { return r }

type dlExportProductRepo struct {
	catalog.ProductRepository
	page   []catalog.Product
	byID   map[string]*catalog.Product
	bySlug map[string]*catalog.Product
}

func (r *dlExportProductRepo) List(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
	if filter.Type == "" {
		return r.page, nil
	}
	out := make([]catalog.Product, 0, len(r.page))
	for _, p := range r.page {
		if p.Type == filter.Type {
			out = append(out, p)
		}
	}
	return out, nil
}
func (r *dlExportProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return r.byID[id], nil
}
func (r *dlExportProductRepo) FindBySlug(_ context.Context, slug string) (*catalog.Product, error) {
	return r.bySlug[slug], nil
}
func (r *dlExportProductRepo) WithTx(*sql.Tx) catalog.ProductRepository { return r }
func (r *dlExportProductRepo) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	p := r.byID[id]
	if p == nil {
		return "", apperror.NotFound("product not found")
	}
	return p.Type, nil
}

type dlExportFileRepo struct {
	catalog.DownloadableFileRepository
	rows []*catalog.DownloadableFile
}

func (r *dlExportFileRepo) ListByProductID(_ context.Context, productID string) ([]catalog.DownloadableFile, error) {
	out := make([]catalog.DownloadableFile, 0, len(r.rows))
	for _, row := range r.rows {
		if row.ProductID == productID {
			out = append(out, *row)
		}
	}
	return out, nil
}
func (r *dlExportFileRepo) CountByProductID(_ context.Context, productID string) (int, error) {
	n := 0
	for _, row := range r.rows {
		if row.ProductID == productID {
			n++
		}
	}
	return n, nil
}
func (r *dlExportFileRepo) CountByAssetID(context.Context, string) (int, error) { return 0, nil }
func (r *dlExportFileRepo) Upsert(_ context.Context, f *catalog.DownloadableFile) error {
	cp := *f
	for i, row := range r.rows {
		if row.ProductID == f.ProductID && row.AssetID == f.AssetID {
			r.rows[i] = &cp
			return nil
		}
	}
	r.rows = append(r.rows, &cp)
	return nil
}
func (r *dlExportFileRepo) Remove(context.Context, string, string) error      { return nil }
func (r *dlExportFileRepo) WithTx(*sql.Tx) catalog.DownloadableFileRepository { return r }

type dlExportAssetRepo struct {
	media.AssetRepository
	byID map[string]*media.Asset
}

func (r *dlExportAssetRepo) FindByID(_ context.Context, id string) (*media.Asset, error) {
	return r.byID[id], nil
}
func (r *dlExportAssetRepo) WithTx(*sql.Tx) media.AssetRepository { return r }
func (r *dlExportAssetRepo) LockForUpdate(_ context.Context, id string) (*media.Asset, error) {
	a := r.byID[id]
	if a == nil {
		return nil, apperror.NotFound("asset not found")
	}
	return a, nil
}
