package email

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akarso/shopanda/internal/domain/mail"
)

// LoadDir reads all .html templates from dir (excluding _layout.html) using the
// given loader and registers them in the Templates registry.
// Existing templates with the same name are overwritten.
func LoadDir(t *mail.Templates, loader mail.TemplateLoader, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("email: read dir %s: %w", dir, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		if strings.HasPrefix(name, "_") {
			continue // skip _layout.html and other partials
		}

		tmplName := strings.TrimSuffix(name, ".html")
		subject, body, err := loader.Load(tmplName)
		if err != nil {
			return fmt.Errorf("email: load template %s: %w", tmplName, err)
		}
		t.Register(tmplName, subject, body)
	}
	return nil
}

// RenderEmail renders a named template and wraps the result in the layout.
// If lr is nil, no layout wrapping is applied.
func RenderEmail(t *mail.Templates, lr *LayoutRenderer, name, to string, data mail.EmailData) (mail.Message, error) {
	msg, err := t.Render(name, to, data)
	if err != nil {
		return mail.Message{}, err
	}

	if lr != nil {
		wrapped, wErr := lr.Wrap(msg.Body, data)
		if wErr != nil {
			return mail.Message{}, wErr
		}
		msg.Body = wrapped
	}

	return msg, nil
}

// Setup initialises both a FileLoader and LayoutRenderer from the given
// templates directory. Returns the loader, the layout renderer, and the
// directory path so callers can use LoadDir afterwards.
func Setup(dir string) (*FileLoader, *LayoutRenderer, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("email: templates dir: %w", err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("email: %s is not a directory", dir)
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("email: abs path: %w", err)
	}

	loader := NewFileLoader(abs)
	lr, err := NewLayoutRenderer(abs)
	if err != nil {
		return nil, nil, err
	}
	return loader, lr, nil
}
