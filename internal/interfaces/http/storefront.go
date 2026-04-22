package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	appAuth "github.com/akarso/shopanda/internal/application/auth"
	cartApp "github.com/akarso/shopanda/internal/application/cart"
	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// StorefrontHandler renders SSR pages using the theme engine.
type StorefrontHandler struct {
	engine   *theme.Engine
	repo     catalog.ProductRepository
	cats     catalog.CategoryRepository
	pdp      *composition.Pipeline[composition.ProductContext]
	plp      *composition.Pipeline[composition.ListingContext]
	search   search.SearchEngine
	variants catalog.VariantRepository
	carts    *cartApp.Service
	auth     *appAuth.Service
	checkout *checkoutApp.Service
	orders   order.OrderRepository
	account  AccountDeleter
	shipping []shipping.Provider
	payment  payment.Provider
	log      logger.Logger
	catNav   storefrontCategoryCache
}

type storefrontCategoryCache struct {
	mu        sync.RWMutex
	data      []catalog.Category
	expiresAt time.Time
	ttl       time.Duration
}

type StorefrontNavLink struct {
	Label string
	URL   string
}

type StorefrontCategoryNavItem struct {
	Label    string
	URL      string
	Children []StorefrontCategoryNavItem
}

type StorefrontBreadcrumb struct {
	Label   string
	URL     string
	Current bool
}

type StorefrontCategorySummary struct {
	Name        string
	URL         string
	Description string
}

type StorefrontLayoutData struct {
	SiteName     string
	SearchAction string
	SearchQuery  string
	CartURL      string
	CartLabel    string
	EnableCart   bool
	CurrentYear  int
	Nav          []StorefrontNavLink
	Categories   []StorefrontCategoryNavItem
}

type StorefrontHomePageData struct {
	Layout StorefrontLayoutData
	Theme  theme.Theme
}

type StorefrontProductPageData struct {
	*composition.ProductContext
	Layout   StorefrontLayoutData
	Theme    theme.Theme
	CartForm *StorefrontCartFormData
}

type StorefrontProductCard struct {
	Name         string
	Slug         string
	Description  string
	ImageURL     string
	HasPrice     bool
	PriceText    string
	Availability string
}

type StorefrontPaginationLink struct {
	Label   string
	URL     string
	Current bool
}

type StorefrontPaginationData struct {
	Page       int
	PerPage    int
	Total      int
	TotalPages int
	PrevURL    string
	NextURL    string
	HasPrev    bool
	HasNext    bool
	Links      []StorefrontPaginationLink
}

type StorefrontSortOption struct {
	Label    string
	Value    string
	URL      string
	Selected bool
}

type StorefrontFilterValue struct {
	Label string
	Count int
}

type StorefrontFilterGroup struct {
	Name   string
	Values []StorefrontFilterValue
}

type StorefrontListingPageData struct {
	Layout        StorefrontLayoutData
	Theme         theme.Theme
	Title         string
	Eyebrow       string
	View          string
	GridURL       string
	ListURL       string
	Query         string
	ResultSummary string
	EmptyMessage  string
	Products      []StorefrontProductCard
	Pagination    StorefrontPaginationData
	SortOptions   []StorefrontSortOption
	Filters       []StorefrontFilterGroup
	Blocks        []composition.Block
	Meta          map[string]interface{}
}

type StorefrontCategoryPageData struct {
	StorefrontListingPageData
	Category      StorefrontCategorySummary
	Breadcrumbs   []StorefrontBreadcrumb
	Subcategories []StorefrontCategorySummary
}

type storefrontListingParams struct {
	Page    int
	PerPage int
	Sort    string
	View    string
	Query   string
}

var storefrontSortOptions = []struct {
	Value      string
	Label      string
	SearchSort string
}{
	{Value: "newest", Label: "Newest", SearchSort: "-created_at"},
	{Value: "oldest", Label: "Oldest", SearchSort: "created_at"},
	{Value: "name_asc", Label: "Name A-Z", SearchSort: "name"},
	{Value: "name_desc", Label: "Name Z-A", SearchSort: "-name"},
}

const storefrontCategoryCacheTTL = 45 * time.Second

// NewStorefrontHandler creates a StorefrontHandler.
func NewStorefrontHandler(
	engine *theme.Engine,
	repo catalog.ProductRepository,
	categories catalog.CategoryRepository,
	pdp *composition.Pipeline[composition.ProductContext],
	plp *composition.Pipeline[composition.ListingContext],
	searchEngine search.SearchEngine,
) *StorefrontHandler {
	return &StorefrontHandler{
		engine: engine,
		repo:   repo,
		cats:   categories,
		pdp:    pdp,
		plp:    plp,
		search: searchEngine,
		log:    logger.New("warn"),
		catNav: storefrontCategoryCache{ttl: storefrontCategoryCacheTTL},
	}
}

// WithCart enables storefront cart rendering and mutations using the provided
// variant repository and cart application service.
func (h *StorefrontHandler) WithCart(variants catalog.VariantRepository, carts *cartApp.Service) *StorefrontHandler {
	h.variants = variants
	h.carts = carts
	return h
}

// WithCheckout enables storefront checkout rendering and order placement using
// the provided shipping providers, payment provider, and checkout service.
func (h *StorefrontHandler) WithCheckout(shippingProviders []shipping.Provider, paymentProvider payment.Provider, checkout *checkoutApp.Service) *StorefrontHandler {
	h.shipping = append([]shipping.Provider(nil), shippingProviders...)
	h.payment = paymentProvider
	h.checkout = checkout
	return h
}

// WithAccount enables storefront account pages using the auth service,
// order repository, and account deletion service.
func (h *StorefrontHandler) WithAccount(authService *appAuth.Service, orders order.OrderRepository, account AccountDeleter) *StorefrontHandler {
	h.auth = authService
	h.orders = orders
	h.account = account
	return h
}

// Home handles GET / and renders the storefront landing page.
func (h *StorefrontHandler) Home() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.engine.HasTemplate("home") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		page := StorefrontHomePageData{
			Layout: h.layoutDataBestEffort(r),
			Theme:  h.engine.Theme(),
		}
		h.renderPage(w, "home", page)
	}
}

// Categories handles GET /categories and renders the root category landing page.
func (h *StorefrontHandler) Categories() http.HandlerFunc {
	return h.renderCategory(true)
}

// Category handles GET /categories/{slug} and renders a category page.
func (h *StorefrontHandler) Category() http.HandlerFunc {
	return h.renderCategory(false)
}

// Products handles GET /products and renders the storefront listing page.
func (h *StorefrontHandler) Products() http.HandlerFunc {
	return h.renderListing(false)
}

// Search handles GET /search and renders the storefront search results page.
func (h *StorefrontHandler) Search() http.HandlerFunc {
	return h.renderListing(true)
}

// Product handles GET /products/{slug} and renders the product page via SSR.
func (h *StorefrontHandler) Product() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		if slug == "" {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		product, err := h.repo.FindBySlug(r.Context(), slug)
		if err != nil {
			var appErr *apperror.Error
			if errors.As(err, &appErr) && appErr.Code == apperror.CodeNotFound {
				http.Error(w, "Not Found", http.StatusNotFound)
				return
			}
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if product == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		ctx := composition.NewProductContext(product)
		ctx.Ctx = r.Context()
		if s := store.FromContext(r.Context()); s != nil {
			ctx.StoreID = s.ID
			if ctx.Currency == "" {
				ctx.Currency = s.Currency
			}
			if ctx.Country == "" {
				ctx.Country = s.Country
			}
		}
		if err := h.pdp.Execute(ctx); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		page := StorefrontProductPageData{
			ProductContext: ctx,
			Layout:         h.layoutDataBestEffort(r),
			Theme:          h.engine.Theme(),
			CartForm:       h.resolveCartForm(r, product.ID),
		}
		h.renderPage(w, "product", page)
	}
}

func (h *StorefrontHandler) renderPage(w http.ResponseWriter, name string, data interface{}) {
	h.renderPageStatus(w, name, data, http.StatusOK)
}

func (h *StorefrontHandler) renderPageStatus(w http.ResponseWriter, name string, data interface{}, status int) {
	var buf bytes.Buffer
	if err := h.engine.Render(&buf, name, data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (h *StorefrontHandler) renderListing(searchMode bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.engine.HasTemplate("product_list") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		params, err := parseStorefrontListingParams(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		result := search.SearchResult{Products: []search.Product{}, Facets: map[string][]search.FacetValue{}}
		if !searchMode || params.Query != "" {
			query := search.SearchQuery{
				Text:   params.Query,
				Sort:   storefrontSearchSort(params.Sort),
				Limit:  params.PerPage,
				Offset: (params.Page - 1) * params.PerPage,
			}
			if s := store.FromContext(r.Context()); s != nil {
				query.StoreID = s.ID
				query.Currency = s.Currency
			}
			result, err = h.search.Search(r.Context(), query)
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
		}

		ctx := composition.NewListingContext(searchProductsToCatalog(result.Products))
		ctx.Ctx = r.Context()
		if s := store.FromContext(r.Context()); s != nil {
			if ctx.Currency == "" {
				ctx.Currency = s.Currency
			}
			if ctx.Country == "" {
				ctx.Country = s.Country
			}
		}
		if err := h.plp.Execute(ctx); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		h.renderPage(w, "product_list", h.buildListingPageData(r, h.layoutDataBestEffort(r), ctx, result, params, searchMode))
	}
}

func (h *StorefrontHandler) renderCategory(root bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.engine.HasTemplate("category") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		params, err := parseStorefrontListingParams(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		allCategories, err := h.cachedCategories(r.Context())
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		var category *catalog.Category
		if !root {
			slug := r.PathValue("slug")
			if slug == "" {
				http.Error(w, "Not Found", http.StatusNotFound)
				return
			}
			category = storefrontCategoryBySlug(allCategories, slug)
			if category == nil {
				http.Error(w, "Not Found", http.StatusNotFound)
				return
			}
		}

		query := search.SearchQuery{
			Sort:    storefrontSearchSort(params.Sort),
			Limit:   params.PerPage,
			Offset:  (params.Page - 1) * params.PerPage,
			Filters: map[string]interface{}{},
		}
		if category != nil {
			query.Filters["category"] = category.ID
		}
		if s := store.FromContext(r.Context()); s != nil {
			query.StoreID = s.ID
			query.Currency = s.Currency
		}

		result, err := h.search.Search(r.Context(), query)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		ctx := composition.NewListingContext(searchProductsToCatalog(result.Products))
		ctx.Ctx = r.Context()
		if s := store.FromContext(r.Context()); s != nil {
			if ctx.Currency == "" {
				ctx.Currency = s.Currency
			}
			if ctx.Country == "" {
				ctx.Country = s.Country
			}
		}
		if err := h.plp.Execute(ctx); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		layout, err := h.layoutData(r, allCategories)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		page := h.buildCategoryPageData(r, layout, ctx, result, params, category, allCategories)
		h.renderPage(w, "category", page)
	}
}

func (h *StorefrontHandler) layoutData(r *http.Request, categories []catalog.Category) (StorefrontLayoutData, error) {
	if categories == nil {
		allCategories, err := h.cachedCategories(r.Context())
		if err != nil {
			return StorefrontLayoutData{}, err
		}
		categories = allCategories
	}
	return h.buildLayoutData(r, categories), nil
}

func (h *StorefrontHandler) layoutDataBestEffort(r *http.Request) StorefrontLayoutData {
	layout, err := h.layoutData(r, nil)
	if err == nil {
		return layout
	}
	h.log.Warn("storefront.categories.load_failed", map[string]interface{}{
		"path":  r.URL.Path,
		"error": err.Error(),
	})
	return h.buildLayoutData(r, nil)
}

func (h *StorefrontHandler) buildLayoutData(r *http.Request, categories []catalog.Category) StorefrontLayoutData {
	themeCfg := h.engine.Theme().Storefront
	siteName := h.engine.Theme().Name
	if s := store.FromContext(r.Context()); s != nil && s.Name != "" {
		siteName = s.Name
	}
	searchAction := themeCfg.SearchAction
	if searchAction == "" {
		searchAction = "/search"
	}
	cartURL := themeCfg.CartURL
	if cartURL == "" {
		cartURL = "/cart"
	}
	cartLabel := themeCfg.CartLabel
	if cartLabel == "" {
		cartLabel = "Cart (0)"
	}
	nav := make([]StorefrontNavLink, 0, len(themeCfg.Nav))
	if len(themeCfg.Nav) > 0 {
		for _, item := range themeCfg.Nav {
			if item.Label == "" || item.URL == "" {
				continue
			}
			nav = append(nav, StorefrontNavLink{Label: item.Label, URL: item.URL})
		}
	}
	if len(nav) == 0 {
		accountURL := "/account/login"
		if storefrontCustomerID(r) != "" {
			accountURL = "/account/orders"
		}
		nav = []StorefrontNavLink{
			{Label: "Home", URL: "/"},
			{Label: "Categories", URL: "/categories"},
			{Label: "Account", URL: accountURL},
		}
	}
	return StorefrontLayoutData{
		SiteName:     siteName,
		SearchAction: searchAction,
		SearchQuery:  strings.TrimSpace(r.URL.Query().Get("q")),
		CartURL:      cartURL,
		CartLabel:    h.cartLabelBestEffort(r, cartLabel),
		EnableCart:   h.carts != nil,
		CurrentYear:  time.Now().UTC().Year(),
		Nav:          nav,
		Categories:   storefrontCategoryTree(categories),
	}
}

func (h *StorefrontHandler) cachedCategories(ctx context.Context) ([]catalog.Category, error) {
	now := time.Now().UTC()
	h.catNav.mu.RLock()
	if h.catNav.ttl > 0 && now.Before(h.catNav.expiresAt) {
		cached := append([]catalog.Category(nil), h.catNav.data...)
		h.catNav.mu.RUnlock()
		return cached, nil
	}
	h.catNav.mu.RUnlock()

	categories, err := h.cats.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	cloned := append([]catalog.Category(nil), categories...)
	h.catNav.mu.Lock()
	h.catNav.data = cloned
	h.catNav.expiresAt = now.Add(h.catNav.ttl)
	h.catNav.mu.Unlock()
	return append([]catalog.Category(nil), cloned...), nil
}

func (h *StorefrontHandler) buildListingPageData(r *http.Request, layout StorefrontLayoutData, ctx *composition.ListingContext, result search.SearchResult, params storefrontListingParams, searchMode bool) StorefrontListingPageData {
	title := "All Products"
	eyebrow := "Catalog"
	resultSummary := fmt.Sprintf("Showing %d product(s)", result.Total)
	emptyMessage := "No products are available yet."
	if searchMode {
		title = "Search"
		eyebrow = "Search results"
		if params.Query != "" {
			title = fmt.Sprintf("Search results for %q", params.Query)
			resultSummary = fmt.Sprintf("%d result(s) for %q", result.Total, params.Query)
			emptyMessage = "No products matched your search."
		} else {
			resultSummary = "Enter a search term to browse matching products."
			emptyMessage = "Try a product name or keyword."
		}
	}

	return StorefrontListingPageData{
		Layout:        layout,
		Theme:         h.engine.Theme(),
		Title:         title,
		Eyebrow:       eyebrow,
		View:          params.View,
		GridURL:       storefrontURL(r, params, map[string]string{"view": "grid", "page": "1"}),
		ListURL:       storefrontURL(r, params, map[string]string{"view": "list", "page": "1"}),
		Query:         params.Query,
		ResultSummary: resultSummary,
		EmptyMessage:  emptyMessage,
		Products:      storefrontCards(ctx.Products, result.Products, ctx.Currency),
		Pagination:    storefrontPagination(r, params, result.Total),
		SortOptions:   storefrontSortLinks(r, params),
		Filters:       storefrontFilters(result.Facets),
		Blocks:        ctx.Blocks,
		Meta:          ctx.Meta,
	}
}

func (h *StorefrontHandler) buildCategoryPageData(r *http.Request, layout StorefrontLayoutData, ctx *composition.ListingContext, result search.SearchResult, params storefrontListingParams, category *catalog.Category, allCategories []catalog.Category) StorefrontCategoryPageData {
	listing := h.buildListingPageData(r, layout, ctx, result, params, false)
	page := StorefrontCategoryPageData{
		StorefrontListingPageData: listing,
		Category: StorefrontCategorySummary{
			Name: "Categories",
			URL:  "/categories",
		},
		Breadcrumbs:   []StorefrontBreadcrumb{{Label: "Home", URL: "/"}, {Label: "Categories", URL: "/categories", Current: true}},
		Subcategories: storefrontSubcategories(allCategories, nil),
	}
	page.Title = "Categories"
	page.Eyebrow = "Browse categories"
	page.ResultSummary = fmt.Sprintf("Showing %d product(s) across all categories", result.Total)
	page.EmptyMessage = "No products are available yet."
	if category != nil {
		page.Category = storefrontCategorySummary(*category)
		page.Breadcrumbs = storefrontBreadcrumbs(allCategories, category)
		page.Subcategories = storefrontSubcategories(allCategories, category)
		page.Title = category.Name
		page.Eyebrow = "Category"
		page.ResultSummary = fmt.Sprintf("Showing %d product(s) in %s", result.Total, category.Name)
		page.EmptyMessage = fmt.Sprintf("No products are available in %s yet.", category.Name)
	}
	return page
}

func parseStorefrontListingParams(r *http.Request) (storefrontListingParams, error) {
	q := r.URL.Query()
	page := 1
	if raw := strings.TrimSpace(q.Get("page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return storefrontListingParams{}, fmt.Errorf("page must be a positive integer")
		}
		page = parsed
	}
	perPage := 12
	if raw := strings.TrimSpace(q.Get("per_page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return storefrontListingParams{}, fmt.Errorf("per_page must be a positive integer")
		}
		if parsed > 48 {
			parsed = 48
		}
		perPage = parsed
	}
	view := strings.TrimSpace(q.Get("view"))
	if view != "list" {
		view = "grid"
	}
	return storefrontListingParams{
		Page:    page,
		PerPage: perPage,
		Sort:    storefrontSortValue(strings.TrimSpace(q.Get("sort"))),
		View:    view,
		Query:   strings.TrimSpace(q.Get("q")),
	}, nil
}

func storefrontSearchSort(value string) string {
	for _, option := range storefrontSortOptions {
		if option.Value == value {
			return option.SearchSort
		}
	}
	return storefrontSortOptions[0].SearchSort
}

func storefrontSortValue(value string) string {
	for _, option := range storefrontSortOptions {
		if option.Value == value {
			return value
		}
	}
	return storefrontSortOptions[0].Value
}

func searchProductsToCatalog(products []search.Product) []*catalog.Product {
	out := make([]*catalog.Product, 0, len(products))
	for i := range products {
		attrs := products[i].Attributes
		if attrs == nil {
			attrs = map[string]interface{}{}
		}
		out = append(out, &catalog.Product{
			ID:          products[i].ID,
			Name:        products[i].Name,
			Slug:        products[i].Slug,
			Description: products[i].Description,
			Status:      catalog.StatusActive,
			Attributes:  attrs,
			CreatedAt:   products[i].CreatedAt,
			UpdatedAt:   products[i].CreatedAt,
		})
	}
	return out
}

func storefrontCards(products []*catalog.Product, indexed []search.Product, currency string) []StorefrontProductCard {
	indexedByID := make(map[string]search.Product, len(indexed))
	for i := range indexed {
		indexedByID[indexed[i].ID] = indexed[i]
	}
	out := make([]StorefrontProductCard, 0, len(products))
	for _, product := range products {
		if product == nil {
			continue
		}
		indexedProduct, hasIndexedProduct := indexedByID[product.ID]
		priceText := ""
		availability := ""
		if hasIndexedProduct {
			priceText = formatStorefrontMoney(indexedProduct.Price, currency)
			availability = "Out of stock"
			if indexedProduct.InStock {
				availability = "In stock"
			}
		}
		out = append(out, StorefrontProductCard{
			Name:         product.Name,
			Slug:         product.Slug,
			Description:  product.Description,
			ImageURL:     imageURLFromAttrs(product.Attributes),
			HasPrice:     hasIndexedProduct,
			PriceText:    priceText,
			Availability: availability,
		})
	}
	return out
}

func imageURLFromAttrs(attrs map[string]interface{}) string {
	if attrs == nil {
		return ""
	}
	if raw, ok := attrs["image_url"].(string); ok {
		return strings.TrimSpace(raw)
	}
	return ""
}

func formatStorefrontMoney(amount int64, currency string) string {
	if currency == "" {
		currency = "EUR"
	}
	return fmt.Sprintf("%s %.2f", currency, float64(amount)/100.0)
}

func storefrontPagination(r *http.Request, params storefrontListingParams, total int) StorefrontPaginationData {
	if total <= 0 {
		return StorefrontPaginationData{Page: params.Page, PerPage: params.PerPage}
	}
	totalPages := int(math.Ceil(float64(total) / float64(params.PerPage)))
	if totalPages < 1 {
		totalPages = 1
	}
	if params.Page > totalPages {
		params.Page = totalPages
	}
	start := params.Page - 2
	if start < 1 {
		start = 1
	}
	end := start + 4
	if end > totalPages {
		end = totalPages
	}
	links := make([]StorefrontPaginationLink, 0, end-start+1)
	for page := start; page <= end; page++ {
		links = append(links, StorefrontPaginationLink{
			Label:   strconv.Itoa(page),
			URL:     storefrontURL(r, params, map[string]string{"page": strconv.Itoa(page)}),
			Current: page == params.Page,
		})
	}
	pagination := StorefrontPaginationData{
		Page:       params.Page,
		PerPage:    params.PerPage,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    params.Page > 1,
		HasNext:    params.Page < totalPages,
		Links:      links,
	}
	if pagination.HasPrev {
		pagination.PrevURL = storefrontURL(r, params, map[string]string{"page": strconv.Itoa(params.Page - 1)})
	}
	if pagination.HasNext {
		pagination.NextURL = storefrontURL(r, params, map[string]string{"page": strconv.Itoa(params.Page + 1)})
	}
	return pagination
}

func storefrontSortLinks(r *http.Request, params storefrontListingParams) []StorefrontSortOption {
	out := make([]StorefrontSortOption, 0, len(storefrontSortOptions))
	for _, option := range storefrontSortOptions {
		out = append(out, StorefrontSortOption{
			Label:    option.Label,
			Value:    option.Value,
			URL:      storefrontURL(r, params, map[string]string{"sort": option.Value, "page": "1"}),
			Selected: params.Sort == option.Value,
		})
	}
	return out
}

func storefrontFilters(facets map[string][]search.FacetValue) []StorefrontFilterGroup {
	groups := make([]StorefrontFilterGroup, 0, len(facets))
	for name, values := range facets {
		group := StorefrontFilterGroup{Name: name, Values: make([]StorefrontFilterValue, 0, len(values))}
		for _, value := range values {
			group.Values = append(group.Values, StorefrontFilterValue{Label: value.Value, Count: value.Count})
		}
		groups = append(groups, group)
	}
	return groups
}

func storefrontURL(r *http.Request, params storefrontListingParams, overrides map[string]string) string {
	q := url.Values{}
	q.Set("page", strconv.Itoa(params.Page))
	q.Set("per_page", strconv.Itoa(params.PerPage))
	q.Set("sort", params.Sort)
	q.Set("view", params.View)
	if params.Query != "" {
		q.Set("q", params.Query)
	}
	for key, value := range overrides {
		if value == "" {
			q.Del(key)
			continue
		}
		q.Set(key, value)
	}
	encoded := q.Encode()
	if encoded == "" {
		return r.URL.Path
	}
	return r.URL.Path + "?" + encoded
}

func storefrontCategoryTree(all []catalog.Category) []StorefrontCategoryNavItem {
	type navNode struct {
		id       string
		item     StorefrontCategoryNavItem
		children []*navNode
	}
	nodes := make(map[string]*navNode, len(all))
	roots := make([]string, 0)
	for _, category := range all {
		nodes[category.ID] = &navNode{id: category.ID, item: StorefrontCategoryNavItem{Label: category.Name, URL: "/categories/" + category.Slug}}
		if category.ParentID == nil {
			roots = append(roots, category.ID)
		}
	}
	for _, category := range all {
		if category.ParentID == nil {
			continue
		}
		parent, ok := nodes[*category.ParentID]
		if !ok {
			continue
		}
		parent.children = append(parent.children, nodes[category.ID])
	}
	var materialize func(node *navNode, visited map[string]struct{}) StorefrontCategoryNavItem
	materialize = func(node *navNode, visited map[string]struct{}) StorefrontCategoryNavItem {
		item := node.item
		if _, seen := visited[node.id]; seen {
			return item
		}
		visited[node.id] = struct{}{}
		for _, child := range node.children {
			childVisited := make(map[string]struct{}, len(visited))
			for key := range visited {
				childVisited[key] = struct{}{}
			}
			item.Children = append(item.Children, materialize(child, childVisited))
		}
		return item
	}
	tree := make([]StorefrontCategoryNavItem, 0, len(roots))
	for _, rootID := range roots {
		if node, ok := nodes[rootID]; ok {
			tree = append(tree, materialize(node, map[string]struct{}{}))
		}
	}
	return tree
}

func storefrontCategoryBySlug(all []catalog.Category, slug string) *catalog.Category {
	for i := range all {
		if all[i].Slug == slug {
			return &all[i]
		}
	}
	return nil
}

func storefrontBreadcrumbs(all []catalog.Category, category *catalog.Category) []StorefrontBreadcrumb {
	byID := make(map[string]catalog.Category, len(all))
	for _, item := range all {
		byID[item.ID] = item
	}
	trail := make([]StorefrontBreadcrumb, 0, len(all)+1)
	trail = append(trail, StorefrontBreadcrumb{Label: "Home", URL: "/"})
	chain := make([]catalog.Category, 0)
	current := category
	visited := make(map[string]struct{}, len(all))
	for current != nil {
		if _, seen := visited[current.ID]; seen {
			break
		}
		visited[current.ID] = struct{}{}
		chain = append([]catalog.Category{*current}, chain...)
		if current.ParentID == nil {
			break
		}
		if _, seen := visited[*current.ParentID]; seen {
			break
		}
		parent, ok := byID[*current.ParentID]
		if !ok {
			break
		}
		current = &parent
	}
	for _, item := range chain {
		trail = append(trail, StorefrontBreadcrumb{Label: item.Name, URL: "/categories/" + item.Slug})
	}
	if len(trail) > 0 {
		trail[len(trail)-1].Current = true
	}
	return trail
}

func storefrontSubcategories(all []catalog.Category, parent *catalog.Category) []StorefrontCategorySummary {
	out := make([]StorefrontCategorySummary, 0)
	for _, category := range all {
		if parent == nil {
			if category.ParentID != nil {
				continue
			}
		} else {
			if category.ParentID == nil || *category.ParentID != parent.ID {
				continue
			}
		}
		out = append(out, storefrontCategorySummary(category))
	}
	return out
}

func storefrontCategorySummary(category catalog.Category) StorefrontCategorySummary {
	return StorefrontCategorySummary{
		Name:        category.Name,
		URL:         "/categories/" + category.Slug,
		Description: storefrontCategoryDescription(category.Meta),
	}
}

func storefrontCategoryDescription(meta map[string]interface{}) string {
	if meta == nil {
		return ""
	}
	if raw, ok := meta["description"].(string); ok {
		return strings.TrimSpace(raw)
	}
	return ""
}
