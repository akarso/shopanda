package webhook

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Endpoint is a merchant-configured outbound webhook destination.
type Endpoint struct {
	ID          string
	URL         string
	Secret      string
	Events      []string
	Active      bool
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Subscribed reports whether the endpoint listens for eventName.
func (e Endpoint) Subscribed(eventName string) bool {
	for _, name := range e.Events {
		if name == eventName {
			return true
		}
	}
	return false
}

// Validate checks endpoint invariants before persistence.
func (e *Endpoint) Validate(supported map[string]struct{}) error {
	if e == nil {
		return fmt.Errorf("webhook: endpoint is nil")
	}
	rawURL := strings.TrimSpace(e.URL)
	if rawURL == "" {
		return fmt.Errorf("webhook: url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("webhook: invalid url: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("webhook: url must use https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("webhook: url host is required")
	}
	e.URL = rawURL

	if strings.TrimSpace(e.Secret) == "" {
		return fmt.Errorf("webhook: secret is required")
	}
	if len(e.Events) == 0 {
		return fmt.Errorf("webhook: at least one event is required")
	}
	normalized := make([]string, 0, len(e.Events))
	seen := make(map[string]struct{}, len(e.Events))
	for _, name := range e.Events {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := supported[name]; !ok {
			return fmt.Errorf("webhook: unsupported event %q", name)
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	if len(normalized) == 0 {
		return fmt.Errorf("webhook: at least one event is required")
	}
	e.Events = normalized
	e.Description = strings.TrimSpace(e.Description)
	return nil
}
