package translation_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/translation"
)

func TestNewContentTranslation(t *testing.T) {
	ct, err := translation.NewContentTranslation("prod-1", "de", "name", "Produkt")
	if err != nil {
		t.Fatalf("NewContentTranslation() error = %v", err)
	}
	if ct.EntityID != "prod-1" {
		t.Errorf("EntityID = %q, want prod-1", ct.EntityID)
	}
	if ct.Language != "de" {
		t.Errorf("Language = %q, want de", ct.Language)
	}
	if ct.Field != "name" {
		t.Errorf("Field = %q, want name", ct.Field)
	}
	if ct.Value != "Produkt" {
		t.Errorf("Value = %q, want Produkt", ct.Value)
	}
}

func TestNewContentTranslation_NormalizesLanguage(t *testing.T) {
	ct, err := translation.NewContentTranslation("e-1", "PT-BR", "title", "Título")
	if err != nil {
		t.Fatalf("NewContentTranslation() error = %v", err)
	}
	if ct.Language != "pt-BR" {
		t.Errorf("Language = %q, want pt-BR (canonical BCP 47)", ct.Language)
	}
}

func TestNewContentTranslation_Validation(t *testing.T) {
	tests := []struct {
		name     string
		entityID string
		language string
		field    string
		value    string
	}{
		{"empty entity_id", "", "en", "name", "value"},
		{"whitespace entity_id", "  ", "en", "name", "value"},
		{"empty language", "e-1", "", "name", "value"},
		{"invalid BCP 47 tag", "e-1", "!!!", "name", "value"},
		{"empty field", "e-1", "en", "", "value"},
		{"whitespace field", "e-1", "en", "  ", "value"},
		{"empty value", "e-1", "en", "name", ""},
		{"whitespace value", "e-1", "en", "name", "  "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := translation.NewContentTranslation(tc.entityID, tc.language, tc.field, tc.value)
			if err == nil {
				t.Error("NewContentTranslation() expected error")
			}
		})
	}
}

// mockContentTranslationRepo implements translation.ContentTranslationRepository for tests.
type mockContentTranslationRepo struct {
	findByEntityFn func(ctx context.Context, entityID, language string) ([]translation.ContentTranslation, error)
}

func (m *mockContentTranslationRepo) FindByEntityAndLanguage(ctx context.Context, entityID, language string) ([]translation.ContentTranslation, error) {
	if m.findByEntityFn != nil {
		return m.findByEntityFn(ctx, entityID, language)
	}
	return []translation.ContentTranslation{}, nil
}
func (m *mockContentTranslationRepo) FindFieldValue(_ context.Context, _, _, _ string) (*translation.ContentTranslation, error) {
	return nil, nil
}
func (m *mockContentTranslationRepo) Upsert(_ context.Context, _ *translation.ContentTranslation) error {
	return nil
}
func (m *mockContentTranslationRepo) DeleteByEntity(_ context.Context, _ string) error {
	return nil
}

func TestContentTranslator_TranslateFields_Found(t *testing.T) {
	repo := &mockContentTranslationRepo{
		findByEntityFn: func(_ context.Context, entityID, lang string) ([]translation.ContentTranslation, error) {
			if entityID == "prod-1" && lang == "de" {
				return []translation.ContentTranslation{
					{EntityID: "prod-1", Language: "de", Field: "name", Value: "Produkt"},
					{EntityID: "prod-1", Language: "de", Field: "description", Value: "Eine Beschreibung"},
				}, nil
			}
			return []translation.ContentTranslation{}, nil
		},
	}
	ct := translation.NewContentTranslator(repo, nil)
	ctx := translation.WithLanguage(context.Background(), "de")

	fields := ct.TranslateFields(ctx, "prod-1")
	if fields == nil {
		t.Fatal("TranslateFields() returned nil")
	}
	if fields["name"] != "Produkt" {
		t.Errorf("name = %q, want Produkt", fields["name"])
	}
	if fields["description"] != "Eine Beschreibung" {
		t.Errorf("description = %q, want Eine Beschreibung", fields["description"])
	}
}

func TestContentTranslator_TranslateFields_NotFound(t *testing.T) {
	repo := &mockContentTranslationRepo{}
	ct := translation.NewContentTranslator(repo, nil)
	ctx := translation.WithLanguage(context.Background(), "de")

	fields := ct.TranslateFields(ctx, "missing")
	if fields != nil {
		t.Errorf("TranslateFields() = %v, want nil", fields)
	}
}

func TestContentTranslator_TranslateFields_DefaultLanguage(t *testing.T) {
	repo := &mockContentTranslationRepo{
		findByEntityFn: func(_ context.Context, entityID, lang string) ([]translation.ContentTranslation, error) {
			if entityID == "page-1" && lang == "en" {
				return []translation.ContentTranslation{
					{EntityID: "page-1", Language: "en", Field: "title", Value: "Welcome"},
				}, nil
			}
			return []translation.ContentTranslation{}, nil
		},
	}
	ct := translation.NewContentTranslator(repo, nil)
	// No language in context → defaults to "en".
	fields := ct.TranslateFields(context.Background(), "page-1")
	if fields == nil {
		t.Fatal("TranslateFields() returned nil")
	}
	if fields["title"] != "Welcome" {
		t.Errorf("title = %q, want Welcome", fields["title"])
	}
}
