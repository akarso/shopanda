package translation

import (
	"errors"
	"strings"

	langtag "golang.org/x/text/language"
)

// Translation holds a single system translation entry.
type Translation struct {
	Key      string
	Language string
	Value    string
}

// NewTranslation creates a Translation with the required fields.
func NewTranslation(key, language, value string) (Translation, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return Translation{}, errors.New("translation key must not be empty")
	}
	language = strings.TrimSpace(language)
	if language == "" {
		return Translation{}, errors.New("translation language must not be empty")
	}
	tag, err := langtag.Parse(language)
	if err != nil {
		return Translation{}, errors.New("translation language must be a valid BCP 47 tag (e.g. en, pt-BR)")
	}
	language = tag.String()
	value = strings.TrimSpace(value)
	if value == "" {
		return Translation{}, errors.New("translation value must not be empty")
	}
	return Translation{
		Key:      key,
		Language: language,
		Value:    value,
	}, nil
}
