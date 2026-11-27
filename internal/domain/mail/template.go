package mail

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"sort"
	"strings"
	"sync"
	texttemplate "text/template"
)

// Template origin labels for admin list/get (not including "db", which is
// computed when a store override row exists).
const (
	OriginBuiltin = "builtin"
	OriginFile    = "file"
)

// NamedParts holds a plain-text subject template and an HTML body template.
type NamedParts struct {
	subject *texttemplate.Template
	body    *htmltemplate.Template
}

// TemplateSource is the raw subject/body retained for admin GET (before execute).
type TemplateSource struct {
	Subject string
	Body    string
	Origin  string // OriginBuiltin or OriginFile
}

// FileEntry is a parsed template plus raw source for LoadDir registration.
type FileEntry struct {
	Parts   *NamedParts
	Subject string
	Body    string
}

// Templates is a registry of named email templates.
type Templates struct {
	mu      sync.RWMutex
	tmpls   map[string]*NamedParts
	sources map[string]TemplateSource
}

// NewTemplates creates an empty template registry.
func NewTemplates() *Templates {
	return &Templates{
		tmpls:   make(map[string]*NamedParts),
		sources: make(map[string]TemplateSource),
	}
}

// Register adds a named template. Subject uses text/template; body uses
// html/template. Panics if name is empty or subject/body fails to parse.
// Prefer TryRegister when loading untrusted file templates (PR-1210).
func (t *Templates) Register(name, subject, body string) {
	if err := t.TryRegister(name, subject, body); err != nil {
		panic(err.Error())
	}
}

// ParseNamed parses subject (text/template) + body (html/template) without
// touching a registry.
func ParseNamed(name, subject, body string) (*NamedParts, error) {
	if name == "" {
		return nil, fmt.Errorf("mail.Templates.Register: name must not be empty")
	}
	subj, err := texttemplate.New(name + ".subject").Parse(subject)
	if err != nil {
		return nil, fmt.Errorf("mail: parse subject %q: %w", name, err)
	}
	bodyTmpl, err := htmltemplate.New(name).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("mail: parse body %q: %w", name, err)
	}
	return &NamedParts{subject: subj, body: bodyTmpl}, nil
}

// TryRegister adds a named builtin template and returns a parse error instead of
// panicking. Subject and body are both parsed before the registry is updated.
func (t *Templates) TryRegister(name, subject, body string) error {
	parts, err := ParseNamed(name, subject, body)
	if err != nil {
		return err
	}
	return t.putAll(map[string]FileEntry{
		name: {Parts: parts, Subject: subject, Body: body},
	}, OriginBuiltin)
}

// PutParsed stores already-parsed NamedParts under one lock without raw sources.
// Prefer PutFile when subject/body strings are available (admin GET).
func (t *Templates) PutParsed(entries map[string]*NamedParts) error {
	if len(entries) == 0 {
		return nil
	}
	mapped := make(map[string]FileEntry, len(entries))
	for name, parts := range entries {
		mapped[name] = FileEntry{Parts: parts}
	}
	return t.putAll(mapped, OriginBuiltin)
}

// PutFile stores parsed templates with raw sources marked OriginFile (LoadDir).
func (t *Templates) PutFile(entries map[string]FileEntry) error {
	return t.putAll(entries, OriginFile)
}

func (t *Templates) putAll(entries map[string]FileEntry, origin string) error {
	if len(entries) == 0 {
		return nil
	}
	for name, entry := range entries {
		if name == "" {
			return fmt.Errorf("mail: PutParsed empty name")
		}
		if entry.Parts == nil || entry.Parts.subject == nil || entry.Parts.body == nil {
			return fmt.Errorf("mail: PutParsed nil template %q", name)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for name, entry := range entries {
		t.tmpls[name] = entry.Parts
		if entry.Subject != "" || entry.Body != "" || origin == OriginFile {
			t.sources[name] = TemplateSource{
				Subject: entry.Subject,
				Body:    entry.Body,
				Origin:  origin,
			}
		} else {
			// PutParsed without raw text must not leave a stale Source entry.
			delete(t.sources, name)
		}
	}
	return nil
}

// Names returns registered template names in sorted order.
func (t *Templates) Names() []string {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]string, 0, len(t.tmpls))
	for name := range t.tmpls {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Source returns the retained raw subject/body and origin for name.
func (t *Templates) Source(name string) (TemplateSource, bool) {
	if t == nil {
		return TemplateSource{}, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	src, ok := t.sources[name]
	return src, ok
}

// Has reports whether name is registered for render.
func (t *Templates) Has(name string) bool {
	if t == nil {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.tmpls[name]
	return ok
}

// Render builds a Message by executing the named template with the given data.
func (t *Templates) Render(name, to string, data interface{}) (Message, error) {
	t.mu.RLock()
	parts, ok := t.tmpls[name]
	t.mu.RUnlock()
	if !ok {
		return Message{}, fmt.Errorf("mail: unknown template %q", name)
	}
	return executeNamed(parts, name, to, data)
}

func executeNamed(parts *NamedParts, name, to string, data interface{}) (Message, error) {
	if parts == nil || parts.subject == nil || parts.body == nil {
		return Message{}, fmt.Errorf("mail: nil template parts for %q", name)
	}
	var subBuf bytes.Buffer
	if err := parts.subject.Execute(&subBuf, data); err != nil {
		return Message{}, fmt.Errorf("mail: render subject %q: %w", name, err)
	}
	subject := subBuf.String()
	if strings.ContainsAny(subject, "\r\n") {
		return Message{}, fmt.Errorf("mail: render subject %q: must not contain CR or LF", name)
	}
	var bodyBuf bytes.Buffer
	if err := parts.body.Execute(&bodyBuf, data); err != nil {
		return Message{}, fmt.Errorf("mail: render body %q: %w", name, err)
	}
	return Message{
		To:      to,
		Subject: subject,
		Body:    bodyBuf.String(),
	}, nil
}
