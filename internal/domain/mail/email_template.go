package mail

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxEmailTemplateNameLen    = 64
	maxEmailTemplateLocaleLen  = 32
	maxEmailTemplateSubjectLen = 998
	maxEmailTemplateBodyLen    = 1 << 20 // 1 MiB
	maxEmailTemplateUpdatedBy  = 64
)

// EmailTemplate is a merchant DB override of a named transactional template.
// Locale empty means the store default language (see RenderWithOverrides).
type EmailTemplate struct {
	ID        string
	StoreID   string
	Name      string
	Locale    string
	Subject   string
	Body      string
	UpdatedAt time.Time
	UpdatedBy string // optional admin actor id
}

// EmailTemplateRepository persists store-scoped template overrides.
type EmailTemplateRepository interface {
	// Get returns the override or (nil, nil) when missing.
	Get(ctx context.Context, storeID, name, locale string) (*EmailTemplate, error)
	Upsert(ctx context.Context, t *EmailTemplate) error
	// Delete removes the override. Missing rows are a no-op.
	Delete(ctx context.Context, storeID, name, locale string) error
	// ListByStore returns all overrides for a store (any locale), newest first.
	ListByStore(ctx context.Context, storeID string) ([]EmailTemplate, error)
}

// OverrideSource is the DB side of render-time resolution (may wrap a repo).
type OverrideSource interface {
	Get(ctx context.Context, storeID, name, locale string) (subject, body string, err error)
}

// RepoOverrideSource adapts EmailTemplateRepository to OverrideSource.
type RepoOverrideSource struct {
	Repo EmailTemplateRepository
}

// Get returns subject/body or ErrTemplateNotFound when missing.
func (s RepoOverrideSource) Get(ctx context.Context, storeID, name, locale string) (string, string, error) {
	if s.Repo == nil {
		return "", "", ErrTemplateNotFound
	}
	row, err := s.Repo.Get(ctx, storeID, name, locale)
	if err != nil {
		return "", "", err
	}
	if row == nil {
		return "", "", ErrTemplateNotFound
	}
	return row.Subject, row.Body, nil
}

// Validate checks required fields and length limits before persistence.
func (t *EmailTemplate) Validate() error {
	if t == nil {
		return fmt.Errorf("mail: email template must not be nil")
	}
	t.StoreID = strings.TrimSpace(t.StoreID)
	t.Name = strings.TrimSpace(t.Name)
	t.Locale = strings.TrimSpace(t.Locale)
	t.UpdatedBy = strings.TrimSpace(t.UpdatedBy)
	if t.StoreID == "" {
		return fmt.Errorf("mail: email template store_id is required")
	}
	if t.Name == "" {
		return fmt.Errorf("mail: email template name is required")
	}
	if utf8.RuneCountInString(t.Name) > maxEmailTemplateNameLen {
		return fmt.Errorf("mail: email template name too long")
	}
	if utf8.RuneCountInString(t.Locale) > maxEmailTemplateLocaleLen {
		return fmt.Errorf("mail: email template locale too long")
	}
	if utf8.RuneCountInString(t.Subject) > maxEmailTemplateSubjectLen {
		return fmt.Errorf("mail: email template subject too long")
	}
	if len(t.Body) > maxEmailTemplateBodyLen {
		return fmt.Errorf("mail: email template body too long")
	}
	if utf8.RuneCountInString(t.UpdatedBy) > maxEmailTemplateUpdatedBy {
		return fmt.Errorf("mail: email template updated_by too long")
	}
	return nil
}
