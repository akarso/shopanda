package extapi

// CartValidationIssue is a structured cart validation finding surfaced to storefront clients.
// Level is "error" (default, blocks mutations) or "warning" (informational on read paths).
type CartValidationIssue struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	VariantID string `json:"variant_id,omitempty"`
	Level     string `json:"level,omitempty"`
}

// AppendValidationError appends a structured issue to the cart.validate hook payload.
// Handlers must not return business-rule failures as handler errors; append issues instead.
func (c *HookContext) AppendValidationError(issue CartValidationIssue) {
	if c == nil || issue.Code == "" || issue.Message == "" {
		return
	}
	raw, _ := c.Get("validation_errors")
	var issues *[]CartValidationIssue
	if raw != nil {
		issues, _ = raw.(*[]CartValidationIssue)
	}
	if issues == nil {
		init := []CartValidationIssue{}
		issues = &init
		c.Set("validation_errors", issues)
	}
	*issues = append(*issues, issue)
}

// IsBlockingValidationIssue reports whether issue should block cart mutations.
func IsBlockingValidationIssue(issue CartValidationIssue) bool {
	return issue.Level != "warning"
}
