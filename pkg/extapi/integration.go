package extapi

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// IntegrationRouteBase is the public inbound integration route prefix.
	IntegrationRouteBase = "/api/v1/integrations"
	// IntegrationAdminRouteBase is the admin inbound integration route prefix.
	IntegrationAdminRouteBase = "/api/v1/admin/integrations"
)

var integrationPluginSlugPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// IntegrationErrorResponse is the ERP-parseable integration error envelope.
type IntegrationErrorResponse struct {
	Error   string                 `json:"error"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// NormalizePluginSlug validates and normalizes an integration plugin route slug.
func NormalizePluginSlug(slug string) (string, error) {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return "", fmt.Errorf("integration plugin slug must not be empty")
	}
	if strings.Contains(slug, "/") {
		return "", fmt.Errorf("integration plugin slug %q must not contain '/'", slug)
	}
	if !integrationPluginSlugPattern.MatchString(slug) {
		return "", fmt.Errorf("integration plugin slug %q: invalid format (use lowercase letters, digits, '-', '_')", slug)
	}
	return slug, nil
}

// IntegrationRoutePrefix returns the route prefix for a plugin slug.
func IntegrationRoutePrefix(slug string) (string, error) {
	normalized, err := NormalizePluginSlug(slug)
	if err != nil {
		return "", err
	}
	return IntegrationRouteBase + "/" + normalized, nil
}

// IntegrationAdminRoutePrefix returns the admin route prefix for a plugin slug.
func IntegrationAdminRoutePrefix(slug string) (string, error) {
	normalized, err := NormalizePluginSlug(slug)
	if err != nil {
		return "", err
	}
	return IntegrationAdminRouteBase + "/" + normalized, nil
}

// IntegrationRoutePattern builds a public integration ServeMux pattern.
func IntegrationRoutePattern(slug, method, path string) (string, error) {
	prefix, err := IntegrationRoutePrefix(slug)
	if err != nil {
		return "", err
	}
	return integrationRoutePattern(prefix, method, path)
}

// IntegrationAdminRoutePattern builds an admin integration ServeMux pattern.
func IntegrationAdminRoutePattern(slug, method, path string) (string, error) {
	prefix, err := IntegrationAdminRoutePrefix(slug)
	if err != nil {
		return "", err
	}
	return integrationRoutePattern(prefix, method, path)
}

func integrationRoutePattern(prefix, method, path string) (string, error) {
	method = strings.TrimSpace(strings.ToUpper(method))
	if method == "" {
		return "", fmt.Errorf("integration route method must not be empty")
	}
	normalizedPath, err := normalizeIntegrationSubPath(path)
	if err != nil {
		return "", err
	}
	return method + " " + prefix + normalizedPath, nil
}

func normalizeIntegrationSubPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("integration route path must not be empty")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("integration route path %q must not contain '..'", path)
	}
	if strings.Contains(path, "//") {
		return "", fmt.Errorf("integration route path %q must not contain '//'", path)
	}
	return path, nil
}
