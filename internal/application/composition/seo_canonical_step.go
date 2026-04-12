package composition

// CanonicalURLStep sets the canonical URL in meta for the product page.
type CanonicalURLStep struct {
	baseURL string
}

// NewCanonicalURLStep creates a CanonicalURLStep.
func NewCanonicalURLStep(baseURL string) *CanonicalURLStep {
	return &CanonicalURLStep{baseURL: baseURL}
}

func (s *CanonicalURLStep) Name() string { return "seo_canonical" }

func (s *CanonicalURLStep) Apply(ctx *ProductContext) error {
	if ctx == nil || ctx.Product == nil {
		return nil
	}
	if ctx.Product.Slug != "" {
		ctx.Meta["canonical"] = s.baseURL + "/products/" + ctx.Product.Slug
	}
	return nil
}

// ListingCanonicalURLStep sets the canonical URL for listing pages.
type ListingCanonicalURLStep struct {
	baseURL string
}

// NewListingCanonicalURLStep creates a ListingCanonicalURLStep.
func NewListingCanonicalURLStep(baseURL string) *ListingCanonicalURLStep {
	return &ListingCanonicalURLStep{baseURL: baseURL}
}

func (s *ListingCanonicalURLStep) Name() string { return "seo_canonical" }

func (s *ListingCanonicalURLStep) Apply(ctx *ListingContext) error {
	if ctx == nil {
		return nil
	}
	ctx.Meta["canonical"] = s.baseURL + "/products"
	return nil
}
