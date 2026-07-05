package extension

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Target identifies an entity or context instance extension values attach to.
type Target struct {
	Type TargetType
	ID   string
}

// ValuePayload holds the typed payload for an extension value.
type ValuePayload struct {
	StringValue *string         `json:"string_value,omitempty"`
	IntValue    *int64          `json:"int_value,omitempty"`
	BoolValue   *bool           `json:"bool_value,omitempty"`
	JSONValue   json.RawMessage `json:"json_value,omitempty"`
}

// Value is a persisted extension field value envelope.
type Value struct {
	FieldCode  string       `json:"field_code"`
	TargetType TargetType   `json:"target_type"`
	TargetID   string       `json:"target_id"`
	Payload    ValuePayload `json:"payload"`
	UpdatedBy  string       `json:"updated_by,omitempty"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

// ValueInput is the write shape for a single field value.
type ValueInput struct {
	FieldCode string
	Value     interface{}
}

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// PayloadFromInput converts a raw API value to a typed payload for field.
func PayloadFromInput(field ExtensionField, raw interface{}) (ValuePayload, error) {
	if raw == nil {
		return ValuePayload{}, ValidationErrf("extension field %q: value must not be null", field.Code)
	}
	switch field.Type {
	case FieldTypeString, FieldTypeEnum, FieldTypeDate, FieldTypeDateTime:
		s, ok := raw.(string)
		if !ok {
			return ValuePayload{}, ValidationErrf("extension field %q: value must be a string", field.Code)
		}
		s = strings.TrimSpace(s)
		if field.Type == FieldTypeDate && !datePattern.MatchString(s) {
			return ValuePayload{}, ValidationErrf("extension field %q: date must be YYYY-MM-DD", field.Code)
		}
		return ValuePayload{StringValue: &s}, nil
	case FieldTypeInt, FieldTypeMoney:
		switch v := raw.(type) {
		case float64:
			if math.Trunc(v) != v {
				return ValuePayload{}, ValidationErrf("extension field %q: value must be an integer", field.Code)
			}
			i := int64(v)
			return ValuePayload{IntValue: &i}, nil
		case json.Number:
			i, err := v.Int64()
			if err != nil {
				return ValuePayload{}, ValidationErrf("extension field %q: value must be an integer", field.Code)
			}
			return ValuePayload{IntValue: &i}, nil
		case int:
			i := int64(v)
			return ValuePayload{IntValue: &i}, nil
		case int64:
			return ValuePayload{IntValue: &v}, nil
		case string:
			i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return ValuePayload{}, ValidationErrf("extension field %q: value must be an integer", field.Code)
			}
			return ValuePayload{IntValue: &i}, nil
		default:
			return ValuePayload{}, ValidationErrf("extension field %q: value must be an integer", field.Code)
		}
	case FieldTypeBool:
		b, ok := raw.(bool)
		if !ok {
			return ValuePayload{}, ValidationErrf("extension field %q: value must be a boolean", field.Code)
		}
		return ValuePayload{BoolValue: &b}, nil
	case FieldTypeJSON:
		switch v := raw.(type) {
		case json.RawMessage:
			if !json.Valid(v) {
				return ValuePayload{}, ValidationErrf("extension field %q: json value is invalid", field.Code)
			}
			cp := make(json.RawMessage, len(v))
			copy(cp, v)
			return ValuePayload{JSONValue: cp}, nil
		default:
			rawBytes, err := json.Marshal(v)
			if err != nil {
				return ValuePayload{}, ValidationErrf("extension field %q: json value is invalid", field.Code)
			}
			return ValuePayload{JSONValue: rawBytes}, nil
		}
	default:
		return ValuePayload{}, ValidationErrf("extension field %q: unsupported type %q", field.Code, field.Type)
	}
}

// ValidatePayload checks payload constraints for field.
func ValidatePayload(field ExtensionField, payload ValuePayload) error {
	switch field.Type {
	case FieldTypeString, FieldTypeDate, FieldTypeDateTime:
		if payload.StringValue == nil {
			if field.Validation.Required {
				return ValidationErrf("extension field %q is required", field.Code)
			}
			return nil
		}
		s := *payload.StringValue
		if field.Validation.Required && s == "" {
			return ValidationErrf("extension field %q is required", field.Code)
		}
		if field.Validation.Min != nil && int64(len(s)) < *field.Validation.Min {
			return ValidationErrf("extension field %q is below minimum length", field.Code)
		}
		if field.Validation.Max != nil && int64(len(s)) > *field.Validation.Max {
			return ValidationErrf("extension field %q exceeds maximum length", field.Code)
		}
		if field.Validation.Regex != "" {
			re, err := regexp.Compile(field.Validation.Regex)
			if err != nil {
				return ValidationErr("extension field " + field.Code + ": invalid validation regex")
			}
			if !re.MatchString(s) {
				return ValidationErrf("extension field %q does not match required pattern", field.Code)
			}
		}
	case FieldTypeEnum:
		if payload.StringValue == nil {
			if field.Validation.Required {
				return ValidationErrf("extension field %q is required", field.Code)
			}
			return nil
		}
		s := *payload.StringValue
		found := false
		for _, opt := range field.Validation.Options {
			if opt == s {
				found = true
				break
			}
		}
		if !found {
			return ValidationErrf("extension field %q value not in allowed options", field.Code)
		}
	case FieldTypeInt, FieldTypeMoney:
		if payload.IntValue == nil {
			if field.Validation.Required {
				return ValidationErrf("extension field %q is required", field.Code)
			}
			return nil
		}
		v := *payload.IntValue
		if field.Validation.Min != nil && v < *field.Validation.Min {
			return ValidationErrf("extension field %q is below minimum", field.Code)
		}
		if field.Validation.Max != nil && v > *field.Validation.Max {
			return ValidationErrf("extension field %q exceeds maximum", field.Code)
		}
	case FieldTypeBool:
		if payload.BoolValue == nil && field.Validation.Required {
			return ValidationErrf("extension field %q is required", field.Code)
		}
	case FieldTypeJSON:
		if len(payload.JSONValue) == 0 {
			if field.Validation.Required {
				return ValidationErrf("extension field %q is required", field.Code)
			}
			return nil
		}
		if !json.Valid(payload.JSONValue) {
			return ValidationErrf("extension field %q: json value is invalid", field.Code)
		}
	}
	return nil
}

// APIValue returns the primary JSON-serializable value for responses.
func APIValue(field ExtensionField, payload ValuePayload) (interface{}, error) {
	switch field.Type {
	case FieldTypeString, FieldTypeEnum, FieldTypeDate, FieldTypeDateTime:
		if payload.StringValue == nil {
			return nil, nil
		}
		return *payload.StringValue, nil
	case FieldTypeInt, FieldTypeMoney:
		if payload.IntValue == nil {
			return nil, nil
		}
		return *payload.IntValue, nil
	case FieldTypeBool:
		if payload.BoolValue == nil {
			return nil, nil
		}
		return *payload.BoolValue, nil
	case FieldTypeJSON:
		if len(payload.JSONValue) == 0 {
			return nil, nil
		}
		var out interface{}
		if err := json.Unmarshal(payload.JSONValue, &out); err != nil {
			return nil, fmt.Errorf("decode json value: %w", err)
		}
		return out, nil
	default:
		return nil, errors.New("unsupported field type")
	}
}
