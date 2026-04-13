package translation

import (
	"errors"
	"strings"

	langtag "golang.org/x/text/language"
)

// ContentTranslation holds a translated field value for a specific entity.
type ContentTranslation struct {
	EntityID string
	Language string
	Field    string
	Value    string
}

// NewContentTranslation creates a ContentTranslation with the required fields.
func NewContentTranslation(entityID, language, field, value string) (ContentTranslation, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return ContentTranslation{}, errors.New("content translation entity_id must not be empty")
	}
	language = strings.TrimSpace(language)
	if language == "" {
		return ContentTranslation{}, errors.New("content translation language must not be empty")
	}
	tag, err := langtag.Parse(language)
	if err != nil {
		return ContentTranslation{}, errors.New("content translation language must be a valid BCP 47 tag (e.g. en, pt-BR)")
	}
	language = tag.String()
	field = strings.ToLower(strings.TrimSpace(field))
	if field == "" {
		return ContentTranslation{}, errors.New("content translation field must not be empty")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return ContentTranslation{}, errors.New("content translation value must not be empty")
	}
	return ContentTranslation{
		EntityID: entityID,
		Language: language,
		Field:    field,
		Value:    value,
	}, nil
}
