package email

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akarso/shopanda/internal/domain/mail"
)

// LoadDir reads all .html templates from the loader's directory
// (excluding _layout.html and other _-prefixed partials) and registers
// them in the Templates registry. Existing names are overwritten.
// Every file is parsed with mail.ParseNamed before any registry write;
// PutParsed applies the whole set under one lock so a parse failure leaves
// the destination unchanged.
// names lists templates that replaced or added registry entries.
func LoadDir(t *mail.Templates, loader *FileLoader) (names []string, err error) {
	dir := loader.Dir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("email: read dir %s: %w", dir, err)
	}

	parsed := make(map[string]*mail.NamedParts, len(entries))
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
		subject, body, loadErr := loader.Load(tmplName)
		if loadErr != nil {
			return nil, fmt.Errorf("email: load template %s: %w", tmplName, loadErr)
		}
		parts, parseErr := mail.ParseNamed(tmplName, subject, body)
		if parseErr != nil {
			return nil, fmt.Errorf("email: parse template %s: %w", tmplName, parseErr)
		}
		parsed[tmplName] = parts
		names = append(names, tmplName)
	}

	if err := t.PutParsed(parsed); err != nil {
		return nil, err
	}
	return names, nil
}

// CountBodyTemplates returns how many non-partial *.html bodies sit in dir.
func CountBodyTemplates(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".html") && !strings.HasPrefix(name, "_") {
			n++
		}
	}
	return n, nil
}

// HasLayout reports whether dir contains _layout.html.
func HasLayout(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "_layout.html"))
	return err == nil && !st.IsDir()
}

// RenderEmail renders a named template and wraps the result in the layout.
// If lr is nil, no layout wrapping is applied.
func RenderEmail(t *mail.Templates, lr mail.Layout, name, to string, data mail.EmailData) (mail.Message, error) {
	msg, err := t.Render(name, to, data)
	if err != nil {
		return mail.Message{}, err
	}
	return mail.ApplyLayout(lr, msg, data)
}

// Setup initialises both a FileLoader and LayoutRenderer from the given
// templates directory. Returns the loader, the layout renderer, and the
// resolved absolute directory path so callers can pass a consistent path
// to LoadDir.
func Setup(dir string) (*FileLoader, *LayoutRenderer, string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, nil, "", fmt.Errorf("email: templates dir: %w", err)
	}
	if !info.IsDir() {
		return nil, nil, "", fmt.Errorf("email: %s is not a directory", dir)
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, "", fmt.Errorf("email: abs path: %w", err)
	}

	loader := NewFileLoader(abs)
	lr, err := NewLayoutRenderer(abs)
	if err != nil {
		return nil, nil, "", err
	}
	return loader, lr, abs, nil
}
