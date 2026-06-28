package cms

import (
	"html/template"
	"regexp"
	"strings"
)

var (
	stripScriptTagRe    = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	stripIframeTagRe    = regexp.MustCompile(`(?is)<iframe\b[^>]*>.*?</iframe>`)
	stripEventHandlerRe = regexp.MustCompile(`(?i)\s(on[a-z]+)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	stripJSURLRe        = regexp.MustCompile(`(?i)\s(href|src)\s*=\s*("|')\s*javascript:[^"']*("|')`)
)

// SanitizeHTML removes common XSS vectors from CMS HTML before SSR rendering.
func SanitizeHTML(raw string) template.HTML {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return ""
	}
	cleaned = stripScriptTagRe.ReplaceAllString(cleaned, "")
	cleaned = stripIframeTagRe.ReplaceAllString(cleaned, "")
	cleaned = stripEventHandlerRe.ReplaceAllString(cleaned, "")
	cleaned = stripJSURLRe.ReplaceAllString(cleaned, "")
	return template.HTML(cleaned)
}
