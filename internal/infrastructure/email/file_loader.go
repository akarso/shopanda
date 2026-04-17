package email

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akarso/shopanda/internal/domain/mail"
)

// FileLoader loads email templates from the filesystem.
// It looks for {dir}/{name}.html and reads:
//   - the subject from the first line (as an HTML comment: <!-- Subject: ... -->)
//   - the remaining lines as the body HTML
type FileLoader struct {
	dir string
}

// NewFileLoader returns a loader that reads templates from dir.
func NewFileLoader(dir string) *FileLoader {
	return &FileLoader{dir: dir}
}

// Dir returns the directory this loader reads templates from.
func (f *FileLoader) Dir() string {
	return f.dir
}

// Load reads {dir}/{name}.html and extracts the subject line and body.
// Returns mail.ErrTemplateNotFound when the file does not exist.
func (f *FileLoader) Load(name string) (subject, body string, err error) {
	path := filepath.Join(f.dir, name+".html")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("%w: %s", mail.ErrTemplateNotFound, name)
		}
		return "", "", fmt.Errorf("email: read template %s: %w", name, err)
	}

	content := string(data)
	subject, body = parseTemplate(content)
	if subject == "" {
		return "", "", fmt.Errorf("email: template %s: missing subject line (expected <!-- Subject: ... -->)", name)
	}

	return subject, body, nil
}

// parseTemplate splits template content into subject and body.
// The subject is extracted from the first line if it matches:
//
//	<!-- Subject: Order {{.Data.OrderID}} — Confirmation -->
//
// Everything after the subject line becomes the body.
func parseTemplate(content string) (subject, body string) {
	const prefix = "<!-- Subject:"
	const suffix = "-->"

	first, rest, _ := strings.Cut(content, "\n")
	first = strings.TrimSpace(first)

	if strings.HasPrefix(first, prefix) && strings.HasSuffix(first, suffix) {
		subject = strings.TrimSpace(first[len(prefix) : len(first)-len(suffix)])
		body = strings.TrimLeft(rest, "\r\n")
		return subject, body
	}

	return "", content
}
