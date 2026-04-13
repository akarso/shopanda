package translation

import (
	"errors"
	"strings"
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
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		return Translation{}, errors.New("translation language must not be empty")
	}
	if len(language) != 2 && len(language) != 5 {
		return Translation{}, errors.New("translation language must be a 2-letter code or 5-character BCP 47 tag")
	}
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
