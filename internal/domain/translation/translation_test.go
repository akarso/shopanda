package translation_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/translation"
)

func TestNewTranslation(t *testing.T) {
	tr, err := translation.NewTranslation("add_to_cart", "de", "In den Warenkorb")
	if err != nil {
		t.Fatalf("NewTranslation() error = %v", err)
	}
	if tr.Key != "add_to_cart" {
		t.Errorf("Key = %q, want add_to_cart", tr.Key)
	}
	if tr.Language != "de" {
		t.Errorf("Language = %q, want de", tr.Language)
	}
	if tr.Value != "In den Warenkorb" {
		t.Errorf("Value = %q, want In den Warenkorb", tr.Value)
	}
}

func TestNewTranslation_NormalizesLanguage(t *testing.T) {
	tr, err := translation.NewTranslation("key", "PT-BR", "valor")
	if err != nil {
		t.Fatalf("NewTranslation() error = %v", err)
	}
	if tr.Language != "pt-BR" {
		t.Errorf("Language = %q, want pt-BR (canonical BCP 47)", tr.Language)
	}
}

func TestNewTranslation_Validation(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		language string
		value    string
	}{
		{"empty key", "", "en", "value"},
		{"whitespace key", "  ", "en", "value"},
		{"empty language", "key", "", "value"},
		{"invalid BCP 47 tag", "key", "!!!", "value"},
		{"malformed BCP 47 tag", "key", "1x", "value"},
		{"empty value", "key", "en", ""},
		{"whitespace value", "key", "en", "  "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := translation.NewTranslation(tc.key, tc.language, tc.value)
			if err == nil {
				t.Error("NewTranslation() expected error")
			}
		})
	}
}

func TestLanguageContext(t *testing.T) {
	ctx := translation.WithLanguage(context.Background(), "de")
	got := translation.LanguageFromContext(ctx)
	if got != "de" {
		t.Errorf("LanguageFromContext() = %q, want de", got)
	}
}

func TestLanguageContext_Default(t *testing.T) {
	got := translation.LanguageFromContext(context.Background())
	if got != "en" {
		t.Errorf("LanguageFromContext() = %q, want en (default)", got)
	}
}

// mockTranslationRepo implements translation.TranslationRepository for tests.
type mockTranslationRepo struct {
	findFn func(ctx context.Context, key, language string) (*translation.Translation, error)
}

func (m *mockTranslationRepo) FindByKeyAndLanguage(ctx context.Context, key, language string) (*translation.Translation, error) {
	if m.findFn != nil {
		return m.findFn(ctx, key, language)
	}
	return nil, nil
}
func (m *mockTranslationRepo) ListByLanguage(_ context.Context, _ string) ([]translation.Translation, error) {
	return nil, nil
}
func (m *mockTranslationRepo) Upsert(_ context.Context, _ *translation.Translation) error {
	return nil
}
func (m *mockTranslationRepo) Delete(_ context.Context, _, _ string) error { return nil }

func TestTranslator_T_Found(t *testing.T) {
	repo := &mockTranslationRepo{
		findFn: func(_ context.Context, key, lang string) (*translation.Translation, error) {
			if key == "add_to_cart" && lang == "de" {
				return &translation.Translation{Key: key, Language: lang, Value: "In den Warenkorb"}, nil
			}
			return nil, nil
		},
	}
	tr := translation.NewTranslator(repo)
	ctx := translation.WithLanguage(context.Background(), "de")

	got := tr.T(ctx, "add_to_cart")
	if got != "In den Warenkorb" {
		t.Errorf("T() = %q, want In den Warenkorb", got)
	}
}

func TestTranslator_T_NotFound(t *testing.T) {
	repo := &mockTranslationRepo{}
	tr := translation.NewTranslator(repo)
	ctx := translation.WithLanguage(context.Background(), "de")

	got := tr.T(ctx, "unknown_key")
	if got != "unknown_key" {
		t.Errorf("T() = %q, want unknown_key (fallback to key)", got)
	}
}

func TestTranslator_T_DefaultLanguage(t *testing.T) {
	repo := &mockTranslationRepo{
		findFn: func(_ context.Context, key, lang string) (*translation.Translation, error) {
			if key == "checkout" && lang == "en" {
				return &translation.Translation{Key: key, Language: lang, Value: "Checkout"}, nil
			}
			return nil, nil
		},
	}
	tr := translation.NewTranslator(repo)
	// No language in context → defaults to "en".
	got := tr.T(context.Background(), "checkout")
	if got != "Checkout" {
		t.Errorf("T() = %q, want Checkout", got)
	}
}
