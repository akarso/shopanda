package meili

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/search"
)

// Compile-time check.
var _ search.SearchEngine = (*Engine)(nil)

// httpTimeout is used for all Meilisearch HTTP calls.
const httpTimeout = 10 * time.Second

// taskPollInterval is the delay between task status checks.
const taskPollInterval = 100 * time.Millisecond

// Config holds the settings needed to connect to a Meilisearch instance.
type Config struct {
	Host   string // e.g. "http://localhost:7700"
	APIKey string // master or search key
	Index  string // index UID, e.g. "products"
}

// taskInfo represents a Meilisearch async task response.
type taskInfo struct {
	TaskUID int64  `json:"taskUid"`
	Status  string `json:"status"`
}

// meiliAPI is the subset of Meilisearch HTTP operations we use.
// Extracting this interface makes unit-testing possible without a real server.
type meiliAPI interface {
	addDocuments(ctx context.Context, docs []document) (int64, error)
	deleteDocument(ctx context.Context, id string) (int64, error)
	search(ctx context.Context, req searchRequest) (searchResponse, error)
	updateSettings(ctx context.Context, settings indexSettings) (int64, error)
	getTask(ctx context.Context, taskUID int64) (taskInfo, error)
}

// Engine implements search.SearchEngine backed by Meilisearch.
type Engine struct {
	api   meiliAPI
	index string
}

// New creates an Engine and configures the Meilisearch index settings.
func New(cfg Config) (*Engine, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("meili: empty host")
	}
	if cfg.Index == "" {
		return nil, fmt.Errorf("meili: empty index")
	}

	host := strings.TrimRight(cfg.Host, "/")
	client := &httpClient{
		base:   host,
		apiKey: cfg.APIKey,
		index:  cfg.Index,
		http:   &http.Client{Timeout: httpTimeout},
	}

	e := &Engine{api: client, index: cfg.Index}

	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()
	taskUID, err := client.updateSettings(ctx, defaultSettings())
	if err != nil {
		return nil, fmt.Errorf("meili: configure index %q: %w", cfg.Index, err)
	}
	if err := e.waitForTask(ctx, taskUID); err != nil {
		return nil, fmt.Errorf("meili: configure index %q: %w", cfg.Index, err)
	}

	return e, nil
}

// newWithAPI is a test-only constructor.
func newWithAPI(api meiliAPI, index string) *Engine {
	return &Engine{api: api, index: index}
}

// Name returns "meilisearch".
func (e *Engine) Name() string { return "meilisearch" }

// waitForTask polls a Meilisearch task until it reaches a terminal state.
func (e *Engine) waitForTask(ctx context.Context, taskUID int64) error {
	for {
		info, err := e.api.getTask(ctx, taskUID)
		if err != nil {
			return fmt.Errorf("meili: get task %d: %w", taskUID, err)
		}
		switch info.Status {
		case "succeeded":
			return nil
		case "failed":
			return fmt.Errorf("meili: task %d failed", taskUID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(taskPollInterval):
		}
	}
}

// IndexProduct adds or updates a product in the Meilisearch index.
func (e *Engine) IndexProduct(ctx context.Context, p search.Product) error {
	doc := productToDoc(p)
	if _, err := e.api.addDocuments(ctx, []document{doc}); err != nil {
		return fmt.Errorf("meili: index product %s: %w", p.ID, err)
	}
	return nil
}

// RemoveProduct removes a product from the Meilisearch index.
func (e *Engine) RemoveProduct(ctx context.Context, productID string) error {
	if _, err := e.api.deleteDocument(ctx, productID); err != nil {
		return fmt.Errorf("meili: remove product %s: %w", productID, err)
	}
	return nil
}

// Search executes a search query against Meilisearch.
func (e *Engine) Search(ctx context.Context, query search.SearchQuery) (search.SearchResult, error) {
	if err := query.Validate(); err != nil {
		return search.SearchResult{}, err
	}

	req := buildSearchRequest(query)
	resp, err := e.api.search(ctx, req)
	if err != nil {
		return search.SearchResult{}, fmt.Errorf("meili: search: %w", err)
	}

	return mapSearchResponse(resp), nil
}

// Suggest returns autocomplete suggestions via Meilisearch prefix search.
// If Meilisearch is unreachable, an empty list is returned (graceful degradation).
func (e *Engine) Suggest(ctx context.Context, prefix string, limit int) ([]search.Suggestion, error) {
	if prefix == "" {
		return nil, nil
	}
	limit = clampSuggestLimit(limit)

	req := searchRequest{
		Q:     prefix,
		Limit: limit,
	}
	resp, err := e.api.search(ctx, req)
	if err != nil {
		// Graceful degradation: return empty list on failure.
		return nil, nil
	}

	suggestions := make([]search.Suggestion, 0, len(resp.Hits))
	for _, raw := range resp.Hits {
		var doc document
		if json.Unmarshal(raw, &doc) == nil {
			suggestions = append(suggestions, search.Suggestion{
				Text: doc.Name,
				Type: "product",
				URL:  "/products/" + doc.Slug,
			})
		}
	}
	return suggestions, nil
}

func clampSuggestLimit(n int) int {
	if n <= 0 {
		return search.DefaultSuggestLimit
	}
	if n > search.MaxSuggestLimit {
		return search.MaxSuggestLimit
	}
	return n
}

// --- document mapping ---

type document struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Slug        string                 `json:"slug"`
	CategoryID  string                 `json:"category_id,omitempty"`
	Price       int64                  `json:"price"`
	InStock     bool                   `json:"in_stock"`
	CreatedAt   int64                  `json:"created_at"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
}

func productToDoc(p search.Product) document {
	return document{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Slug:        p.Slug,
		CategoryID:  p.CategoryID,
		Price:       p.Price,
		InStock:     p.InStock,
		CreatedAt:   p.CreatedAt.Unix(),
		Attributes:  p.Attributes,
	}
}

// --- search request / response ---

type searchRequest struct {
	Q      string   `json:"q"`
	Filter string   `json:"filter,omitempty"`
	Sort   []string `json:"sort,omitempty"`
	Limit  int      `json:"limit"`
	Offset int      `json:"offset"`
	Facets []string `json:"facets,omitempty"`
}

type searchResponse struct {
	Hits               []json.RawMessage         `json:"hits"`
	EstimatedTotalHits int                       `json:"estimatedTotalHits"`
	FacetDistribution  map[string]map[string]int `json:"facetDistribution,omitempty"`
}

func buildSearchRequest(q search.SearchQuery) searchRequest {
	req := searchRequest{
		Q:      q.Text,
		Limit:  q.EffectiveLimit(),
		Offset: q.Offset,
		Facets: []string{"category_id"},
	}

	var filters []string
	for k, v := range q.Filters {
		switch k {
		case "category":
			filters = append(filters, fmt.Sprintf("category_id = %q", v))
		case "price_min":
			filters = append(filters, fmt.Sprintf("price >= %v", v))
		case "price_max":
			filters = append(filters, fmt.Sprintf("price <= %v", v))
		case "in_stock":
			filters = append(filters, fmt.Sprintf("in_stock = %v", v))
		}
	}
	if len(filters) > 0 {
		req.Filter = strings.Join(filters, " AND ")
	}

	if q.Sort != "" {
		if strings.HasPrefix(q.Sort, "-") {
			req.Sort = []string{q.Sort[1:] + ":desc"}
		} else {
			req.Sort = []string{q.Sort + ":asc"}
		}
	}

	return req
}

func mapSearchResponse(resp searchResponse) search.SearchResult {
	products := make([]search.Product, 0, len(resp.Hits))
	for _, raw := range resp.Hits {
		var doc document
		if json.Unmarshal(raw, &doc) == nil {
			products = append(products, search.Product{
				ID:          doc.ID,
				Name:        doc.Name,
				Slug:        doc.Slug,
				Description: doc.Description,
				CategoryID:  doc.CategoryID,
				Price:       doc.Price,
				InStock:     doc.InStock,
				CreatedAt:   time.Unix(doc.CreatedAt, 0).UTC(),
				Attributes:  doc.Attributes,
			})
		}
	}

	facets := make(map[string][]search.FacetValue)
	for key, dist := range resp.FacetDistribution {
		fv := make([]search.FacetValue, 0, len(dist))
		for val, count := range dist {
			fv = append(fv, search.FacetValue{Value: val, Count: count})
		}
		facets[key] = fv
	}

	return search.SearchResult{
		Products: products,
		Total:    resp.EstimatedTotalHits,
		Facets:   facets,
	}
}

// --- index settings ---

type indexSettings struct {
	SearchableAttributes []string `json:"searchableAttributes"`
	FilterableAttributes []string `json:"filterableAttributes"`
	SortableAttributes   []string `json:"sortableAttributes"`
	DisplayedAttributes  []string `json:"displayedAttributes"`
}

func defaultSettings() indexSettings {
	return indexSettings{
		SearchableAttributes: []string{"name", "description", "slug"},
		FilterableAttributes: []string{"category_id", "price", "in_stock", "attributes"},
		SortableAttributes:   []string{"price", "name", "created_at"},
		DisplayedAttributes:  []string{"*"},
	}
}

// --- HTTP client ---

type httpClient struct {
	base   string
	apiKey string
	index  string
	http   *http.Client
}

func (c *httpClient) addDocuments(ctx context.Context, docs []document) (int64, error) {
	body, err := json.Marshal(docs)
	if err != nil {
		return 0, err
	}
	url := fmt.Sprintf("%s/indexes/%s/documents", c.base, c.index)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return 0, err
	}
	var task taskInfo
	if err := c.doRequest(req, &task); err != nil {
		return 0, err
	}
	return task.TaskUID, nil
}

func (c *httpClient) deleteDocument(ctx context.Context, id string) (int64, error) {
	url := fmt.Sprintf("%s/indexes/%s/documents/%s", c.base, c.index, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return 0, err
	}
	var task taskInfo
	if err := c.doRequest(req, &task); err != nil {
		return 0, err
	}
	return task.TaskUID, nil
}

func (c *httpClient) search(ctx context.Context, sr searchRequest) (searchResponse, error) {
	body, err := json.Marshal(sr)
	if err != nil {
		return searchResponse{}, err
	}
	url := fmt.Sprintf("%s/indexes/%s/search", c.base, c.index)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return searchResponse{}, err
	}
	var resp searchResponse
	if err := c.doRequest(req, &resp); err != nil {
		return searchResponse{}, err
	}
	return resp, nil
}

func (c *httpClient) updateSettings(ctx context.Context, settings indexSettings) (int64, error) {
	body, err := json.Marshal(settings)
	if err != nil {
		return 0, err
	}
	url := fmt.Sprintf("%s/indexes/%s/settings", c.base, c.index)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, strings.NewReader(string(body)))
	if err != nil {
		return 0, err
	}
	var task taskInfo
	if err := c.doRequest(req, &task); err != nil {
		return 0, err
	}
	return task.TaskUID, nil
}

func (c *httpClient) getTask(ctx context.Context, taskUID int64) (taskInfo, error) {
	url := fmt.Sprintf("%s/tasks/%d", c.base, taskUID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return taskInfo{}, err
	}
	var info taskInfo
	if err := c.doRequest(req, &info); err != nil {
		return taskInfo{}, err
	}
	return info, nil
}

func (c *httpClient) doRequest(req *http.Request, dest interface{}) error {
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("meili: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("meili: %s %s: status %d: %s", req.Method, req.URL.Path, resp.StatusCode, string(respBody))
	}

	if dest != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, dest); err != nil {
			return fmt.Errorf("meili: decode response: %w", err)
		}
	}
	return nil
}
