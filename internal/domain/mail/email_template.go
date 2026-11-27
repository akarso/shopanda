package mail

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akarso/shopanda/internal/domain/translation"
)

const (
	maxEmailTemplateNameLen    = 64
	maxEmailTemplateLocaleLen  = 32
	maxEmailTemplateSubjectLen = 998
	maxEmailTemplateBodyLen    = 1 << 20 // 1 MiB
	maxEmailTemplateUpdatedBy  = 64
)

// emailTemplateNameRE matches the Postgres CHECK on email_templates.name.
var emailTemplateNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// overridableTemplateNames are the only templates merchants may override.
// Auth/token templates stay file/builtin-only so a DB row cannot rewrite
// ResetURL / VerifyURL destinations.
var overridableTemplateNames = map[string]struct{}{
	"order_confirmed": {},
	"order_shipped":   {},
	"invoice_created": {},
	"cart_recovery":   {},
	"order_claim":     {},
	"voucher_issued":  {},
}

// IsOverridableName reports whether name may be stored or applied as a DB override.
func IsOverridableName(name string) bool {
	_, ok := overridableTemplateNames[strings.TrimSpace(name)]
	return ok
}

// CanonicalLocale returns the BCP 47 primary subtag (lower case), or "" when
// empty/invalid. Never invents "en". Used for storage and lookup keys.
func CanonicalLocale(locale string) string {
	return translation.PrimaryLanguage(locale)
}

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
	UpdatedBy string // required admin actor id on write
}

// EmailTemplateRepository persists store-scoped template overrides.
type EmailTemplateRepository interface {
	// Get returns the override or (nil, nil) when missing.
	// locale is canonicalized before lookup.
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

// Validate checks required fields, allowlist, and length limits before persistence.
func (t *EmailTemplate) Validate() error {
	if t == nil {
		return fmt.Errorf("mail: email template must not be nil")
	}
	t.StoreID = strings.TrimSpace(t.StoreID)
	t.Name = strings.TrimSpace(t.Name)
	t.Locale = CanonicalLocale(t.Locale)
	t.Subject = strings.TrimSpace(t.Subject)
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
	if !emailTemplateNameRE.MatchString(t.Name) {
		return fmt.Errorf("mail: email template name must match [a-z][a-z0-9_]*")
	}
	if !IsOverridableName(t.Name) {
		return fmt.Errorf("mail: email template %q is not overridable", t.Name)
	}
	if utf8.RuneCountInString(t.Locale) > maxEmailTemplateLocaleLen {
		return fmt.Errorf("mail: email template locale too long")
	}
	if t.Subject == "" {
		return fmt.Errorf("mail: email template subject is required")
	}
	if utf8.RuneCountInString(t.Subject) > maxEmailTemplateSubjectLen {
		return fmt.Errorf("mail: email template subject too long")
	}
	if strings.TrimSpace(t.Body) == "" {
		return fmt.Errorf("mail: email template body is required")
	}
	if len(t.Body) > maxEmailTemplateBodyLen {
		return fmt.Errorf("mail: email template body too long")
	}
	if t.UpdatedBy == "" {
		return fmt.Errorf("mail: email template updated_by is required")
	}
	if utf8.RuneCountInString(t.UpdatedBy) > maxEmailTemplateUpdatedBy {
		return fmt.Errorf("mail: email template updated_by too long")
	}
	return nil
}
