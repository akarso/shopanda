package graphql_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gql "github.com/graphql-go/graphql"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/interfaces/http/storefront"
	cgraphql "github.com/akarso/shopanda/plugins/core/graphql"
)

type mapVisibilityFacts map[string]catalog.VisibilitySnapshot

func (m mapVisibilityFacts) LoadMany(_ context.Context, ids []string) (map[string]catalog.VisibilitySnapshot, error) {
	out := make(map[string]catalog.VisibilitySnapshot, len(ids))
	for _, id := range ids {
		if snap, ok := m[id]; ok {
			out[id] = snap
		}
	}
	return out, nil
}

type errVisibilityFacts struct{}

func (errVisibilityFacts) LoadMany(context.Context, []string) (map[string]catalog.VisibilitySnapshot, error) {
	return nil, errors.New("facts unavailable")
}

func forcedVisibleSnap() catalog.VisibilitySnapshot {
	return catalog.VisibilitySnapshot{
		Status: catalog.StatusActive,
		Modes: catalog.VisibilityAxes{
			Catalog:      catalog.VisibilityModeVisible,
			Search:       catalog.VisibilityModeVisible,
			Individually: catalog.VisibilityModeVisible,
			Purchasable:  catalog.VisibilityModeVisible,
		},
		Inputs: catalog.VisibilityInputs{
			QuantityPositive:   true,
			HasPrice:           true,
			HasStoreAssignment: true,
		},
	}
}

func hiddenIndividuallySnap() catalog.VisibilitySnapshot {
	snap := forcedVisibleSnap()
	snap.Modes.Individually = catalog.VisibilityModeHidden
	return snap
}

func hiddenCatalogSnap() catalog.VisibilitySnapshot {
	snap := forcedVisibleSnap()
	snap.Modes.Catalog = catalog.VisibilityModeHidden
	return snap
}

func catalogHiddenIndividuallyVisibleSnap() catalog.VisibilitySnapshot {
	snap := forcedVisibleSnap()
	snap.Modes.Catalog = catalog.VisibilityModeHidden
	snap.Modes.Individually = catalog.VisibilityModeVisible
	return snap
}

type stubBundleLister struct {
	listFn func(ctx context.Context, bundleID string) ([]catalog.BundleComponent, error)
}

func (s *stubBundleLister) List(ctx context.Context, bundleID string) ([]catalog.BundleComponent, error) {
	if s.listFn != nil {
		return s.listFn(ctx, bundleID)
	}
	return nil, nil
}

type stubGroupedLister struct {
	listFn func(ctx context.Context, groupID string) ([]catalog.GroupedMember, error)
}

func (s *stubGroupedLister) List(ctx context.Context, groupID string) ([]catalog.GroupedMember, error) {
	if s.listFn != nil {
		return s.listFn(ctx, groupID)
	}
	return nil, nil
}

type batchProductRepo struct {
	stubProductRepo
	byID            map[string]*catalog.Product
	listByIDsCalls  int
	listByIDsErr    error
}

func (r *batchProductRepo) ListByIDs(_ context.Context, ids []string) (map[string]*catalog.Product, error) {
	r.listByIDsCalls++
	if r.listByIDsErr != nil {
		return nil, r.listByIDsErr
	}
	out := make(map[string]*catalog.Product, len(ids))
	for _, id := range ids {
		if p, ok := r.byID[id]; ok {
			out[id] = p
		}
	}
	return out, nil
}

func mustActiveProduct(t *testing.T, id, name, slug string) catalog.Product {
	t.Helper()
	p, err := catalog.NewProduct(id, name, slug)
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	p.Status = catalog.StatusActive
	return p
}

func execGraphQL(t *testing.T, resolver *cgraphql.Resolver, query string) map[string]interface{} {
	t.Helper()
	schema, err := cgraphql.NewSchema(resolver)
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	h := cgraphql.NewHandler(schema, testLogger())
	body, _ := json.Marshal(map[string]string{"query": query})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errs, ok := resp["errors"]; ok && errs != nil {
		t.Fatalf("graphql errors: %v", errs)
	}
	return resp
}

func execGraphQLAllowErrors(t *testing.T, resolver *cgraphql.Resolver, query string) map[string]interface{} {
	t.Helper()
	schema, err := cgraphql.NewSchema(resolver)
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	h := cgraphql.NewHandler(schema, testLogger())
	body, _ := json.Marshal(map[string]string{"query": query})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// Resolver failures are sanitized to HTTP 500 with GraphQL errors (fail-closed).
	if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError && rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func connectionItems(t *testing.T, conn interface{}) []interface{} {
	t.Helper()
	m, ok := conn.(map[string]interface{})
	if !ok {
		t.Fatalf("connection type = %T", conn)
	}
	items, ok := m["items"].([]interface{})
	if !ok {
		t.Fatalf("items missing: %+v", m)
	}
	return items
}

func TestHandler_ProductParityFields(t *testing.T) {
	product := mustActiveProduct(t, "prod-1", "Widget", "widget")
	product.Type = catalog.TypeSimple
	product.RequiresLinkedParent = true
	product.VisibilityModes = catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeHidden,
		Search:       catalog.VisibilityModeAuto,
		Individually: catalog.VisibilityModeVisible,
		Purchasable:  catalog.VisibilityModeAuto,
	}

	resolver, err := cgraphql.NewResolver(
		&stubProductRepo{
			findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
				if id == product.ID {
					return &product, nil
				}
				return nil, nil
			},
		},
		&stubCategoryRepo{},
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	resp := execGraphQL(t, resolver, `{ product(id: "prod-1") {
		id type requiresLinkedParent
		visibilityModes { catalog search individually purchasable }
		bundleComponents { componentProductId }
		groupedMembers { memberProductId }
	} }`)

	data := resp["data"].(map[string]interface{})
	prod := data["product"].(map[string]interface{})
	if prod["type"] != "simple" {
		t.Fatalf("type = %v", prod["type"])
	}
	if prod["requiresLinkedParent"] != true {
		t.Fatalf("requiresLinkedParent = %v", prod["requiresLinkedParent"])
	}
	modes := prod["visibilityModes"].(map[string]interface{})
	if modes["catalog"] != "hidden" || modes["search"] != "auto" ||
		modes["individually"] != "visible" || modes["purchasable"] != "auto" {
		t.Fatalf("visibilityModes = %+v", modes)
	}
	for _, field := range []string{"bundleComponents", "groupedMembers"} {
		list, ok := prod[field].([]interface{})
		if !ok || len(list) != 0 {
			t.Fatalf("%s = %v, want empty list", field, prod[field])
		}
	}
}

func TestHandler_IndividuallyGateReturnsNull(t *testing.T) {
	product := mustActiveProduct(t, "prod-1", "Widget", "widget")
	resolver, err := cgraphql.NewResolver(
		&stubProductRepo{
			findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
				if id == product.ID {
					return &product, nil
				}
				return nil, nil
			},
			findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
				if slug == product.Slug {
					return &product, nil
				}
				return nil, nil
			},
		},
		&stubCategoryRepo{},
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	resolver.WithVisibility(mapVisibilityFacts{
		product.ID: hiddenIndividuallySnap(),
	}, catalog.VisibilityOptions{})

	for _, query := range []string{
		`{ product(id: "prod-1") { id } }`,
		`{ productBySlug(slug: "widget") { id } }`,
	} {
		resp := execGraphQL(t, resolver, query)
		data := resp["data"].(map[string]interface{})
		for _, v := range data {
			if v != nil {
				t.Fatalf("query %q: want null product, got %v", query, data)
			}
		}
	}
}

func TestHandler_IndividuallyGateAllowsWhenVisible(t *testing.T) {
	product := mustActiveProduct(t, "prod-1", "Widget", "widget")
	resolver, err := cgraphql.NewResolver(
		&stubProductRepo{
			findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
				if id == product.ID {
					return &product, nil
				}
				return nil, nil
			},
		},
		&stubCategoryRepo{},
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	resolver.WithVisibility(mapVisibilityFacts{
		product.ID: forcedVisibleSnap(),
	}, catalog.VisibilityOptions{})

	resp := execGraphQL(t, resolver, `{ product(id: "prod-1") { id } }`)
	data := resp["data"].(map[string]interface{})
	prod := data["product"].(map[string]interface{})
	if prod["id"] != "prod-1" {
		t.Fatalf("product = %+v", prod)
	}
}

func TestHandler_CatalogHiddenIndividuallyVisibleSplit(t *testing.T) {
	product := mustActiveProduct(t, "split-1", "Split", "split")
	all := []catalog.Product{product}

	resolver, err := cgraphql.NewResolver(
		&stubProductRepo{
			findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
				if id == product.ID {
					return &product, nil
				}
				return nil, nil
			},
			listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
				start := filter.Offset
				if start > len(all) {
					start = len(all)
				}
				end := start + filter.Limit
				if end > len(all) {
					end = len(all)
				}
				return all[start:end], nil
			},
		},
		&stubCategoryRepo{},
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	resolver.WithVisibility(mapVisibilityFacts{
		product.ID: catalogHiddenIndividuallyVisibleSnap(),
	}, catalog.VisibilityOptions{})

	resp := execGraphQL(t, resolver, `{ product(id: "split-1") { id } }`)
	if resp["data"].(map[string]interface{})["product"] == nil {
		t.Fatal("individually-visible catalog-hidden product must resolve on product(id)")
	}

	listResp := execGraphQL(t, resolver, `{ products(offset: 0, limit: 10) { items { id } truncated hasMore } }`)
	conn := listResp["data"].(map[string]interface{})["products"]
	if len(connectionItems(t, conn)) != 0 {
		t.Fatalf("catalog listing must omit catalog-hidden product, got %v", conn)
	}
}

func TestHandler_ListingGateFiltersCatalogHidden(t *testing.T) {
	visible := mustActiveProduct(t, "visible", "Visible", "visible")
	hidden := mustActiveProduct(t, "hidden", "Hidden", "hidden")
	all := []catalog.Product{hidden, visible}

	resolver, err := cgraphql.NewResolver(
		&stubProductRepo{
			listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
				start := filter.Offset
				if start > len(all) {
					start = len(all)
				}
				end := start + filter.Limit
				if end > len(all) {
					end = len(all)
				}
				return all[start:end], nil
			},
			findByCategoryIDFn: func(_ context.Context, _ string, offset, limit int) ([]catalog.Product, error) {
				start := offset
				if start > len(all) {
					start = len(all)
				}
				end := start + limit
				if end > len(all) {
					end = len(all)
				}
				return all[start:end], nil
			},
		},
		&stubCategoryRepo{},
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	resolver.WithVisibility(mapVisibilityFacts{
		visible.ID: forcedVisibleSnap(),
		hidden.ID:  hiddenCatalogSnap(),
	}, catalog.VisibilityOptions{})

	for _, query := range []string{
		`{ products(offset: 0, limit: 10) { items { id } truncated hasMore nextAfter depthLimited } }`,
		`{ categoryProducts(categoryId: "cat-1", offset: 0, limit: 10) { items { id } truncated hasMore nextAfter depthLimited } }`,
	} {
		resp := execGraphQL(t, resolver, query)
		data := resp["data"].(map[string]interface{})
		var conn interface{}
		for _, v := range data {
			conn = v
		}
		list := connectionItems(t, conn)
		if len(list) != 1 {
			t.Fatalf("query %q: got %d products, want 1 (%v)", query, len(list), list)
		}
		if list[0].(map[string]interface{})["id"] != "visible" {
			t.Fatalf("query %q: got %+v", query, list)
		}
	}
}

func TestHandler_ListingWithoutFactsPassesThrough(t *testing.T) {
	a := mustActiveProduct(t, "a", "A", "a")
	b := mustActiveProduct(t, "b", "B", "b")
	all := []catalog.Product{a, b}

	resolver, err := cgraphql.NewResolver(
		&stubProductRepo{
			listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
				start := filter.Offset
				end := start + filter.Limit
				if end > len(all) {
					end = len(all)
				}
				return all[start:end], nil
			},
		},
		&stubCategoryRepo{},
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	resp := execGraphQL(t, resolver, `{ products(offset: 0, limit: 10) { items { id } truncated hasMore } }`)
	list := connectionItems(t, resp["data"].(map[string]interface{})["products"])
	if len(list) != 2 {
		t.Fatalf("products = %v, want both when enforce/facts off", list)
	}
}

func TestHandler_ListingTruncationExposesResumeMeta(t *testing.T) {
	// Default ListVisiblePage budget is needed+2000. Fill the scan window with
	// catalog-hidden rows so the first page is empty but truncated/hasMore/nextAfter
	// tell the client the catalog is not exhausted.
	const hiddenCount = 2100
	all := make([]catalog.Product, 0, hiddenCount+1)
	facts := mapVisibilityFacts{}
	for i := 0; i < hiddenCount; i++ {
		id := fmt.Sprintf("h-%d", i)
		p := mustActiveProduct(t, id, id, id)
		all = append(all, p)
		facts[id] = hiddenCatalogSnap()
	}
	visible := mustActiveProduct(t, "visible-tail", "Visible", "visible-tail")
	all = append(all, visible)
	facts[visible.ID] = forcedVisibleSnap()

	resolver, err := cgraphql.NewResolver(
		&stubProductRepo{
			listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
				start := filter.Offset
				if start > len(all) {
					start = len(all)
				}
				end := start + filter.Limit
				if end > len(all) {
					end = len(all)
				}
				return all[start:end], nil
			},
		},
		&stubCategoryRepo{},
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	resolver.WithVisibility(facts, catalog.VisibilityOptions{})

	resp := execGraphQL(t, resolver, `{ products(offset: 0, limit: 1) {
		items { id }
		truncated
		hasMore
		nextAfter
		depthLimited
	} }`)
	conn := resp["data"].(map[string]interface{})["products"].(map[string]interface{})
	if len(connectionItems(t, conn)) != 0 {
		t.Fatalf("want empty truncated page, got items=%v", conn["items"])
	}
	if conn["truncated"] != true || conn["hasMore"] != true {
		t.Fatalf("want truncated+hasMore, got %+v", conn)
	}
	after, _ := conn["nextAfter"].(string)
	if after == "" {
		t.Fatalf("nextAfter required for resume, got %+v", conn)
	}

	resume := execGraphQL(t, resolver, fmt.Sprintf(`{ products(after: %q, limit: 1) {
		items { id }
		truncated
		hasMore
	} }`, after))
	resumeConn := resume["data"].(map[string]interface{})["products"].(map[string]interface{})
	// Resume may still truncate if remaining hidden rows fill the next budget;
	// the client must keep following nextAfter. Walk until the visible row appears
	// or we stop making progress.
	seen := map[string]bool{}
	for step := 0; step < 5; step++ {
		items := connectionItems(t, resumeConn)
		for _, raw := range items {
			seen[raw.(map[string]interface{})["id"].(string)] = true
		}
		if seen["visible-tail"] {
			break
		}
		next, _ := resumeConn["nextAfter"].(string)
		if next == "" || resumeConn["hasMore"] != true {
			break
		}
		resume = execGraphQL(t, resolver, fmt.Sprintf(`{ products(after: %q, limit: 1) {
			items { id }
			truncated
			hasMore
			nextAfter
		} }`, next))
		resumeConn = resume["data"].(map[string]interface{})["products"].(map[string]interface{})
	}
	if !seen["visible-tail"] {
		t.Fatalf("resume via nextAfter never reached visible-tail; seen=%v last=%+v first=%+v", seen, resumeConn, conn)
	}
}

func TestHandler_InvalidAfterCursorReturns400(t *testing.T) {
	resolver, err := cgraphql.NewResolver(
		&stubProductRepo{
			listFn: func(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
				return nil, nil
			},
		},
		&stubCategoryRepo{},
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	schema, err := cgraphql.NewSchema(resolver)
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	h := cgraphql.NewHandler(schema, testLogger())
	body, _ := json.Marshal(map[string]string{
		"query": `{ products(after: "nope", limit: 1) { items { id } } }`,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Errors) == 0 {
		t.Fatalf("want GraphQL errors, got %s", rec.Body.String())
	}
	if resp.Errors[0].Message == "internal server error" {
		t.Fatalf("invalid after must not sanitize to internal server error, got %+v", resp.Errors)
	}
}

func TestHandler_BundleGroupedShapes(t *testing.T) {
	bundle := mustActiveProduct(t, "bundle-1", "Kit", "kit")
	bundle.Type = catalog.TypeBundle
	leaf := mustActiveProduct(t, "leaf-1", "Leaf", "leaf")
	leaf.Type = catalog.TypeSimple
	hiddenLeaf := mustActiveProduct(t, "leaf-hidden", "HiddenLeaf", "hidden-leaf")
	hiddenLeaf.Type = catalog.TypeSimple
	group := mustActiveProduct(t, "group-1", "Group", "group")
	group.Type = catalog.TypeGrouped
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	byID := map[string]*catalog.Product{
		bundle.ID:     &bundle,
		leaf.ID:       &leaf,
		hiddenLeaf.ID: &hiddenLeaf,
		group.ID:      &group,
	}
	repo := &batchProductRepo{
		byID: byID,
		stubProductRepo: stubProductRepo{
			findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
				if p, ok := byID[id]; ok {
					return p, nil
				}
				return nil, nil
			},
		},
	}

	resolver, err := cgraphql.NewResolver(repo, &stubCategoryRepo{})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	resolver.WithVisibility(mapVisibilityFacts{
		bundle.ID:     forcedVisibleSnap(),
		leaf.ID:       forcedVisibleSnap(),
		hiddenLeaf.ID: hiddenIndividuallySnap(),
		group.ID:      forcedVisibleSnap(),
	}, catalog.VisibilityOptions{})
	resolver.WithComposition(
		&stubBundleLister{
			listFn: func(_ context.Context, bundleID string) ([]catalog.BundleComponent, error) {
				if bundleID != bundle.ID {
					t.Fatalf("unexpected bundle id %q", bundleID)
				}
				return []catalog.BundleComponent{
					{
						BundleProductID:    bundle.ID,
						ComponentProductID: leaf.ID,
						Quantity:           2,
						Required:           true,
						CreatedAt:          now,
					},
					{
						BundleProductID:    bundle.ID,
						ComponentProductID: hiddenLeaf.ID,
						Quantity:           1,
						Required:           false,
						CreatedAt:          now,
					},
				}, nil
			},
		},
		&stubGroupedLister{
			listFn: func(_ context.Context, groupID string) ([]catalog.GroupedMember, error) {
				if groupID != group.ID {
					t.Fatalf("unexpected group id %q", groupID)
				}
				return []catalog.GroupedMember{
					{
						GroupProductID:  group.ID,
						MemberProductID: leaf.ID,
						CreatedAt:       now,
					},
					{
						GroupProductID:  group.ID,
						MemberProductID: hiddenLeaf.ID,
						CreatedAt:       now,
					},
				}, nil
			},
		},
	)

	t.Run("bundleComponents", func(t *testing.T) {
		repo.listByIDsCalls = 0
		resp := execGraphQL(t, resolver, `{ product(id: "bundle-1") {
			type
			bundleComponents { componentProductId quantity required name slug type createdAt }
			groupedMembers { memberProductId }
		} }`)
		prod := resp["data"].(map[string]interface{})["product"].(map[string]interface{})
		if prod["type"] != "bundle" {
			t.Fatalf("type = %v", prod["type"])
		}
		comps := prod["bundleComponents"].([]interface{})
		if len(comps) != 1 {
			t.Fatalf("bundleComponents = %v (hidden leaf must be omitted)", comps)
		}
		c := comps[0].(map[string]interface{})
		if c["componentProductId"] != "leaf-1" || c["quantity"] != float64(2) || c["required"] != true ||
			c["name"] != "Leaf" || c["slug"] != "leaf" || c["type"] != "simple" {
			t.Fatalf("component = %+v", c)
		}
		if len(prod["groupedMembers"].([]interface{})) != 0 {
			t.Fatalf("groupedMembers should be empty on bundle")
		}
		if repo.listByIDsCalls != 1 {
			t.Fatalf("ListByIDs calls = %d, want 1", repo.listByIDsCalls)
		}
	})

	t.Run("groupedMembers", func(t *testing.T) {
		repo.listByIDsCalls = 0
		resp := execGraphQL(t, resolver, `{ product(id: "group-1") {
			type
			bundleComponents { componentProductId }
			groupedMembers { memberProductId name slug type }
		} }`)
		prod := resp["data"].(map[string]interface{})["product"].(map[string]interface{})
		if prod["type"] != "grouped" {
			t.Fatalf("type = %v", prod["type"])
		}
		if len(prod["bundleComponents"].([]interface{})) != 0 {
			t.Fatalf("bundleComponents should be empty on grouped")
		}
		members := prod["groupedMembers"].([]interface{})
		if len(members) != 1 {
			t.Fatalf("groupedMembers = %v (hidden leaf must be omitted)", members)
		}
		m := members[0].(map[string]interface{})
		if m["memberProductId"] != "leaf-1" || m["name"] != "Leaf" {
			t.Fatalf("member = %+v", m)
		}
		if repo.listByIDsCalls != 1 {
			t.Fatalf("ListByIDs calls = %d, want 1", repo.listByIDsCalls)
		}
	})
}

func TestHandler_BundleListByIDsErrorFailsField(t *testing.T) {
	bundle := mustActiveProduct(t, "bundle-1", "Kit", "kit")
	bundle.Type = catalog.TypeBundle
	leaf := mustActiveProduct(t, "leaf-1", "Leaf", "leaf")
	byID := map[string]*catalog.Product{bundle.ID: &bundle, leaf.ID: &leaf}
	repo := &batchProductRepo{
		byID:         byID,
		listByIDsErr: errors.New("db down"),
		stubProductRepo: stubProductRepo{
			findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
				if p, ok := byID[id]; ok {
					return p, nil
				}
				return nil, nil
			},
		},
	}
	resolver, err := cgraphql.NewResolver(repo, &stubCategoryRepo{})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	resolver.WithComposition(
		&stubBundleLister{
			listFn: func(context.Context, string) ([]catalog.BundleComponent, error) {
				return []catalog.BundleComponent{{
					BundleProductID:    bundle.ID,
					ComponentProductID: leaf.ID,
					Quantity:           1,
					Required:           true,
				}}, nil
			},
		},
		&stubGroupedLister{},
	)

	resp := execGraphQLAllowErrors(t, resolver, `{ product(id: "bundle-1") { bundleComponents { componentProductId } } }`)
	if resp["errors"] == nil {
		t.Fatalf("want GraphQL error on ListByIDs failure, got %v", resp)
	}
}

func TestHandler_AllowWithFactsErrorFailsProduct(t *testing.T) {
	product := mustActiveProduct(t, "prod-1", "Widget", "widget")
	resolver, err := cgraphql.NewResolver(
		&stubProductRepo{
			findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
				if id == product.ID {
					return &product, nil
				}
				return nil, nil
			},
		},
		&stubCategoryRepo{},
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	resolver.WithVisibility(errVisibilityFacts{}, catalog.VisibilityOptions{})

	resp := execGraphQLAllowErrors(t, resolver, `{ product(id: "prod-1") { id } }`)
	if resp["errors"] == nil {
		t.Fatalf("want GraphQL error on AllowWithFacts failure, got %v", resp)
	}
}

func graphqlTypeName(t gql.Type) string {
	switch x := t.(type) {
	case *gql.NonNull:
		return graphqlTypeName(x.OfType)
	case *gql.List:
		return graphqlTypeName(x.OfType)
	case *gql.Object:
		return x.Name()
	default:
		return ""
	}
}

func TestSchema_ProductReturningRootsMatchVisibilityAudit(t *testing.T) {
	resolver, err := cgraphql.NewResolver(&stubProductRepo{}, &stubCategoryRepo{})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	schema, err := cgraphql.NewSchema(resolver)
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}

	productReturning := map[string]struct{}{}
	for name, field := range schema.QueryType().Fields() {
		switch graphqlTypeName(field.Type) {
		case "Product", "ProductConnection":
			productReturning[name] = struct{}{}
		}
	}
	if len(productReturning) == 0 {
		t.Fatal("schema has no product-returning Query fields")
	}

	audited := map[string]struct{}{}
	for _, r := range storefront.PublicProductVisibilityRoutes() {
		if r.Method != "GQL" {
			continue
		}
		audited[r.Path] = struct{}{}
	}

	for name := range productReturning {
		if _, ok := audited[name]; !ok {
			t.Errorf("schema Query.%s returns products but is missing from PublicProductVisibilityRoutes GQL audit", name)
		}
	}
	for name := range audited {
		if _, ok := productReturning[name]; !ok {
			t.Errorf("PublicProductVisibilityRoutes GQL %q is not a product-returning Query field in NewSchema", name)
		}
	}
}

func TestSchema_LinkedEligibilityNotExposed(t *testing.T) {
	resolver, err := cgraphql.NewResolver(&stubProductRepo{}, &stubCategoryRepo{})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	schema, err := cgraphql.NewSchema(resolver)
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	productFields := schema.QueryType().Fields()["product"].Type.(*gql.Object).Fields()
	for _, banned := range []string{"linkedEligibleParents", "linkedEligibleCategories"} {
		if _, ok := productFields[banned]; ok {
			t.Fatalf("public schema must not expose %s", banned)
		}
	}
	if _, ok := productFields["requiresLinkedParent"]; !ok {
		t.Fatal("requiresLinkedParent must remain on Product")
	}
}
