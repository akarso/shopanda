package pluginsdk

import (
	"errors"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// IsNotFound reports a not_found application error.
func IsNotFound(err error) bool {
	return apperror.Is(err, apperror.CodeNotFound)
}

// IsConflict reports a conflict application error or exclusive string upsert clash.
func IsConflict(err error) bool {
	return apperror.Is(err, apperror.CodeConflict) || errors.Is(err, domainext.ErrStringValueConflict)
}

// IsForbidden reports a forbidden application error.
func IsForbidden(err error) bool {
	return apperror.Is(err, apperror.CodeForbidden) ||
		apperror.Is(err, apperror.CodeForbiddenPluginOnlyField) ||
		apperror.Is(err, apperror.CodeForbiddenPrivateField)
}

// IsValidation reports a validation / field-validation application error or
// domain extension ValidationError.
func IsValidation(err error) bool {
	if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeFieldValidationFailed) {
		return true
	}
	var ve *domainext.ValidationError
	return errors.As(err, &ve)
}

// IsUnavailable reports an unavailable (503) application error.
func IsUnavailable(err error) bool {
	return apperror.Is(err, apperror.CodeUnavailable)
}
