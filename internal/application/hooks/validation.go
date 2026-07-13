package hooks

import "github.com/akarso/shopanda/pkg/extapi"

// AppendValidationIssue appends a structured issue to hook payload key validation_errors.
func AppendValidationIssue(ctx *Context, issue extapi.CartValidationIssue) {
	if ctx == nil || issue.Code == "" || issue.Message == "" {
		return
	}
	raw, _ := ctx.Get("validation_errors")
	var issues *[]extapi.CartValidationIssue
	if raw != nil {
		issues, _ = raw.(*[]extapi.CartValidationIssue)
	}
	if issues == nil {
		init := []extapi.CartValidationIssue{}
		issues = &init
		ctx.Set("validation_errors", issues)
	}
	*issues = append(*issues, issue)
}

// ValidationIssuesFromContext returns issues collected during a cart.validate chain.
func ValidationIssuesFromContext(ctx *Context) []extapi.CartValidationIssue {
	if ctx == nil {
		return nil
	}
	raw, ok := ctx.Get("validation_errors")
	if !ok {
		return nil
	}
	issues, ok := raw.(*[]extapi.CartValidationIssue)
	if !ok || issues == nil {
		return nil
	}
	return *issues
}

// HasBlockingValidationIssues reports whether any issue should block cart mutations.
func HasBlockingValidationIssues(issues []extapi.CartValidationIssue) bool {
	for _, issue := range issues {
		if extapi.IsBlockingValidationIssue(issue) {
			return true
		}
	}
	return false
}
