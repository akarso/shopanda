package composition_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestProductOffers_CheapestPriceAndAnyVariantInStock(t *testing.T) {
	product := &catalog.Product{ID: "p1", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{product},
		variants: []catalog.Variant{
			{ID: "expensive", ProductID: "p1"},
			{ID: "cheap", ProductID: "p1"},
		},
		prices: []offerPrice{
			{variantID: "expensive", currency: "EUR", storeID: "", amount: 5000},
			{variantID: "cheap", currency: "EUR", storeID: "", amount: 1200},
		},
		stock: map[string]int{"expensive": 3, "cheap": 0},
	})

	offers, err := src.ProductOffers(context.Background(), []*catalog.Product{product}, "", "")
	if err != nil {
		t.Fatalf("ProductOffers: %v", err)
	}
	got := offers["p1"]
	if got.VariantID != "cheap" {
		t.Fatalf("variant_id = %q, want cheap", got.VariantID)
	}
	if got.Amount == nil || *got.Amount != 1200 {
		t.Fatalf("amount = %v, want 1200", got.Amount)
	}
	if got.Currency != "EUR" {
		t.Fatalf("currency = %q, want EUR", got.Currency)
	}
	if !got.InStock {
		t.Fatal("in_stock = false, want true because another variant has quantity")
	}
}

func TestProductOffers_PrefersStorePrice(t *testing.T) {
	product := &catalog.Product{ID: "p1", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{product},
		variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}},
		prices: []offerPrice{
			{variantID: "v1", currency: "EUR", storeID: "", amount: 2000},
			{variantID: "v1", currency: "EUR", storeID: "store-1", amount: 1500},
		},
		stock: map[string]int{"v1": 1},
	})

	offers, err := src.ProductOffers(context.Background(), []*catalog.Product{product}, "store-1", "eur")
	if err != nil {
		t.Fatalf("ProductOffers: %v", err)
	}
	got := offers["p1"]
	if got.Amount == nil || *got.Amount != 1500 || got.Currency != "EUR" {
		t.Fatalf("offer = %+v, want store price 1500 EUR", got)
	}
	// Store-then-global fallback is the PriceRepository contract. Offer code
	// asks once, with the request store and currency.
	prices := src.Prices.(*offerPrices)
	if len(prices.calls) != 1 || prices.calls[0].storeID != "store-1" || prices.calls[0].currency != "EUR" {
		t.Fatalf("price lookup = %+v, want one EUR lookup for store-1", prices.calls)
	}
}

func TestProductOffers_MissingPriceIsNullAndZeroIsReal(t *testing.T) {
	bareProduct := &catalog.Product{ID: "bare", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	freeProduct := &catalog.Product{ID: "free", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{bareProduct, freeProduct},
		variants: []catalog.Variant{
			{ID: "bare-v", ProductID: "bare"},
			{ID: "free-v", ProductID: "free"},
		},
		prices: []offerPrice{{variantID: "free-v", currency: "EUR", amount: 0}},
		stock:  map[string]int{"bare-v": 0, "free-v": 2},
	})

	offers, err := src.ProductOffers(context.Background(), []*catalog.Product{bareProduct, freeProduct}, "", "EUR")
	if err != nil {
		t.Fatalf("ProductOffers: %v", err)
	}
	bare := offers["bare"]
	if bare.Amount != nil || bare.InStock {
		t.Fatalf("bare offer = %+v, want null amount and out of stock", bare)
	}
	free := offers["free"]
	if free.Amount == nil || *free.Amount != 0 || !free.InStock {
		t.Fatalf("free offer = %+v, want amount 0 and in stock", free)
	}
}

func TestProductOffers_GroupedHasNoPriceOrStock(t *testing.T) {
	kit := &catalog.Product{ID: "kit", Type: catalog.TypeGrouped, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{kit},
		variants: []catalog.Variant{{ID: "kit-v", ProductID: "kit"}},
		prices:   []offerPrice{{variantID: "kit-v", currency: "EUR", amount: 999}},
		stock:    map[string]int{"kit-v": 5},
	})

	offers, err := src.ProductOffers(context.Background(), []*catalog.Product{kit}, "", "EUR")
	if err != nil {
		t.Fatalf("ProductOffers: %v", err)
	}
	got := offers["kit"]
	if got.Amount != nil || got.VariantID != "" || got.InStock {
		t.Fatalf("grouped offer = %+v, want empty price and out of stock", got)
	}
}

func TestProductOffers_DynamicBundleSumsComponents(t *testing.T) {
	bundle := &catalog.Product{
		ID: "bundle", Type: catalog.TypeBundle, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeDynamic, StockMode: catalog.StockModeFixed,
	}
	leaf := &catalog.Product{
		ID: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeFixed,
	}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{bundle, leaf},
		variants: []catalog.Variant{
			{ID: "bundle-v", ProductID: "bundle"},
			{ID: "leaf-v", ProductID: "leaf"},
		},
		prices: []offerPrice{
			{variantID: "bundle-v", currency: "EUR", amount: 1},
			{variantID: "leaf-v", currency: "EUR", amount: 100},
		},
		stock: map[string]int{"bundle-v": 4},
		components: []catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "leaf", Quantity: 2, Required: true,
		}},
	})

	offers, err := src.ProductOffers(context.Background(), []*catalog.Product{bundle}, "", "EUR")
	if err != nil {
		t.Fatalf("ProductOffers: %v", err)
	}
	got := offers["bundle"]
	if got.Amount == nil || *got.Amount != 200 {
		t.Fatalf("amount = %v, want 200 (2 x 100), ignoring the bundle's own price row", got.Amount)
	}
	if !got.InStock {
		t.Fatal("in_stock = false, want true from the bundle variant stock row")
	}
}

func TestProductOffers_DynamicStockIgnoresOwnRow(t *testing.T) {
	bundle := &catalog.Product{
		ID: "bundle", Type: catalog.TypeBundle, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeDynamic,
	}
	leaf := &catalog.Product{
		ID: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeFixed,
	}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{bundle, leaf},
		variants: []catalog.Variant{
			{ID: "bundle-v", ProductID: "bundle"},
			{ID: "leaf-v", ProductID: "leaf"},
		},
		prices: []offerPrice{{variantID: "bundle-v", currency: "EUR", amount: 900}},
		stock:  map[string]int{"bundle-v": 0, "leaf-v": 5},
		components: []catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "leaf", Quantity: 2, Required: true,
		}},
	})

	offers, err := src.ProductOffers(context.Background(), []*catalog.Product{bundle}, "", "EUR")
	if err != nil {
		t.Fatalf("ProductOffers: %v", err)
	}
	got := offers["bundle"]
	if got.Amount == nil || *got.Amount != 900 {
		t.Fatalf("amount = %v, want stored 900", got.Amount)
	}
	if !got.InStock {
		t.Fatal("in_stock = false, want true from floor(5/2) component kits")
	}
}

func TestVariantOffers_PerVariantPriceAndStock(t *testing.T) {
	product := &catalog.Product{ID: "p1", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	variants := []catalog.Variant{
		{ID: "v1", ProductID: "p1"},
		{ID: "v2", ProductID: "p1"},
	}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{product},
		variants: variants,
		prices: []offerPrice{
			{variantID: "v1", currency: "EUR", amount: 1000},
			{variantID: "v2", currency: "EUR", amount: 2500},
		},
		stock: map[string]int{"v1": 0, "v2": 1},
	})

	offers, err := src.VariantOffers(context.Background(), product, variants, "", "EUR")
	if err != nil {
		t.Fatalf("VariantOffers: %v", err)
	}
	if offers["v1"].Amount == nil || *offers["v1"].Amount != 1000 || offers["v1"].InStock {
		t.Fatalf("v1 = %+v, want 1000 and out of stock", offers["v1"])
	}
	if offers["v2"].Amount == nil || *offers["v2"].Amount != 2500 || !offers["v2"].InStock {
		t.Fatalf("v2 = %+v, want 2500 and in stock", offers["v2"])
	}
}

func TestListingOfferStep_WritesMeta(t *testing.T) {
	product := &catalog.Product{ID: "p1", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{product},
		variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}},
		prices:   []offerPrice{{variantID: "v1", currency: "EUR", amount: 800}},
		stock:    map[string]int{"v1": 1},
	})
	step := composition.NewListingOfferStep(src)
	if step.Name() != "listing_offer" {
		t.Fatalf("Name() = %q", step.Name())
	}
	ctx := composition.NewListingContext([]*catalog.Product{product})
	ctx.Currency = "EUR"
	if err := step.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	offers := composition.OffersFromMeta(ctx.Meta)
	if offers["p1"].Amount == nil || *offers["p1"].Amount != 800 {
		t.Fatalf("meta offers = %+v", offers["p1"])
	}
}

func TestProductOfferStep_WritesMeta(t *testing.T) {
	product := &catalog.Product{ID: "p1", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{product},
		variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}},
		prices:   []offerPrice{{variantID: "v1", currency: "EUR", amount: 800}},
		stock:    map[string]int{"v1": 1},
	})
	step := composition.NewProductOfferStep(src)
	if step.Name() != "product_offer" {
		t.Fatalf("Name() = %q", step.Name())
	}
	ctx := composition.NewProductContext(product)
	ctx.Currency = "EUR"
	if err := step.Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, ok := composition.OfferFromMeta(ctx.Meta)
	if !ok || got.Amount == nil || *got.Amount != 800 || !got.InStock {
		t.Fatalf("meta offer = %+v ok=%v", got, ok)
	}
}

func TestProductOffers_DigitalWithoutStockRowIsOutOfStock(t *testing.T) {
	// ResolveAvailableQty and the cart quantity gate treat a missing stock
	// row as 0 for a non-bundle, including virtual and downloadable.
	virtual := &catalog.Product{ID: "virt", Type: catalog.TypeVirtual, Status: catalog.StatusActive}
	download := &catalog.Product{ID: "dl", Type: catalog.TypeDownloadable, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{virtual, download},
		variants: []catalog.Variant{
			{ID: "virt-v", ProductID: "virt"},
			{ID: "dl-v", ProductID: "dl"},
		},
		prices: []offerPrice{
			{variantID: "virt-v", currency: "EUR", amount: 500},
			{variantID: "dl-v", currency: "EUR", amount: 500},
		},
	})

	offers, err := src.ProductOffers(context.Background(), []*catalog.Product{virtual, download}, "", "EUR")
	if err != nil {
		t.Fatalf("ProductOffers: %v", err)
	}
	for _, id := range []string{"virt", "dl"} {
		if offers[id].InStock || offers[id].Amount == nil {
			t.Fatalf("%s offer = %+v, want a price and in_stock false", id, offers[id])
		}
	}
}

func TestProductOffers_DynamicBundleWithoutComponentsIsBlank(t *testing.T) {
	bundle := &catalog.Product{
		ID: "bundle", Type: catalog.TypeBundle, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeDynamic,
	}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{bundle},
		variants: []catalog.Variant{{ID: "bundle-v", ProductID: "bundle"}},
	})
	src.Components = nil

	_, err := src.ProductOffers(context.Background(), []*catalog.Product{bundle}, "", "EUR")
	if err == nil {
		t.Fatal("unwired components must fail ready(), not a blank offer per request")
	}
}

func TestProductOffers_OneBundleInfraErrorKeepsTheOthers(t *testing.T) {
	plain := &catalog.Product{ID: "plain", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	bundle := &catalog.Product{
		ID: "bundle", Type: catalog.TypeBundle, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeDynamic, StockMode: catalog.StockModeFixed,
	}
	leaf := &catalog.Product{ID: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{plain, bundle, leaf},
		variants: []catalog.Variant{
			{ID: "plain-v", ProductID: "plain"},
			{ID: "bundle-v", ProductID: "bundle"},
			{ID: "leaf-v", ProductID: "leaf"},
		},
		prices: []offerPrice{{variantID: "plain-v", currency: "EUR", amount: 500}},
		stock:  map[string]int{"bundle-v": 2},
		components: []catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "leaf", Quantity: 1, Required: true,
		}},
	})
	prices := src.Prices.(*offerPrices)
	prices.failVariant = "leaf-v"
	prices.failErr = errors.New("price db timeout")
	log := &captureLog{}
	src.Log = log

	offers, err := src.ProductOffers(context.Background(), []*catalog.Product{plain, bundle}, "", "EUR")
	if err != nil {
		t.Fatalf("ProductOffers: %v", err)
	}
	if offers["plain"].Amount == nil || *offers["plain"].Amount != 500 {
		t.Fatalf("plain = %+v", offers["plain"])
	}
	if offers["bundle"].Amount != nil || !offers["bundle"].InStock {
		t.Fatalf("bundle = %+v, want no price and its own stock", offers["bundle"])
	}
	if len(log.errors) != 1 || len(log.warns) != 0 {
		t.Fatalf("log errors=%v warns=%v", log.errors, log.warns)
	}
}

func TestProductOffers_MisconfiguredBundleWarns(t *testing.T) {
	bundle := &catalog.Product{
		ID: "bundle", Type: catalog.TypeBundle, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeDynamic, StockMode: catalog.StockModeFixed,
	}
	leaf := &catalog.Product{ID: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{bundle, leaf},
		variants: []catalog.Variant{
			{ID: "bundle-v", ProductID: "bundle"},
			{ID: "leaf-v", ProductID: "leaf"},
		},
		stock: map[string]int{"bundle-v": 1},
		components: []catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "leaf", Quantity: 1, Required: true,
		}},
	})
	log := &captureLog{}
	src.Log = log

	offers, err := src.ProductOffers(context.Background(), []*catalog.Product{bundle}, "", "EUR")
	if err != nil {
		t.Fatalf("ProductOffers: %v", err)
	}
	if offers["bundle"].Amount != nil || !offers["bundle"].InStock {
		t.Fatalf("bundle = %+v", offers["bundle"])
	}
	if len(log.warns) == 0 || len(log.errors) != 0 {
		t.Fatalf("log errors=%v warns=%v", log.errors, log.warns)
	}
}

func TestProductOffers_CancelledContextDoesNotLog(t *testing.T) {
	bundle := &catalog.Product{
		ID: "bundle", Type: catalog.TypeBundle, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeDynamic,
	}
	leaf := &catalog.Product{ID: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{bundle, leaf},
		variants: []catalog.Variant{
			{ID: "bundle-v", ProductID: "bundle"},
			{ID: "leaf-v", ProductID: "leaf"},
		},
		components: []catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "leaf", Quantity: 1, Required: true,
		}},
	})
	prices := src.Prices.(*offerPrices)
	prices.failVariant = "leaf-v"
	prices.failErr = context.Canceled
	log := &captureLog{}
	src.Log = log
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := src.ProductOffers(ctx, []*catalog.Product{bundle}, "", "EUR")
	if err == nil {
		t.Fatal("cancelled request returned nil")
	}
	if len(log.errors) != 0 || len(log.warns) != 0 {
		t.Fatalf("log errors=%v warns=%v", log.errors, log.warns)
	}
}

func TestListingOfferStep_StockBatchError(t *testing.T) {
	plain := &catalog.Product{ID: "plain", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{plain},
		variants: []catalog.Variant{{ID: "plain-v", ProductID: "plain"}},
		prices:   []offerPrice{{variantID: "plain-v", currency: "EUR", amount: 100}},
	})
	src.Stocks = &offerStock{err: errors.New("stock timeout")}
	ctx := composition.NewListingContext([]*catalog.Product{plain})
	err := composition.NewListingOfferStep(src).Apply(ctx)
	if err == nil {
		t.Fatal("listing stock batch error was swallowed")
	}
}

func TestProductOfferStep_StockBatchErrorLeavesPage(t *testing.T) {
	plain := &catalog.Product{ID: "plain", Type: catalog.TypeSimple, Status: catalog.StatusActive, Name: "Widget"}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{plain},
		variants: []catalog.Variant{{ID: "plain-v", ProductID: "plain"}},
		prices:   []offerPrice{{variantID: "plain-v", currency: "EUR", amount: 100}},
	})
	src.Stocks = &offerStock{err: errors.New("stock timeout")}
	log := &captureLog{}
	src.Log = log
	ctx := composition.NewProductContext(plain)
	if err := composition.NewProductOfferStep(src).Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := composition.OfferFromMeta(ctx.Meta); ok {
		t.Fatal("batch failure published meta.offer")
	}
	if !composition.OfferReadFailed(ctx.Meta) {
		t.Fatal("batch failure did not record that the offer step ran")
	}
	public := composition.PublicMeta(ctx.Meta)
	if _, ok := public[composition.MetaOffer]; ok || composition.OfferReadFailed(public) {
		t.Fatal("public meta still exposes the failed read")
	}
	if len(log.errors) != 1 {
		t.Fatalf("log errors=%v", log.errors)
	}

	prices := &mockPriceRepo{err: errors.New("price read")}
	variants := &mockVariantRepo{err: errors.New("variant read")}
	indication := composition.NewPriceIndicationStep(variants, prices, &mockPriceHistoryRepo{}, nil)
	if err := indication.Apply(ctx); err != nil {
		t.Fatalf("price indication: %v", err)
	}
	if len(ctx.Blocks) != 0 {
		t.Fatal("omnibus block built after the offer step failed")
	}
	if prices.capturedVariant != "" || variants.capturedCtx != nil {
		t.Fatal("price indication read variants or prices after a failed offer step")
	}
}

func TestProductOfferStep_PriceBatchErrorSkipsIndication(t *testing.T) {
	plain := &catalog.Product{ID: "plain", Type: catalog.TypeSimple, Status: catalog.StatusActive, Name: "Widget"}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{plain},
		variants: []catalog.Variant{{ID: "plain-v", ProductID: "plain"}},
		prices:   []offerPrice{{variantID: "plain-v", currency: "EUR", amount: 100}},
	})
	src.Prices.(*offerPrices).failVariant = "plain-v"
	src.Prices.(*offerPrices).failErr = errors.New("price db timeout")
	ctx := composition.NewProductContext(plain)
	if err := composition.NewProductOfferStep(src).Apply(ctx); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := composition.OfferFromMeta(ctx.Meta); ok || !composition.OfferReadFailed(ctx.Meta) {
		t.Fatal("price batch failure published an offer or skipped the failure flag")
	}
	prices := &mockPriceRepo{err: errors.New("price read")}
	variants := &mockVariantRepo{err: errors.New("variant read")}
	if err := composition.NewPriceIndicationStep(variants, prices, &mockPriceHistoryRepo{}, nil).Apply(ctx); err != nil {
		t.Fatalf("price indication: %v", err)
	}
	if prices.capturedVariant != "" || variants.capturedCtx != nil {
		t.Fatal("price indication read variants or prices after a failed price batch")
	}
}

func TestProductOffers_WarnsOncePerProduct(t *testing.T) {
	bundle := &catalog.Product{
		ID: "bundle", Type: catalog.TypeBundle, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeDynamic, StockMode: catalog.StockModeFixed,
	}
	leaf := &catalog.Product{ID: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{bundle, leaf},
		variants: []catalog.Variant{
			{ID: "bundle-a", ProductID: "bundle"},
			{ID: "bundle-b", ProductID: "bundle"},
			{ID: "leaf-v", ProductID: "leaf"},
		},
		stock: map[string]int{"bundle-a": 1},
		components: []catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "leaf", Quantity: 1, Required: true,
		}},
	})
	prices := src.Prices.(*offerPrices)
	prices.failVariant = "leaf-v"
	prices.failErr = apperror.Validation("component has no price")
	log := &captureLog{}
	src.Log = log

	offers, err := src.ProductOffers(context.Background(), []*catalog.Product{bundle}, "", "EUR")
	if err != nil {
		t.Fatalf("ProductOffers: %v", err)
	}
	if offers["bundle"].Amount != nil || !offers["bundle"].InStock {
		t.Fatalf("bundle = %+v", offers["bundle"])
	}
	if len(log.warns) != 1 || len(log.errors) != 0 {
		t.Fatalf("log errors=%v warns=%v, want one warning", log.errors, log.warns)
	}
}

func TestProductOffers_StockErrorStillLoggedAfterWarning(t *testing.T) {
	bundle := &catalog.Product{
		ID: "bundle", Type: catalog.TypeBundle, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeDynamic, StockMode: catalog.StockModeDynamic,
	}
	leaf := &catalog.Product{ID: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{bundle, leaf},
		variants: []catalog.Variant{
			{ID: "bundle-v", ProductID: "bundle"},
			{ID: "leaf-v", ProductID: "leaf"},
		},
		components: []catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "leaf", Quantity: 1, Required: true,
		}},
	})
	src.Prices.(*offerPrices).failVariant = "leaf-v"
	src.Prices.(*offerPrices).failErr = apperror.Validation("component has no price")
	src.Stock.(*offerStock).getErr = errors.New("leaf stock timeout")
	log := &captureLog{}
	src.Log = log

	offers, err := src.ProductOffers(context.Background(), []*catalog.Product{bundle}, "", "EUR")
	if err != nil {
		t.Fatalf("ProductOffers: %v", err)
	}
	if offers["bundle"].Amount != nil || offers["bundle"].InStock {
		t.Fatalf("bundle = %+v", offers["bundle"])
	}
	if len(log.warns) != 1 || len(log.errors) != 1 {
		t.Fatalf("log errors=%v warns=%v, want one warning and one error", log.errors, log.warns)
	}
}

func TestVariantOffers_GroupedDynamicAndPerVariantPrice(t *testing.T) {
	grouped := &catalog.Product{ID: "kit", Type: catalog.TypeGrouped, Status: catalog.StatusActive}
	src := offerSources(offerCatalog{
		products: []*catalog.Product{grouped},
		variants: []catalog.Variant{{ID: "kit-v", ProductID: "kit"}},
		prices:   []offerPrice{{variantID: "kit-v", currency: "EUR", amount: 999}},
		stock:    map[string]int{"kit-v": 4},
	})
	offers, err := src.VariantOffers(context.Background(), grouped, []catalog.Variant{{ID: "kit-v", ProductID: "kit"}}, "", "EUR")
	if err != nil {
		t.Fatalf("grouped: %v", err)
	}
	if offers["kit-v"].Amount != nil || offers["kit-v"].InStock {
		t.Fatalf("grouped variant = %+v", offers["kit-v"])
	}

	bundle := &catalog.Product{
		ID: "bundle", Type: catalog.TypeBundle, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeDynamic, StockMode: catalog.StockModeDynamic,
	}
	leaf := &catalog.Product{
		ID: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive,
		PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeFixed,
	}
	src = offerSources(offerCatalog{
		products: []*catalog.Product{bundle, leaf},
		variants: []catalog.Variant{
			{ID: "bundle-a", ProductID: "bundle"},
			{ID: "bundle-b", ProductID: "bundle"},
			{ID: "leaf-v", ProductID: "leaf"},
		},
		prices: []offerPrice{{variantID: "leaf-v", currency: "EUR", amount: 40}},
		stock:  map[string]int{"bundle-a": 0, "leaf-v": 3},
		components: []catalog.BundleComponent{{
			BundleProductID: "bundle", ComponentProductID: "leaf", Quantity: 1, Required: true,
		}},
	})
	page := []catalog.Variant{{ID: "bundle-b", ProductID: "bundle"}, {ID: "bundle-a", ProductID: "bundle"}}
	offers, err = src.VariantOffers(context.Background(), bundle, page, "", "EUR")
	if err != nil {
		t.Fatalf("dynamic: %v", err)
	}
	for _, id := range []string{"bundle-a", "bundle-b"} {
		got := offers[id]
		if got.Amount == nil || *got.Amount != 40 || !got.InStock {
			t.Fatalf("%s = %+v, want summed 40 and in stock from the leaf, not the page's first variant only", id, got)
		}
	}
}

type offerPrice struct {
	variantID string
	currency  string
	storeID   string
	amount    int64
}

type offerCatalog struct {
	products   []*catalog.Product
	variants   []catalog.Variant
	prices     []offerPrice
	stock      map[string]int
	components []catalog.BundleComponent
}

func offerSources(cat offerCatalog) composition.OfferSources {
	products := map[string]*catalog.Product{}
	for _, p := range cat.products {
		products[p.ID] = p
	}
	stock := &offerStock{qty: cat.stock}
	return composition.OfferSources{
		Variants:   offerVariants{rows: cat.variants},
		Prices:     &offerPrices{rows: cat.prices},
		Stock:      stock,
		Stocks:     stock,
		Products:   offerProducts{rows: products},
		Components: offerComponents{rows: cat.components},
	}
}

type offerVariants struct{ rows []catalog.Variant }

func (r offerVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	for i := range r.rows {
		if r.rows[i].ID == id {
			v := r.rows[i]
			return &v, nil
		}
	}
	return nil, nil
}
func (offerVariants) FindBySKU(context.Context, string) (*catalog.Variant, error) { return nil, nil }
func (offerVariants) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (r offerVariants) ListByProductID(_ context.Context, productID string, _, limit int) ([]catalog.Variant, error) {
	var out []catalog.Variant
	for _, v := range r.rows {
		if v.ProductID == productID {
			out = append(out, v)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r offerVariants) ListByProductIDs(_ context.Context, productIDs []string, limit int) (map[string][]catalog.Variant, error) {
	want := map[string]struct{}{}
	for _, id := range productIDs {
		want[id] = struct{}{}
	}
	out := map[string][]catalog.Variant{}
	for _, v := range r.rows {
		if _, ok := want[v.ProductID]; !ok {
			continue
		}
		out[v.ProductID] = append(out[v.ProductID], v)
	}
	if limit > 0 {
		for id, vs := range out {
			if len(vs) > limit {
				out[id] = vs[:limit]
			}
		}
	}
	return out, nil
}
func (offerVariants) Create(context.Context, *catalog.Variant) error { return nil }
func (offerVariants) Update(context.Context, *catalog.Variant) error { return nil }

type offerPrices struct {
	rows        []offerPrice
	calls       []struct{ currency, storeID string }
	failVariant string
	failErr     error
}

func (r *offerPrices) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, storeID string) (*pricing.Price, error) {
	if r.failVariant != "" && variantID == r.failVariant {
		if r.failErr != nil {
			return nil, r.failErr
		}
		return nil, errors.New("price read failed")
	}
	for _, row := range r.rows {
		if row.variantID == variantID && row.currency == currency && row.storeID == storeID {
			p, err := pricing.NewPrice("price-"+variantID, variantID, storeID, shared.MustNewMoney(row.amount, currency))
			if err != nil {
				return nil, err
			}
			return &p, nil
		}
	}
	return nil, nil
}
func (r *offerPrices) FindByVariantsCurrencyAndStore(ctx context.Context, variantIDs []string, currency, storeID string) (map[string]*pricing.Price, error) {
	if r != nil {
		r.calls = append(r.calls, struct{ currency, storeID string }{currency, storeID})
	}
	out := map[string]*pricing.Price{}
	for _, id := range variantIDs {
		price, err := r.FindByVariantCurrencyAndStore(ctx, id, currency, storeID)
		if err != nil {
			return nil, err
		}
		if price == nil && storeID != "" {
			price, err = r.FindByVariantCurrencyAndStore(ctx, id, currency, "")
			if err != nil {
				return nil, err
			}
		}
		if price != nil {
			out[id] = price
		}
	}
	return out, nil
}

type captureLog struct {
	errors []string
	warns  []string
}

func (c *captureLog) Info(string, map[string]interface{}) {}
func (c *captureLog) Warn(event string, _ map[string]interface{}) {
	c.warns = append(c.warns, event)
}
func (c *captureLog) Error(event string, _ error, _ map[string]interface{}) {
	c.errors = append(c.errors, event)
}

func (*offerPrices) ListByVariantID(context.Context, string) ([]pricing.Price, error) {
	return nil, nil
}
func (*offerPrices) List(context.Context, int, int) ([]pricing.Price, error) { return nil, nil }
func (*offerPrices) Upsert(context.Context, *pricing.Price) error            { return nil }

type offerStock struct {
	qty    map[string]int
	err    error
	getErr error
}

func (s *offerStock) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	if s.getErr != nil {
		return inventory.StockEntry{}, s.getErr
	}
	return inventory.StockEntry{VariantID: variantID, Quantity: s.qty[variantID]}, nil
}
func (s *offerStock) GetStocks(_ context.Context, variantIDs []string) (map[string]inventory.StockEntry, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string]inventory.StockEntry, len(variantIDs))
	for _, id := range variantIDs {
		if qty, ok := s.qty[id]; ok {
			out[id] = inventory.StockEntry{VariantID: id, Quantity: qty}
		}
	}
	return out, nil
}
func (*offerStock) SetStock(context.Context, *inventory.StockEntry) error   { return nil }
func (*offerStock) SetStocks(context.Context, []inventory.StockEntry) error { return nil }
func (*offerStock) ListStock(context.Context, int, int) ([]inventory.StockEntry, error) {
	return nil, nil
}
func (*offerStock) ListInventory(context.Context, int, int, string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}
func (*offerStock) GetInventoryItem(context.Context, string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{}, nil
}

type offerProducts struct{ rows map[string]*catalog.Product }

func (r offerProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	p := r.rows[id]
	if p == nil {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}
func (offerProducts) FindBySlug(context.Context, string) (*catalog.Product, error) { return nil, nil }
func (offerProducts) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (offerProducts) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (offerProducts) Create(context.Context, *catalog.Product) error { return nil }
func (offerProducts) Update(context.Context, *catalog.Product) error { return nil }
func (r offerProducts) WithTx(*sql.Tx) catalog.ProductRepository     { return r }

type offerComponents struct{ rows []catalog.BundleComponent }

func (r offerComponents) ListByBundleID(_ context.Context, bundleProductID string) ([]catalog.BundleComponent, error) {
	out := make([]catalog.BundleComponent, 0)
	for _, row := range r.rows {
		if row.BundleProductID == bundleProductID {
			out = append(out, row)
		}
	}
	return out, nil
}
func (offerComponents) CountByBundleID(context.Context, string) (int, error)    { return 0, nil }
func (offerComponents) CountByComponentID(context.Context, string) (int, error) { return 0, nil }
func (offerComponents) Upsert(context.Context, *catalog.BundleComponent) error  { return nil }
func (offerComponents) Remove(context.Context, string, string) error            { return nil }
